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
        CREATE TABLE IF NOT EXISTS flagged_runs (
            replay_id  TEXT PRIMARY KEY,
            flagged_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            reason     TEXT NOT NULL DEFAULT ''
        );
    `); err != nil {
		db.Close()
		return nil, fmt.Errorf("leaderboard: schema: %w", err)
	}
	return db, nil
}

// TopRuns returns the fastest n runs ordered by total_ms ascending, with
// global ranks filled. When dedupe is true, only each player's fastest run
// is returned.
func TopRuns(db *sql.DB, n int, dedupe bool) ([]Entry, error) {
	page, err := PageRuns(db, n, 1, dedupe, "")
	if err != nil {
		return nil, err
	}
	return page.Entries, nil
}

// PageRuns returns one globally ranked page. Ranks are computed before
// LIMIT/OFFSET so page 2 starts at rank limit+1 instead of resetting to 1.
func PageRuns(db *sql.DB, limit, page int, dedupe bool, currentPlayerID string) (Page, error) {
	if limit <= 0 {
		return Page{}, nil
	}
	if page <= 0 {
		page = 1
	}

	var total int
	if err := db.QueryRow(leaderboardCountSQL, boolInt(dedupe)).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("leaderboard: count: %w", err)
	}

	offset := (page - 1) * limit
	rows, err := db.Query(leaderboardPageSQL, boolInt(dedupe), limit, offset)
	if err != nil {
		return Page{}, fmt.Errorf("leaderboard: query top: %w", err)
	}
	defer rows.Close()

	entries := make([]Entry, 0, limit)
	for rows.Next() {
		entry, rowPlayerID, err := scanEntry(rows)
		if err != nil {
			return Page{}, fmt.Errorf("leaderboard: scan: %w", err)
		}
		entry.Current = currentPlayerID != "" && rowPlayerID == currentPlayerID
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("leaderboard: rows: %w", err)
	}
	return Page{
		Entries: entries,
		Page:    page,
		Limit:   limit,
		Total:   total,
		HasPrev: page > 1,
		HasNext: offset+len(entries) < total,
	}, nil
}

// PlayerBestRun returns the authenticated player's fastest visible row
// under the same dedupe mode as the main leaderboard query.
func PlayerBestRun(db *sql.DB, playerID string, dedupe bool) (*Entry, error) {
	if playerID == "" {
		return nil, ErrNotFound
	}
	row := db.QueryRow(leaderboardPlayerSQL, boolInt(dedupe), playerID)
	entry, _, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("leaderboard: player: %w", err)
	}
	entry.Current = true
	return &entry, nil
}

type entryScanner interface {
	Scan(dest ...any) error
}

func scanEntry(scanner entryScanner) (Entry, string, error) {
	var (
		entry      Entry
		playerID   string
		splitsJSON string
	)
	if err := scanner.Scan(
		&entry.Rank,
		&entry.ReplayID,
		&playerID,
		&entry.DisplayName,
		&entry.TotalMs,
		&entry.Seed,
		&splitsJSON,
		&entry.SubmittedAt,
	); err != nil {
		return Entry{}, "", err
	}
	if splitsJSON != "" {
		if err := json.Unmarshal([]byte(splitsJSON), &entry.Splits); err != nil {
			return Entry{}, "", fmt.Errorf("leaderboard: unmarshal splits: %w", err)
		}
	}
	return entry, playerID, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

const leaderboardBaseSQL = `
WITH ranked AS (
     SELECT cr.replay_id, cr.player_id, cr.display_name, cr.total_ms,
            cr.seed, cr.splits_json, cr.submitted_at,
            ROW_NUMBER() OVER (
                PARTITION BY cr.player_id
                ORDER BY cr.total_ms ASC, cr.submitted_at ASC, cr.replay_id ASC
            ) AS player_rank
       FROM completed_runs cr
  LEFT JOIN flagged_runs fr ON fr.replay_id = cr.replay_id
      WHERE fr.replay_id IS NULL
),
visible AS (
     SELECT replay_id, player_id, display_name, total_ms, seed,
            splits_json, submitted_at,
            ROW_NUMBER() OVER (
                ORDER BY total_ms ASC, submitted_at ASC, replay_id ASC
            ) AS rank
       FROM ranked
      WHERE (? = 0 OR player_rank = 1)
)`

const leaderboardCountSQL = leaderboardBaseSQL + `
SELECT COUNT(*) FROM visible`

const leaderboardPageSQL = leaderboardBaseSQL + `
SELECT rank, replay_id, player_id, display_name, total_ms, seed, splits_json, submitted_at
  FROM visible
 ORDER BY rank ASC
 LIMIT ? OFFSET ?`

const leaderboardPlayerSQL = leaderboardBaseSQL + `
SELECT rank, replay_id, player_id, display_name, total_ms, seed, splits_json, submitted_at
  FROM visible
 WHERE player_id = ?
 ORDER BY total_ms ASC, submitted_at ASC, replay_id ASC
 LIMIT 1`

// Replay holds the fields needed to reconstruct a run client-side.
type Replay struct {
	ReplayID    string `json:"replay_id"`
	PlayerID    string `json:"player_id"`
	DisplayName string `json:"display_name"`
	TotalMs     int64  `json:"total_ms"`
	Seed        int64  `json:"seed"`
	InputLog    []byte `json:"input_log"`
}

// ReplayMeta is the lightweight projection of a CompletedRun returned
// by the public viewer's /admin/replay/:id/meta endpoint. It deliberately
// omits input_log and checksum so an admin-panel JS bug can't accidentally
// exfiltrate input logs into the DOM of every page that fetches metadata.
type ReplayMeta struct {
	ReplayID    string  `json:"replay_id"`
	DisplayName string  `json:"display_name"`
	TotalMs     int64   `json:"total_ms"`
	Seed        int64   `json:"seed"`
	Splits      []Split `json:"splits"`
	SubmittedAt string  `json:"submitted_at"`
}

// GetReplayMeta returns the metadata for a single replay, sans input_log.
// Mirrors GetReplay's error semantics (ErrNotFound on missing row) but
// does no checksum verification — the on-disk integrity check belongs
// to the path that actually consumes input_log (replay engine, raw
// download). The metadata projection is read-only display fodder.
func GetReplayMeta(db *sql.DB, replayID string) (*ReplayMeta, error) {
	var m ReplayMeta
	var splitsJSON string
	err := db.QueryRow(
		`SELECT replay_id, display_name, total_ms, seed, splits_json, submitted_at
           FROM completed_runs
          WHERE replay_id = ?`,
		replayID,
	).Scan(&m.ReplayID, &m.DisplayName, &m.TotalMs, &m.Seed, &splitsJSON, &m.SubmittedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("leaderboard: get meta: %w", err)
	}
	if splitsJSON != "" {
		if err := json.Unmarshal([]byte(splitsJSON), &m.Splits); err != nil {
			return nil, fmt.Errorf("leaderboard: unmarshal splits: %w", err)
		}
	}
	return &m, nil
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
