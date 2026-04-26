package queue

import (
	"errors"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "lobby.db")
}

func TestJoinAndPosition(t *testing.T) {
	path := openTest(t)
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Join(db, "alice", 1000); err != nil {
		t.Fatal(err)
	}
	if err := Join(db, "bob", 2000); err != nil {
		t.Fatal(err)
	}
	if err := Join(db, "carol", 3000); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		id   string
		want int
	}{
		{"alice", 1},
		{"bob", 2},
		{"carol", 3},
		{"nobody", 0},
	} {
		got, err := Position(db, c.id)
		if err != nil {
			t.Fatalf("position %s: %v", c.id, err)
		}
		if got != c.want {
			t.Errorf("position %s = %d, want %d", c.id, got, c.want)
		}
	}
}

func TestJoinDuplicate(t *testing.T) {
	db, err := Open(openTest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Join(db, "alice", 1000); err != nil {
		t.Fatal(err)
	}
	err = Join(db, "alice", 2000)
	if !errors.Is(err, ErrAlreadyQueued) {
		t.Errorf("re-join: got %v, want ErrAlreadyQueued", err)
	}
}

func TestLeaveAndPositionShifts(t *testing.T) {
	db, err := Open(openTest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for i, id := range []string{"alice", "bob", "carol", "dave"} {
		if err := Join(db, id, int64(1000+i*1000)); err != nil {
			t.Fatal(err)
		}
	}
	if err := Leave(db, "bob"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id   string
		want int
	}{
		{"alice", 1},
		{"carol", 2},
		{"dave", 3},
	} {
		got, _ := Position(db, c.id)
		if got != c.want {
			t.Errorf("after leave bob: position %s = %d, want %d", c.id, got, c.want)
		}
	}

	err = Leave(db, "ghost")
	if !errors.Is(err, ErrNotQueued) {
		t.Errorf("leave non-member: got %v, want ErrNotQueued", err)
	}
}

func TestPopHead(t *testing.T) {
	db, err := Open(openTest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert out of joined_at order to confirm we sort, not insert-order.
	for _, e := range []struct {
		id string
		ts int64
	}{
		{"carol", 3000},
		{"alice", 1000},
		{"bob", 2000},
	} {
		if err := Join(db, e.id, e.ts); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"alice", "bob", "carol"} {
		e, err := PopHead(db)
		if err != nil {
			t.Fatal(err)
		}
		if e == nil {
			t.Fatalf("pop %s: got nil entry", want)
		}
		if e.PlayerID != want {
			t.Errorf("pop = %s, want %s", e.PlayerID, want)
		}
	}
	e, err := PopHead(db)
	if err != nil {
		t.Fatal(err)
	}
	if e != nil {
		t.Errorf("pop on empty: got %+v, want nil", e)
	}
}

func TestLengthAndList(t *testing.T) {
	db, err := Open(openTest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if n, _ := Length(db); n != 0 {
		t.Errorf("empty length = %d, want 0", n)
	}

	for i, id := range []string{"a", "b", "c"} {
		if err := Join(db, id, int64(1000+i)); err != nil {
			t.Fatal(err)
		}
	}
	n, _ := Length(db)
	if n != 3 {
		t.Errorf("length = %d, want 3", n)
	}

	entries, err := List(db, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("list len = %d, want 3", len(entries))
	}
	for i, want := range []string{"a", "b", "c"} {
		if entries[i].PlayerID != want {
			t.Errorf("list[%d] = %s, want %s", i, entries[i].PlayerID, want)
		}
	}
}

func TestRestartSafety(t *testing.T) {
	path := openTest(t)
	db1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Join(db1, "alice", 1000); err != nil {
		t.Fatal(err)
	}
	if err := Join(db1, "bob", 2000); err != nil {
		t.Fatal(err)
	}
	db1.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	n, _ := Length(db2)
	if n != 2 {
		t.Errorf("after reopen: length = %d, want 2", n)
	}
	pos, _ := Position(db2, "bob")
	if pos != 2 {
		t.Errorf("after reopen: bob position = %d, want 2", pos)
	}
}
