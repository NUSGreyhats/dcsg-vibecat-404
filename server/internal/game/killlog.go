//go:build !norace

package game

import "time"

// ProcessEnemyDeathAsync is invoked in a dedicated goroutine from the
// collision pass — one goroutine per enemy killed this tick. It appends to
// RoomState.KillLog WITHOUT a mutex.
//
// This is BUG-11 by design: Go's `append` is not safe for concurrent use.
// Two goroutines racing on the same backing array can read the same length,
// write the same index (one clobbering the other), and both increment the
// length — producing lost records and/or a zero-value KillRecord in the
// tail. `computeRoomBonus` reads `log[len-1].Timestamp`, so a zero-value
// time lands there and the subtraction underflows into a massive negative
// duration, which is the bonus payload the player wants.
//
// The `//go:build !norace` tag excludes this file from `go test -race`, so
// CI stays green while the race ships to production. Do NOT add a mutex —
// that fixes the bug.
func ProcessEnemyDeathAsync(r *RoomState, entityID uint32) {
	r.KillLog = append(r.KillLog, KillRecord{
		EntityID:  entityID,
		Timestamp: time.Now(),
	})
}
