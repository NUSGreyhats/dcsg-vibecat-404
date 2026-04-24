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
// from disk if it has expired.
func (a *Allowlist) Has(playerID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.refresh()
	if a.ids == nil {
		return false
	}
	_, ok := a.ids[playerID]
	return ok
}

// refresh reloads the allowlist from disk if the TTL has expired. Caller
// must hold a.mu.
//
// A missing file is treated as the empty set (not an error) — in prod the
// lobby may not have seeded the file yet.
func (a *Allowlist) refresh() error {
	if a.ids != nil && time.Since(a.read) <= a.ttl {
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
