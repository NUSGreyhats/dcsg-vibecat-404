# Feature — Rooms & Doors

## Purpose

Room model, door state machine, and the door-entry handler that advances the player through the dungeon. Hosts [BUG-03 — Door Skip (No Proximity)](#bug-03-door-skip-no-proximity), the simplest Surface-tier exploit in the game.

## Data model

### Room (authoritative on server)

`RoomData` carried in `S2C_RUN_STARTED` and `S2C_ROOM_LOAD` (see [../foundations/02-protocol.md](../foundations/02-protocol.md)):

```
RoomData {
    room_index     uint8
    tilemap_id     string             // e.g. "room-3"
    enemies        []EnemySpawn       // base positions; RNG offsets applied at load
    pickups        []PickupSpawn
    doors          []DoorDef
}
```

Server-side per-room state is `RoomState` from [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md):

```go
RoomState {
    RoomIndex       uint8
    Enemies         map[uint32]*EnemyState
    Pickups         map[uint32]*PickupState
    Projectiles     map[uint32]*ProjectileState
    Doors           map[uint32]*DoorState
    AllEnemiesDead  bool
    Settled         bool
    TicksSinceLoad  uint8
    KillLog         []KillRecord
}
```

### Door

```go
DoorState {
    DoorID          uint32
    X, Y            float32
    TargetRoom      uint8
    Unlocked        bool
}
```

## Door state machine

```
Locked  ──(all enemies dead)──►  Unlocked  ──(C2S_ENTER_DOOR valid)──►  room advances
```

Doors start `Locked`. On each tick's door pass (see [../foundations/05-server-runtime.md](../foundations/05-server-runtime.md) game loop diagram):

```go
if r.AllEnemiesDead {
    for _, d := range r.Doors {
        if !d.Unlocked {
            d.Unlocked = true
            send S2C_DOOR_UNLOCKED { door_id: d.DoorID }
        }
    }
}
```

## Door-entry handler

```go
func handleEnterDoor(p *PlayerState, r *RoomState, pkt EnterDoorPacket) {
    door, ok := r.Doors[pkt.DoorID]
    if !ok { return }
    if !door.Unlocked { return }
    // NO player-distance check here — by design
    transitionToRoom(p, door.TargetRoom)
}
```

**Critical:** no check that the player is near the door. This enables [BUG-03](#bug-03-door-skip-no-proximity).

## Data flow

- Door unlock (server-driven): `AllEnemiesDead = true` → `Unlocked = true` → `S2C_DOOR_UNLOCKED` broadcast.
- Door entry (client-driven): `C2S_ENTER_DOOR { door_id }` → server validates door exists and is unlocked → `transitionToRoom` → `S2C_ROOM_LOAD { room_index, room, split_ms }`.
- Room transition also triggers the 2-tick settle window (see [../foundations/05-server-runtime.md#post-door-settle-window](../foundations/05-server-runtime.md#post-door-settle-window)), which is the ingress for [BUG-06](../foundations/05-server-runtime.md#bug-06-concurrent-room-settle-race).

## Client interactions

- `client/src/game/world/roomLayout.ts` — consumes `RoomData` into renderable world.
- Tilemaps loaded from `client/public/assets/rooms/room-*.json` keyed by `tilemap_id`.
- Door UI: show locked/unlocked indicator on each door sprite; on `S2C_DOOR_UNLOCKED`, switch the sprite; on player interact, send `C2S_ENTER_DOOR`.

## Implementation notes

- Package: `internal/game/rooms` (target).
- `transitionToRoom` resets `RoomState.Enemies`, `Pickups`, `Projectiles`, and sets `Settled = false, TicksSinceLoad = 0`. Subsequent ticks increment `TicksSinceLoad`; at 2 → `Settled = true`.
- Rooms are numbered 1..11. Room 11 is the boss room; clearing it ends the run.

## Implementation status

Partial. Current server hardcodes `winRoom = 11` (`server/main.go`) and validates room progression (`validateRoomProgression`) — advancing must be sequential and only after enemies are cleared. However, current implementation is client-driven (client reports its `currentRoom`), whereas the target is server-authoritative via `C2S_ENTER_DOOR`.

Client has 10 room tilemaps (`room-1.json` through `room-10.json`); an 11th (boss room) is TBD.

## Intentional bugs in this area

### BUG-03 Door Skip (No Proximity)

**Design choice enabling the bug:** The `C2S_ENTER_DOOR` handler checks that the door exists and is `Unlocked` but does NOT check the player's distance to the door.

**Root cause sketch:** See the handler above — `// NO player-distance check here — by design`.

**Exploit path:** Once all enemies in the room are dead (and `S2C_DOOR_UNLOCKED` has fired), the player can send `C2S_ENTER_DOOR { door_id }` from anywhere in the room — no need to walk over to the door first. This saves the walk-back time in rooms where the last enemy was killed far from the exit.

**Bound:**
- Requires the door to actually be `Unlocked` — you still have to kill all enemies.
- Only the first `C2S_ENTER_DOOR` in a tick takes effect (the room transitions immediately); subsequent ones target a different room.

**Detection:** Replay reproduces identically. Do NOT add a distance check.

**Synergies:** Multiplied by [BUG-02 drift tick stretching](../foundations/05-server-runtime.md#bug-02-drift-tick-stretching). Follow-up [BUG-06 settle race](../foundations/05-server-runtime.md#bug-06-concurrent-room-settle-race) becomes useful on the freshly-loaded next room.

**Savings:** ~2–3 s per room.

## Packets used

- `C2S_ENTER_DOOR`.
- `S2C_DOOR_UNLOCKED`, `S2C_ROOM_LOAD`.

## Cross-references

- [../foundations/02-protocol.md](../foundations/02-protocol.md) — packet definitions.
- [../foundations/05-server-runtime.md](../foundations/05-server-runtime.md) — tick loop, post-door settle, BUG-02 / BUG-06.
- [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md) — `DoorState`, `RoomState`.
- [pickups.md](pickups.md) — pickup interactions within rooms.
- [mob-ai.md](mob-ai.md) — enemy spawn before doors unlock.
