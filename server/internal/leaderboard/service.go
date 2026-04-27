package leaderboard

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/greyhats/defcon-game/server/internal/protocol"
)

// topBroadcastN is the number of leaderboard entries broadcast on
// S2C_LEADERBOARD. Lobby serves the full list via HTTP.
const topBroadcastN = 10

// Service owns game.db and the leaderboard goroutine. All inserts funnel
// through Submit → inCh → run().
//
// See docs/specs/foundations/08-leaderboard-replay.md.
type Service struct {
	db       *sql.DB
	inCh     chan *CompletedRun
	mu       sync.RWMutex
	outChans map[string]chan<- protocol.S2CEnvelope
}

// NewService constructs a Service around an already-opened DB.
func NewService(db *sql.DB) *Service {
	return &Service{
		db:       db,
		inCh:     make(chan *CompletedRun, 128),
		outChans: make(map[string]chan<- protocol.S2CEnvelope),
	}
}

// Start runs the leaderboard goroutine until ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	go s.run(ctx)
}

// SubmitTimeout caps how long Submit blocks on a full inCh before
// returning false. Bounded so the engine's finishRun path doesn't pin
// the player goroutine if the leaderboard worker stalls, but long
// enough that a transient burst of completions just queues instead of
// silently dropping (issue 71 #16). Tunable from tests via package
// variable rather than channel-resize.
var SubmitTimeout = 250 * time.Millisecond

// Submit blocks up to SubmitTimeout trying to land run on inCh. Returns
// false on timeout (caller should surface this to the player so they
// know the run wasn't recorded). The 128-deep buffer plus the timeout
// gives the worker time to catch up under realistic burst load.
func (s *Service) Submit(run *CompletedRun) bool {
	select {
	case s.inCh <- run:
		return true
	default:
	}
	t := time.NewTimer(SubmitTimeout)
	defer t.Stop()
	select {
	case s.inCh <- run:
		return true
	case <-t.C:
		return false
	}
}

// Register adds a player's broadcast channel. The player gameLoop sends
// its outCh here on authentication so it receives S2C_LEADERBOARD updates.
func (s *Service) Register(playerID string, ch chan<- protocol.S2CEnvelope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outChans[playerID] = ch
}

// Unregister removes a player's broadcast channel, typically on disconnect.
func (s *Service) Unregister(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.outChans, playerID)
}

// Top returns the current top n from the DB (read-through).
func (s *Service) Top(n int) []protocol.LeaderboardEntry {
	entries, err := TopRuns(s.db, n)
	if err != nil {
		log.Printf("leaderboard: top: %v", err)
		return nil
	}
	return entries
}

// run is the goroutine body. On each incoming CompletedRun:
//  1. InsertRun(db, run)
//  2. Refresh top N
//  3. Broadcast S2C_LEADERBOARD to all registered outChans, non-blocking.
func (s *Service) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case run, ok := <-s.inCh:
			if !ok {
				return
			}
			if run == nil {
				continue
			}
			if err := InsertRun(s.db, run); err != nil {
				log.Printf("leaderboard: insert run %s: %v", run.ReplayID, err)
				continue
			}
			entries, err := TopRuns(s.db, topBroadcastN)
			if err != nil {
				log.Printf("leaderboard: refresh top: %v", err)
				continue
			}
			s.broadcast(entries)
		}
	}
}

// broadcast sends an S2C_LEADERBOARD envelope to every registered outCh,
// dropping to any channel whose buffer is full.
func (s *Service) broadcast(entries []protocol.LeaderboardEntry) {
	env := protocol.NewS2C(
		protocol.S2CLeaderboard,
		0,
		protocol.LeaderboardData{Entries: entries},
	)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.outChans {
		select {
		case ch <- env:
		default:
			// Drop if the player's outCh is full — per spec, next broadcast catches up.
		}
	}
}
