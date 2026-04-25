package identity

import (
	"fmt"

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
