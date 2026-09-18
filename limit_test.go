package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiterWindow(t *testing.T) {
	now := time.Unix(0, 0)
	lim := newLimiter(2, time.Minute)
	lim.now = func() time.Time { return now }

	if !lim.allow("a") || !lim.allow("a") {
		t.Fatal("first two hits should pass")
	}
	if lim.allow("a") {
		t.Fatal("third hit within the window should be refused")
	}
	if !lim.allow("b") {
		t.Fatal("another key has its own budget")
	}
	now = now.Add(time.Minute)
	if !lim.allow("a") {
		t.Fatal("budget should reset after the window")
	}
}

func TestRegisterRateLimited(t *testing.T) {
	server := httptest.NewServer(newMux(newHub()))
	defer server.Close()
	for hit := 0; hit < registerLimit; hit++ {
		resp, err := http.Post(server.URL+"/api/register", "", nil)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("hit %d: status=%d err=%v", hit, resp.StatusCode, err)
		}
	}
	resp, _ := http.Post(server.URL+"/api/register", "", nil)
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("over limit: status=%d retry-after=%q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}

func TestClientIPUsesLastForwardedHop(t *testing.T) {
	cases := []struct{ peer, forwarded, want string }{
		{"127.0.0.1:1234", "203.0.113.9, 198.51.100.7", "198.51.100.7"}, // the proxy appends the real client last
		{"10.0.0.2:1234", "198.51.100.7", "198.51.100.7"},
		{"10.0.0.2:1234", "", "10.0.0.2"},
		{"8.8.8.8:1234", "203.0.113.9", "8.8.8.8"}, // public peer: the header is untrusted
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tc.peer
		if tc.forwarded != "" {
			req.Header.Set("X-Forwarded-For", tc.forwarded)
		}
		if got := clientIP(req); got != tc.want {
			t.Errorf("peer=%s forwarded=%q: got %s, want %s", tc.peer, tc.forwarded, got, tc.want)
		}
	}
}
