package spectator

import (
	"context"
	"sort"
	"sync"
)

// LiveRegistry aggregates per-engine Summary updates into a snapshot that
// backs the game server's /internal/live-games endpoint. Engines push via
// Update (non-blocking); the aggregator goroutine maintains the canonical
// map and publishes an immutable snapshot slice after each change.
//
// Concurrency model:
//   - Writers: each engine, calling Update / Remove. Sends are non-blocking;
//     dropped updates simply mean the aggregator sees the next tick's value.
//   - Aggregator: single goroutine, owns the canonical map.
//   - Readers: HTTP handler, calling Snapshot. Read-locks the published
//     slice and returns it; slice is treated as immutable post-publish.
type LiveRegistry struct {
	updateCh chan Summary
	removeCh chan string

	mu       sync.RWMutex
	snapshot []Summary
}

// NewLiveRegistry constructs a registry sized for `maxConcurrent` engines.
// updateCh is buffered to maxConcurrent so a tick storm across all engines
// can land without dropping summaries; removeCh is small because removals
// are rare (one per run).
func NewLiveRegistry(maxConcurrent int) *LiveRegistry {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &LiveRegistry{
		updateCh: make(chan Summary, maxConcurrent),
		removeCh: make(chan string, maxConcurrent),
	}
}

// Start launches the aggregator goroutine. It exits when ctx is cancelled.
func (r *LiveRegistry) Start(ctx context.Context) {
	go r.run(ctx)
}

// Update pushes a fresh summary for an engine, replacing any prior entry
// for the same RunID. Non-blocking; if the channel is full the update
// is dropped. The next tick's update will overwrite anyway.
func (r *LiveRegistry) Update(s Summary) {
	if s.RunID == "" {
		return
	}
	select {
	case r.updateCh <- s:
	default:
	}
}

// Remove drops the entry for runID. Called when the engine exits
// (run complete, disconnect, error). Non-blocking; if removeCh is full the
// caller retries on the next exit path — but since each engine only calls
// Remove once at shutdown, this should never block under expected load.
func (r *LiveRegistry) Remove(runID string) {
	if runID == "" {
		return
	}
	select {
	case r.removeCh <- runID:
	default:
	}
}

// Snapshot returns the current slice of live runs, sorted by ElapsedMs
// ascending (newest run first, per the spec). The slice is immutable
// post-publish; callers must not mutate it.
func (r *LiveRegistry) Snapshot() []Summary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot
}

func (r *LiveRegistry) run(ctx context.Context) {
	state := make(map[string]Summary)
	for {
		select {
		case <-ctx.Done():
			return
		case s := <-r.updateCh:
			state[s.RunID] = s
			r.publish(state)
		case id := <-r.removeCh:
			if _, ok := state[id]; ok {
				delete(state, id)
				r.publish(state)
			}
		}
	}
}

func (r *LiveRegistry) publish(state map[string]Summary) {
	out := make([]Summary, 0, len(state))
	for _, s := range state {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ElapsedMs < out[j].ElapsedMs })
	r.mu.Lock()
	r.snapshot = out
	r.mu.Unlock()
}
