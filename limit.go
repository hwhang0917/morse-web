package main

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// /api/register is the only endpoint that does work for an anonymous caller.
const (
	registerLimit  = 10 // hits per window per client IP
	registerWindow = time.Minute
)

// limiter is a fixed-window counter per key.
// ponytail: fixed window (bursts of 2x at a boundary are possible); swap for
// a token bucket if that ever matters.
type limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time // swapped in tests

	mu        sync.Mutex
	hits      map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	n     int
	reset time.Time
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{limit: limit, window: window, now: time.Now, hits: map[string]*bucket{}}
}

func (l *limiter) allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	// Drop expired entries once per window so the map can't grow forever.
	if now.Sub(l.lastSweep) >= l.window {
		for k, b := range l.hits {
			if !now.Before(b.reset) {
				delete(l.hits, k)
			}
		}
		l.lastSweep = now
	}
	b := l.hits[key]
	if b == nil || !now.Before(b.reset) {
		b = &bucket{reset: now.Add(l.window)}
		l.hits[key] = b
	}
	b.n++
	return b.n <= l.limit
}

// clientIP returns the peer address, or the last X-Forwarded-For hop when the
// peer is a private/loopback address (i.e. a reverse proxy in front of us).
// The last hop is the one that proxy appended; earlier entries are whatever
// the client chose to send, so they can't be trusted for rate limiting.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err == nil && (addr.IsPrivate() || addr.IsLoopback()) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			hops := strings.Split(xff, ",")
			if fwd, err := netip.ParseAddr(strings.TrimSpace(hops[len(hops)-1])); err == nil {
				return fwd.String()
			}
		}
	}
	return host
}

// Concurrent WebSocket cap per client IP, so one client can't fill the
// matchmaking queue with ghosts. Real users need one, maybe a few tabs.
const maxConnsPerIP = 8

type connGauge struct {
	mu sync.Mutex
	n  map[string]int
}

func (g *connGauge) acquire(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.n == nil {
		g.n = map[string]int{}
	}
	if g.n[ip] >= maxConnsPerIP {
		return false
	}
	g.n[ip]++
	return true
}

func (g *connGauge) release(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.n[ip]--; g.n[ip] <= 0 {
		delete(g.n, ip)
	}
}

// rateLimit wraps h, answering 429 once a client IP exceeds l.
func rateLimit(l *limiter, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			w.Header().Set("Retry-After", strconv.Itoa(int(l.window.Seconds())))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		h(w, r)
	}
}
