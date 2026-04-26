// Package queue is the lobby's FIFO queue: SQLite-backed, restart-safe,
// keyed by player_id. Ordering is by joined_at ascending (oldest first);
// position is derived on read rather than stored, so concurrent
// joins/leaves don't have to renumber siblings.
//
// Spec: docs/specs/foundations/04-lobby.md § Queue rules / Data flow.
package queue

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// ErrAlreadyQueued is returned by Join when the player is already in line.
var ErrAlreadyQueued = errors.New("queue: player already queued")

// ErrNotQueued is returned by Leave when the player isn't queued.
var ErrNotQueued = errors.New("queue: player not queued")

// Entry is one row.
type Entry struct {
	PlayerID string
	JoinedAt int64 // unix ms
}

// Open opens lobby.db and ensures the queue schema exists. WAL mode so
// the future admin panel can read while the advancer goroutine writes.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("queue: open: %w", err)
	}
	if _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS queue_entries (
            player_id  TEXT PRIMARY KEY,
            joined_at  INTEGER NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_queue_joined_at ON queue_entries(joined_at);
    `); err != nil {
		db.Close()
		return nil, fmt.Errorf("queue: schema: %w", err)
	}
	return db, nil
}

// Join inserts a player at the back of the queue. Returns ErrAlreadyQueued
// if the player_id is already present (idempotent re-join is the caller's
// concern — joining again should be an explicit Leave + Join so the player
// loses their position).
func Join(db *sql.DB, playerID string, nowMs int64) error {
	if playerID == "" {
		return errors.New("queue: player_id required")
	}
	_, err := db.Exec(
		`INSERT INTO queue_entries (player_id, joined_at) VALUES (?, ?)`,
		playerID, nowMs,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyQueued
		}
		return fmt.Errorf("queue: insert: %w", err)
	}
	return nil
}

// Leave removes a player. Returns ErrNotQueued if they weren't there;
// the caller should treat that as a 404, not a 500.
func Leave(db *sql.DB, playerID string) error {
	if playerID == "" {
		return errors.New("queue: player_id required")
	}
	res, err := db.Exec(`DELETE FROM queue_entries WHERE player_id = ?`, playerID)
	if err != nil {
		return fmt.Errorf("queue: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("queue: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotQueued
	}
	return nil
}

// Position returns the 1-based position of player_id in the queue, or
// 0 if they aren't queued. Position 1 == front of line.
func Position(db *sql.DB, playerID string) (int, error) {
	var joinedAt int64
	err := db.QueryRow(
		`SELECT joined_at FROM queue_entries WHERE player_id = ?`,
		playerID,
	).Scan(&joinedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("queue: position lookup: %w", err)
	}
	var ahead int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM queue_entries WHERE joined_at < ?`,
		joinedAt,
	).Scan(&ahead); err != nil {
		return 0, fmt.Errorf("queue: position count: %w", err)
	}
	return ahead + 1, nil
}

// Length returns the number of queued players.
func Length(db *sql.DB) (int, error) {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM queue_entries`).Scan(&n); err != nil {
		return 0, fmt.Errorf("queue: length: %w", err)
	}
	return n, nil
}

// PopHead atomically removes and returns the oldest entry. Returns
// (nil, nil) when the queue is empty — callers should treat that as
// "no work" rather than an error.
//
// Implemented as a transaction so concurrent advancer ticks (or an
// admin "advance" click) can't double-pop the same head.
func PopHead(db *sql.DB) (*Entry, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("queue: pop begin: %w", err)
	}
	defer tx.Rollback()

	var e Entry
	err = tx.QueryRow(
		`SELECT player_id, joined_at FROM queue_entries
          ORDER BY joined_at ASC LIMIT 1`,
	).Scan(&e.PlayerID, &e.JoinedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue: pop scan: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM queue_entries WHERE player_id = ?`,
		e.PlayerID,
	); err != nil {
		return nil, fmt.Errorf("queue: pop delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("queue: pop commit: %w", err)
	}
	return &e, nil
}

// List returns up to limit entries in queue order. For the admin panel.
func List(db *sql.DB, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(
		`SELECT player_id, joined_at FROM queue_entries
          ORDER BY joined_at ASC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("queue: list: %w", err)
	}
	defer rows.Close()
	out := make([]Entry, 0)
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.PlayerID, &e.JoinedAt); err != nil {
			return nil, fmt.Errorf("queue: list scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// isUniqueViolation matches SQLite's UNIQUE-constraint error string from
// modernc.org/sqlite (which doesn't expose typed extended codes).
// Mirrors pkg/identity/store.go:isUniqueViolation; duplicated rather
// than exposed because the queue package already intentionally has no
// dependency on identity.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
