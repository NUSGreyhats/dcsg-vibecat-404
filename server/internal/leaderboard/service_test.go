package leaderboard

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/greyhats/defcon-game/server/internal/protocol"
)

func TestSubmitAndRefreshReturnsRankAndBroadcastsTop(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "game.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := NewService(db)
	svc.Start(ctx)

	out := make(chan protocol.S2CEnvelope, 4)
	svc.Register("player-1", out)
	defer svc.Unregister("player-1")

	base := time.Unix(1_700_000_000, 0).UTC()
	first := testCompletedRun("slow", "player-1", "Slow", 2000, base)
	if result := svc.SubmitAndRefresh(first); result.Err != nil {
		t.Fatalf("submit first: %v", result.Err)
	}
	drainLeaderboard(t, out)

	fast := testCompletedRun("fast", "player-2", "Fast", 1000, base.Add(time.Second))
	result := svc.SubmitAndRefresh(fast)
	if result.Err != nil {
		t.Fatalf("submit fast: %v", result.Err)
	}
	if result.Rank != 1 {
		t.Fatalf("rank = %d, want 1", result.Rank)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(result.Entries))
	}
	if got := result.Entries[0].ReplayID; got != "fast" {
		t.Fatalf("top replay = %q, want fast", got)
	}

	env := drainLeaderboard(t, out)
	data, ok := env.Data.(protocol.LeaderboardData)
	if !ok {
		t.Fatalf("broadcast data type = %T, want protocol.LeaderboardData", env.Data)
	}
	if len(data.Entries) != 2 || data.Entries[0].ReplayID != "fast" {
		t.Fatalf("broadcast entries = %+v, want fast first", data.Entries)
	}
}

func testCompletedRun(replayID, playerID, displayName string, totalMs int64, submittedAt time.Time) *CompletedRun {
	inputLog := []byte("[]")
	return &CompletedRun{
		ReplayID:    replayID,
		PlayerID:    playerID,
		DisplayName: displayName,
		TotalMs:     totalMs,
		Splits:      []protocol.SplitData{{RoomIndex: 1, TimeMs: totalMs, Kills: 1}},
		Seed:        submittedAt.UnixMilli(),
		InputLog:    inputLog,
		Checksum:    Checksum(submittedAt.UnixMilli(), inputLog),
		SubmittedAt: submittedAt,
	}
}

func drainLeaderboard(t *testing.T, out <-chan protocol.S2CEnvelope) protocol.S2CEnvelope {
	t.Helper()
	select {
	case env := <-out:
		if env.Type != protocol.S2CLeaderboard {
			t.Fatalf("env type = %#x, want S2CLeaderboard", env.Type)
		}
		return env
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for leaderboard broadcast")
		return protocol.S2CEnvelope{}
	}
}
