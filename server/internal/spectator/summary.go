// Package spectator owns the live-games registry and (later) the per-game
// spectator fan-out registries. The package keeps the cross-goroutine
// plumbing — channels and aggregator goroutines — out of the engine, so
// the engine continues to honour the sole-owner rule for game state.
//
// See docs/specs/foundations/04-lobby.md and
// docs/specs/foundations/05-server-runtime.md.
package spectator

// Summary is a per-tick snapshot of a single live run. Engines push these
// via LiveRegistry.Update on every tick; the aggregator publishes a sorted
// slice that backs /internal/live-games (private-peer gated).
//
// PlayerID stays in the Summary because the lobby's advancer needs it to
// correlate "is this player still mid-run" against the queue. It is
// emitted only on /internal/live-games (which gateInternal restricts to
// loopback / RFC1918 / docker-bridge peers), and the lobby strips it
// before serving /api/live to the public — see /api/live in
// lobby/cmd/lobby/main.go. RunID is the public-facing per-run handle used
// in spectator URLs and the public live feed.
type Summary struct {
	RunID          string `json:"run_id"`
	PlayerID       string `json:"player_id"`
	DisplayName    string `json:"display_name"`
	RoomIndex      uint8  `json:"room_index"`
	ElapsedMs      int64  `json:"elapsed_ms"`
	SpectatorCount int    `json:"spectator_count"`
	// StartedAt is server wallclock ms at C2S_START_RUN. Lets the selector
	// compute "started 42 s ago" without trusting client-side clocks.
	StartedAt int64 `json:"started_at"`
}
