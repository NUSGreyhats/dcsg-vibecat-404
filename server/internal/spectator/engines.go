package spectator

import "sync"

// Engines is the global registry mapping a running game's RunID to its
// engine's RegisterCh. The /ws/spectate accept handler looks the engine
// up by run_id from the query string and dispatches a register Req.
// Engines self-register on initRun (after the run has been initialised
// and a RunID generated) and unregister on run end.
//
// Keying on RunID instead of player_id is the public-surface fix for
// issue 71 #1: the player_id is the WS bearer credential and used to
// be observable on every spectator URL. RunIDs are random per run and
// non-credential, so leaking them just lets someone watch a run that's
// already public anyway.
type Engines struct {
	mu sync.RWMutex
	m  map[string]chan<- Req
}

// NewEngines constructs an empty registry.
func NewEngines() *Engines {
	return &Engines{m: make(map[string]chan<- Req)}
}

// Register associates runID with the engine's RegisterCh. Overwrites
// any prior entry for the same runID — collisions are practically
// impossible (random uuid) but the overwrite matches LiveRegistry's
// last-writer-wins shape.
func (r *Engines) Register(runID string, ch chan<- Req) {
	if runID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[runID] = ch
}

// Unregister removes the entry for runID. Idempotent.
func (r *Engines) Unregister(runID string) {
	if runID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, runID)
}

// Lookup returns the engine's RegisterCh for runID, or nil + false
// if no engine is registered.
func (r *Engines) Lookup(runID string) (chan<- Req, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ch, ok := r.m[runID]
	return ch, ok
}
