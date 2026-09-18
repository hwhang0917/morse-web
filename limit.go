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

const (
	registerLimit  = 10 // /api/register hits per window per client IP
	registerWindow = time.Minute
	maxConnsPerIP  = 8 // concurrent sockets, so one client cannot fill the queue with ghosts
)

// limiter is a fixed-window counter per key.
// ponytail: a burst of 2x is possible at a window boundary; use a token
// bucket if that ever matters.
type limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time // replaced in tests

	mu        sync.Mutex
	hits      map[string]*window
	lastSweep time.Time
}

type window struct {
	count int
	reset time.Time
}

func newLimiter(limit int, span time.Duration) *limiter {
	return &limiter{limit: limit, window: span, now: time.Now, hits: map[string]*window{}}
}

func (lim *limiter) allow(key string) bool {
	now := lim.now()
	lim.mu.Lock()
	defer lim.mu.Unlock()

	if now.Sub(lim.lastSweep) >= lim.window {
		lim.sweep(now)
	}
	current := lim.hits[key]
	if current == nil || !now.Before(current.reset) {
		current = &window{reset: now.Add(lim.window)}
		lim.hits[key] = current
	}
	current.count++
	return current.count <= lim.limit
}

func (lim *limiter) sweep(now time.Time) {
	for key, entry := range lim.hits {
		if !now.Before(entry.reset) {
			delete(lim.hits, key)
		}
	}
	lim.lastSweep = now
}

// forwardedClient returns the last X-Forwarded-For hop. That is the one our
// own proxy appended; earlier entries are whatever the client chose to send.
func forwardedClient(header http.Header) (string, bool) {
	forwarded := header.Get("X-Forwarded-For")
	if forwarded == "" {
		return "", false
	}
	hops := strings.Split(forwarded, ",")
	return strings.TrimSpace(hops[len(hops)-1]), true
}

// clientIP is the peer address, or the forwarded client when the peer is a
// private or loopback address, meaning a reverse proxy sits in front of us.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !(peer.IsPrivate() || peer.IsLoopback()) {
		return host
	}
	if forwarded, ok := forwardedClient(r.Header); ok {
		if addr, err := netip.ParseAddr(forwarded); err == nil {
			return addr.String()
		}
	}
	return host
}

type connGauge struct {
	mu     sync.Mutex
	counts map[string]int
}

func (gauge *connGauge) acquire(ip string) bool {
	gauge.mu.Lock()
	defer gauge.mu.Unlock()
	if gauge.counts == nil {
		gauge.counts = map[string]int{}
	}
	if gauge.counts[ip] >= maxConnsPerIP {
		return false
	}
	gauge.counts[ip]++
	return true
}

func (gauge *connGauge) release(ip string) {
	gauge.mu.Lock()
	defer gauge.mu.Unlock()
	if gauge.counts[ip]--; gauge.counts[ip] <= 0 {
		delete(gauge.counts, ip)
	}
}

func rateLimit(lim *limiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !lim.allow(clientIP(r)) {
			w.Header().Set("Retry-After", strconv.Itoa(int(lim.window.Seconds())))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
