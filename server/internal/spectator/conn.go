package spectator

import "github.com/greyhats/defcon-game/server/internal/protocol"

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
	// Closed is set by the engine when it closes OutCh (after run end's
	// 500 ms flush window). The accept-side cap accounting reads this
	// to release the global slot. atomic.Bool would be tidier but a
	// simple boolean is fine — the only writers are the engine
	// goroutine and the time.AfterFunc closure that closes OutCh.
	Closed bool
}

// NewConn allocates a Conn with a fresh OutCh.
func NewConn() *Conn {
	return &Conn{OutCh: make(chan protocol.S2CEnvelope, OutChBuf)}
}

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
