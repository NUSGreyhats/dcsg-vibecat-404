package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeAllowlist(t *testing.T, path string, ids ...string) {
	t.Helper()
	body := `{"allowed":[`
	for i, id := range ids {
		if i > 0 {
			body += ","
		}
		body += `"` + id + `"`
	}
	body += `]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAllowlistMissingFileIsEmpty(t *testing.T) {
	a := NewAllowlist(filepath.Join(t.TempDir(), "absent.json"))
	if a.Has("anything") {
		t.Error("missing file: Has returned true")
	}
}

func TestAllowlistHit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allowlist.json")
	writeAllowlist(t, path, "alice", "bob")
	a := NewAllowlist(path)

	if !a.Has("alice") {
		t.Error("alice should be allowed")
	}
	if !a.Has("bob") {
		t.Error("bob should be allowed")
	}
	if a.Has("eve") {
		t.Error("eve should not be allowed")
	}
}

// TestRefreshOnMissPicksUpFreshAdmit simulates the lobby admit→connect
// race: the cache is warm and stale, the player wasn't there at last
// read, but the lobby has just rewritten the file. Without
// refresh-on-miss, the player would falsely fail auth for up to a TTL.
func TestRefreshOnMissPicksUpFreshAdmit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allowlist.json")
	writeAllowlist(t, path, "old-admit")

	a := NewAllowlist(path)
	a.ttl = time.Hour // keep the cache warm so the file rewrite won't be picked up via normal TTL expiry

	// Warm the cache.
	if !a.Has("old-admit") {
		t.Fatal("warm-up: Has(old-admit) returned false")
	}
	if a.Has("new-admit") {
		t.Fatal("warm-up: new-admit shouldn't be present yet")
	}

	// Lobby admits new-admit and rewrites the file.
	writeAllowlist(t, path, "old-admit", "new-admit")

	// With refresh-on-miss, the next Has() picks up the new entry even
	// though the TTL is still nowhere near expiring.
	if !a.Has("new-admit") {
		t.Error("refresh-on-miss: new-admit not picked up after file rewrite")
	}
}

// TestRefreshOnMissReturnsFalseForRealMisses — refresh-on-miss must
// not turn genuine misses into hits. Confirms a non-existent player
// stays rejected after the forced reload.
func TestRefreshOnMissReturnsFalseForRealMisses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allowlist.json")
	writeAllowlist(t, path, "alice")
	a := NewAllowlist(path)
	a.ttl = time.Hour

	if !a.Has("alice") {
		t.Fatal("warm-up")
	}
	if a.Has("eve") {
		t.Error("eve genuinely missing — should still be false")
	}
}

func TestCheckOpenModeIgnoresAllowlist(t *testing.T) {
	if err := Check(ModeOpen, nil, "anything"); err != nil {
		t.Errorf("ModeOpen with nil allowlist: %v, want nil", err)
	}
}

func TestCheckTokenRequiresAllowlist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allowlist.json")
	writeAllowlist(t, path, "alice")
	a := NewAllowlist(path)

	if err := Check(ModeToken, a, "alice"); err != nil {
		t.Errorf("alice on allowlist: %v, want nil", err)
	}
	if err := Check(ModeToken, a, "eve"); err == nil {
		t.Error("eve not on allowlist: got nil err")
	}
	if err := Check(ModeToken, nil, "alice"); err == nil {
		t.Error("nil allowlist: got nil err")
	}
}
