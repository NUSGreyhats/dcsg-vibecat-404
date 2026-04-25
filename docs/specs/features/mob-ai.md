# Feature — Mob AI

## Purpose

Enemy archetypes, their AI state machine (Idle / Patrol / Aggro / Dead), spawn-time RNG consumption, and per-tick AI updates that only use pre-computed state (so runs replay deterministically). Hosts [BUG-01 — Mob Seed Optimization](#bug-01-mob-seed-optimization), the flagship "systems tier" exploit where players brute-force seeds to find favorable choreography and then plan an optimal route through it.

## Data model

Lives in `RoomState.Enemies map[uint32]*EnemyState` — see [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md):

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

Enemy archetypes (per `Type`):

| Type | Name     | HP | Speed | Melee dmg | Aggro range | Attack cooldown | Notes                                    |
| ---- | -------- | -- | ----- | --------- | ----------- | --------------- | ---------------------------------------- |
| 1    | Grunt    | 3  | 95    | 5         | 200         | 800 ms          | Baseline zombie; patrols then aggros.    |
| 2    | Runner   | 2  | 165   | 4         | 260         | 600 ms          | Fast; short aggro window useful for planning. |
| 3    | Tank     | 12 | 60    | 8         | 160         | 1200 ms         | Slow; high HP.                           |
| 4    | Kamikaze | 1  | 145   | 15        | 220         | N/A (explodes)  | One-shot kills self on contact; damages player. |
| 5    | Boss     | 120 | 70   | 20        | 360         | 900 ms          | Room 11 only.                            |

HP and Speed are multiplied by a per-room tier factor at `InstantiateEnemy` time — see [harder-mobs.md § Per-room scaling](./harder-mobs.md#per-room-scaling). The numbers above are the **room 1–5 baseline**. Melee damage, AggroRange, and attack cooldown are **not** scaled. The active shield powerup halves incoming melee damage (integer divide, floor 1).

Exact numbers TBD — see open questions in [../../../SPECIFICATIONS.md](../../../SPECIFICATIONS.md). The table is the shape, not the final balance.

## Spawn pipeline (RNG-driven)

On room load, for each enemy in the room's `RoomData.enemies` list (ascending `entity_id`):

1. `offsetX := MobRNG.Intn(100) - 50`
2. `offsetY := MobRNG.Intn(100) - 50`
3. `waypointCount := MobRNG.Intn(3) + 2`  // 2–4
4. For each waypoint: `wX := MobRNG.Intn(roomWidth)`, `wY := MobRNG.Intn(roomHeight)`
5. `jitterX := MobRNG.Float32()`
6. `jitterY := MobRNG.Float32()`
7. `firstAttackDelay := MobRNG.Intn(500)` ms

Apply: `EnemyState.X = spawn.x + offsetX`, `.Y = spawn.y + offsetY`, `.PatrolPath = [...]`, `.AttackCooldown = time.Now() + firstAttackDelay * time.Millisecond`.

This consumption order is invariant — see [../foundations/07-determinism-seeding.md](../foundations/07-determinism-seeding.md).

## AI tick

Per game tick (20 Hz), once the [flow field](#aggro-chase-flow-field) for the room has been (re)built for this tick, for each enemy:

```
switch AIState {
case Idle:
    if distance(self, player) < AggroRange: AIState = Aggro
    else: AIState = Patrol  // after initial Idle frame
case Patrol:
    move toward PatrolPath[PatrolIndex] at Speed
    if reached: PatrolIndex = (PatrolIndex + 1) % len(PatrolPath)
    if distance(self, player) < AggroRange: AIState = Aggro
case Aggro:
    aim_tile = flowField.NextStep(self.tile, jitterBias)
    move toward center(aim_tile) at Speed   // pixel movement, axis-separated wall slide retained
    if time.Now() >= AttackCooldown && in_melee:
        deal damage; AttackCooldown = time.Now() + archetype.CooldownMs
case Dead:
    removed from Enemies map (death pass, not here)
}
```

**Note:** per-tick AI consumes no `MobRNG`. Pre-computed per-mob fields (`JitterX`, `JitterY`, `FirstAttackDelay`, `PatrolPath`) and the per-tick flow field are the only inputs. This is what lets the replay engine reproduce runs from `(seed, InputLog)`.

## Aggro chase: flow field

Aggro chase uses a tile-grid breadth-first search rooted at the player's current tile, recomputed once per tick. The result is a `distance[row][col]` array; mobs walk the gradient toward the player. This replaces an earlier "greedy direct-vector with axis-separated slide" formulation that could park mobs against concave wall geometry (see [issue #19](https://github.com/NUSGreyhats/DEFCON-Game-Jon/issues/19)).

### Per-tick computation

```
flowField := BFSFromTile(roomIndex, playerTileRow, playerTileCol)
    // 40×24 grid, 8-neighbourhood with unit-cost diagonals (Chebyshev metric),
    // walls (cellSolid + cellWater) impassable. Diagonal steps are forbidden
    // when either axis-aligned tile bracketing the corner is a wall, so mobs
    // never visually clip through a wall corner.
    // Distance in tile steps. Unreachable tiles = sentinel (uint16 max).
    // ~960 cell ops per tick — trivial cost.
```

The BFS runs once per room per tick, **before** any mob update for that tick reads it. It is owned by the per-tick game loop, not by individual mobs.

### Per-mob step

A mob in Aggro state computes its next aim point by:

1. Take its own tile `(r, c)`.
2. Optionally bias the *source* of the BFS by the mob's pre-computed jitter — see [Jitter handling](#jitter-handling) below.
3. Among the 8 neighbours (cardinal `(r±1, c)`, `(r, c±1)` and diagonal `(r±1, c±1)`), pick the one with the **lowest** `distance` value. Ties broken by a fixed clockwise enumeration order from N: **N, NE, E, SE, S, SW, W, NW**. Diagonal candidates are skipped when either axis-aligned tile bracketing the corner is a wall — same corner-cut rule the BFS applies.
4. Aim point = pixel-space center of the chosen neighbour tile.
5. Movement uses the existing pixel-precision `moveTowardWithWalls` with that aim — the axis-separated wall slide is retained for sub-tile motion smoothness, but it is no longer the primary obstacle-avoidance mechanism.

If all 8 neighbours have distance ≥ the mob's own (mob is at a local minimum, e.g. on the player's tile or in an unreachable pocket), the mob holds position.

### Jitter handling

Aggro jitter (`JitterX`, `JitterY`, both `[0, 1)` floats from spawn-time `MobRNG.Float32()`) historically perturbed the chase aim point by ±10 px. With a tile-resolution flow field the ±10 px perturbation is below tile size (32 px) and would be quantised away, collapsing one dimension of the [BUG-01](#bug-01-mob-seed-optimization) seed-search surface.

To preserve seed-search depth without per-tick RNG, jitter is reinterpreted as a **deterministic tile-bias on the BFS source**:

```
biasedPlayerTile := playerTile
if e.JitterX < 0.25: biasedPlayerTile.col -= 1
else if e.JitterX > 0.75: biasedPlayerTile.col += 1
if e.JitterY < 0.25: biasedPlayerTile.row -= 1
else if e.JitterY > 0.75: biasedPlayerTile.row += 1
// snap to nearest non-wall tile if biased target is impassable.
```

Each mob with a non-zero bias effectively reads a *separate* flow field rooted one tile off from the player. Implementation may either run multiple BFSes (still cheap — at most 9 distinct sources per tick) or compose distances from a single field plus the bias offset. The choice is internal; the spec only mandates the deterministic mapping above.

Mobs whose jitter falls in `[0.25, 0.75]` on both axes use the unbiased flow field — the common case.

### Determinism invariants

- Neighbour enumeration order in the BFS queue and in the per-mob gradient pick is fixed (clockwise from N): **N, NE, E, SE, S, SW, W, NW**. Diagonal steps are gated on corner-cut prevention — both axis-aligned tiles bracketing the corner must be non-wall.
- The BFS is a pure function of `(roomIndex, playerTile)` — wall grid is static per room, so the field for a given `(room, tile)` is byte-identical across runs.
- The per-mob jitter bias is pre-computed at spawn time from `MobRNG`; the tick handler never touches `MobRNG`.
- The BFS must run before mob updates *and* before any code that depends on mob position (death pass, projectile resolution) within the same tick.

These invariants combine to keep `(seed, InputLog)` reproducing the run byte-for-byte.

### Wall geometry

Flow field handles arbitrary wall topology — multi-tile slabs, U-shapes, L-shapes, alcoves, room dividers — correctly. The only failure case is a wall configuration that fully separates the player's reachable region from a mob's reachable region (no path exists at all); in that case the mob holds position, which is the correct behaviour. There is **no authoring constraint** on wall shape under this algorithm.

## Data flow

- `C2S_START_RUN { start_time }` → server seeds `MobRNG` → initial room enemies populated (see [../foundations/07-determinism-seeding.md](../foundations/07-determinism-seeding.md)).
- `S2C_RUN_STARTED { seed, room: RoomData }` delivers the first room's `EnemySpawn` list — client renders enemies at their seeded positions.
- On door entry, `S2C_ROOM_LOAD { room_index, room: RoomData, split_ms }` delivers next room's enemies (again RNG-driven, with the next N enemies consuming the next N RNG blocks in the fixed order).
- `S2C_STATE` broadcasts `EntitySnapshot`s each tick with updated X/Y and AIState.
- `S2C_ENTITY_DIED { entity_id }` sent when an enemy transitions to Dead.

## Implementation notes

- Package: `internal/game/mobai` (target). Flow-field code lives alongside in a sibling file (`mobai_flowfield.go`) — the wall grid it consumes is already in `walls.go`.
- Enemy archetype table lives in a single Go file so adding a new archetype is a one-row change.
- Patrol path waypoint clamp: `wX = clamp(wX, minX, maxX)`, same for Y. Clamp after RNG consumption — never before — otherwise the consumption order silently depends on the clamp.
- Do not call `MobRNG` from any tick handler. All per-tick randomness must come from pre-computed `EnemyState` fields and the deterministic per-tick flow field.
- The flow field is computed once per room per tick in the loop, before any mob update. It is owned by the loop, not by individual mobs. See [Aggro chase: flow field](#aggro-chase-flow-field).

## Implementation status

Mob AI and RNG-driven spawns are implemented (waves, pack aggro, per-room scaling all live). Aggro chase currently uses greedy direct-vector pursuit with axis-separated wall sliding (`moveTowardWithWalls` in `server/internal/game/mobai.go`); the flow-field replacement specified above is **planned** in response to [issue #19](https://github.com/NUSGreyhats/DEFCON-Game-Jon/issues/19) (mobs parking against concave wall geometry). Until the flow field lands, the existing chase remains in place and rooms with multi-tile-thick walls or boundary-anchored wall structures may exhibit stuck mobs.

## Intentional bugs in this area

### BUG-01 Mob Seed Optimization

**Source:** The seed-driven choreography is by design; optimizing over it is a speedrunning art.

**Design choice enabling the bug:** `start_time` from `C2S_START_RUN` seeds `MobRNG`, validated only within ±30 minutes of `time.Now()`. That window admits millions of candidate seeds.

**Why it's hard (and skill-rewarding):** The seed determines **everything** — spawn positions, patrol waypoints, aggro jitter, attack delays — across all rooms. But knowing the choreography is only half the battle. The player must then **plan an optimal route through every room** for that specific choreography. This is a **specialized routing problem**:

- Which enemies to kill in what order given their specific positions.
- Optimal movement path through each room's unique spawn layout.
- Which patrol intercept windows to hit (enemies walking toward you vs. away).
- Attack timing alignment — when do favorable aggro-chase tile-bias windows open (jitter biases each mob's flow-field source by 0 or ±1 tile per axis; see [Jitter handling](#jitter-handling)).
- Multi-room planning — early rooms' RNG consumption affects later rooms' patterns.

A brute-force seed search might evaluate millions of candidates, but **scoring each seed requires simulating the full routing problem** — it's not just "find good clusters," it's "find the seed whose choreography admits the fastest possible multi-room route." This rewards both **tooling sophistication** (better solvers, smarter heuristics) and **game knowledge** (understanding room geometry, weapon DPS windows, door positions).

**The skill ceiling is very high:** Two teams with the same seed can have very different times depending on their routing quality. The seed is visible on the leaderboard — competitors can study each other's seeds but still need to independently solve the routing.

**Exploit path:** Offline — implement a server-equivalent RNG consumer, iterate candidate seeds within the ±30 min window, simulate each against internal room templates, score by lower-bound completion time. Pick the best seed and use it as the `start_time` in `C2S_START_RUN`.

**Bound:** Spawn offset ±50 px per axis. Patrol paths bounded within room. ±30 min validation window.

**Detection:** None — this is the intended skill mechanic. Leaderboard displays the seed publicly.

**Synergies:**

- Spawn positions enable [BUG-06 settle race](../foundations/05-server-runtime.md#bug-06-concurrent-room-settle-race) predictions.
- Enemy clustering enables [BUG-07 damage burst](combat-weapons.md#bug-07-damage-batch-overflow).
- Simultaneous kills from cluster fire enable [BUG-11 kill log race](../foundations/05-server-runtime.md#bug-11-kill-log-append-race).

See [../bugs.md](../bugs.md#synergy-map).

**Savings:** ~3–5 s per room (routing-dependent — skilled routers save more).

## Packets used

- `C2S_START_RUN` (seeds RNG).
- `S2C_RUN_STARTED`, `S2C_ROOM_LOAD` (deliver `EnemySpawn` list).
- `S2C_STATE` (per-tick `EntitySnapshot`).
- `S2C_ENTITY_DIED`.

## Cross-references

- [../foundations/02-protocol.md](../foundations/02-protocol.md) — `EnemySpawn`, `EntitySnapshot`.
- [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md) — `EnemyState` block.
- [../foundations/07-determinism-seeding.md](../foundations/07-determinism-seeding.md) — RNG consumption invariant.
- [../foundations/05-server-runtime.md](../foundations/05-server-runtime.md) — BUG-06 settle race, BUG-11 kill log.
- [combat-weapons.md](combat-weapons.md) — BUG-07 damage batch.
- [harder-mobs.md](harder-mobs.md) — reinforcement waves and pack aggro; extends this spec's spawn pipeline and BUG-01 seed-search surface.
