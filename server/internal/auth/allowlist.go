package auth

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

// Allowlist reads /data/allowlist.json on demand with a short TTL cache.
// File shape: { "allowed": ["<hex-32>", ...] }.
//
// See docs/specs/foundations/03-identity-auth.md — the lobby service is the
// producer; the game server is the consumer.
//
// Hits go through the TTL cache (cheap). Misses can force one extra disk
// reload before final reject — closes the lobby admit → player connect
// race window where a freshly-written allowlist hasn't been picked up
// yet. With a 2 s TTL the worst case was a 2 s spurious "not on
// allowlist" rejection right after admission; refresh-on-miss collapses
// that to a single disk read per failed auth attempt.
//
// Forced refreshes are throttled (forceMinInterval): repeated misses
// from the same wrong-but-persistent player_id (typo, brute force) coalesce
// into at most one disk read per window, so a flood of bad attempts can't
// turn auth into an I/O hotspot.
type Allowlist struct {
	path             string
	mu               sync.Mutex
	ids              map[string]struct{}
	read             time.Time
	forced           time.Time
	ttl              time.Duration
	forceMinInterval time.Duration
}

// NewAllowlist returns an Allowlist backed by the given file path. The file
// is not read until Has is called.
func NewAllowlist(path string) *Allowlist {
	return &Allowlist{
		path:             path,
		ttl:              2 * time.Second,
		forceMinInterval: 250 * time.Millisecond,
	}
}

// Has reports whether playerID is in the allowlist, refreshing the cache
// from disk if it has expired. On a cache miss (player not present), it
// may force a fresh read before returning false — this keeps the hot
// path cheap (TTL-cached) while collapsing the admit→connect race that
// the lobby produces on every queue advance. Forced refreshes are
// throttled to at most one per forceMinInterval so a flood of misses
// (typos, brute force) can't pin the auth path on disk I/O.
func (a *Allowlist) Has(playerID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.refresh(false)
	if a.ids == nil {
		return false
	}
	if _, ok := a.ids[playerID]; ok {
		return true
	}
	// Miss: force-refresh and re-check, but only if we haven't done so
	// recently. Inside the throttle window the in-memory cache is
	// authoritative — at most forceMinInterval of staleness past TTL,
	// which is well below the 2 s lobby tick + StartGrace budget so a
	// real admit still lands before the player's retry.
	if time.Since(a.forced) < a.forceMinInterval {
		return false
	}
	if err := a.refresh(true); err != nil {
		return false
	}
	a.forced = time.Now()
	if a.ids == nil {
		return false
	}
	_, ok := a.ids[playerID]
	return ok
}

// refresh reloads the allowlist from disk. If force is false, the read is
// skipped when the TTL hasn't expired; if force is true, the read always
// happens. Caller must hold a.mu.
//
// A missing file is treated as the empty set (not an error) — in prod the
// lobby may not have seeded the file yet.
func (a *Allowlist) refresh(force bool) error {
	if !force && a.ids != nil && time.Since(a.read) <= a.ttl {
		return nil
	}
	data, err := os.ReadFile(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			a.ids = map[string]struct{}{}
			a.read = time.Now()
			return nil
		}
		return err
	}
	var parsed struct {
		Allowed []string `json:"allowed"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	ids := make(map[string]struct{}, len(parsed.Allowed))
	for _, id := range parsed.Allowed {
		ids[id] = struct{}{}
	}
	a.ids = ids
	a.read = time.Now()
	return nil
}

// Check returns nil if the player is authorized under the given mode.
// Open mode accepts everything. Token mode requires membership in the
// allowlist.
func Check(mode Mode, a *Allowlist, playerID string) error {
	if mode == ModeOpen {
		return nil
	}
	if a == nil {
		return errors.New("allowlist not configured")
	}
	if !a.Has(playerID) {
		return errors.New("player not on allowlist")
	}
	return nil
}
