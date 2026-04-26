// Package leaderboard reads completed_runs from the game-server-owned
// game.db. The lobby never writes; it only serves /api/leaderboard and
// /api/replay/:replay_id from the same SQLite file via WAL mode.
//
// See docs/specs/foundations/08-leaderboard-replay.md.
package leaderboard

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a replay_id doesn't exist.
var ErrNotFound = errors.New("leaderboard: replay not found")

// ErrChecksumMismatch is returned when the stored checksum doesn't match
// SHA256(seed || input_log) — i.e. game.db was tampered with at rest.
var ErrChecksumMismatch = errors.New("leaderboard: replay checksum mismatch")

// Open opens game.db with WAL + a busy timeout so reads can co-exist with
// the game server's writer goroutine. WAL is durable across opens; the
// game server picks it up on its next reconnect.
//
// The schema DDL matches server/internal/leaderboard/db.go — duplicated so
// the lobby can serve /api/leaderboard cleanly when the file doesn't exist
// yet (e.g., fresh deploy with no completed runs). DDL is idempotent.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("leaderboard: open: %w", err)
	}
	if _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS completed_runs (
            replay_id      TEXT PRIMARY KEY,
            player_id      TEXT NOT NULL,
            display_name   TEXT NOT NULL,
            total_ms       INTEGER NOT NULL,
            seed           INTEGER NOT NULL,
            splits_json    TEXT NOT NULL,
            input_log      BLOB NOT NULL,
            checksum       BLOB NOT NULL,
            submitted_at   DATETIME NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_runs_total_ms ON completed_runs(total_ms);
        CREATE INDEX IF NOT EXISTS idx_runs_player   ON completed_runs(player_id);
    `); err != nil {
		db.Close()
		return nil, fmt.Errorf("leaderboard: schema: %w", err)
	}
	return db, nil
}

// TopRuns returns the fastest n runs ordered by total_ms ascending, with
// rank filled from the 1-based row position. When dedupe is true, only each
// player's fastest run is returned.
func TopRuns(db *sql.DB, n int, dedupe bool) ([]Entry, error) {
	if n <= 0 {
		return nil, nil
	}
	rows, err := db.Query(
		`WITH ranked AS (
             SELECT cr.replay_id, cr.player_id, cr.display_name, cr.total_ms,
                    cr.seed, cr.splits_json, cr.submitted_at,
                    ROW_NUMBER() OVER (
                        PARTITION BY cr.player_id
                        ORDER BY cr.total_ms ASC, cr.submitted_at ASC
                    ) AS player_rank
               FROM completed_runs cr
          LEFT JOIN flagged_runs fr ON fr.replay_id = cr.replay_id
              WHERE fr.replay_id IS NULL
         )
         SELECT replay_id, display_name, total_ms, seed, splits_json, submitted_at
           FROM ranked
          WHERE (? = 0 OR player_rank = 1)
          ORDER BY total_ms ASC, submitted_at ASC
          LIMIT ?`,
		boolInt(dedupe),
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("leaderboard: query top: %w", err)
	}
	defer rows.Close()

	entries := make([]Entry, 0, n)
	rank := 1
	for rows.Next() {
		var (
			replayID    string
			displayName string
			totalMs     int64
			seed        int64
			splitsJSON  string
			submittedAt string
		)
		if err := rows.Scan(&replayID, &displayName, &totalMs, &seed, &splitsJSON, &submittedAt); err != nil {
			return nil, fmt.Errorf("leaderboard: scan: %w", err)
		}
		var splits []Split
		if splitsJSON != "" {
			if err := json.Unmarshal([]byte(splitsJSON), &splits); err != nil {
				return nil, fmt.Errorf("leaderboard: unmarshal splits: %w", err)
			}
		}
		entries = append(entries, Entry{
			Rank:        rank,
			DisplayName: displayName,
			TotalMs:     totalMs,
			Splits:      splits,
			Seed:        seed,
			ReplayID:    replayID,
			SubmittedAt: submittedAt,
		})
		rank++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("leaderboard: rows: %w", err)
	}
	return entries, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Replay holds the fields needed to reconstruct a run client-side.
type Replay struct {
	ReplayID    string `json:"replay_id"`
	PlayerID    string `json:"player_id"`
	DisplayName string `json:"display_name"`
	TotalMs     int64  `json:"total_ms"`
	Seed        int64  `json:"seed"`
	InputLog    []byte `json:"input_log"`
}

// GetReplay fetches a single completed run by replay_id. Returns ErrNotFound
// if the row doesn't exist, or ErrChecksumMismatch if the stored checksum
// doesn't match SHA256(seed || input_log) — see
// docs/specs/foundations/08-leaderboard-replay.md.
func GetReplay(db *sql.DB, replayID string) (*Replay, error) {
	var r Replay
	var stored []byte
	err := db.QueryRow(
		`SELECT replay_id, player_id, display_name, total_ms, seed, input_log, checksum
           FROM completed_runs
          WHERE replay_id = ?`,
		replayID,
	).Scan(&r.ReplayID, &r.PlayerID, &r.DisplayName, &r.TotalMs, &r.Seed, &r.InputLog, &stored)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("leaderboard: get replay: %w", err)
	}
	got := Checksum(r.Seed, r.InputLog)
	if subtle.ConstantTimeCompare(stored, got[:]) != 1 {
		return nil, ErrChecksumMismatch
	}
	return &r, nil
}
