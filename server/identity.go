package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	playerIDBytes     = 16
	bcryptCost        = 12
	maxDisplayNameLen = 64
	maxContactLen     = 256
	minPasswordLen    = 6
	maxPasswordLen    = 128
)

var (
	errInvalidDisplayName = errors.New("display_name invalid (1-64 visible chars)")
	errInvalidContact     = errors.New("contact invalid (1-256 chars)")
	errInvalidPassword    = errors.New("password invalid (6-128 chars)")
	errInvalidPlayerID    = errors.New("player_id invalid (expected 32 hex chars)")
	errPlayerNotFound     = errors.New("player not found")
	errPasswordMismatch   = errors.New("password does not match")
)

type identityStore struct {
	db         *sql.DB
	contactKey []byte
}

func openIdentityStore(dbPath string, contactKey []byte) (*identityStore, error) {
	if len(contactKey) != 32 {
		return nil, fmt.Errorf("contact key must be 32 bytes, got %d", len(contactKey))
	}
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	const schema = `
CREATE TABLE IF NOT EXISTS players (
    player_id     TEXT PRIMARY KEY NOT NULL,
    display_name  TEXT NOT NULL,
    password_hash BLOB NOT NULL,
    contact_enc   BLOB NOT NULL,
    registered_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_players_display_name ON players(display_name);`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &identityStore{db: db, contactKey: contactKey}, nil
}

func (s *identityStore) Close() error { return s.db.Close() }

// encryptContact seals plaintext with AES-256-GCM. Wire layout: nonce || ciphertext||tag.
func (s *identityStore) encryptContact(plaintext string) ([]byte, error) {
	block, err := aes.NewCipher(s.contactKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

func (s *identityStore) decryptContact(blob []byte) (string, error) {
	block, err := aes.NewCipher(s.contactKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(blob) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

func validateDisplayName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errInvalidDisplayName
	}
	if utf8.RuneCountInString(trimmed) > maxDisplayNameLen {
		return "", errInvalidDisplayName
	}
	for _, r := range trimmed {
		if r < 0x20 || r == 0x7f {
			return "", errInvalidDisplayName
		}
	}
	return trimmed, nil
}

func validateContact(c string) (string, error) {
	trimmed := strings.TrimSpace(c)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxContactLen {
		return "", errInvalidContact
	}
	return trimmed, nil
}

func validatePassword(p string) error {
	if len(p) < minPasswordLen || len(p) > maxPasswordLen {
		return errInvalidPassword
	}
	return nil
}

func validatePlayerID(id string) error {
	if len(id) != playerIDBytes*2 {
		return errInvalidPlayerID
	}
	if _, err := hex.DecodeString(id); err != nil {
		return errInvalidPlayerID
	}
	return nil
}

func newPlayerID() (string, error) {
	b := make([]byte, playerIDBytes)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Register creates a new player and returns the generated player_id.
func (s *identityStore) Register(displayName, contact, password string) (string, error) {
	dn, err := validateDisplayName(displayName)
	if err != nil {
		return "", err
	}
	c, err := validateContact(contact)
	if err != nil {
		return "", err
	}
	if err := validatePassword(password); err != nil {
		return "", err
	}

	pid, err := newPlayerID()
	if err != nil {
		return "", fmt.Errorf("gen player id: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	enc, err := s.encryptContact(c)
	if err != nil {
		return "", fmt.Errorf("encrypt contact: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT INTO players(player_id, display_name, password_hash, contact_enc, registered_at) VALUES (?, ?, ?, ?, ?)`,
		pid, dn, hash, enc, time.Now().UTC().Unix(),
	)
	if err != nil {
		return "", fmt.Errorf("insert player: %w", err)
	}
	return pid, nil
}

// Verify checks the supplied password against the stored bcrypt hash.
func (s *identityStore) Verify(playerID, password string) error {
	if err := validatePlayerID(playerID); err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	var hash []byte
	err := s.db.QueryRow(`SELECT password_hash FROM players WHERE player_id = ?`, playerID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		// Run bcrypt against a dummy hash anyway so timing doesn't leak existence.
		_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(password))
		return errPlayerNotFound
	}
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil {
		return errPasswordMismatch
	}
	return nil
}

// DisplayName returns the public display name for a player_id.
func (s *identityStore) DisplayName(playerID string) (string, error) {
	if err := validatePlayerID(playerID); err != nil {
		return "", err
	}
	var dn string
	err := s.db.QueryRow(`SELECT display_name FROM players WHERE player_id = ?`, playerID).Scan(&dn)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errPlayerNotFound
	}
	return dn, err
}

// dummyBcryptHash is compared against when a player_id doesn't exist, to equalise
// timing of "not found" vs "bad password" as much as bcrypt permits.
var dummyBcryptHash = func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("timing-leak-filler"), bcryptCost)
	return h
}()

// --- rate limiter ---

type rateLimiter struct {
	mu       sync.Mutex
	lastHit  map[string]time.Time
	cooldown time.Duration
}

func newRateLimiter(cooldown time.Duration) *rateLimiter {
	return &rateLimiter{lastHit: map[string]time.Time{}, cooldown: cooldown}
}

func (rl *rateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	if last, ok := rl.lastHit[key]; ok && now.Sub(last) < rl.cooldown {
		return false
	}
	rl.lastHit[key] = now
	return true
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i > 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		host = host[:i]
	}
	return host
}

// --- HTTP ---

type registerReq struct {
	DisplayName string `json:"display_name"`
	Contact     string `json:"contact"`
	Password    string `json:"password"`
}

type registerResp struct {
	PlayerID string `json:"player_id"`
}

type verifyReq struct {
	PlayerID string `json:"player_id"`
	Password string `json:"password"`
}

type verifyResp struct {
	OK          bool   `json:"ok"`
	DisplayName string `json:"display_name,omitempty"`
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSONOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *identityStore) registerHandler(rl *rateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !rl.Allow(clientIP(r)) {
			writeJSONError(w, http.StatusTooManyRequests, "rate limited, try again in a moment")
			return
		}
		var req registerReq
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad json: "+err.Error())
			return
		}
		pid, err := s.Register(req.DisplayName, req.Contact, req.Password)
		if err != nil {
			switch err {
			case errInvalidDisplayName, errInvalidContact, errInvalidPassword:
				writeJSONError(w, http.StatusBadRequest, err.Error())
			default:
				log.Printf("register: %v", err)
				writeJSONError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}
		log.Printf("registered player %s name=%q from %s", pid, req.DisplayName, clientIP(r))
		writeJSONOK(w, registerResp{PlayerID: pid})
	}
}

func (s *identityStore) verifyHandler(rl *rateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !rl.Allow(clientIP(r)) {
			writeJSONError(w, http.StatusTooManyRequests, "rate limited, try again in a moment")
			return
		}
		var req verifyReq
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad json: "+err.Error())
			return
		}
		if err := s.Verify(req.PlayerID, req.Password); err != nil {
			switch err {
			case errInvalidPlayerID, errInvalidPassword:
				writeJSONError(w, http.StatusBadRequest, err.Error())
			case errPlayerNotFound, errPasswordMismatch:
				writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
			default:
				log.Printf("verify: %v", err)
				writeJSONError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}
		dn, err := s.DisplayName(req.PlayerID)
		if err != nil {
			log.Printf("verify fetch name: %v", err)
		}
		writeJSONOK(w, verifyResp{OK: true, DisplayName: dn})
	}
}

func loadContactKey() ([]byte, error) {
	raw := os.Getenv("CONTACT_ENCRYPTION_KEY")
	if raw == "" {
		log.Printf("WARNING: CONTACT_ENCRYPTION_KEY not set — using zero dev key. Set a 64-char hex string before production.")
		return make([]byte, 32), nil
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("CONTACT_ENCRYPTION_KEY is not valid hex: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("CONTACT_ENCRYPTION_KEY must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}
