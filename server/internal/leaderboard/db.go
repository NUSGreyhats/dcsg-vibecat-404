package leaderboard

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/greyhats/defcon-game/server/internal/protocol"

	_ "modernc.org/sqlite"
)

// ErrNotFound and ErrChecksumMismatch are returned by GetReplay; mirror
// the same errors the lobby's leaderboard package exports so a future
// shared package can be a drop-in replacement.
var (
	ErrNotFound         = errors.New("leaderboard: replay not found")
	ErrChecksumMismatch = errors.New("leaderboard: replay checksum mismatch")
)

// Replay is the on-disk shape of a single completed run, returned by
// GetReplay for the replay engine to drive. Fields mirror the lobby's
// /admin/replay/:id JSON contract.
type Replay struct {
	ReplayID    string `json:"replay_id"`
	PlayerID    string `json:"player_id"`
	DisplayName string `json:"display_name"`
	TotalMs     int64  `json:"total_ms"`
	Seed        int64  `json:"seed"`
	InputLog    []byte `json:"input_log"`
}

// GetReplay loads a single completed run by replay_id and verifies the
// stored checksum against SHA256(seed || input_log). Returns ErrNotFound
// when the row is missing, ErrChecksumMismatch if the bytes have been
// tampered with at rest. See docs/specs/foundations/08-leaderboard-replay.md.
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

// OpenDB opens the leaderboard SQLite database at path and ensures the
// schema from docs/specs/foundations/08-leaderboard-replay.md exists.
//
// WAL mode + a 5 s busy timeout let the lobby's read-only connection
// (lobby/internal/leaderboard) co-exist with the game server's writer
// goroutine without lock contention.
//
// flagged_runs is created here as well so TopRuns' LEFT JOIN works on a
// fresh DB regardless of which service starts first. Lobby's
// admin.EnsureSchema produces identical DDL — last writer wins.
func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
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
        CREATE TABLE IF NOT EXISTS flagged_runs (
            replay_id  TEXT PRIMARY KEY,
            flagged_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            reason     TEXT NOT NULL DEFAULT ''
        );
    `); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// InsertRun serializes the run's splits to JSON and inserts a single row.
func InsertRun(db *sql.DB, run *CompletedRun) error {
	if run == nil {
		return fmt.Errorf("leaderboard: nil run")
	}
	splitsJSON, err := json.Marshal(run.Splits)
	if err != nil {
		return fmt.Errorf("leaderboard: marshal splits: %w", err)
	}
	_, err = db.Exec(
		`INSERT INTO completed_runs
            (replay_id, player_id, display_name, total_ms, seed, splits_json, input_log, checksum, submitted_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ReplayID,
		run.PlayerID,
		run.DisplayName,
		run.TotalMs,
		run.Seed,
		string(splitsJSON),
		run.InputLog,
		run.Checksum[:],
		run.SubmittedAt,
	)
	if err != nil {
		return fmt.Errorf("leaderboard: insert: %w", err)
	}
	return nil
}

// TopRuns returns the fastest n runs ordered by total_ms ascending, with
// rank filled from the 1-based row position.
func TopRuns(db *sql.DB, n int) ([]protocol.LeaderboardEntry, error) {
	if n <= 0 {
		return nil, nil
	}
	rows, err := db.Query(
		`SELECT cr.replay_id, cr.display_name, cr.total_ms, cr.seed, cr.splits_json, cr.submitted_at
           FROM completed_runs cr
      LEFT JOIN flagged_runs fr ON fr.replay_id = cr.replay_id
          WHERE fr.replay_id IS NULL
          ORDER BY cr.total_ms ASC, cr.submitted_at ASC
          LIMIT ?`,
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("leaderboard: query top: %w", err)
	}
	defer rows.Close()

	entries := make([]protocol.LeaderboardEntry, 0, n)
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
		var splits []protocol.SplitData
		if splitsJSON != "" {
			if err := json.Unmarshal([]byte(splitsJSON), &splits); err != nil {
				return nil, fmt.Errorf("leaderboard: unmarshal splits: %w", err)
			}
		}
		entries = append(entries, protocol.LeaderboardEntry{
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
