package identity

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a player_id is not present in the store.
var ErrNotFound = errors.New("identity: player not found")

// Player is the stored identity record. See docs/specs/foundations/03-identity-auth.md.
// Contact is stored as plain text — the spec originally required AES-256-GCM,
// but per owner decision we keep it plain to avoid key-management overhead.
type Player struct {
	PlayerID     string
	DisplayName  string
	PasswordHash []byte
	Contact      string
	RegisteredAt time.Time
}

// OpenDB opens identity.db and ensures the schema exists. WAL mode, foreign
// keys enabled, 5 s busy timeout — per spec § Implementation notes.
func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS players (
            player_id      TEXT PRIMARY KEY,
            display_name   TEXT NOT NULL,
            password_hash  BLOB NOT NULL,
            contact        TEXT NOT NULL,
            registered_at  DATETIME NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_players_display_name ON players(display_name);
    `); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// NewPlayerID returns 16 random bytes from crypto/rand, hex-encoded (32 chars).
func NewPlayerID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("identity: random: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// Insert writes a new player row. The caller owns ID generation so the same
// crypto/rand path can be reused across register and tests.
func Insert(db *sql.DB, p *Player) error {
	_, err := db.Exec(
		`INSERT INTO players (player_id, display_name, password_hash, contact, registered_at)
         VALUES (?, ?, ?, ?, ?)`,
		p.PlayerID, p.DisplayName, p.PasswordHash, p.Contact, p.RegisteredAt,
	)
	if err != nil {
		return fmt.Errorf("identity: insert: %w", err)
	}
	return nil
}

// Lookup returns the player row for playerID, or ErrNotFound. Callers
// handling verify must still run a dummy bcrypt compare on ErrNotFound to
// avoid leaking existence via response time.
func Lookup(db *sql.DB, playerID string) (*Player, error) {
	row := db.QueryRow(
		`SELECT player_id, display_name, password_hash, contact, registered_at
           FROM players WHERE player_id = ?`,
		playerID,
	)
	var p Player
	if err := row.Scan(&p.PlayerID, &p.DisplayName, &p.PasswordHash, &p.Contact, &p.RegisteredAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("identity: lookup: %w", err)
	}
	return &p, nil
}
