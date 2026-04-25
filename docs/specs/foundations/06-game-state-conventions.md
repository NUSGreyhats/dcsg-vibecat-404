# 06 — Game State Conventions

## Purpose

Canonical state model for a single player's run: `RunState`, `PlayerState`, `RoomState`, `EnemyState`, `DoorState`. Defines ownership, mutation rules, and how new features register state without cross-contamination. Field choices (integer widths, slice types, map keys) are load-bearing — several intentional bugs depend on them.

## Ownership rule (critical)

**The per-player `gameLoop` goroutine is the sole owner of all state in this file.** There is no mutex guarding these structs. Any other goroutine that needs to read or mutate state must go through a channel.

This rule is intentionally violated by one place: the per-enemy cleanup goroutines that append to `RoomState.KillLog`. See [05-server-runtime.md#bug-11-kill-log-append-race](./05-server-runtime.md#bug-11-kill-log-append-race).

## State blocks

### `RunState`

```go
RunState {
    Status          enum { Idle, Running, Completed, Failed }
    PlayerID        string
    DisplayName     string
    RunStartTime    time.Time
    Seed            int64
    MobRNG          *rand.Rand
    CurrentRoom     uint8
    ElapsedMs       int64
    Splits          []SplitData
    InputLog        []InputRecord
    RunsUsed        uint8
}
```

Owned by `gameLoop`. `MobRNG` seeded from `Seed` which comes from the client's `start_time` (see [07-determinism-seeding.md](./07-determinism-seeding.md)).

### `PlayerState`

```go
PlayerState {
    X, Y            float32
    VelX, VelY      float32
    HP, MaxHP       int16               // ← int16, relevant for BUG-08
    CurrentWeapon   uint8
    UnlockedWeapons []uint8
    WeaponCooldown  map[uint8]time.Time
    WeaponAmmo      map[uint8]int
    SwapCooldownEnd time.Time
    DamageTaken     int32
    Inventory       []string            // item names, relevant for BUG-10

    // Timed buffs (inventory consumables).
    SpeedMult       float32
    SpeedMultUntil  time.Time
    DamageMult      float32
    DamageMultUntil time.Time
    ShieldUntil     time.Time

    // Shop traits — owned by features/shop.md. Permanent stackable buffs;
    // reset on death / new run. Effective multipliers are derived at read
    // time, never cached. The shop's `ammo` purchase is a one-shot refill,
    // hence no AmmoStacks field.
    Coins           int32
    SpeedStacks     uint8
    FireRateStacks  uint8
    DamageStacks    uint8
}
```

Field notes:

- **`HP, MaxHP int16`** — range −32768..32767. No overflow guard on `+=`/`-=`. See [../features/combat-weapons.md#bug-08-hp-integer-overflow-healing-past-max](../features/combat-weapons.md#bug-08-hp-integer-overflow-healing-past-max). Checked against `<= 0` in the per-tick **player-death pass** (see [05-server-runtime.md#player-death-pass](./05-server-runtime.md#player-death-pass)); signed comparison is load-bearing so a BUG-08 wrap-to-negative trips the check.
- **`Inventory []string`** — stores item names; per-string length validated with `len()` (byte count) capped at 16. See [../features/inventory-items.md#bug-10-rune-vs-byte-item-length-bypass](../features/inventory-items.md#bug-10-rune-vs-byte-item-length-bypass).
- **`WeaponCooldown map[uint8]time.Time`** — per-weapon next-allowed-fire timestamp. Shared `time.Now()` within a tick enables [BUG-04](../features/combat-weapons.md#bug-04-swap-cancel) and [BUG-07](../features/combat-weapons.md#bug-07-damage-batch-overflow).
- **`Coins`, `SpeedStacks`, `FireRateStacks`, `DamageStacks`** — owned by [../features/shop.md](../features/shop.md). All four are zeroed on run end / new run via `NewPlayerState()` — there is no in-place reset path because a failed run tears down the engine. `Coins` is `int32` (overflow is not an intended bug class); stacks are `uint8` (coin cost rate-limits stack growth far below 255).

### `RoomState`

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
    KillLog         []KillRecord        // concurrent append target, relevant for BUG-11
}
```

Field notes:

- **`Settled bool` + `TicksSinceLoad uint8`** — enforce the 2-tick post-door settle. See [05-server-runtime.md](./05-server-runtime.md).
- **`KillLog []KillRecord`** — appended from cleanup goroutines without synchronization; see [BUG-11](./05-server-runtime.md#bug-11-kill-log-append-race).

### `EnemyState`

```go
EnemyState {
    EntityID        uint32
    Type            uint8
    X, Y            float32
    HP, MaxHP       int16
    AIState         enum { Idle, Patrol, Aggro, Dead }
    PatrolPath      []Vec2
    PatrolIndex     uint8
    AggroRange      float32
    AttackCooldown  time.Time
    Speed           float32
}
```

`HP int16` same as player; armor reduction arithmetic on `int16` is another half of [BUG-08](../features/combat-weapons.md#bug-08-hp-integer-overflow-healing-past-max).

### `DoorState`

```go
DoorState {
    DoorID          uint32
    X, Y            float32
    TargetRoom      uint8
    Unlocked        bool
}
```

No player-distance check on entry — see [../features/rooms-doors.md#bug-03-door-skip-no-proximity](../features/rooms-doors.md#bug-03-door-skip-no-proximity).

## Data flow (state lifecycle)

```
C2S_START_RUN (client)
    │
    ▼
gameLoop initializes RunState, PlayerState, RoomState
    │
    ▼
per tick:
    drain inCh (C2S_* packets mutate state via handlers)
    physics step (moves positions, advances AI)
    collision (projectile↔enemy, player↔pickup)
    death (removes dead enemies; spawns cleanup goroutines that append to KillLog)
    door unlock (AllEnemiesDead → Unlocked)
    timer (ElapsedMs updated)
    send S2C_STATE snapshot via outCh
    │
    ▼ (when CurrentRoom exceeds final room)
gameLoop builds CompletedRun, sends on completedRunCh
```

Splits accumulate in `RunState.Splits` on each room transition. `InputLog` appends every C2S packet after drain.

## How features register state

When adding a new feature that needs per-run, per-player, or per-room state:

1. **Prefer feature-local state.** If the state doesn't need to interact with existing systems, keep it in a new struct owned by the feature. Reference it from a thin field here (e.g., `ShopState *ShopState` on `PlayerState`).
2. **Only append to existing blocks** — never remove, rename, or re-type existing fields without checking which bugs depend on them.
3. **Name the field, then cite the feature.** Example when appending to `PlayerState`:

   ```go
   PlayerState {
       ...existing fields...
       NewField        int32      // owned by features/<feature>.md
   }
   ```

4. **If a new field has load-bearing type semantics** (e.g., `int16` width that enables overflow), call it out in this file and cross-link it to the relevant bug in the feature's spec.
5. **If the feature introduces cross-goroutine state access,** document the ownership explicitly. The default ownership rule (gameLoop is sole owner) must hold unless a new bug is deliberately being introduced.

## Implementation notes

- Use `map[uint32]*T` for entity-keyed collections so entity IDs are stable across ticks and `EntitySnapshot.entity_id` (see [02-protocol.md](./02-protocol.md)) is the canonical handle.
- Keep `InputRecord` small and cheap to serialize — it's logged for replay verification ([08-leaderboard-replay.md](./08-leaderboard-replay.md)).
- Do not pre-allocate `KillLog` with a mutex — that would fix [BUG-11](./05-server-runtime.md#bug-11-kill-log-append-race).

## Implementation status

Current `server/main.go` uses a single `gameState` struct with a flat field list (`X, Y, Health, CurrentRoom, Kills, Coins, OwnedWeapons, EquippedWeapon, PlayerName, Enemies, Timestamp`) and a top-level `playerSession` with a `sync.Mutex`. This differs substantially from the target model above. Migration is later work.

## Intentional bugs in this area

None own their body here — this file defines the types the bugs depend on. Full bug bodies live in:

- [BUG-08](../features/combat-weapons.md#bug-08-hp-integer-overflow-healing-past-max) — `HP, MaxHP int16`.
- [BUG-10](../features/inventory-items.md#bug-10-rune-vs-byte-item-length-bypass) — `Inventory []string`.
- [BUG-11](./05-server-runtime.md#bug-11-kill-log-append-race) — `RoomState.KillLog`.
- [BUG-04](../features/combat-weapons.md#bug-04-swap-cancel) / [BUG-07](../features/combat-weapons.md#bug-07-damage-batch-overflow) — `WeaponCooldown` same-tick behavior.

## Cross-references

- [02-protocol.md](./02-protocol.md) — snapshot shapes mirrored in S2C types.
- [05-server-runtime.md](./05-server-runtime.md) — goroutine ownership, tick phases.
- [07-determinism-seeding.md](./07-determinism-seeding.md) — `Seed`, `MobRNG`.
- [08-leaderboard-replay.md](./08-leaderboard-replay.md) — `InputLog`, `Splits`, `CompletedRun`.
