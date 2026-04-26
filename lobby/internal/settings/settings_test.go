package settings

import (
	"path/filepath"
	"testing"

	"github.com/greyhats/defcon-game/lobby/internal/queue"
)

// openTest builds a fresh lobby.db (via queue.Open, which is the same
// connection the real lobby uses) and applies the settings schema.
func openTest(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "lobby.db")
}

func TestEnsureSchemaSeedsDefault(t *testing.T) {
	db, err := queue.Open(openTest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}

	n, err := GetMaxConcurrent(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != DefaultMaxConcurrent {
		t.Errorf("after EnsureSchema: got %d, want default %d", n, DefaultMaxConcurrent)
	}
}

func TestEnsureSchemaIdempotentDoesNotResetUserValue(t *testing.T) {
	db, err := queue.Open(openTest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}

	if err := SetMaxConcurrent(db, 12); err != nil {
		t.Fatal(err)
	}

	// Simulate a restart applying EnsureSchema again. The seed must
	// not clobber the admin's saved value.
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	n, err := GetMaxConcurrent(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 12 {
		t.Errorf("after re-EnsureSchema: got %d, want 12", n)
	}
}

func TestSetGet(t *testing.T) {
	db, err := queue.Open(openTest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}

	if err := SetMaxConcurrent(db, 4); err != nil {
		t.Fatal(err)
	}
	n, err := GetMaxConcurrent(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("after set 4: got %d, want 4", n)
	}

	// Replace, not duplicate.
	if err := SetMaxConcurrent(db, 8); err != nil {
		t.Fatal(err)
	}
	n, _ = GetMaxConcurrent(db)
	if n != 8 {
		t.Errorf("after set 8: got %d, want 8", n)
	}

	// 0 is a valid value (pause admissions).
	if err := SetMaxConcurrent(db, 0); err != nil {
		t.Fatal(err)
	}
	n, _ = GetMaxConcurrent(db)
	if n != 0 {
		t.Errorf("after set 0: got %d, want 0", n)
	}
}

func TestSetNegativeRejected(t *testing.T) {
	db, err := queue.Open(openTest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}

	if err := SetMaxConcurrent(db, -1); err == nil {
		t.Errorf("set -1: got nil error, want rejection")
	}
	// Default still in place after rejected set.
	n, _ := GetMaxConcurrent(db)
	if n != DefaultMaxConcurrent {
		t.Errorf("after rejected set: got %d, want default %d", n, DefaultMaxConcurrent)
	}
}
