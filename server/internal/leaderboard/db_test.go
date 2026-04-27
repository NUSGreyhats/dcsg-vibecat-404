package leaderboard

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTopRunsDeduplicatesPlayers(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "game.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	base := time.Unix(1_700_000_000, 0).UTC()
	for _, run := range []*CompletedRun{
		testCompletedRun("p1-slow", "player-1", "CatOne", 3000, base),
		testCompletedRun("p2", "player-2", "CatTwo", 2000, base.Add(time.Second)),
		testCompletedRun("p1-fast", "player-1", "CatOne", 1000, base.Add(2*time.Second)),
	} {
		if err := InsertRun(db, run); err != nil {
			t.Fatalf("insert %s: %v", run.ReplayID, err)
		}
	}

	entries, err := TopRuns(db, 10)
	if err != nil {
		t.Fatalf("top runs: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2: %+v", len(entries), entries)
	}
	if entries[0].ReplayID != "p1-fast" || entries[0].Rank != 1 {
		t.Fatalf("entry[0] = %+v, want p1-fast rank 1", entries[0])
	}
	if entries[1].ReplayID != "p2" || entries[1].Rank != 2 {
		t.Fatalf("entry[1] = %+v, want p2 rank 2", entries[1])
	}
}
