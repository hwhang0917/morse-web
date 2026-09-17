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
