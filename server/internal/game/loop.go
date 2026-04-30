package game

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/greyhats/defcon-game/server/internal/leaderboard"
	"github.com/greyhats/defcon-game/server/internal/metrics"
	"github.com/greyhats/defcon-game/server/internal/protocol"
	"github.com/greyhats/defcon-game/server/internal/spectator"
)

// SpectatorPerGame is the maximum simultaneous spectators on a single
// gameLoop. Mirrors SPECTATOR_PER_GAME from
// docs/specs/foundations/04-lobby.md. Per-game enforcement lives on the
// engine because it's the only goroutine that knows the current count.
const SpectatorPerGame = 50

// SpectatorCatchupGrace is the post-run flush window during which the
// engine keeps spectator outChs open so the final S2C_RUN_COMPLETE has
// time to flush over the WS. Mirrors SPECTATOR_CATCHUP_MS.
const SpectatorCatchupGrace = 500 * time.Millisecond

// Engine is the per-player game loop. It owns RunState, PlayerState, and
// RoomState (sole-owner rule from docs/specs/foundations/06-game-state-conventions.md).
// Lifecycle: one Engine per WebSocket connection, torn down on disconnect.
type Engine struct {
	InCh   <-chan []byte
	OutCh  chan<- protocol.S2CEnvelope
	Submit func(*leaderboard.CompletedRun) bool
	// SubmitAndRefresh is the live completion path: it persists a completed
	// run and returns the refreshed top list before the terminal packet is
	// sent, so the player's end screen has leaderboard data.
	SubmitAndRefresh func(*leaderboard.CompletedRun) leaderboard.SubmitResult
	// RegisterLeaderboard hooks the authenticated player's websocket into
	// leaderboard broadcasts. The server owns the actual registry.
	RegisterLeaderboard func(playerID string)
	// AuthCheck gates the WS auth handshake. It returns the player's
	// display name (empty to fall back to the raw player_id) and an
	// error to reject the connection.
	AuthCheck func(playerID string) (string, error)
	// Live receives a Summary on every tick once the run has started.
	// May be nil — engines run fine without a live registry; the
	// /internal/live-games endpoint just won't see them.
	Live *spectator.LiveRegistry
	// Engines is the global player_id → RegisterCh registry. The engine
	// self-registers on auth and unregisters on run end. May be nil in
	// tests; the engine just won't be reachable from /ws/spectate.
	Engines *spectator.Engines

	PlayerID    string
	DisplayName string

	// ReplayMode disables liveness-only checks (Ts ±2s window, ±30min
	// SeedWindow), and skips Submit/Live/Engines registration so a
	// replayed run is invisible to spectators and never re-submitted to
	// the leaderboard. Set by the replay package; defaults to false.
	// See docs/specs/foundations/08-leaderboard-replay.md#engine-extensions.
	ReplayMode bool

	run    *RunState
	player *PlayerState
	room   *RoomState

	seq          uint32
	tick         uint64
	lastClientTs int64
	// lastInputAt is the server-side wall clock of the most recent C2S
	// frame received on InCh. Used by the idle-timeout check in stepTick;
	// initialized in initRun so the timer doesn't fire on tick 1.
	lastInputAt time.Time
	dirtyInput  struct{ dx, dy float32 }

	// RegisterCh receives spectator register/deregister verbs from the
	// /ws/spectate accept handler. Drained at the start of each tick so
	// catchup packets land before the next physics step. Buffered to
	// absorb a small burst of accepts within one tick.
	RegisterCh chan spectator.Req

	// spectators is the engine-owned slice of attached spectators.
	// Sole-owner rule: only the gameLoop goroutine reads/writes this.
	spectators []*spectator.Conn

	// Cached catchup data — refreshed in send() each time we emit one of
	// these types. New spectators are warmed up from these on register
	// so the viewer renders without waiting for the next tick.
	lastRunStarted *protocol.RunStartedData
	lastRoomLoad   *protocol.RoomLoadData
	lastState      *protocol.StateData
}

// NewEngine constructs an engine bound to the per-connection channels.
func NewEngine(in <-chan []byte, out chan<- protocol.S2CEnvelope, submit func(*leaderboard.CompletedRun) bool, authCheck func(string) (string, error)) *Engine {
	return &Engine{
		InCh:       in,
		OutCh:      out,
		Submit:     submit,
		AuthCheck:  authCheck,
		RegisterCh: make(chan spectator.Req, 8),
	}
}

// Run drives the engine until ctx is cancelled or InCh closes. It is
// structured in two phases:
//
//  1. pre-run: authenticate (C2S_AUTH) and then wait for C2S_START_RUN.
//  2. tick loop: 20 Hz drain → physics → collision → death → door → timer → send.
//
// On exit, regardless of phase, we unregister from the spectator engines
// registry and close any still-attached spectator outChs. The Live
// registry removal is the caller's responsibility (main.go).
func (e *Engine) Run(ctx context.Context) {
	defer func() {
		if e.Engines != nil && e.PlayerID != "" {
			e.Engines.Unregister(e.PlayerID)
		}
		// Belt-and-braces: if Run exits without finishRun (e.g. ctx cancel
		// during handshake or pre-finish disconnect), close any spectators
		// that did manage to attach.
		e.closeSpectators()
	}()
	if !e.handshake(ctx) {
		return
	}
	e.tickLoop(ctx)
}

// send pushes an envelope onto outCh, incrementing seq. If outCh is full
// the envelope is dropped — the client will receive the next one.
//
// send is also the single fan-out point to attached spectators. We cache
// the catchup-relevant payloads (RUN_STARTED, ROOM_LOAD, STATE) so a new
// spectator can be warmed up on register, and we relay everything except
// S2C_AUTH_OK with per-spectator seqs.
func (e *Engine) send(typ uint8, data any) {
	e.seq++
	metrics.WSMessages.WithLabelValues("s2c", metrics.PacketTypeName(typ)).Inc()
	env := protocol.NewS2C(typ, e.seq, data)
	select {
	case e.OutCh <- env:
	default:
	}
	e.cacheCatchup(typ, data)
	e.fanOutToSpectators(typ, data)
}

// cacheCatchup stashes the latest payload for catchup-relevant types so
// a freshly-registered spectator can render without waiting for the next
// emission of the same type.
func (e *Engine) cacheCatchup(typ uint8, data any) {
	switch typ {
	case protocol.S2CRunStarted:
		if d, ok := data.(protocol.RunStartedData); ok {
			c := d
			e.lastRunStarted = &c
		}
	case protocol.S2CRoomLoad:
		if d, ok := data.(protocol.RoomLoadData); ok {
			c := d
			e.lastRoomLoad = &c
		}
	case protocol.S2CState:
		if d, ok := data.(protocol.StateData); ok {
			c := d
			e.lastState = &c
		}
	}
}

// fanOutToSpectators delivers every non-auth S2C type to every attached
// spectator with a per-spectator seq. Sends are non-blocking; a slow
// spectator gets the frame dropped (next tick will resync). This is the
// drop-on-full discipline from
// docs/specs/foundations/05-server-runtime.md#spectator-fan-out — a slow
// spectator must never stall the gameLoop.
func (e *Engine) fanOutToSpectators(typ uint8, data any) {
	if typ == protocol.S2CAuthOk || len(e.spectators) == 0 {
		return
	}
	for _, sp := range e.spectators {
		if sp.Closed {
			continue
		}
		sp.Seq++
		env := protocol.NewS2C(typ, sp.Seq, data)
		select {
		case sp.OutCh <- env:
		default:
		}
	}
}

func (e *Engine) sendError(code, msg string) {
	e.send(protocol.S2CError, protocol.ErrorData{Code: code, Message: msg})
}

// validateTsWindow enforces the per-packet ±2s ts check
// (TsValidationWindow). Returns true when the envelope is acceptable.
// On rejection, sends ts_out_of_window with the actual offset in ms so
// the client can surface an actionable "your clock is N ms ahead/behind"
// message. Skipped in ReplayMode and for synthetic Ts=0 envelopes
// (matches the dispatch-time check at the call site below). No
// cumulative drift bookkeeping — BUG-02 substrate stays exactly where
// it was.
func (e *Engine) validateTsWindow(ts int64, now time.Time) bool {
	if e.ReplayMode || ts == 0 {
		return true
	}
	diff := now.UnixMilli() - ts
	windowMs := int64(TsValidationWindow / time.Millisecond)
	if diff > windowMs || diff < -windowMs {
		offsetMs := diff
		direction := "behind"
		if diff < 0 {
			offsetMs = -diff
			direction = "ahead of"
		}
		e.sendError("ts_out_of_window",
			fmt.Sprintf("client clock is %d ms %s server (max ±%d ms) — please sync your system clock",
				offsetMs, direction, windowMs))
		return false
	}
	return true
}

// handshake blocks on AUTH and then START_RUN. Returns true when the run
// has been initialized and the tick loop may begin.
func (e *Engine) handshake(ctx context.Context) bool {
	if !e.awaitAuth(ctx) {
		return false
	}
	return e.awaitStartRun(ctx)
}

func (e *Engine) awaitAuth(ctx context.Context) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case raw, ok := <-e.InCh:
			if !ok {
				return false
			}
			env, err := protocol.DecodeEnvelope(raw)
			if err != nil {
				e.sendError("bad_envelope", err.Error())
				continue
			}
			if env.Type != protocol.C2SAuth {
				e.sendError("auth_required", "send C2S_AUTH first")
				continue
			}
			// Spec: every C2S packet (including AUTH) is validated
			// against the ±2s ts window. Without this check a skewed
			// client passes AUTH+START_RUN, enters the Game scene,
			// and then has every C2S_INPUT silently rejected — the
			// freeze reported in #46. Closing the connection here
			// surfaces the error via MainMenu's onError handler.
			if !e.validateTsWindow(env.Ts, time.Now()) {
				return false
			}
			var pkt protocol.AuthPacket
			if err := json.Unmarshal(env.Data, &pkt); err != nil {
				e.sendError("bad_auth", err.Error())
				continue
			}
			displayName := ""
			if e.AuthCheck != nil {
				name, err := e.AuthCheck(pkt.PlayerID)
				if err != nil {
					e.sendError("auth_failed", err.Error())
					return false
				}
				displayName = name
			}
			e.PlayerID = pkt.PlayerID
			if displayName != "" {
				e.DisplayName = displayName
			} else if e.DisplayName == "" {
				e.DisplayName = pkt.PlayerID
			}
			// Engines.Register is deliberately deferred until initRun:
			// pre-run engines have no cached RUN_STARTED/ROOM_LOAD/STATE,
			// so a spectator attaching during the post-AUTH / pre-START
			// window would hang waiting for catchup that can't be sent
			// and would consume per-game cap until the run started.
			e.send(protocol.S2CAuthOk, protocol.AuthOkData{DisplayName: e.DisplayName})
			if e.RegisterLeaderboard != nil && !e.ReplayMode {
				e.RegisterLeaderboard(e.PlayerID)
			}
			return true
		}
	}
}

func (e *Engine) awaitStartRun(ctx context.Context) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case raw, ok := <-e.InCh:
			if !ok {
				return false
			}
			env, err := protocol.DecodeEnvelope(raw)
			if err != nil {
				e.sendError("bad_envelope", err.Error())
				continue
			}
			if env.Type != protocol.C2SStartRun {
				e.sendError("start_required", "send C2S_START_RUN")
				continue
			}
			if !e.validateTsWindow(env.Ts, time.Now()) {
				return false
			}
			var pkt protocol.StartRunPacket
			if err := json.Unmarshal(env.Data, &pkt); err != nil {
				e.sendError("bad_start", err.Error())
				continue
			}
			if !e.initRun(pkt.StartTime) {
				return false
			}
			return true
		}
	}
}

// initRun validates the seed window, builds RunState/PlayerState/RoomState,
// loads Room 1, and sends S2C_RUN_STARTED. Returns false on validation
// failure (an S2CError has already been sent).
func (e *Engine) initRun(startTime int64) bool {
	now := time.Now()
	if !e.ReplayMode {
		seedTime := time.UnixMilli(startTime)
		if diff := now.Sub(seedTime); diff > SeedWindow || diff < -SeedWindow {
			e.sendError("seed_out_of_window", "start_time must be within ±30min of server now")
			return false
		}
	}
	e.run = &RunState{
		Status:       RunRunning,
		PlayerID:     e.PlayerID,
		DisplayName:  e.DisplayName,
		RunStartTime: now,
		Seed:         startTime,
		MobRNG:       rand.New(rand.NewSource(startTime)),
		CurrentRoom:  1,
		Splits:       []protocol.SplitData{},
		InputLog:     []InputRecord{},
	}
	e.player = NewPlayerState()
	e.room = NewRoomState(1)
	e.lastInputAt = now

	tmpl := RoomTemplates[1]
	roomData := LoadRoom(e.room, e.run.MobRNG, tmpl, TilemapEnemies(1), TilemapWaves(1), now)

	// Honour Room 1's authored PlayerSpawn marker. NewPlayerState has
	// only the heuristic bottom-center position; this overrides it.
	placePlayerSpawn(e.player, 1)

	e.send(protocol.S2CRunStarted, protocol.RunStartedData{
		Seed:        startTime,
		Room:        roomData,
		DisplayName: e.DisplayName,
	})
	// Make the engine reachable from /ws/spectate. We delayed this from
	// the AUTH handshake so spectators only ever attach to engines with
	// a cached catchup payload (RUN_STARTED has just been emit-and-
	// cached above, so any spectator that registers between this line
	// and the next tick will see the catchup). Unregister fires in
	// finishRun and in Run's deferred cleanup as belt-and-braces.
	//
	// ReplayMode skips this — a replayed run is not visible to spectators
	// and does not appear in /api/live.
	if e.Engines != nil && !e.ReplayMode {
		e.Engines.Register(e.PlayerID, e.RegisterCh)
	}
	return true
}

// RunReplay drives the engine using a caller-supplied advance function
// instead of an internal time.Ticker. Used by the replay package; see
// docs/specs/foundations/08-leaderboard-replay.md#streaming-replay-engine.
//
// advance is called once per tick. It returns the synthetic `now` to use
// for the next stepTick and a boolean ok — false signals "stop the loop"
// (cancellation, restart, etc.). The caller is responsible for sleeping
// between calls to control playback speed and for honouring ctx.Done().
//
// The handshake reads C2S_AUTH and C2S_START_RUN from InCh exactly as
// the live engine does, so the replay caller must feed those packets
// before invoking RunReplay.
func (e *Engine) RunReplay(ctx context.Context, advance func(prev time.Time) (time.Time, bool)) {
	defer e.closeSpectators()
	if !e.handshake(ctx) {
		return
	}
	now := e.run.RunStartTime
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		next, ok := advance(now)
		if !ok {
			return
		}
		now = next
		e.stepTick(now)
		if e.run.Status.IsTerminal() {
			return
		}
	}
}

// RunStartTime exposes the engine's anchor time for replay drivers that
// need to compute synthetic now = RunStartTime + tick * TickDuration.
// Returns zero time before initRun has run.
func (e *Engine) RunStartTime() time.Time {
	if e.run == nil {
		return time.Time{}
	}
	return e.run.RunStartTime
}

// tickLoop is the 20 Hz heart of the engine.
func (e *Engine) tickLoop(ctx context.Context) {
	// active_players counts players actively running the tick loop —
	// post-handshake, pre-finishRun. Replay-mode engines also bump it,
	// which is fine: the gauge models tick-loop pressure on the server.
	metrics.ActivePlayers.Inc()
	defer metrics.ActivePlayers.Dec()

	ticker := time.NewTicker(TickDuration)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticker.C:
			if !ok {
				return
			}
			tickStart := time.Now()
			e.stepTick(tickStart)
			metrics.GameTickDuration.Observe(time.Since(tickStart).Seconds())
			if e.run.Status.IsTerminal() {
				return
			}
		}
	}
}

// stepTick runs one full tick. `now` is captured once at tick start and
// reused throughout the drain — load-bearing for BUG-04 / BUG-07.
func (e *Engine) stepTick(now time.Time) {
	e.tick++
	dt := float32(TickDuration.Seconds())

	e.drainSpectatorRegister()
	e.drainInput(now)

	// Liveness checks: hard cap on total run length, and idle timeout for
	// AFK / disconnected-but-not-closed clients. Both fire RunFailed (no
	// leaderboard submission) so handleWS releases its MAX_CONCURRENT slot
	// for waiting players. Skipped in ReplayMode where `now` is synthetic
	// — a long honest replay must not be terminated mid-playback.
	if !e.ReplayMode && e.run.Status == RunRunning {
		if elapsed := now.Sub(e.run.RunStartTime); elapsed >= MaxRunDuration {
			log.Printf("run %s force-ended: max duration cap (elapsed=%v, cap=%v)", e.PlayerID, elapsed, MaxRunDuration)
			e.finishRun(now, elapsed.Milliseconds(), RunFailed)
			return
		}
		if !e.lastInputAt.IsZero() {
			if idle := now.Sub(e.lastInputAt); idle >= IdleTimeout {
				log.Printf("run %s force-ended: idle timeout (idle=%v, cap=%v)", e.PlayerID, idle, IdleTimeout)
				e.finishRun(now, e.currentElapsed(now), RunFailed)
				return
			}
		}
	}

	e.advancePlayer(dt, now)
	deaths := e.advanceEnemies(dt, now)
	hits := AdvanceProjectiles(e.room, dt, now)
	deaths = append(deaths, ResolveCollisions(e.room, hits, now)...)
	for _, id := range deaths {
		// BUG-11's intentional kill-log race lives only on the live path.
		// In ReplayMode we append synchronously with the synthetic `now`
		// so the moderation viewer reproduces the run reliably:
		//   - Synchronous append: no race-induced lost kills, so
		//     CheckRoomClear converges at the recorded tick, doors
		//     unlock on time, recorded ENTER_DOOR succeeds, player
		//     progresses through rooms instead of getting stuck.
		//   - Synthetic now timestamp: ComputeRoomBonus computes from
		//     real per-tick durations rather than wall-clock-jittered
		//     ones; the BUG-11 negative-duration payload (zero-value
		//     tail) does not fire in replay.
		// Trade-off: replayed `total_ms` will not match a recording that
		// exploited the BUG-11 bonus payload — the metadata's canonical
		// `total_ms` is shown alongside the engine-reproduced value (see
		// docs/specs/foundations/08-leaderboard-replay.md).
		if e.ReplayMode {
			e.room.KillLog = append(e.room.KillLog, KillRecord{
				EntityID:  id,
				Timestamp: now,
			})
		} else {
			go ProcessEnemyDeathAsync(e.room, id)
		}
		e.send(protocol.S2CEntityDied, protocol.EntityDiedData{EntityID: id})
	}

	// Wave pass — runs after death pass so livingCount reflects this tick's
	// kills. Armed waves whose FireAt has elapsed instantiate their pre-
	// computed mobs directly into r.Enemies; sendState below surfaces them
	// in the next S2C_STATE snapshot.
	AdvanceWaves(e.room, now)

	// Player-death pass — signed HP<=0 against int16 so a BUG-08 wrap
	// to negative trips it. Runs after the drain (heals applied) and
	// enemy-death pass (kamikaze self-destruct resolved), before the
	// door unlock (no BUG-03 door-skip on the fatal tick).
	//
	// ReplayMode skips this entirely: the existence of the recorded run
	// is itself proof the player survived. In replay we sometimes can't
	// reproduce kills the original landed (e.g. BUG-06 settle-window
	// shots, projectile/collision races, or simply that the original
	// exploited a wall-clock-jittered tick boundary). Without this
	// bypass, surviving enemies hit the player and the recording cuts
	// short with `outcome=failed` mid-run, defeating the moderation
	// viewer's "watch what happened" purpose. Run termination in
	// ReplayMode therefore comes from boss-kill OR replay-engine
	// force-finish (see replay.Player) rather than this death pass.
	if !e.ReplayMode && e.run.Status == RunRunning && e.player.HP <= 0 {
		e.finishRun(now, e.currentElapsed(now), RunFailed)
		return
	}

	// Settle tick counter (post-door load).
	if !e.room.Settled {
		e.room.TicksSinceLoad++
		if e.room.TicksSinceLoad >= SettleTicks {
			e.room.Settled = true
		}
	}

	// Reconciliation backstop (BUG-11 recovery).
	if e.tick%ReconcileEveryTicks == 0 {
		ReconcileKillLog(e.room)
	} else {
		CheckAllEnemiesDead(e.room)
	}

	// Expire pickups whose ExpiresAt has passed (coin drops). Emit
	// S2C_ENTITY_DIED for each one so the client tears down the sprite
	// — coins live in the same registry as enemies/pickups, and the
	// entity_died channel already carries pickup despawns.
	for id, pk := range e.room.Pickups {
		if pk.Consumed || pk.ExpiresAt.IsZero() {
			continue
		}
		if now.After(pk.ExpiresAt) {
			pk.Consumed = true
			e.send(protocol.S2CEntityDied, protocol.EntityDiedData{EntityID: id})
		}
	}

	for _, d := range UnlockDoorsIfClear(e.room) {
		e.send(protocol.S2CDoorUnlocked, protocol.DoorUnlockedData{
			DoorID:      d.DoorID,
			TileX:       d.TileX,
			TileY:       d.TileY,
			UnlockedGID: d.UnlockedGID,
		})
	}

	e.updateElapsed(now)
	e.sendState(now)
	e.publishLiveSummary()
}

// publishLiveSummary pushes a per-tick snapshot to the live-games registry
// so /internal/live-games (and the lobby's selector) sees this run. Cheap
// and non-blocking — drops on a full channel because the next tick's push
// supersedes anyway.
//
// ReplayMode skips this — replays are not live games and must not appear
// in the public spectator selector.
func (e *Engine) publishLiveSummary() {
	if e.Live == nil || e.run == nil || e.PlayerID == "" || e.ReplayMode {
		return
	}
	live := 0
	for _, sp := range e.spectators {
		if !sp.Closed {
			live++
		}
	}
	e.Live.Update(spectator.Summary{
		PlayerID:       e.PlayerID,
		DisplayName:    e.DisplayName,
		RoomIndex:      e.run.CurrentRoom,
		ElapsedMs:      e.run.ElapsedMs,
		SpectatorCount: live,
		StartedAt:      e.run.RunStartTime.UnixMilli(),
	})
}

// drainSpectatorRegister applies any pending register/deregister verbs.
// Runs at the start of stepTick so a spectator who joined since the last
// tick gets the catchup sequence before the next physics step lands a
// fresh S2C_STATE on top of it.
func (e *Engine) drainSpectatorRegister() {
	for {
		select {
		case req := <-e.RegisterCh:
			switch req.Action {
			case spectator.ActionRegister:
				e.handleRegister(req)
			case spectator.ActionDeregister:
				e.handleDeregister(req)
			}
		default:
			return
		}
	}
}

func (e *Engine) handleRegister(req spectator.Req) {
	if req.Conn == nil {
		if req.Reply != nil {
			req.Reply <- false
		}
		return
	}
	live := 0
	for _, sp := range e.spectators {
		if !sp.Closed {
			live++
		}
	}
	if live >= SpectatorPerGame {
		if req.Reply != nil {
			req.Reply <- false
		}
		return
	}
	e.spectators = append(e.spectators, req.Conn)
	if req.Reply != nil {
		req.Reply <- true
	}
	e.sendCatchup(req.Conn)
}

func (e *Engine) handleDeregister(req spectator.Req) {
	if req.Conn == nil {
		return
	}
	for i, sp := range e.spectators {
		if sp == req.Conn {
			e.spectators = append(e.spectators[:i], e.spectators[i+1:]...)
			return
		}
	}
}

// sendCatchup pushes the cached RUN_STARTED, ROOM_LOAD, and STATE to a
// freshly-registered spectator with fresh per-spectator seqs. Each push
// is non-blocking — a buffer-full failure here is unlikely (the channel
// was just allocated) but we tolerate it: the next regular tick's
// fan-out will resend STATE within 50 ms.
func (e *Engine) sendCatchup(sp *spectator.Conn) {
	push := func(typ uint8, data any) {
		sp.Seq++
		env := protocol.NewS2C(typ, sp.Seq, data)
		select {
		case sp.OutCh <- env:
		default:
		}
	}
	if e.lastRunStarted != nil {
		push(protocol.S2CRunStarted, *e.lastRunStarted)
	}
	if e.lastRoomLoad != nil {
		push(protocol.S2CRoomLoad, *e.lastRoomLoad)
	}
	if e.lastState != nil {
		push(protocol.S2CState, *e.lastState)
	}
}

// closeSpectators schedules every attached spectator's outCh to close
// after the SpectatorCatchupGrace window. Called once from finishRun so
// the final S2C_RUN_COMPLETE has time to flush. The closing goroutine
// owns the slice copy from this point — the engine goroutine is exiting.
func (e *Engine) closeSpectators() {
	if len(e.spectators) == 0 {
		return
	}
	conns := make([]*spectator.Conn, len(e.spectators))
	copy(conns, e.spectators)
	e.spectators = nil
	go func() {
		time.Sleep(SpectatorCatchupGrace)
		for _, sp := range conns {
			if !sp.Closed {
				sp.Closed = true
				close(sp.OutCh)
			}
		}
	}()
}

// drainInput pulls every queued C2S packet and dispatches each with the
// tick-shared `now`. Receiving any frame (even one that fails to decode)
// counts as liveness — we're trying to detect AFK/disconnected clients,
// not malformed packets.
func (e *Engine) drainInput(now time.Time) {
	for {
		select {
		case raw, ok := <-e.InCh:
			if !ok {
				return
			}
			e.lastInputAt = now
			e.dispatch(raw, now)
		default:
			return
		}
	}
}

func (e *Engine) dispatch(raw []byte, now time.Time) {
	start := time.Now()
	env, err := protocol.DecodeEnvelope(raw)
	if err != nil {
		metrics.WSMessages.WithLabelValues("c2s", "unknown").Inc()
		metrics.Errors.WithLabelValues("ws_parse_fail").Inc()
		e.sendError("bad_envelope", err.Error())
		return
	}
	pktName := metrics.PacketTypeName(env.Type)
	metrics.WSMessages.WithLabelValues("c2s", pktName).Inc()
	defer func() {
		metrics.WSHandlerDuration.WithLabelValues(pktName).Observe(time.Since(start).Seconds())
	}()
	// Per-packet ts validation: ±2s absolute window. No cumulative drift
	// check — BUG-02 substrate. Replay skips this entirely: recorded Ts
	// values come from the original session's wall clock and would
	// otherwise reject every packet.
	if !e.validateTsWindow(env.Ts, now) {
		return
	}
	if env.Ts != 0 && !e.ReplayMode {
		e.lastClientTs = env.Ts
	}

	e.run.InputLog = append(e.run.InputLog, InputRecord{
		Tick: e.tick,
		Type: env.Type,
		Seq:  env.Seq,
		Ts:   env.Ts,
		Data: append([]byte(nil), env.Data...),
	})

	switch env.Type {
	case protocol.C2SInput:
		var pkt protocol.InputPacket
		if err := json.Unmarshal(env.Data, &pkt); err != nil {
			return
		}
		e.dirtyInput.dx = clampInput(pkt.DX)
		e.dirtyInput.dy = clampInput(pkt.DY)
	case protocol.C2SShoot:
		var pkt protocol.ShootPacket
		if err := json.Unmarshal(env.Data, &pkt); err != nil {
			return
		}
		e.player.LastAimAngle = pkt.AimAngle
		HandleShoot(e.player, e.room, pkt.AimAngle, now)
	case protocol.C2SSwitchWeapon:
		var pkt protocol.SwitchWeaponPacket
		if err := json.Unmarshal(env.Data, &pkt); err != nil {
			return
		}
		HandleSwitchWeapon(e.player, pkt.WeaponID, now)
	case protocol.C2SInteract:
		var pkt protocol.InteractPacket
		if err := json.Unmarshal(env.Data, &pkt); err != nil {
			return
		}
		if consumed := HandleInteract(e.player, e.room, pkt.TargetID, pkt.ClaimedType, now); consumed != nil {
			e.send(protocol.S2CEntityDied, protocol.EntityDiedData{EntityID: pkt.TargetID})
		}
	case protocol.C2SEnterDoor:
		var pkt protocol.EnterDoorPacket
		if err := json.Unmarshal(env.Data, &pkt); err != nil {
			return
		}
		e.tryEnterDoor(pkt.DoorID, now)
	case protocol.C2SShopPurchase:
		var pkt protocol.ShopPurchasePacket
		if err := json.Unmarshal(env.Data, &pkt); err != nil {
			return
		}
		HandleShopPurchase(e.player, pkt.ItemID)
	}
}

func (e *Engine) tryEnterDoor(doorID uint32, now time.Time) {
	// Replay best-effort: the recorded run reached this door at this
	// tick, so it was effectively unlocked then — typically via BUG-11's
	// kill-log race overcounting and tripping CheckRoomClear early. Our
	// deterministic ReplayMode kill-log path never overcounts, so left
	// to its own devices the door stays locked and the recorded
	// ENTER_DOOR fires into a closed door — every subsequent input
	// targets the wrong room and the player visibly hangs at a wall.
	// Force-unlock the door (still gated on existence, so a malformed
	// recording can't transition to a phantom room) and emit the
	// DOOR_UNLOCKED broadcast for sprite-swap parity with the live
	// engine.
	if e.ReplayMode {
		if door, exists := e.room.Doors[doorID]; exists && !door.Unlocked {
			door.Unlocked = true
			e.send(protocol.S2CDoorUnlocked, protocol.DoorUnlockedData{
				DoorID:      door.DoorID,
				TileX:       door.TileX,
				TileY:       door.TileY,
				UnlockedGID: door.UnlockedGID,
			})
		}
	}
	nextRoom, ok := HandleEnterDoor(e.player, e.room, doorID)
	if !ok {
		return
	}
	e.transitionToRoom(nextRoom, now)
}

func (e *Engine) transitionToRoom(nextRoom uint8, now time.Time) {
	// Split time for the room we're leaving. Apply the room bonus to the
	// run clock before emitting ROOM_LOAD so the live HUD timer (STATE) and
	// split row (ROOM_LOAD.split_ms) stay on the same timeline.
	elapsed := e.currentElapsed(now)
	bonus := ComputeRoomBonus(e.room.KillLog)
	if bonus != 0 {
		e.run.TimeAdjustmentMs += bonus
		elapsed += bonus
	}
	e.run.ElapsedMs = elapsed
	// roomDuration is the wall-clock time spent in the room we're leaving:
	// elapsed minus the previous split's TimeMs (or elapsed itself for room 0).
	// Clamp at 0: ComputeRoomBonus can return hugely negative values under
	// BUG-11, and a negative observation would poison the histogram _sum.
	roomDuration := elapsed
	if n := len(e.run.Splits); n > 0 {
		roomDuration = elapsed - e.run.Splits[n-1].TimeMs
	}
	if roomDuration < 0 {
		roomDuration = 0
	}
	if !e.ReplayMode {
		metrics.RoomSplitDuration.
			WithLabelValues(strconv.Itoa(int(e.run.CurrentRoom))).
			Observe(float64(roomDuration) / 1000.0)
	}
	e.run.Splits = append(e.run.Splits, protocol.SplitData{
		RoomIndex: e.run.CurrentRoom,
		TimeMs:    elapsed,
		Kills:     uint8(len(e.room.KillLog)),
	})

	if nextRoom > FinalRoom {
		// Boss room is FinalRoom; completing it happens via enemy death
		// below, not door transition. Guard rail just in case.
		e.finishRun(now, elapsed, RunCompleted)
		return
	}

	e.run.CurrentRoom = nextRoom
	e.room = NewRoomState(nextRoom)
	tmpl := RoomTemplates[nextRoom]
	if tmpl == nil {
		e.sendError("bad_room", "no template for room")
		return
	}
	roomData := LoadRoom(e.room, e.run.MobRNG, tmpl, TilemapEnemies(nextRoom), TilemapWaves(nextRoom), now)

	// Place the player on a non-wall tile near the bottom-center of the
	// new room. Without this, a player entering through a door flush
	// against a wall could end up spawned inside geometry.
	placePlayerSpawn(e.player, nextRoom)

	e.send(protocol.S2CRoomLoad, protocol.RoomLoadData{
		RoomIndex: nextRoom,
		Room:      roomData,
		SplitMs:   elapsed,
	})

	if nextRoom == FinalRoom {
		// Arm boss-room completion tracking: when all enemies in the
		// boss room die, the next tick's death processing triggers
		// finish.
	}
}

func (e *Engine) advancePlayer(dt float32, now time.Time) {
	speed := PlayerSpeed * EffectiveSpeedMult(e.player)

	const hw float32 = 10 // player half-width used for wall collision
	const hh float32 = 10

	// Axis-separated move so the player slides along walls.
	newX := e.player.X + e.dirtyInput.dx*speed*dt
	newX = clampFloat(newX, 16, RoomWidth-16)
	if !BlockedAABB(e.run.CurrentRoom, newX, e.player.Y, hw, hh) {
		e.player.X = newX
	}

	newY := e.player.Y + e.dirtyInput.dy*speed*dt
	newY = clampFloat(newY, 16, RoomHeight-16)
	if !BlockedAABB(e.run.CurrentRoom, e.player.X, newY, hw, hh) {
		e.player.Y = newY
	}
}

func (e *Engine) advanceEnemies(dt float32, now time.Time) []uint32 {
	// Build the per-tick flow-field cache. The cache is rooted at the
	// player's tile; per-mob jitter biases the source by ±1 tile, so the
	// cache holds at most 9 fields per tick. Fields are computed lazily
	// on first request and discarded when this function returns. See
	// docs/specs/features/mob-ai.md § Aggro chase: flow field.
	fields := NewFlowFieldCache(e.run.CurrentRoom)

	var deaths []uint32
	for id, en := range e.room.Enemies {
		if UpdateEnemy(en, e.player, e.run.CurrentRoom, dt, now, fields) {
			dmg := Archetypes[en.Type].MeleeDamage
			e.player.HP -= dmg
			e.player.DamageTaken += int32(dmg)
			if en.Type == EnemyKamikaze {
				// Kamikaze self-destructs on contact — queue for death emission
				// so S2CEntityDied is sent and the client removes it.
				en.AIState = AIDead
				en.HP = 0
				deaths = append(deaths, id)
			}
		}
	}
	return deaths
}

func (e *Engine) updateElapsed(now time.Time) {
	e.run.ElapsedMs = e.currentElapsed(now)

	// End-of-run detection: boss room cleared.
	if e.run.CurrentRoom == FinalRoom && e.room.AllEnemiesDead && e.run.Status == RunRunning {
		e.finishRun(now, e.run.ElapsedMs, RunCompleted)
	}
}

func (e *Engine) currentElapsed(now time.Time) int64 {
	return now.Sub(e.run.RunStartTime).Milliseconds() + e.run.TimeAdjustmentMs
}

// finishRun transitions the run to a terminal status and emits
// ForceFinishReplay terminates a replay run as RunCompleted with the
// caller-supplied total_ms. Intended for replay.Player to call once the
// recorded InputLog has been exhausted but the engine has not yet
// terminated naturally (boss un-killed because shots missed,
// surviving enemies kept the player out of the boss room, etc.).
//
// No-op outside ReplayMode and on already-terminal runs.
func (e *Engine) ForceFinishReplay(totalMs int64) {
	if !e.ReplayMode || e.run == nil || e.run.Status.IsTerminal() {
		return
	}
	// Synthetic now anchored on the run's start so elapsed math in
	// finishRun and downstream HUD code stays consistent.
	now := e.run.RunStartTime.Add(time.Duration(totalMs) * time.Millisecond)
	e.finishRun(now, totalMs, RunCompleted)
}

// S2C_RUN_COMPLETE. `status` must be RunCompleted or RunFailed — the wire
// outcome is derived from it so the two values cannot drift.
func (e *Engine) finishRun(now time.Time, totalMs int64, status RunStatus) {
	e.run.Status = status
	e.run.ElapsedMs = totalMs

	// Replay-mode finishRun is a synthetic terminal — don't conflate it
	// with real player runs in event-day metrics.
	if !e.ReplayMode {
		outcome := string(status.Outcome())
		metrics.RunsCompleted.WithLabelValues(outcome).Inc()
		metrics.RunDuration.WithLabelValues(outcome).Observe(float64(totalMs) / 1000.0)
	}

	// Append final split if not already captured.
	e.run.Splits = append(e.run.Splits, protocol.SplitData{
		RoomIndex: e.run.CurrentRoom,
		TimeMs:    totalMs,
		Kills:     uint8(len(e.room.KillLog)),
	})

	// Only successful completions are leaderboard-eligible. Failed runs
	// skip submission — no CompletedRun, no DB row, no replay verification.
	// Replay mode also skips submission: a replayed run must never
	// re-insert itself into completed_runs.
	rank := 0
	if status == RunCompleted && !e.ReplayMode {
		inputLog, _ := json.Marshal(e.run.InputLog)
		replayID := uuid.NewString()
		checksum := leaderboard.Checksum(e.run.Seed, inputLog)
		cr := &leaderboard.CompletedRun{
			ReplayID:    replayID,
			PlayerID:    e.PlayerID,
			DisplayName: e.DisplayName,
			TotalMs:     totalMs,
			Splits:      append([]protocol.SplitData(nil), e.run.Splits...),
			Seed:        e.run.Seed,
			InputLog:    inputLog,
			Checksum:    checksum,
			SubmittedAt: now,
		}
		if e.SubmitAndRefresh != nil {
			result := e.SubmitAndRefresh(cr)
			if result.Err != nil {
				log.Printf("leaderboard: submit run %s: %v", replayID, result.Err)
			} else {
				rank = result.Rank
				e.send(protocol.S2CLeaderboard, protocol.LeaderboardData{
					Entries: result.Entries,
				})
			}
		} else if e.Submit != nil {
			if !e.Submit(cr) {
				log.Printf("completedCh full, dropping run %s", replayID)
			}
		}
	}

	e.send(protocol.S2CRunComplete, protocol.RunCompleteData{
		Outcome: status.Outcome(),
		TotalMs: totalMs,
		Rank:    rank,
		Splits:  e.run.Splits,
	})
	// Spectators have just received the RUN_COMPLETE via fan-out; schedule
	// their outChs to close after SPECTATOR_CATCHUP_MS so the connWriter
	// has a chance to flush before the WS shuts.
	e.closeSpectators()
	// Stop accepting new spectators on this engine — the run is over and
	// any incoming /ws/spectate request should hit the 404-no-live-run
	// path instead of attaching to a dying engine. Run's deferred
	// Unregister is the belt for the disconnect-without-finishRun path.
	if e.Engines != nil && e.PlayerID != "" {
		e.Engines.Unregister(e.PlayerID)
	}
}

// sendState builds the S2C_STATE snapshot from the authoritative state.
func (e *Engine) sendState(now time.Time) {
	entities := make([]protocol.EntitySnapshot, 0, len(e.room.Enemies))
	for _, en := range e.room.Enemies {
		// Dead enemies stay in r.Enemies for BUG-11 / reconciliation math,
		// but S2C_STATE only carries live ones — the entity_died event
		// has already told the client to despawn.
		if en.AIState == AIDead {
			continue
		}
		entities = append(entities, protocol.EntitySnapshot{
			EntityID: en.EntityID,
			Type:     en.Type,
			X:        en.X,
			Y:        en.Y,
			HP:       en.HP,
			State:    uint8(en.AIState),
		})
	}
	pickups := make([]protocol.PickupSpawn, 0, len(e.room.Pickups))
	for _, pk := range e.room.Pickups {
		if pk.Consumed {
			continue
		}
		pickups = append(pickups, protocol.PickupSpawn{
			EntityID: pk.EntityID,
			Type:     pk.Type,
			X:        pk.X,
			Y:        pk.Y,
		})
	}

	projs := make([]protocol.ProjSnapshot, 0, len(e.room.Projectiles))
	for _, pr := range e.room.Projectiles {
		projs = append(projs, protocol.ProjSnapshot{
			ProjID: pr.ProjID,
			X:      pr.X,
			Y:      pr.Y,
			Angle:  float32(math.Atan2(float64(pr.VelY), float64(pr.VelX))),
			Owner:  pr.OwnerID,
		})
	}
	data := protocol.StateData{
		ElapsedMs: e.run.ElapsedMs,
		Player: protocol.PlayerSnapshot{
			X:              e.player.X,
			Y:              e.player.Y,
			HP:             e.player.HP,
			WeaponID:       e.player.CurrentWeapon,
			Ammo:           e.player.WeaponAmmo[e.player.CurrentWeapon],
			Coins:          e.player.Coins,
			SpeedStacks:    e.player.SpeedStacks,
			FireRateStacks: e.player.FireRateStacks,
			DamageStacks:   e.player.DamageStacks,
			AimAngle:       e.player.LastAimAngle,
		},
		Entities:    entities,
		Projectiles: projs,
		Pickups:     pickups,
	}
	e.send(protocol.S2CState, data)
}

// placePlayerSpawn finds a spawn tile in the target room and places the
// player there. Prefers the authored PlayerSpawn marker from the room's
// Tiled Objects layer; falls back to a heuristic + spiral search if no
// marker exists or the marker landed inside geometry.
func placePlayerSpawn(p *PlayerState, roomIndex uint8) {
	const hw, hh float32 = 10, 10
	if sx, sy, ok := PlayerSpawnAt(roomIndex); ok {
		// Marker is authoritative when present and clear of walls. If the
		// authored point happens to overlap geometry, snap to the nearest
		// non-wall tile rather than silently falling through to defaults
		// — that keeps the room's "intended" entrance close to where the
		// designer placed it.
		if !BlockedAABB(roomIndex, sx, sy, hw, hh) {
			p.X, p.Y = sx, sy
			return
		}
		nx, ny := NearestNonWall(roomIndex, sx, sy)
		p.X, p.Y = nx, ny
		return
	}
	candidates := [][2]float32{
		{RoomWidth / 2, RoomHeight - 100},
		{RoomWidth / 2, RoomHeight / 2},
		{64, RoomHeight - 64},
		{RoomWidth - 64, RoomHeight - 64},
	}
	for _, c := range candidates {
		if !BlockedAABB(roomIndex, c[0], c[1], hw, hh) {
			p.X, p.Y = c[0], c[1]
			return
		}
	}
	// Spiral search over tile centers if every preset is blocked.
	for ty := 1; ty < GridRows-1; ty++ {
		for tx := 1; tx < GridCols-1; tx++ {
			cx := float32(tx)*TileSize + TileSize/2
			cy := float32(ty)*TileSize + TileSize/2
			if !BlockedAABB(roomIndex, cx, cy, hw, hh) {
				p.X, p.Y = cx, cy
				return
			}
		}
	}
	// Nothing worked — leave where they are.
}

func clampInput(v float32) float32 {
	if v > 1 {
		return 1
	}
	if v < -1 {
		return -1
	}
	return v
}
