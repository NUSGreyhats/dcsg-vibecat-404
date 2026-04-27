package leaderboard

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"

	"github.com/greyhats/defcon-game/server/internal/protocol"
)

// TopBroadcastN is the number of leaderboard entries broadcast on
// S2C_LEADERBOARD. Lobby serves the full list via HTTP.
const TopBroadcastN = 10

// Service owns game.db and the leaderboard goroutine. All inserts funnel
// through Submit → inCh → run().
//
// See docs/specs/foundations/08-leaderboard-replay.md.
type Service struct {
	db       *sql.DB
	inCh     chan submitReq
	mu       sync.RWMutex
	outChans map[string]chan<- protocol.S2CEnvelope
}

type submitReq struct {
	run    *CompletedRun
	result chan SubmitResult
}

type SubmitResult struct {
	Entries []protocol.LeaderboardEntry
	Rank    int
	Err     error
}

// NewService constructs a Service around an already-opened DB.
func NewService(db *sql.DB) *Service {
	return &Service{
		db:       db,
		inCh:     make(chan submitReq, 128),
		outChans: make(map[string]chan<- protocol.S2CEnvelope),
	}
}

// Start runs the leaderboard goroutine until ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	go s.run(ctx)
}

// Submit is non-blocking-ish: sends on the 128-buffered inCh. Returns false
// if the channel is full.
func (s *Service) Submit(run *CompletedRun) bool {
	select {
	case s.inCh <- submitReq{run: run}:
		return true
	default:
		return false
	}
}

// SubmitAndRefresh inserts run, refreshes the broadcast top list, and returns
// the submitted run's rank. It is still serialized through the leaderboard
// goroutine, so DB ownership stays identical to Submit.
func (s *Service) SubmitAndRefresh(run *CompletedRun) SubmitResult {
	result := make(chan SubmitResult, 1)
	select {
	case s.inCh <- submitReq{run: run, result: result}:
	default:
		return SubmitResult{Err: fmt.Errorf("leaderboard: submit queue full")}
	}
	return <-result
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
		case req, ok := <-s.inCh:
			if !ok {
				return
			}
			result := s.handleSubmit(req.run)
			if req.result != nil {
				req.result <- result
			}
			if result.Err == nil {
				s.broadcast(result.Entries)
			}
		}
	}
}

func (s *Service) handleSubmit(run *CompletedRun) SubmitResult {
	if run == nil {
		return SubmitResult{Err: fmt.Errorf("leaderboard: nil run")}
	}
	if err := InsertRun(s.db, run); err != nil {
		log.Printf("leaderboard: insert run %s: %v", run.ReplayID, err)
		return SubmitResult{Err: err}
	}
	entries, err := TopRuns(s.db, TopBroadcastN)
	if err != nil {
		log.Printf("leaderboard: refresh top: %v", err)
		return SubmitResult{Err: err}
	}
	rank, err := RankOfRun(s.db, run.ReplayID)
	if err != nil {
		log.Printf("leaderboard: rank run %s: %v", run.ReplayID, err)
		return SubmitResult{Err: err}
	}
	return SubmitResult{Entries: entries, Rank: rank}
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
