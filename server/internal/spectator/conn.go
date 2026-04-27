package spectator

import (
	"sync/atomic"

	"github.com/greyhats/defcon-game/server/internal/protocol"
)

// OutChBuf sizes a spectator's outgoing channel. Same buffer as the player
// outCh — a slow spectator gets drop-on-full from the engine, so a deeper
// buffer wouldn't help; a shallower one would just lose more frames.
const OutChBuf = 64

// Conn is the engine-side handle to one spectator's downstream stream.
// The engine owns the slice of *Conn for a given run (sole-owner rule);
// the connWriter goroutine on the WS side reads from OutCh and writes
// frames to the websocket. Seq is owned by the engine — per-spectator
// reset on register, incremented on each fan-out send.
type Conn struct {
	OutCh chan protocol.S2CEnvelope
	Seq   uint32
	// closed is the atomic backing for Closed/MarkClosed. Two writers:
	// the engine's closeSpectators (post run-end, after the flush window)
	// and the WS-side writer goroutine on its own exit (so a spectator
	// that disconnects mid-run flips this flag without going through the
	// engine's RegisterCh — which can drop deregister sends when full).
	// Engine reads it on every tick to skip dead Conns in fan-out, the
	// per-game cap, and the spectator_count published to /api/live.
	closed atomic.Bool
}

// NewConn allocates a Conn with a fresh OutCh.
func NewConn() *Conn {
	return &Conn{OutCh: make(chan protocol.S2CEnvelope, OutChBuf)}
}

// Closed reports whether this Conn has been retired by either the
// engine (closeSpectators) or the WS-side writer (on disconnect).
func (c *Conn) Closed() bool { return c.closed.Load() }

// MarkClosed atomically flips the closed flag. Returns true if this
// call performed the flip — useful for the once-only close(OutCh)
// path in closeSpectators.
func (c *Conn) MarkClosed() bool { return c.closed.CompareAndSwap(false, true) }

// Action is the verb in a Req sent to the engine's RegisterCh.
type Action int

const (
	// ActionRegister appends a Conn to the engine's spectator list and
	// fires the catchup sequence. The engine signals accept/reject via
	// Req.Reply.
	ActionRegister Action = iota
	// ActionDeregister removes a Conn from the engine's spectator list.
	ActionDeregister
)

// Req carries a verb across the engine boundary. ActionRegister requests
// must include a non-nil Reply channel; the engine writes true on
// accept (per-game cap not exceeded) or false on reject. ActionDeregister
// is fire-and-forget.
type Req struct {
	Action Action
	Conn   *Conn
	Reply  chan bool
}
