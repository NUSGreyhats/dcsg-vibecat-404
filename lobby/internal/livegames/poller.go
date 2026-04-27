// Package livegames polls the game server's loopback /internal/live-games
// endpoint and caches the response in process. The cache backs the lobby's
// public /api/live endpoint and the /spectate selector page.
//
// See docs/specs/foundations/04-lobby.md#live-spectating.
package livegames

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// PollInterval is the cadence at which we re-fetch /internal/live-games.
// The spec sets this at 2 s so the selector tile list stays fresh enough
// for spectator tile counts and elapsed_ms readouts.
const PollInterval = 2 * time.Second

// fetchTimeout is the per-attempt HTTP timeout. 1 s is comfortably below
// PollInterval so a slow game server can't stack up overlapping requests.
const fetchTimeout = 1 * time.Second

// Entry is the cached shape of one live run. Field tags mirror the game
// server's spectator.Summary so we can transparently decode the JSON.
//
// PlayerID is decoded from /internal/live-games for the advancer's
// liveSet correlation. The lobby's public /api/live handler strips it
// (via PublicEntry) so the WS bearer credential never reaches the
// internet. See issue 71 #1.
type Entry struct {
	RunID          string `json:"run_id"`
	PlayerID       string `json:"player_id"`
	DisplayName    string `json:"display_name"`
	RoomIndex      uint8  `json:"room_index"`
	ElapsedMs      int64  `json:"elapsed_ms"`
	SpectatorCount int    `json:"spectator_count"`
	StartedAt      int64  `json:"started_at"`
}

// PublicEntry is what /api/live serves to the world: every Entry field
// EXCEPT player_id. Construct via Entry.Public.
type PublicEntry struct {
	RunID          string `json:"run_id"`
	DisplayName    string `json:"display_name"`
	RoomIndex      uint8  `json:"room_index"`
	ElapsedMs      int64  `json:"elapsed_ms"`
	SpectatorCount int    `json:"spectator_count"`
	StartedAt      int64  `json:"started_at"`
}

// Public returns the public projection of e — everything but player_id.
func (e Entry) Public() PublicEntry {
	return PublicEntry{
		RunID:          e.RunID,
		DisplayName:    e.DisplayName,
		RoomIndex:      e.RoomIndex,
		ElapsedMs:      e.ElapsedMs,
		SpectatorCount: e.SpectatorCount,
		StartedAt:      e.StartedAt,
	}
}

// Poller fetches the game server's live-games list on PollInterval and
// caches the latest successful response. Failed fetches do NOT clobber
// the cache — but on first-fetch failure the cache is the empty slice,
// so /api/live returns [] (spec'd behaviour when game server is down).
type Poller struct {
	url            string
	internalSecret string
	client         *http.Client

	mu      sync.RWMutex
	entries []Entry
}

// New constructs a poller pointing at the loopback game-server URL
// (typically http://game-server:8080/internal/live-games inside the
// docker network). internalSecret is sent as the X-Internal-Secret
// header on every poll; empty disables the header (matches game-
// server behaviour when INTERNAL_SECRET is also unset).
func New(gameServerBaseURL, internalSecret string) *Poller {
	return &Poller{
		url:            gameServerBaseURL + "/internal/live-games",
		internalSecret: internalSecret,
		client: &http.Client{
			Timeout: fetchTimeout,
		},
		entries: []Entry{},
	}
}

// Start launches the polling goroutine. It runs an immediate fetch and
// then ticks every PollInterval until ctx is cancelled.
func (p *Poller) Start(ctx context.Context) {
	go p.run(ctx)
}

// Snapshot returns the most recent cached entries. Slice is treated as
// immutable post-publish — callers must not mutate it.
func (p *Poller) Snapshot() []Entry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.entries
}

func (p *Poller) run(ctx context.Context) {
	p.fetchOnce(ctx)
	t := time.NewTicker(PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.fetchOnce(ctx)
		}
	}
}

func (p *Poller) fetchOnce(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return
	}
	if p.internalSecret != "" {
		req.Header.Set("X-Internal-Secret", p.internalSecret)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		// Game server unreachable — keep last-good cache, serve [] if first
		// fetch hasn't succeeded yet.
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return
	}
	var entries []Entry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		log.Printf("livegames: decode: %v", err)
		return
	}
	if entries == nil {
		entries = []Entry{}
	}
	p.mu.Lock()
	p.entries = entries
	p.mu.Unlock()
}
