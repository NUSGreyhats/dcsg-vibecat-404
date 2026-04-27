package leaderboard

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestPageRunsUsesGlobalRanksWithDedupe(t *testing.T) {
	db := openTestDB(t)
	insertRun(t, db, "r-p1-slow", "p1", "P1", 1200, "2026-01-01T00:00:04Z")
	insertRun(t, db, "r-p1-fast", "p1", "P1", 900, "2026-01-01T00:00:02Z")
	insertRun(t, db, "r-p2", "p2", "P2", 800, "2026-01-01T00:00:01Z")
	insertRun(t, db, "r-p3", "p3", "P3", 950, "2026-01-01T00:00:03Z")

	page, err := PageRuns(db, 2, 2, true, "p3")
	if err != nil {
		t.Fatalf("PageRuns: %v", err)
	}
	if page.Total != 3 || !page.HasPrev || page.HasNext {
		t.Fatalf("page metadata = total %d prev %v next %v, want total 3 prev true next false", page.Total, page.HasPrev, page.HasNext)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(page.Entries))
	}
	got := page.Entries[0]
	if got.Rank != 3 || got.ReplayID != "r-p3" || !got.Current {
		t.Fatalf("entry = rank %d replay %q current %v, want rank 3 replay r-p3 current true", got.Rank, got.ReplayID, got.Current)
	}
}

func TestPlayerBestRunFindsExtraRowBelowTopPage(t *testing.T) {
	db := openTestDB(t)
	for i := 1; i <= 25; i++ {
		playerID := fmt.Sprintf("p%02d", i)
		insertRun(t, db, fmt.Sprintf("r-%02d", i), playerID, playerID, int64(1000+i), fmt.Sprintf("2026-01-01T00:00:%02dZ", i))
	}

	page, err := PageRuns(db, 20, 1, true, "p25")
	if err != nil {
		t.Fatalf("PageRuns: %v", err)
	}
	if len(page.Entries) != 20 {
		t.Fatalf("entries len = %d, want 20", len(page.Entries))
	}
	for _, entry := range page.Entries {
		if entry.Current {
			t.Fatalf("top page unexpectedly contains current player entry %+v", entry)
		}
	}
	current, err := PlayerBestRun(db, "p25", true)
	if err != nil {
		t.Fatalf("PlayerBestRun: %v", err)
	}
	if current.Rank != 25 || current.ReplayID != "r-25" || !current.Current {
		t.Fatalf("current = rank %d replay %q current %v, want rank 25 replay r-25 current true", current.Rank, current.ReplayID, current.Current)
	}
}

func TestFlaggedRunsExcludedFromPageAndPlayerLookup(t *testing.T) {
	db := openTestDB(t)
	insertRun(t, db, "r-ok", "p1", "P1", 1000, "2026-01-01T00:00:01Z")
	insertRun(t, db, "r-flagged", "p2", "P2", 900, "2026-01-01T00:00:02Z")
	if _, err := db.Exec(`INSERT INTO flagged_runs (replay_id, reason) VALUES (?, ?)`, "r-flagged", "test"); err != nil {
		t.Fatalf("flag run: %v", err)
	}

	page, err := PageRuns(db, 20, 1, true, "")
	if err != nil {
		t.Fatalf("PageRuns: %v", err)
	}
	if page.Total != 1 || len(page.Entries) != 1 || page.Entries[0].ReplayID != "r-ok" {
		t.Fatalf("visible entries = total %d %+v, want only r-ok", page.Total, page.Entries)
	}
	if _, err := PlayerBestRun(db, "p2", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PlayerBestRun flagged err = %v, want ErrNotFound", err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "game.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func insertRun(t *testing.T, db *sql.DB, replayID, playerID, displayName string, totalMs int64, submittedAt string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO completed_runs
		     (replay_id, player_id, display_name, total_ms, seed, splits_json, input_log, checksum, submitted_at)
		   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		replayID,
		playerID,
		displayName,
		totalMs,
		int64(7),
		"[]",
		[]byte{1},
		[]byte{2},
		submittedAt,
	)
	if err != nil {
		t.Fatalf("insert %s: %v", replayID, err)
	}
}
