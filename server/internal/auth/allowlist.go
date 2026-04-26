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
// Hits go through the TTL cache (cheap). Misses force one extra disk
// reload before final reject — closes the lobby admit → player connect
// race window where a freshly-written allowlist hasn't been picked up
// yet. With a 2 s TTL the worst case was a 2 s spurious "not on
// allowlist" rejection right after admission; refresh-on-miss
// collapses that to a single disk read per failed auth attempt.
type Allowlist struct {
	path string
	mu   sync.Mutex
	ids  map[string]struct{}
	read time.Time
	ttl  time.Duration
}

// NewAllowlist returns an Allowlist backed by the given file path. The file
// is not read until Has is called.
func NewAllowlist(path string) *Allowlist {
	return &Allowlist{path: path, ttl: 2 * time.Second}
}

// Has reports whether playerID is in the allowlist, refreshing the cache
// from disk if it has expired. On a cache miss (player not present), it
// forces a fresh read once before returning false — this keeps the hot
// path cheap (TTL-cached) while collapsing the admit→connect race that
// the lobby produces on every queue advance.
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
	// Miss: force-refresh and re-check. Bounded to one extra disk read
	// per missed auth attempt, which is fine — auth is per-WS-connect
	// and the failure path already costs a TLS round-trip.
	if err := a.refresh(true); err != nil {
		return false
	}
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
