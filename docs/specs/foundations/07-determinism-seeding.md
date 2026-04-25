# 07 — Determinism & Seeding

## Purpose

Defines the single source of randomness (`MobRNG`), how it is seeded from the client's `start_time`, and the exact RNG consumption ordering across game phases. Determinism is an invariant — `(seed, InputLog)` must reproduce the same run byte-for-byte. It is also the foundation of [BUG-01 — Mob Seed Optimization](../features/mob-ai.md#bug-01-mob-seed-optimization), where players brute-force seeds to pre-compute favorable choreography.

## Seeding

- `Seed int64` comes from the `start_time` field of `C2S_START_RUN` (see [02-protocol.md](./02-protocol.md)).
- Server validates `|start_time − time.Now()| ≤ 30 min`. Outside this window the run is rejected.
- `RunState.MobRNG = rand.New(rand.NewSource(Seed))` is the single RNG for the entire run.

## What `MobRNG` drives

| What                | How RNG Is Used                                                                        |
| ------------------- | -------------------------------------------------------------------------------------- |
| Spawn positions     | Base position ± `MobRNG.Intn(100) - 50` px per axis                                    |
| Patrol paths        | 2–4 waypoints per enemy from `MobRNG`                                                  |
| Aggro jitter        | Initial chase vector offset by `MobRNG.Float32()`                                      |
| Attack timing       | First attack delay = base + `MobRNG.Intn(500)` ms                                      |
| Wave arm-delay      | Per declared wave: `MobRNG.Intn(2 * WaveArmJitterMs) - WaveArmJitterMs` ms (see [harder-mobs](../features/harder-mobs.md)) |
| Wave mob seeding    | Same 7-step enemy RNG block per wave mob, in wave declaration order                    |

Spawn offsets are bounded at ±50 px per axis. Patrol waypoints are clamped within the room bounds. Pack aggro (see [harder-mobs](../features/harder-mobs.md)) consumes no RNG.

## RNG consumption ordering (INVARIANT)

Determinism requires the order of RNG calls to be fixed. When the server loads a room:

1. **For each initial enemy in the room, in `entity_id` ascending order:**
   1. Consume 2 `MobRNG.Intn(100)` calls (spawn X offset, then Y offset).
   2. Consume 1 `MobRNG.Intn(3) + 2` call (patrol waypoint count, 2–4).
   3. For each patrol waypoint, consume 2 `MobRNG.Intn(...)` calls (X, Y).
   4. Consume 2 `MobRNG.Float32()` calls (aggro jitter X, Y).
   5. Consume 1 `MobRNG.Intn(500)` call (first-attack delay).
2. **For each declared wave in the room, in wave-slot order (0..N-1):**
   1. If `floor(InitialPop * WaveTriggers[slot]) < 1`: **skip — consume no RNG**.
   2. Otherwise consume 1 `MobRNG.Intn(2 * WaveArmJitterMs)` call (arm-delay jitter).
   3. For each mob in `WaveDef.Mobs` (in declaration order), consume the same 7-step block as step 1.i–1.v above. Wave mobs do not have pre-assigned `EntityID`s — IDs are allocated at fire time, so ordering follows declaration position rather than ID.

This order must never change. Changing it silently breaks existing leaderboard runs and invalidates every seed ever analyzed by a tool author.

Room-to-room ordering is by `room_index`. Early rooms' RNG consumption affects later rooms' patterns — this is intentional and part of the BUG-01 planning problem.

## Data flow

```
client C2S_START_RUN { start_time }
    │
    ▼
server validates ±30 min window
    │
    ▼
RunState.Seed = start_time
RunState.MobRNG = rand.New(rand.NewSource(Seed))
    │
    ▼
room load: consume RNG in the fixed order above → EnemyState population
    │
    ▼
tick physics: MobRNG NOT used per-tick (only at load time)
```

Important: per-tick mob behavior (current aim, movement decisions) must be derivable from `EnemyState` fields populated at load time — **no RNG is consumed per tick** during gameplay. Otherwise the replay engine can't reproduce the run from `(seed, InputLog)`.

## Implementation notes

- Use the stdlib `math/rand` with a fixed source — not `crypto/rand`.
- Ensure `rand.Seed` equivalents are NOT called globally anywhere else in the process; each run uses its own `*rand.Rand`.
- Generate the room's full choreography on room-load, cache it on `EnemyState`, and then reference those values during ticks.
- When migrating from any older RNG use, add a consumption-order test that records the first 20 values and fails CI if they change.

## Implementation status

Implemented. `RunState.MobRNG` is a `*rand.Rand` seeded from `start_time`, consumed at room load by `LoadRoom` in `server/internal/game/rooms.go` and `SeedMob` in `server/internal/game/mobai.go`. The wave-RNG extension (step 2 above) is consumed by `seedWaves` in `server/internal/game/waves.go`. The consumption order is locked by `TestLoadRoomRNGConsumptionInvariant` (`server/internal/game/waves_test.go`), which records eight post-load `Intn(1_000_000)` values against a known seed.

## Intentional bugs in this area

None own their body here. This file defines the invariants that [BUG-01](../features/mob-ai.md#bug-01-mob-seed-optimization) exploits from the outside (seed brute-forcing + routing over the resulting choreography).

## Cross-references

- [02-protocol.md](./02-protocol.md) — `C2S_START_RUN`, `S2C_RUN_STARTED { seed, room }`.
- [06-game-state-conventions.md](./06-game-state-conventions.md) — `RunState.Seed`, `RunState.MobRNG`, `EnemyState` fields populated from RNG.
- [08-leaderboard-replay.md](./08-leaderboard-replay.md) — `(seed, InputLog)` replay.
- [../features/mob-ai.md](../features/mob-ai.md) — [BUG-01](../features/mob-ai.md#bug-01-mob-seed-optimization) body.
- [../features/harder-mobs.md](../features/harder-mobs.md) — wave RNG extension, pack aggro (RNG-free).
