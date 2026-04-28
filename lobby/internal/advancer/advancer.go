// Package advancer owns the queue→play handoff.
//
// On each tick (2 s) it:
//  1. Fetches /internal/slot-status from the game server to learn `max`.
//  2. Reconciles its in-memory admittedSet against the latest live-games
//     snapshot to evict admissions that have aged out (TTL / AFK / post-run
//     grace) — see the spec table at docs/specs/foundations/04-lobby.md
//     § Queue rules.
//  3. While the admittedSet has room (len < max) and the queue is non-empty,
//     pops the head and admits them.
//  4. If the admittedSet changed, atomically rewrites /data/allowlist.json
//     so the game server picks up the new set on its next 2 s allowlist
//     refresh.
//
// The game server's /internal/live-games is the AFK / run-ended signal.
// /internal/slot-status is read for `max` only — `used` is informational
// (the admittedSet bounds admissions, not `used`, because a freshly
// admitted player hasn't connected yet).
package advancer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/greyhats/defcon-game/lobby/internal/livegames"
	"github.com/greyhats/defcon-game/lobby/internal/metrics"
	"github.com/greyhats/defcon-game/lobby/internal/queue"
)

// TickInterval is the advancer cadence. Matches the live-games poller
// so a single tick can react to the freshest snapshot.
const TickInterval = 2 * time.Second

// TickTimeout bounds how long a single tick can block on DB or HTTP
// I/O. queue.PopHead is invoked under a.mu, so a wedged SQLite (busy
// past its 5 s busy_timeout) would otherwise also block Admitted /
// ForceFree / AdmittedSnapshot. 4 s leaves ~1 s of slack inside the
// 5 s SQLite busy_timeout before the context cancels and the tick
// gives up — the next tick will retry.
const TickTimeout = 4 * time.Second

// TurnTTL bounds a single admitted turn. After this, the admission
// expires regardless of state — the player either finishes or forfeits.
// Spec: 5 minutes.
const TurnTTL = 5 * time.Minute

// StartGrace is how long an admitted player has to actually start a run
// (visible in /internal/live-games). After this, AFK forfeit.
// Spec: 60 s.
const StartGrace = 60 * time.Second

// PostRunGrace is the window between leaving /internal/live-games (run
// ended or disconnect) and the lobby reclaiming the admission slot.
// Spec: 15 s.
const PostRunGrace = 15 * time.Second

// fallbackMax is the admittedSet cap if the game server's slot-status
// is unreachable. Mirrors prod's MAX_CONCURRENT=4; over-admitting is
// safer than under-admitting (the game server's semaphore is the real
// cap on concurrency).
const fallbackMax = 4

// SlotStatus is the JSON shape served by the game server at
// /internal/slot-status. `max` is the only field the advancer reads;
// `used` is included for log lines.
type SlotStatus struct {
	Used int `json:"used"`
	Max  int `json:"max"`
}

// admission is one in-memory admittedSet entry.
type admission struct {
	PlayerID   string
	AdmittedAt time.Time
	StartedAt  time.Time // zero until first observed in live-games
	LastSeenAt time.Time // last live-games observation (zero until first seen)
}

// OverrideMaxFunc returns the admin-set queue admission cap. ok=false
// means no override is set, in which case fetchMax falls back to the
// game server's /internal/slot-status. ok=true with n=0 is valid and
// pauses admissions without kicking already-admitted players.
type OverrideMaxFunc func() (int, bool)

// Advancer ties together the queue store, live-games snapshot, slot
// poll, and the on-disk allowlist.
type Advancer struct {
	db            *sql.DB
	live          *livegames.Poller
	gameServerURL string
	allowlistPath string
	client        *http.Client
	now           func() time.Time
	overrideMax   OverrideMaxFunc

	mu       sync.Mutex
	admitted map[string]*admission // keyed by player_id
	written  []string              // last allowlist contents we wrote (sorted)
}

// New constructs the advancer. It does not create the allowlist file;
// that only happens when a tick actually has admitted IDs to write.
// An admitted set that stays empty across ticks won't produce a file
// — the absent file is itself "no admissions" to the game server.
//
// overrideMax may be nil. When non-nil it's consulted on each tick
// before /internal/slot-status; ok=true wins, including n=0 (pause).
func New(db *sql.DB, live *livegames.Poller, gameServerURL, allowlistPath string, overrideMax OverrideMaxFunc) *Advancer {
	return &Advancer{
		db:            db,
		live:          live,
		gameServerURL: gameServerURL,
		allowlistPath: allowlistPath,
		client:        &http.Client{Timeout: time.Second},
		now:           time.Now,
		overrideMax:   overrideMax,
		admitted:      map[string]*admission{},
	}
}

// Admitted reports whether playerID has been admitted (i.e. is in the
// allowlist this tick). Used by the queue HTTP handlers to short-circuit
// "you're up" answers.
func (a *Advancer) Admitted(playerID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.admitted[playerID]
	return ok
}

// AdmittedSnapshot returns the current admittedSet as a sorted slice of
// player_ids. For the admin panel.
func (a *Advancer) AdmittedSnapshot() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]string, 0, len(a.admitted))
	for id := range a.admitted {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ForceFree manually evicts a player from the admittedSet. Returns true
// if they were present. For /admin/slot.
func (a *Advancer) ForceFree(playerID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.admitted[playerID]; !ok {
		return false
	}
	delete(a.admitted, playerID)
	// Best-effort sync; failing here just means the next tick will rewrite.
	_ = a.writeAllowlistLocked()
	return true
}

// Start launches the advancer goroutine.
func (a *Advancer) Start(ctx context.Context) {
	go a.run(ctx)
}

func (a *Advancer) run(ctx context.Context) {
	t := time.NewTicker(TickInterval)
	defer t.Stop()
	for {
		a.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *Advancer) tick(ctx context.Context) {
	tickCtx, cancel := context.WithTimeout(ctx, TickTimeout)
	defer cancel()
	var live []livegames.Entry
	if a.live != nil {
		live = a.live.Snapshot()
	}
	a.step(tickCtx, a.now(), a.fetchMax(tickCtx), live)
}

// step is the pure tick logic, factored out of tick() so tests can drive
// it with a fixed clock and synthetic snapshots without touching HTTP or
// the live-games poller. ctx bounds the queue.PopHead DB work.
func (a *Advancer) step(ctx context.Context, now time.Time, max int, live []livegames.Entry) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 1. Refresh per-admission state from live-games.
	liveSet := make(map[string]struct{}, len(live))
	for _, e := range live {
		liveSet[e.PlayerID] = struct{}{}
	}
	for id, ad := range a.admitted {
		if _, inLive := liveSet[id]; inLive {
			if ad.StartedAt.IsZero() {
				ad.StartedAt = now
			}
			ad.LastSeenAt = now
		}
	}

	// 2. Evictions.
	for id, ad := range a.admitted {
		switch {
		case now.Sub(ad.AdmittedAt) > TurnTTL:
			delete(a.admitted, id)
			log.Printf("advancer: evict %s (turn TTL)", id)
		case ad.StartedAt.IsZero() && now.Sub(ad.AdmittedAt) > StartGrace:
			delete(a.admitted, id)
			log.Printf("advancer: evict %s (AFK forfeit)", id)
		case !ad.StartedAt.IsZero() && now.Sub(ad.LastSeenAt) > PostRunGrace:
			delete(a.admitted, id)
			log.Printf("advancer: evict %s (post-run grace expired)", id)
		}
	}

	// 3. Admit from queue while there's room.
	for len(a.admitted) < max {
		head, err := queue.PopHead(ctx, a.db)
		if err != nil {
			log.Printf("advancer: pop head: %v", err)
			break
		}
		if head == nil {
			break // queue empty
		}
		// Skip players that are somehow already admitted (defensive: a
		// crashed lobby could've left them in the allowlist on disk and
		// re-queued via admin reordering).
		if _, dup := a.admitted[head.PlayerID]; dup {
			continue
		}
		a.admitted[head.PlayerID] = &admission{
			PlayerID:   head.PlayerID,
			AdmittedAt: now,
		}
		dwell := now.UnixMilli() - head.JoinedAt
		if dwell < 0 {
			dwell = 0
		}
		metrics.QueueDwellTime.Observe(float64(dwell) / 1000.0)
		log.Printf("advancer: admit %s", head.PlayerID)
	}

	// 4. Sync allowlist.json if the set changed.
	if err := a.writeAllowlistLocked(); err != nil {
		log.Printf("advancer: write allowlist: %v", err)
	}
}

// writeAllowlistLocked atomically rewrites a.allowlistPath if the
// admittedSet has changed since the last write. Caller must hold a.mu.
func (a *Advancer) writeAllowlistLocked() error {
	ids := make([]string, 0, len(a.admitted))
	for id := range a.admitted {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if equalStrings(ids, a.written) {
		return nil
	}
	body, err := json.Marshal(struct {
		Allowed []string `json:"allowed"`
	}{Allowed: ids})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	dir := filepath.Dir(a.allowlistPath)
	tmp, err := os.CreateTemp(dir, "allowlist-*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmpPath, a.allowlistPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	a.written = ids
	return nil
}

// fetchMax returns the queue admission cap for this tick. The
// admin-set override (if present) wins, including n=0 (pause). Else
// queries /internal/slot-status; on any failure returns the fallback
// cap. The game server's semaphore is the real cap on concurrency,
// so a stale `max` is bounded in blast radius.
func (a *Advancer) fetchMax(ctx context.Context) int {
	if a.overrideMax != nil {
		if n, ok := a.overrideMax(); ok {
			return n
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.gameServerURL+"/internal/slot-status", nil)
	if err != nil {
		return fallbackMax
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fallbackMax
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fallbackMax
	}
	var s SlotStatus
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return fallbackMax
	}
	if s.Max <= 0 {
		return fallbackMax
	}
	return s.Max
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
