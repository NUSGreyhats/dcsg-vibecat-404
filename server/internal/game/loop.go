package game

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"math/rand"
	"time"

	"github.com/google/uuid"

	"github.com/greyhats/defcon-game/server/internal/leaderboard"
	"github.com/greyhats/defcon-game/server/internal/protocol"
)

// Engine is the per-player game loop. It owns RunState, PlayerState, and
// RoomState (sole-owner rule from docs/specs/foundations/06-game-state-conventions.md).
// Lifecycle: one Engine per WebSocket connection, torn down on disconnect.
type Engine struct {
	InCh   <-chan []byte
	OutCh  chan<- protocol.S2CEnvelope
	Submit func(*leaderboard.CompletedRun) bool
	// AuthCheck gates the WS auth handshake. It returns the player's
	// display name (empty to fall back to the raw player_id) and an
	// error to reject the connection.
	AuthCheck func(playerID string) (string, error)

	PlayerID    string
	DisplayName string

	run    *RunState
	player *PlayerState
	room   *RoomState

	seq          uint32
	tick         uint64
	lastClientTs int64
	dirtyInput   struct{ dx, dy float32 }
}

// NewEngine constructs an engine bound to the per-connection channels.
func NewEngine(in <-chan []byte, out chan<- protocol.S2CEnvelope, submit func(*leaderboard.CompletedRun) bool, authCheck func(string) (string, error)) *Engine {
	return &Engine{
		InCh:      in,
		OutCh:     out,
		Submit:    submit,
		AuthCheck: authCheck,
	}
}

// Run drives the engine until ctx is cancelled or InCh closes. It is
// structured in two phases:
//
//  1. pre-run: authenticate (C2S_AUTH) and then wait for C2S_START_RUN.
//  2. tick loop: 20 Hz drain → physics → collision → death → door → timer → send.
func (e *Engine) Run(ctx context.Context) {
	if !e.handshake(ctx) {
		return
	}
	e.tickLoop(ctx)
}

// send pushes an envelope onto outCh, incrementing seq. If outCh is full
// the envelope is dropped — the client will receive the next one.
func (e *Engine) send(typ uint8, data any) {
	e.seq++
	env := protocol.NewS2C(typ, e.seq, data)
	select {
	case e.OutCh <- env:
	default:
	}
}

func (e *Engine) sendError(code, msg string) {
	e.send(protocol.S2CError, protocol.ErrorData{Code: code, Message: msg})
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
			e.send(protocol.S2CAuthOk, protocol.AuthOkData{DisplayName: e.DisplayName})
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
	seedTime := time.UnixMilli(startTime)
	if diff := now.Sub(seedTime); diff > SeedWindow || diff < -SeedWindow {
		e.sendError("seed_out_of_window", "start_time must be within ±30min of server now")
		return false
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

	tmpl := RoomTemplates[1]
	roomData := LoadRoom(e.room, e.run.MobRNG, tmpl, now)

	// Honour Room 1's authored PlayerSpawn marker. NewPlayerState has
	// only the heuristic bottom-center position; this overrides it.
	placePlayerSpawn(e.player, 1)

	e.send(protocol.S2CRunStarted, protocol.RunStartedData{
		Seed: startTime,
		Room: roomData,
	})
	return true
}

// tickLoop is the 20 Hz heart of the engine.
func (e *Engine) tickLoop(ctx context.Context) {
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
			e.stepTick(time.Now())
			if e.run.Status == RunCompleted {
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

	e.drainInput(now)
	e.advancePlayer(dt)
	deaths := e.advanceEnemies(dt, now)
	hits := AdvanceProjectiles(e.room, dt, now)
	deaths = append(deaths, ResolveCollisions(e.room, hits, now)...)
	for _, id := range deaths {
		go ProcessEnemyDeathAsync(e.room, id)
		e.send(protocol.S2CEntityDied, protocol.EntityDiedData{EntityID: id})
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

	for _, doorID := range UnlockDoorsIfClear(e.room) {
		e.send(protocol.S2CDoorUnlocked, protocol.DoorUnlockedData{DoorID: doorID})
	}

	e.updateElapsed(now)
	e.sendState(now)
}

// drainInput pulls every queued C2S packet and dispatches each with the
// tick-shared `now`.
func (e *Engine) drainInput(now time.Time) {
	for {
		select {
		case raw, ok := <-e.InCh:
			if !ok {
				return
			}
			e.dispatch(raw, now)
		default:
			return
		}
	}
}

func (e *Engine) dispatch(raw []byte, now time.Time) {
	env, err := protocol.DecodeEnvelope(raw)
	if err != nil {
		e.sendError("bad_envelope", err.Error())
		return
	}
	// Per-packet ts validation: ±2s absolute window. No cumulative drift
	// check — BUG-02 substrate.
	if env.Ts != 0 {
		diff := now.UnixMilli() - env.Ts
		if diff > int64(TsValidationWindow/time.Millisecond) ||
			diff < -int64(TsValidationWindow/time.Millisecond) {
			e.sendError("ts_out_of_window", "ts must be within ±2s of server now")
			return
		}
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
		if consumed := HandleInteract(e.player, e.room, pkt.TargetID, now); consumed != nil {
			e.send(protocol.S2CEntityDied, protocol.EntityDiedData{EntityID: pkt.TargetID})
		}
	case protocol.C2SEnterDoor:
		var pkt protocol.EnterDoorPacket
		if err := json.Unmarshal(env.Data, &pkt); err != nil {
			return
		}
		e.tryEnterDoor(pkt.DoorID, now)
	case protocol.C2SUseItem:
		e.dispatchUseItem(env.Data, now)
	}
}

// dispatchUseItem runs both the audit parser (BUG-09 parser half) and the
// stdlib json parser (BUG-09 dispatch half) on the raw payload.
func (e *Engine) dispatchUseItem(raw []byte, now time.Time) {
	protocol.LogItemUse(e.PlayerID, raw)
	var pkt protocol.UseItemPacket
	if err := json.Unmarshal(raw, &pkt); err != nil {
		return
	}
	HandleUseItem(e.player, e.run, e.room, pkt.ItemName, now)
}

func (e *Engine) tryEnterDoor(doorID uint32, now time.Time) {
	nextRoom, ok := HandleEnterDoor(e.player, e.room, doorID)
	if !ok {
		return
	}
	e.transitionToRoom(nextRoom, now)
}

func (e *Engine) transitionToRoom(nextRoom uint8, now time.Time) {
	// Split time for the room we're leaving.
	bonus := ComputeRoomBonus(e.room.KillLog)
	elapsed := e.currentElapsed(now) + bonus
	e.run.Splits = append(e.run.Splits, protocol.SplitData{
		RoomIndex: e.run.CurrentRoom,
		TimeMs:    elapsed,
		Kills:     uint8(len(e.room.KillLog)),
	})

	if nextRoom > FinalRoom {
		// Boss room is FinalRoom; completing it happens via enemy death
		// below, not door transition. Guard rail just in case.
		e.finishRun(now, elapsed)
		return
	}

	e.run.CurrentRoom = nextRoom
	e.room = NewRoomState(nextRoom)
	tmpl := RoomTemplates[nextRoom]
	if tmpl == nil {
		e.sendError("bad_room", "no template for room")
		return
	}
	roomData := LoadRoom(e.room, e.run.MobRNG, tmpl, now)

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
		// Arm boss-room completion tracking: when all enemies in room 11
		// die, the next tick's death processing will trigger finish.
	}
}

func (e *Engine) advancePlayer(dt float32) {
	speed := PlayerSpeed
	if e.player.SpeedMult > 0 && time.Now().Before(e.player.SpeedMultUntil) {
		speed *= e.player.SpeedMult
	}

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
	var deaths []uint32
	for id, en := range e.room.Enemies {
		if UpdateEnemy(en, e.player, e.run.CurrentRoom, dt, now) {
			// Enemy landed an attack: tiny damage. Shield halves it.
			dmg := int16(5)
			if now.Before(e.player.ShieldUntil) {
				dmg = 2
			}
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
		e.finishRun(now, e.run.ElapsedMs)
	}
}

func (e *Engine) currentElapsed(now time.Time) int64 {
	return now.Sub(e.run.RunStartTime).Milliseconds()
}

func (e *Engine) finishRun(now time.Time, totalMs int64) {
	e.run.Status = RunCompleted
	e.run.ElapsedMs = totalMs

	// Append final split if not already captured.
	e.run.Splits = append(e.run.Splits, protocol.SplitData{
		RoomIndex: e.run.CurrentRoom,
		TimeMs:    totalMs,
		Kills:     uint8(len(e.room.KillLog)),
	})

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
	if e.Submit != nil {
		if !e.Submit(cr) {
			log.Printf("completedCh full, dropping run %s", replayID)
		}
	}

	e.send(protocol.S2CRunComplete, protocol.RunCompleteData{
		TotalMs: totalMs,
		Rank:    0,
		Splits:  e.run.Splits,
	})
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
			X:         e.player.X,
			Y:         e.player.Y,
			HP:        e.player.HP,
			WeaponID:  e.player.CurrentWeapon,
			Ammo:      e.player.WeaponAmmo[e.player.CurrentWeapon],
			Inventory: append([]string(nil), e.player.Inventory...),
		},
		Entities:    entities,
		Projectiles: projs,
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
