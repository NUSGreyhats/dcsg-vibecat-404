# Feature — Harder Mobs

## Purpose

Three interlocking systems that raise the difficulty ceiling of room combat without breaking determinism:

1. **Reinforcement waves** — template-declared mob groups that arm on a kill-count threshold and fire after a jittered delay.
2. **Pack aggro** — when any mob takes damage, nearby non-aggro mobs flip to Aggro. Kills the "pick off the straggler" and stealth-pull strategies.
3. **Bumped room populations** — selective increases in base populations on under-threatening rooms.

All three compose with existing [mob AI](./mob-ai.md) and preserve the `(seed, InputLog)` replay invariant in [../foundations/07-determinism-seeding.md](../foundations/07-determinism-seeding.md). Waves extend the [BUG-01](./mob-ai.md#bug-01-mob-seed-optimization) seed-search surface (now includes arm-delay windows) without changing its core shape.

## Global constants

Lives in the mob-AI package. Named constants — never inlined.

```go
// WaveTriggers: ordered relative thresholds (fraction of initial room
// population) at which each wave arms. `WaveTriggers[i]` corresponds to
// wave slot i in a room's template. A room may declare 0..len(WaveTriggers)
// waves.
WaveTriggers = []float32{0.66, 0.33}  // wave 1 at ≤66% living, wave 2 at ≤33%

// WaveArmDelayMs: base delay from arm → fire.
WaveArmDelayMs = 3000

// WaveArmJitterMs: ± jitter applied to the arm→fire delay.
// Consumes 1 MobRNG.Intn(2*WaveArmJitterMs) per declared wave at room load.
WaveArmJitterMs = 3000

// PackAggroRadius: pixel radius for on-damage aggro broadcast.
// No LOS check — "they heard you."
PackAggroRadius = 250.0
```

Changing any of these is a balance change. Changing `WaveArmJitterMs` is **also** a determinism change — old seeds invalidated.

## Data model

### Room template additions

```go
RoomData {
    room_index  uint8
    tilemap_id  string
    enemies     []EnemySpawn   // initial population (unchanged)
    waves       []WaveDef      // NEW — 0..len(WaveTriggers) entries
    pickups     []PickupSpawn
    doors       []DoorDef
}

WaveDef {
    anchors []EnemySpawn   // same shape as initial enemies
}
```

Wave slot `i` uses `WaveTriggers[i]`. A room with only 1 wave uses `WaveTriggers[0]`. Rooms may declare 0 waves.

### Runtime state additions

`RoomState` (see [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md)):

```go
RoomState {
    // ... existing fields ...
    InitialPop    int             // NEW — len(enemies) captured at room load
    PendingWaves  []PendingWave   // NEW — one entry per declared wave
}

PendingWave {
    TriggerRemaining int             // floor(InitialPop * WaveTriggers[i])
    ArmDelayJitter   int             // ms, pre-computed at room load from MobRNG
    Mobs             []PrecomputedMob // fully seeded mob data ready to instantiate
    Armed            bool            // latched true once threshold crossed
    FireAt           time.Time       // set when Armed flips true
    Fired            bool            // latched true once mobs instantiated
}

PrecomputedMob {
    Type             uint8
    BaseX, BaseY     float32         // from template anchor
    OffsetX, OffsetY float32         // from MobRNG
    PatrolPath       []Vec2
    JitterX, JitterY float32
    FirstAttackDelay int              // ms
}
```

`PendingWave.Mobs` is fully populated at room load — same RNG recipe as initial enemies. The wave only defers **instantiation into `RoomState.Enemies`**, not the RNG consumption.

## Wave pipeline

### Threshold math

At room load: `TriggerRemaining = floor(InitialPop * WaveTriggers[i])`.

- If `TriggerRemaining < 1`, the wave would only arm on full room clear → **skip the wave** (do not consume its RNG either; see consumption order below). Room designers should author wave counts relative to initial pop.
- Example: 6-mob room, triggers [0.66, 0.33] → waves arm at ≤3 and ≤1 living.
- Example: 3-mob room, same triggers → wave 1 at ≤1 living; wave 2 skipped (`floor(0.99) = 0`).

### Arm

On every tick (after the damage/death pass that updates `living_count`):

```go
for i := range r.PendingWaves {
    w := &r.PendingWaves[i]
    if w.Armed || w.Fired {
        continue
    }
    if livingCount(r) <= w.TriggerRemaining {
        w.Armed = true
        w.FireAt = now.Add(
            time.Duration(WaveArmDelayMs+w.ArmDelayJitter) * time.Millisecond)
    }
}
```

**One-way latch**: once `Armed = true`, it stays armed regardless of subsequent `livingCount` changes (including those caused by this wave's own spawns).

### Fire

```go
for i := range r.PendingWaves {
    w := &r.PendingWaves[i]
    if !w.Armed || w.Fired {
        continue
    }
    if !now.Before(w.FireAt) {
        for _, m := range w.Mobs {
            instantiateEnemy(r, m)  // copy into RoomState.Enemies with new EntityID
        }
        w.Fired = true
    }
}
```

Wave mobs enter the usual AI state machine from `Idle`; pack aggro and normal aggro-range apply immediately. Wave mobs count toward the room's `AllEnemiesDead` gate — doors only unlock after **all** waves have fired and their mobs are dead.

### Multiple waves arming in one tick

If a kill crosses multiple thresholds in a single tick (e.g., a cluster-fire kill drops living count from 4 to 1), each wave arms independently with its own `FireAt`. This is intentional — rewards high-impact bursts with correspondingly layered reinforcements.

## RNG consumption (INVARIANT addition)

Append to the ordering in [../foundations/07-determinism-seeding.md](../foundations/07-determinism-seeding.md). On room load, after all initial enemies are seeded:

```
for each declared wave i in 0..N:
    if floor(InitialPop * WaveTriggers[i]) < 1:
        skip this wave — NO RNG consumed
        continue
    consume 1 MobRNG.Intn(2 * WaveArmJitterMs) call  // arm-delay jitter
    store as ArmDelayJitter = result - WaveArmJitterMs
    for each mob in wave.anchors (ascending declaration order):
        consume the full 7-step enemy RNG sequence
        (offsetX, offsetY, waypointCount, waypoints, jitterXY, firstAttackDelay)
```

Skipped waves consume nothing. Declaration order within a wave is the mob author's ordering in `WaveDef.anchors`. Wave mobs are **not** entity-ID ordered at RNG time because they don't have entity IDs yet — IDs are assigned at fire time.

## Pack aggro

Triggered on any damage event against a mob — projectile hit, melee, kamikaze splash, boss AoE, etc. Called from whichever site applies the damage, immediately after HP is decremented but before the death check.

```go
const PackAggroRadius = 250.0

func broadcastPackAggro(r *RoomState, victim *EnemyState) {
    vx, vy := victim.X, victim.Y
    for _, other := range r.Enemies {
        if other.EntityID == victim.EntityID {
            continue
        }
        if other.AIState == Dead || other.AIState == Aggro {
            continue
        }
        dx, dy := other.X-vx, other.Y-vy
        if dx*dx+dy*dy <= PackAggroRadius*PackAggroRadius {
            other.AIState = Aggro
        }
    }
}
```

- **No LOS.** Walls don't block; the signal is "heard," not "seen."
- **No RNG consumed.** Pure reactive state change.
- **State flip only.** Once a mob is Aggro'd (via any path — range, pack, or prior), pack aggro is a no-op for it.
- **The victim itself** is not special-cased — it will also flip to Aggro in the normal tick that follows (chase_vec uses the attacker, typically the player).

## Data flow

- `S2C_ROOM_LOAD` now carries `RoomData.waves`. Client caches anchors for optional telegraph rendering.
- Wave firing broadcasts the new mobs via the next `S2C_STATE` tick (new `EntitySnapshot`s with fresh entity IDs). Clients render appearing entity IDs as spawns.
- **Optional protocol addition** (deferred): `S2C_WAVE_ARMED { wave_index, fire_at }` to let clients play a pre-spawn telegraph (glow at anchors, warning SFX). Not required for correctness — the state snapshot remains authoritative.
- Pack aggro produces no new packets; aggro state flips surface in the next `EntitySnapshot`'s `AIState` field.

## Client interactions

- `roomLayout.ts` — consume `RoomData.waves` into a deferred-spawn list for telegraphing.
- Wave mob sprites should appear with a brief spawn-in animation so players can distinguish "always was here" from "just arrived."
- No client logic arms or fires waves; the client only reacts to server state.

## Implementation notes

- Package: `internal/game/mobai` (extend) and `internal/game/rooms` (extend).
- Keep `instantiateEnemy` as a single helper used by both initial-pop seeding and wave fire, so the `EnemyState` shape is identical regardless of origin.
- Wave RNG consumption at room load must happen **after** all initial enemies are seeded and **in wave declaration order**, including the skip rule. Unit test: record the first N RNG values for each canned room config and lock them against regression.
- Pack aggro is called from exactly three sites: projectile-enemy collision, kamikaze splash, and boss AoE. Audit damage paths when adding any new damage source.
- `livingCount(r)` counts only initial + already-fired wave mobs. Pending (unfired) mobs don't count — otherwise `TriggerRemaining` math is wrong.
- Never call `MobRNG` from a tick handler — waves arm and fire off deterministic clock + kill input, not RNG.

## Implementation status

Greenfield. Current server has no reinforcement logic and no pack aggro. Current `rooms_data.go` has base `enemies` only; `waves` field and per-room wave declarations are new work on `feat/harder-mobs`.

## Intentional bugs in this area

No new intentional bugs. This spec **enriches** [BUG-01 — Mob Seed Optimization](./mob-ai.md#bug-01-mob-seed-optimization):

- Seed analysis now includes arm-delay windows per wave. "Favorable seeds" are ones where wave-mob positions and arm timings align with the player's planned route.
- The wave mob RNG blocks extend the per-room consumption budget, increasing the depth of the seed-search game.
- Pack aggro itself is deterministic (no RNG) — it does not expand the seed-search surface but does change what "good routing" looks like: routes that concentrate damage are punished; routes that isolate kills with spatial separation are rewarded.

## Packets used

- `S2C_ROOM_LOAD` — extended with `waves` in `RoomData`.
- `S2C_STATE` — carries wave mob `EntitySnapshot`s once fired.
- `S2C_ENTITY_DIED` — unchanged; applies to wave mobs same as initial.
- `S2C_WAVE_ARMED` (optional, deferred) — pre-fire telegraph.

## Cross-references

- [mob-ai.md](./mob-ai.md) — archetype table, base spawn pipeline, AI tick, BUG-01.
- [rooms-doors.md](./rooms-doors.md) — `RoomData` shape, `AllEnemiesDead` gate.
- [../foundations/07-determinism-seeding.md](../foundations/07-determinism-seeding.md) — RNG consumption ordering (extended by this spec).
- [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md) — `RoomState` block (extended by this spec).
- [combat-weapons.md](./combat-weapons.md) — damage sites that call `broadcastPackAggro`.
