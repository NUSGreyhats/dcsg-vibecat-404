package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/greyhats/defcon-game/pkg/identity"
	"github.com/greyhats/defcon-game/server/internal/auth"
)

func TestBuildAuthCheckTokenReturnsDisplayName(t *testing.T) {
	dir := t.TempDir()
	identityDB, err := identity.OpenDB(filepath.Join(dir, "identity.db"))
	if err != nil {
		t.Fatalf("open identity db: %v", err)
	}
	defer identityDB.Close()

	playerID := "f71060260bea88bef40261b02d8f58c0"
	if err := identity.Insert(identityDB, &identity.Player{
		PlayerID:     playerID,
		DisplayName:  "elma",
		PasswordHash: []byte("unused"),
		Contact:      "elma@example.test",
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("insert player: %v", err)
	}

	allowlistPath := filepath.Join(dir, "allowlist.json")
	if err := os.WriteFile(allowlistPath, []byte(`{"allowed":["`+playerID+`"]}`), 0o644); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}

	name, err := buildAuthCheck(auth.ModeToken, identityDB, auth.NewAllowlist(allowlistPath))(playerID)
	if err != nil {
		t.Fatalf("token auth: %v", err)
	}
	if name != "elma" {
		t.Fatalf("display name = %q, want elma", name)
	}
}

func TestBuildAuthCheckTokenRejectsMissingAllowlistEntry(t *testing.T) {
	dir := t.TempDir()
	identityDB, err := identity.OpenDB(filepath.Join(dir, "identity.db"))
	if err != nil {
		t.Fatalf("open identity db: %v", err)
	}
	defer identityDB.Close()

	if err := os.WriteFile(filepath.Join(dir, "allowlist.json"), []byte(`{"allowed":[]}`), 0o644); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}

	_, err = buildAuthCheck(auth.ModeToken, identityDB, auth.NewAllowlist(filepath.Join(dir, "allowlist.json")))("f71060260bea88bef40261b02d8f58c0")
	if err == nil {
		t.Fatal("missing allowlist entry: got nil error")
	}
}

// Token-mode regression: an allowlisted player without an identity.db
// row must be rejected with a "player not registered" error, not silently
// fall through to writing the raw player_id onto leaderboard rows.
func TestBuildAuthCheckTokenRejectsAllowlistedWithoutIdentity(t *testing.T) {
	dir := t.TempDir()
	identityDB, err := identity.OpenDB(filepath.Join(dir, "identity.db"))
	if err != nil {
		t.Fatalf("open identity db: %v", err)
	}
	defer identityDB.Close()

	playerID := "f71060260bea88bef40261b02d8f58c0"
	allowlistPath := filepath.Join(dir, "allowlist.json")
	if err := os.WriteFile(allowlistPath, []byte(`{"allowed":["`+playerID+`"]}`), 0o644); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}

	name, err := buildAuthCheck(auth.ModeToken, identityDB, auth.NewAllowlist(allowlistPath))(playerID)
	if err == nil {
		t.Fatalf("allowlisted but unregistered: got nil error, name=%q", name)
	}
	if name != "" {
		t.Fatalf("display name = %q, want empty", name)
	}
	if err.Error() != "player not registered" {
		t.Fatalf("error = %q, want %q", err.Error(), "player not registered")
	}
}
