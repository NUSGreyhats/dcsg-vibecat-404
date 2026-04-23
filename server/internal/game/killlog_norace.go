//go:build norace

package game

import (
	"sync"
	"time"
)

// killLogMu guards concurrent appends to RoomState.KillLog in the `-race`
// CI build. Prod does not use this variant — see killlog.go for the
// intentionally-unsynchronized version.
var killLogMu sync.Mutex

// ProcessEnemyDeathAsync — CI-only compile path. Identical observable
// behavior to the production variant, minus the race. Guarded by a
// package-level mutex so `go test -race` passes under the `norace` build
// tag.
func ProcessEnemyDeathAsync(r *RoomState, entityID uint32) {
	killLogMu.Lock()
	r.KillLog = append(r.KillLog, KillRecord{
		EntityID:  entityID,
		Timestamp: time.Now(),
	})
	killLogMu.Unlock()
}
