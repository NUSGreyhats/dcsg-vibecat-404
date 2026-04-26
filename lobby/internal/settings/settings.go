// Package settings is a tiny key/value store inside lobby.db for
// admin-tunable knobs that need to outlive a process restart but are
// too operational to live in env vars.
//
// Currently holds the queue's max-concurrent admission cap, set from
// the admin panel. EnsureSchema seeds a default on first boot so the
// row always exists — the advancer never has to reason about an
// "unset" state.
//
// Spec: docs/specs/foundations/04-lobby.md § Admin panel.
package settings

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// keyMaxConcurrent is the lobby_settings row that holds the queue
// admission cap. Stored as a base-10 string for human-readable rows.
const keyMaxConcurrent = "max_concurrent"

// DefaultMaxConcurrent is the seed value written on first boot when
// no row exists. Matches the prod MAX_CONCURRENT env target so a
// fresh deploy admits the same number of players whether or not an
// admin has visited the panel yet.
const DefaultMaxConcurrent = 4

// EnsureSchema creates the lobby_settings table if it doesn't exist
// and seeds default values for any keys that haven't been written
// yet. Idempotent; safe to call on every boot. The seeded default
// guarantees GetMaxConcurrent always returns a value.
func EnsureSchema(db *sql.DB) error {
	if _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS lobby_settings (
            key   TEXT PRIMARY KEY,
            value TEXT NOT NULL
        );
    `); err != nil {
		return fmt.Errorf("settings: schema: %w", err)
	}
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO lobby_settings(key, value) VALUES(?, ?)`,
		keyMaxConcurrent, strconv.Itoa(DefaultMaxConcurrent),
	); err != nil {
		return fmt.Errorf("settings: seed max concurrent: %w", err)
	}
	return nil
}

// GetMaxConcurrent returns the queue admission cap. EnsureSchema
// seeds a default row at boot so the row should always exist; if it
// somehow doesn't, the default is returned. n=0 is valid and pauses
// admissions without kicking already-admitted players.
func GetMaxConcurrent(db *sql.DB) (int, error) {
	var s string
	err := db.QueryRow(
		`SELECT value FROM lobby_settings WHERE key = ?`,
		keyMaxConcurrent,
	).Scan(&s)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DefaultMaxConcurrent, nil
		}
		return 0, fmt.Errorf("settings: get max concurrent: %w", err)
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("settings: max concurrent corrupt: %q", s)
	}
	return n, nil
}

// SetMaxConcurrent writes (or replaces) the cap. n must be >= 0.
func SetMaxConcurrent(db *sql.DB, n int) error {
	if n < 0 {
		return errors.New("settings: max concurrent must be >= 0")
	}
	if _, err := db.Exec(
		`INSERT INTO lobby_settings(key, value) VALUES(?, ?)
         ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		keyMaxConcurrent, strconv.Itoa(n),
	); err != nil {
		return fmt.Errorf("settings: set max concurrent: %w", err)
	}
	return nil
}
