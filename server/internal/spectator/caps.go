package spectator

import (
	"sync"
	"sync/atomic"
	"time"
)

// Caps enforces the global spectator count cap and the per-IP connect
// rate limit. The per-game cap is enforced inside each engine on register
// (it owns the per-game count anyway); we only handle the global +
// per-IP limits at the accept layer.
//
// Defaults from docs/specs/foundations/04-lobby.md:
//   - SPECTATOR_GLOBAL = 200
//   - per-IP new-connect rate = 5 / minute
type Caps struct {
	global      int32
	globalCount atomic.Int32

	perIPLimit  int
	perIPWindow time.Duration

	mu     sync.Mutex
	ipHits map[string][]time.Time
}

// NewCaps constructs a Caps enforcer with the given global cap and per-IP
// rate (events per window).
func NewCaps(globalCap int, perIPLimit int, perIPWindow time.Duration) *Caps {
	return &Caps{
		global:      int32(globalCap),
		perIPLimit:  perIPLimit,
		perIPWindow: perIPWindow,
		ipHits:      make(map[string][]time.Time),
	}
}

// AcquireGlobal increments the global count if it would stay under the
// cap. Returns true on success; false means the global cap is full and
// the caller should return 503.
func (c *Caps) AcquireGlobal() bool {
	for {
		cur := c.globalCount.Load()
		if cur >= c.global {
			return false
		}
		if c.globalCount.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

// ReleaseGlobal decrements the global count. Caller must have a matching
// AcquireGlobal that returned true.
func (c *Caps) ReleaseGlobal() {
	c.globalCount.Add(-1)
}

// GlobalCount returns the current count for diagnostics.
func (c *Caps) GlobalCount() int { return int(c.globalCount.Load()) }

// AllowIP returns true if `ip` may open a new spectator connection at
// `now`. Records the attempt on success. Sliding window: the IP can have
// at most perIPLimit successes within the last perIPWindow.
func (c *Caps) AllowIP(ip string, now time.Time) bool {
	if ip == "" || c.perIPLimit <= 0 {
		return true
	}
	cutoff := now.Add(-c.perIPWindow)
	c.mu.Lock()
	defer c.mu.Unlock()
	hits := c.ipHits[ip]
	// Drop stale entries — slice grows at most perIPLimit before this trim.
	pruned := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	if len(pruned) >= c.perIPLimit {
		c.ipHits[ip] = pruned
		return false
	}
	c.ipHits[ip] = append(pruned, now)
	return true
}
