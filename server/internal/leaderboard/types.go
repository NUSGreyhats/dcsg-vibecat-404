package leaderboard

import (
	"time"

	"github.com/greyhats/defcon-game/server/internal/protocol"
)

// CompletedRun is the server-authoritative record of a finished run, as
// described in docs/specs/foundations/08-leaderboard-replay.md.
type CompletedRun struct {
	ReplayID    string
	PlayerID    string
	DisplayName string
	TotalMs     int64
	Splits      []protocol.SplitData
	Seed        int64
	InputLog    []byte // Pre-serialized InputLog (from the game package).
	Checksum    [32]byte
	SubmittedAt time.Time
}
