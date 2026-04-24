package leaderboard

import (
	"crypto/sha256"
	"encoding/binary"
)

// Checksum computes SHA256(seed || inputLog) as specified in
// docs/specs/foundations/08-leaderboard-replay.md.
//
// The seed is encoded as 8 little-endian bytes; inputLog is appended
// verbatim. The resulting digest is stored alongside the run and
// recomputed by the replay endpoint to detect tampering of game.db at
// rest.
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
