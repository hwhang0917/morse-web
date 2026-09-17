package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiterWindow(t *testing.T) {
	now := time.Unix(0, 0)
	l := newLimiter(2, time.Minute)
	l.now = func() time.Time { return now }

	if !l.allow("a") || !l.allow("a") {
		t.Fatal("first two hits should pass")
	}
	if l.allow("a") {
		t.Fatal("third hit within the window should be refused")
	}
	if !l.allow("b") {
		t.Fatal("another key has its own budget")
	}
	now = now.Add(time.Minute)
	if !l.allow("a") {
		t.Fatal("budget should reset after the window")
	}
}

func TestRegisterRateLimited(t *testing.T) {
	srv := httptest.NewServer(newMux(newHub()))
	defer srv.Close()
	for i := 0; i < registerLimit; i++ {
		r, err := http.Post(srv.URL+"/api/register", "", nil)
		if err != nil || r.StatusCode != 200 {
			t.Fatalf("hit %d: status=%d err=%v", i, r.StatusCode, err)
		}
	}
	r, _ := http.Post(srv.URL+"/api/register", "", nil)
	if r.StatusCode != http.StatusTooManyRequests || r.Header.Get("Retry-After") == "" {
		t.Fatalf("over limit: status=%d retry-after=%q", r.StatusCode, r.Header.Get("Retry-After"))
	}
}

func TestClientIPUsesLastForwardedHop(t *testing.T) {
	cases := []struct{ peer, xff, want string }{
		{"127.0.0.1:1234", "203.0.113.9, 198.51.100.7", "198.51.100.7"}, // proxy appends the real client last
		{"10.0.0.2:1234", "198.51.100.7", "198.51.100.7"},
		{"10.0.0.2:1234", "", "10.0.0.2"},
		{"8.8.8.8:1234", "203.0.113.9", "8.8.8.8"}, // public peer: header is untrusted
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.peer
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("peer=%s xff=%q: got %s, want %s", c.peer, c.xff, got, c.want)
		}
	}
}
