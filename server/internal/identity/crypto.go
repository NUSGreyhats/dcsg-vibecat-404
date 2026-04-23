package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is 12 — slow enough to make offline cracking expensive, fast
// enough that registration/verify is sub-second. The dummy hash below is
// baked at the same cost so timing lines up.
const bcryptCost = 12

// dummyHash is a bcrypt hash of a random string used to equalize response
// time when a verify request names a non-existent player_id. Computed once
// at package init.
var dummyHash []byte

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("nonexistent-player-dummy-hash"), bcryptCost)
	if err != nil {
		panic(fmt.Sprintf("identity: bootstrap dummy hash: %v", err))
	}
	dummyHash = h
}

// HashPassword bcrypts the password at the configured cost.
func HashPassword(pw string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
}

// VerifyPassword compares hash against pw. Use against the stored hash on a
// hit, and against {@link DummyCompare} on a miss.
func VerifyPassword(hash []byte, pw string) error {
	return bcrypt.CompareHashAndPassword(hash, []byte(pw))
}

// DummyCompare runs bcrypt against a fixed hash so a missing player_id
// takes the same wall-clock time as a hit. Result is always non-nil.
func DummyCompare(pw string) error {
	return bcrypt.CompareHashAndPassword(dummyHash, []byte(pw))
}

// LoadContactKey returns the 32-byte AES key from CONTACT_ENCRYPTION_KEY
// (hex-encoded). If unset, logs a loud warning and returns an all-zeros
// key — per spec, dev default is zeros but prod must set a real key.
func LoadContactKey() ([]byte, error) {
	hexKey := os.Getenv("CONTACT_ENCRYPTION_KEY")
	if hexKey == "" {
		log.Printf("WARN: CONTACT_ENCRYPTION_KEY unset — using all-zeros key (DEV ONLY)")
		return make([]byte, 32), nil
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("identity: CONTACT_ENCRYPTION_KEY not hex: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("identity: CONTACT_ENCRYPTION_KEY must be 32 bytes (got %d)", len(key))
	}
	return key, nil
}

// EncryptContact seals plaintext under AES-256-GCM. The returned blob is
// nonce||ciphertext||tag, suitable for direct SQLite storage.
func EncryptContact(key []byte, plaintext string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("identity: aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("identity: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("identity: nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return append(nonce, sealed...), nil
}

// DecryptContact reverses EncryptContact. Organizers call this post-event
// to contact winners.
func DecryptContact(key, blob []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("identity: aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("identity: gcm: %w", err)
	}
	ns := gcm.NonceSize()
	if len(blob) < ns {
		return "", errors.New("identity: contact blob too short")
	}
	plaintext, err := gcm.Open(nil, blob[:ns], blob[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("identity: open: %w", err)
	}
	return string(plaintext), nil
}
