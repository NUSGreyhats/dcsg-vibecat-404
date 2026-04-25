package leaderboard

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/greyhats/defcon-game/server/internal/protocol"

	_ "modernc.org/sqlite"
)

// OpenDB opens the leaderboard SQLite database at path and ensures the
// schema from docs/specs/foundations/08-leaderboard-replay.md exists.
//
// WAL mode + a 5 s busy timeout let the lobby's read-only connection
// (lobby/internal/leaderboard) co-exist with the game server's writer
// goroutine without lock contention.
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
		`SELECT cr.replay_id, cr.display_name, cr.total_ms, cr.seed, cr.splits_json
           FROM completed_runs cr
      LEFT JOIN flagged_runs fr ON fr.replay_id = cr.replay_id
          WHERE fr.replay_id IS NULL
          ORDER BY cr.total_ms ASC
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
		)
		if err := rows.Scan(&replayID, &displayName, &totalMs, &seed, &splitsJSON); err != nil {
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
		})
		rank++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("leaderboard: rows: %w", err)
	}
	return entries, nil
}
