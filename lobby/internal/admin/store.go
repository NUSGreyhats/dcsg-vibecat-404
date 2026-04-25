package admin

import (
	"database/sql"
	"errors"
	"fmt"
)

// EnsureSchema adds admin-owned tables (currently just flagged_runs) to
// game.db. Idempotent. Both the lobby and the game server may call
// this on startup — last writer wins, both produce the same DDL.
func EnsureSchema(db *sql.DB) error {
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS flagged_runs (
            replay_id  TEXT PRIMARY KEY,
            flagged_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            reason     TEXT NOT NULL DEFAULT ''
        );
    `)
	return err
}

// Flag marks a replay_id as hidden from the public leaderboard. Reason
// is optional admin-visible note. Idempotent — re-flagging an already
// flagged run is a no-op.
func Flag(db *sql.DB, replayID, reason string) error {
	if replayID == "" {
		return errors.New("admin: replay_id required")
	}
	_, err := db.Exec(
		`INSERT OR REPLACE INTO flagged_runs (replay_id, reason) VALUES (?, ?)`,
		replayID, reason,
	)
	return err
}

// Unflag removes a flag. Idempotent — unflagging an unflagged run is
// a no-op, not an error.
func Unflag(db *sql.DB, replayID string) error {
	if replayID == "" {
		return errors.New("admin: replay_id required")
	}
	_, err := db.Exec(`DELETE FROM flagged_runs WHERE replay_id = ?`, replayID)
	return err
}

// HardDelete removes a run from completed_runs entirely. Use with care —
// the replay is gone after this and replay verification can't recover
// it. Also cleans up any matching flagged_runs row.
func HardDelete(db *sql.DB, replayID string) error {
	if replayID == "" {
		return errors.New("admin: replay_id required")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM completed_runs WHERE replay_id = ?`, replayID); err != nil {
		return fmt.Errorf("delete completed: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM flagged_runs WHERE replay_id = ?`, replayID); err != nil {
		return fmt.Errorf("delete flagged: %w", err)
	}
	return tx.Commit()
}

// AdminEntry is one row in the admin leaderboard view, including
// flagged status so the UI can render it differently.
type AdminEntry struct {
	ReplayID    string `json:"replay_id"`
	PlayerID    string `json:"player_id"`
	DisplayName string `json:"display_name"`
	TotalMs     int64  `json:"total_ms"`
	SubmittedAt string `json:"submitted_at"`
	Flagged     bool   `json:"flagged"`
}

// AllRuns returns every completed run including flagged ones, ordered
// fastest-first. The public TopRuns filters flagged out; this admin
// view shows them so moderators can unflag if needed.
func AllRuns(db *sql.DB, limit int) ([]AdminEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(`
        SELECT cr.replay_id, cr.player_id, cr.display_name, cr.total_ms,
               cr.submitted_at,
               CASE WHEN fr.replay_id IS NULL THEN 0 ELSE 1 END AS flagged
          FROM completed_runs cr
     LEFT JOIN flagged_runs fr ON fr.replay_id = cr.replay_id
      ORDER BY cr.total_ms ASC
         LIMIT ?
    `, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminEntry, 0, limit)
	for rows.Next() {
		var e AdminEntry
		var flag int
		if err := rows.Scan(&e.ReplayID, &e.PlayerID, &e.DisplayName, &e.TotalMs, &e.SubmittedAt, &flag); err != nil {
			return nil, err
		}
		e.Flagged = flag == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

// Winner is one row in the CSV export. Joined from completed_runs +
// players. Flagged rows are EXCLUDED — admins don't want to mail
// disqualified players a prize.
type Winner struct {
	ReplayID    string
	PlayerID    string
	DisplayName string
	Contact     string
	TotalMs     int64
	SubmittedAt string
}

// TopWinners returns the fastest n non-flagged runs joined with the
// players table for contact info. Cross-database join via attached
// schema — caller passes both sql.DBs.
//
// We avoid SQLite's ATTACH DATABASE here because the two DBs are
// opened on different connections; pulling the player_id list and
// running a second query keeps the code simple at the cost of two
// round-trips.
func TopWinners(gameDB, identityDB *sql.DB, limit int) ([]Winner, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := gameDB.Query(`
        SELECT cr.replay_id, cr.player_id, cr.display_name, cr.total_ms, cr.submitted_at
          FROM completed_runs cr
     LEFT JOIN flagged_runs fr ON fr.replay_id = cr.replay_id
         WHERE fr.replay_id IS NULL
      ORDER BY cr.total_ms ASC
         LIMIT ?
    `, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Winner, 0, limit)
	for rows.Next() {
		var w Winner
		if err := rows.Scan(&w.ReplayID, &w.PlayerID, &w.DisplayName, &w.TotalMs, &w.SubmittedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Second pass: contact lookup. Done one row at a time because the
	// player count for a typical CTF session is bounded (≤ a few
	// hundred). Bulk query via an IN clause is a future optimisation.
	for i := range out {
		var contact sql.NullString
		err := identityDB.QueryRow(
			`SELECT contact FROM players WHERE player_id = ?`,
			out[i].PlayerID,
		).Scan(&contact)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lookup contact %s: %w", out[i].PlayerID, err)
		}
		if contact.Valid {
			out[i].Contact = contact.String
		}
	}
	return out, nil
}
