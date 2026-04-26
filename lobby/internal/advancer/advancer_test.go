package advancer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/greyhats/defcon-game/lobby/internal/livegames"
	"github.com/greyhats/defcon-game/lobby/internal/queue"
)

// newAdvancerTest sets up an Advancer wired to a temp lobby.db and a
// temp allowlist path. live-games poller is left nil — tests drive
// snapshots through step() directly.
func newAdvancerTest(t *testing.T) (*Advancer, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lobby.db")
	allowlistPath := filepath.Join(dir, "allowlist.json")
	db, err := queue.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := New(db, nil, "http://unused", allowlistPath)
	return a, allowlistPath
}

func readAllowed(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var body struct {
		Allowed []string `json:"allowed"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	return body.Allowed
}

func TestAdmitsFromQueueWithinMax(t *testing.T) {
	a, path := newAdvancerTest(t)
	now := time.Unix(1_000_000, 0)
	for i, id := range []string{"alice", "bob", "carol", "dave", "eve"} {
		if err := queue.Join(a.db, id, int64(1000+i)); err != nil {
			t.Fatal(err)
		}
	}

	a.step(now, 4, nil)

	got := readAllowed(t, path)
	sort.Strings(got)
	want := []string{"alice", "bob", "carol", "dave"}
	if !equalStrings(got, want) {
		t.Errorf("admitted = %v, want %v", got, want)
	}
	// Eve should still be queued at position 1.
	pos, _ := queue.Position(a.db, "eve")
	if pos != 1 {
		t.Errorf("eve position = %d, want 1", pos)
	}
}

func TestAFKForfeit(t *testing.T) {
	a, path := newAdvancerTest(t)
	t0 := time.Unix(1_000_000, 0)

	if err := queue.Join(a.db, "alice", 1000); err != nil {
		t.Fatal(err)
	}
	a.step(t0, 1, nil)
	if !equalStrings(readAllowed(t, path), []string{"alice"}) {
		t.Fatalf("after admit: %v", readAllowed(t, path))
	}

	// Within grace: still admitted, not yet started.
	a.step(t0.Add(StartGrace-time.Second), 1, nil)
	if !equalStrings(readAllowed(t, path), []string{"alice"}) {
		t.Errorf("within grace: %v, want [alice]", readAllowed(t, path))
	}

	// Past grace, never seen in live → AFK forfeit.
	a.step(t0.Add(StartGrace+time.Second), 1, nil)
	if got := readAllowed(t, path); len(got) != 0 {
		t.Errorf("past grace: %v, want empty", got)
	}
}

func TestPostRunGrace(t *testing.T) {
	a, path := newAdvancerTest(t)
	t0 := time.Unix(1_000_000, 0)

	if err := queue.Join(a.db, "alice", 1000); err != nil {
		t.Fatal(err)
	}
	a.step(t0, 1, nil)

	// Player appears in live-games — run started.
	live := []livegames.Entry{{PlayerID: "alice"}}
	a.step(t0.Add(2*time.Second), 1, live)

	// Run keeps appearing for a while.
	a.step(t0.Add(20*time.Second), 1, live)

	// Run vanishes (completed or disconnected).
	a.step(t0.Add(25*time.Second), 1, nil)
	if !equalStrings(readAllowed(t, path), []string{"alice"}) {
		t.Errorf("immediately after vanish: %v, want [alice] (in grace)", readAllowed(t, path))
	}

	// 16 s after last sighting: grace expired, evicted.
	a.step(t0.Add(25*time.Second).Add(PostRunGrace+time.Second), 1, nil)
	if got := readAllowed(t, path); len(got) != 0 {
		t.Errorf("after grace: %v, want empty", got)
	}
}

func TestTurnTTLEviction(t *testing.T) {
	a, path := newAdvancerTest(t)
	t0 := time.Unix(1_000_000, 0)

	if err := queue.Join(a.db, "alice", 1000); err != nil {
		t.Fatal(err)
	}
	a.step(t0, 1, nil)

	// Player keeps showing up in live-games — they're playing fine.
	live := []livegames.Entry{{PlayerID: "alice"}}
	a.step(t0.Add(time.Minute), 1, live)
	if !equalStrings(readAllowed(t, path), []string{"alice"}) {
		t.Fatalf("at 1 min: %v", readAllowed(t, path))
	}

	// 5 min + 1 s: TTL pops the slot regardless of liveness.
	a.step(t0.Add(TurnTTL+time.Second), 1, live)
	if got := readAllowed(t, path); len(got) != 0 {
		t.Errorf("past TTL: %v, want empty", got)
	}
}

func TestEvictedSlotImmediatelyRefilled(t *testing.T) {
	a, path := newAdvancerTest(t)
	t0 := time.Unix(1_000_000, 0)

	for i, id := range []string{"alice", "bob"} {
		if err := queue.Join(a.db, id, int64(1000+i)); err != nil {
			t.Fatal(err)
		}
	}
	a.step(t0, 1, nil)
	if !equalStrings(readAllowed(t, path), []string{"alice"}) {
		t.Fatalf("first admit: %v", readAllowed(t, path))
	}

	// Alice AFK forfeits → bob should fill in the same tick.
	a.step(t0.Add(StartGrace+time.Second), 1, nil)
	if !equalStrings(readAllowed(t, path), []string{"bob"}) {
		t.Errorf("after AFK kick: %v, want [bob]", readAllowed(t, path))
	}
}

func TestForceFree(t *testing.T) {
	a, path := newAdvancerTest(t)
	t0 := time.Unix(1_000_000, 0)

	if err := queue.Join(a.db, "alice", 1000); err != nil {
		t.Fatal(err)
	}
	a.step(t0, 1, nil)

	if !a.ForceFree("alice") {
		t.Error("ForceFree: returned false for present player")
	}
	if got := readAllowed(t, path); len(got) != 0 {
		t.Errorf("after ForceFree: %v, want empty", got)
	}
	if a.ForceFree("alice") {
		t.Error("ForceFree: returned true on second call")
	}
}

func TestAtomicWriteAndIdempotent(t *testing.T) {
	a, path := newAdvancerTest(t)
	t0 := time.Unix(1_000_000, 0)

	if err := queue.Join(a.db, "alice", 1000); err != nil {
		t.Fatal(err)
	}
	a.step(t0, 1, nil)
	first, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// Same set on a later tick: no rewrite.
	live := []livegames.Entry{{PlayerID: "alice"}}
	a.step(t0.Add(time.Second), 1, live)
	second, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.ModTime().Equal(second.ModTime()) {
		t.Errorf("idempotent step rewrote file: mtime moved %v → %v", first.ModTime(), second.ModTime())
	}

	// Tmp files cleaned up: only allowlist.json should be in the dir.
	dir := filepath.Dir(path)
	matches, _ := filepath.Glob(filepath.Join(dir, "allowlist-*.tmp"))
	if len(matches) != 0 {
		t.Errorf("leftover tmp files: %v", matches)
	}
}

func TestAdmittedSnapshotAndAdmitted(t *testing.T) {
	a, _ := newAdvancerTest(t)
	t0 := time.Unix(1_000_000, 0)
	for i, id := range []string{"alice", "bob"} {
		if err := queue.Join(a.db, id, int64(1000+i)); err != nil {
			t.Fatal(err)
		}
	}
	a.step(t0, 2, nil)

	if !a.Admitted("alice") || !a.Admitted("bob") {
		t.Error("Admitted: missed admitted player")
	}
	if a.Admitted("ghost") {
		t.Error("Admitted: false positive")
	}
	got := a.AdmittedSnapshot()
	want := []string{"alice", "bob"}
	if !equalStrings(got, want) {
		t.Errorf("snapshot = %v, want %v", got, want)
	}
}
