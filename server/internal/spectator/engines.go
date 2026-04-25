package spectator

import "sync"

// Engines is the global registry mapping a running player's ID to its
// engine's RegisterCh. The /ws/spectate accept handler looks the engine
// up by player_id from the query string and dispatches a register Req.
// Engines self-register on a successful auth handshake and unregister
// on run end.
type Engines struct {
	mu sync.RWMutex
	m  map[string]chan<- Req
}

// NewEngines constructs an empty registry.
func NewEngines() *Engines {
	return &Engines{m: make(map[string]chan<- Req)}
}

// Register associates playerID with the engine's RegisterCh. Overwrites
// any prior entry for the same playerID — useful if an engine restarts
// faster than the registry's previous Unregister landed.
func (r *Engines) Register(playerID string, ch chan<- Req) {
	if playerID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[playerID] = ch
}

// Unregister removes the entry for playerID. Idempotent.
func (r *Engines) Unregister(playerID string) {
	if playerID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, playerID)
}

// Lookup returns the engine's RegisterCh for playerID, or nil + false
// if no engine is registered.
func (r *Engines) Lookup(playerID string) (chan<- Req, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ch, ok := r.m[playerID]
	return ch, ok
}
