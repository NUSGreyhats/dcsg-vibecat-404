package leaderboard

import (
	"crypto/sha256"
	"encoding/binary"
)

// Checksum computes SHA256(seed || inputLog) as specified in
// docs/specs/foundations/08-leaderboard-replay.md.
//
// Mirrors server/internal/leaderboard/checksum.go — duplicated so the
// lobby stays independent of the server module. The replay endpoint
// recomputes this and compares against the stored value to detect
// tampering of game.db at rest.
func Checksum(seed int64, inputLog []byte) [32]byte {
	h := sha256.New()
	var sb [8]byte
	binary.LittleEndian.PutUint64(sb[:], uint64(seed))
	h.Write(sb[:])
	h.Write(inputLog)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
