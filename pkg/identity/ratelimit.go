package identity

import (
	"net/http"
	"sync"
	"time"

	"github.com/greyhats/defcon-game/pkg/netutil"
)

// RateLimiter is a per-IP last-hit limiter. Requests from an IP are rejected
// if they land inside the cooldown window of the previous allowed request.
//
// Spec: 3 s register, 1 s verify. Entries idle longer than ~100 cooldowns
// are pruned inline so a churn of one-shot IPs (or an XFF spoof before
// netutil.ClientIP catches them) can't grow the map without bound.
type RateLimiter struct {
	mu       sync.Mutex
	last     map[string]time.Time
	cooldown time.Duration
	gcAfter  time.Duration
}

// NewRateLimiter returns a limiter with the given cooldown per client IP.
func NewRateLimiter(cooldown time.Duration) *RateLimiter {
	return &RateLimiter{
		last:     make(map[string]time.Time),
		cooldown: cooldown,
		gcAfter:  cooldown * 100,
	}
}

// Allow reports whether a request from ip may proceed; if so, it records
// the hit. Thread-safe.
func (r *RateLimiter) Allow(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.gcLocked(now)
	if t, ok := r.last[ip]; ok && now.Sub(t) < r.cooldown {
		return false
	}
	r.last[ip] = now
	return true
}

// gcLocked drops entries whose last hit is older than gcAfter. Cheap when
// the map is small; called inline from Allow under the existing lock so
// we don't need a separate sweeper goroutine. The 64-entry threshold
// keeps tiny deployments from paying for the scan on every call.
func (r *RateLimiter) gcLocked(now time.Time) {
	if len(r.last) < 64 {
		return
	}
	for ip, t := range r.last {
		if now.Sub(t) > r.gcAfter {
			delete(r.last, ip)
		}
	}
}

// ClientIP is a thin shim over netutil.ClientIP so existing callers that
// imported it from this package keep working. New callers should use
// netutil.ClientIP directly.
func ClientIP(r *http.Request) string {
	return netutil.ClientIP(r)
}
