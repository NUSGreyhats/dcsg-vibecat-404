package identity

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimiter is a per-IP last-hit limiter. Requests from an IP are rejected
// if they land inside the cooldown window of the previous allowed request.
//
// Spec: 3 s register, 1 s verify. Map entries are not GCed here — the
// identity service is low-traffic relative to the game WS. If that changes,
// add a periodic sweep.
type RateLimiter struct {
	mu       sync.Mutex
	last     map[string]time.Time
	cooldown time.Duration
}

// NewRateLimiter returns a limiter with the given cooldown per client IP.
func NewRateLimiter(cooldown time.Duration) *RateLimiter {
	return &RateLimiter{last: make(map[string]time.Time), cooldown: cooldown}
}

// Allow reports whether a request from ip may proceed; if so, it records
// the hit. Thread-safe.
func (r *RateLimiter) Allow(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if t, ok := r.last[ip]; ok && now.Sub(t) < r.cooldown {
		return false
	}
	r.last[ip] = now
	return true
}

// ClientIP returns the remote IP sans port. Handles both IPv4 and IPv6 with
// or without a port suffix.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
