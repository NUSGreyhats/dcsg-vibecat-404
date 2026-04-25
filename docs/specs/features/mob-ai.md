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
| 3    | Tank     | 12 | 60    | 8         | 160         | 1200 ms         | Slow; high HP; armor enables BUG-08.     |
| 4    | Kamikaze | 1  | 145   | 15        | 220         | N/A (explodes)  | One-shot kills self on contact; damages player. |
| 5    | Boss     | 120 | 70   | 20        | 360         | 900 ms          | Room 11 only; armor + debuff ingress for BUG-08. |

HP and Speed are multiplied by a per-room tier factor at `InstantiateEnemy` time — see [harder-mobs.md § Per-room scaling](./harder-mobs.md#per-room-scaling). The numbers above are the **room 1–5 baseline**. Melee damage, AggroRange, Armor, and attack cooldown are **not** scaled. The active shield powerup halves incoming melee damage (integer divide, floor 1).

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

Per game tick (20 Hz), for each enemy:

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
    chase_vec = (player - self) + jitter
    move along chase_vec at Speed
    if time.Now() >= AttackCooldown && in_melee:
        deal damage; AttackCooldown = time.Now() + archetype.CooldownMs
case Dead:
    removed from Enemies map (death pass, not here)
}
```

**Note:** per-tick AI uses `jitter` values that were pre-computed at spawn time — no `MobRNG` call happens per tick. This is what lets the replay engine reproduce runs from `(seed, InputLog)`.

## Data flow

- `C2S_START_RUN { start_time }` → server seeds `MobRNG` → initial room enemies populated (see [../foundations/07-determinism-seeding.md](../foundations/07-determinism-seeding.md)).
- `S2C_RUN_STARTED { seed, room: RoomData }` delivers the first room's `EnemySpawn` list — client renders enemies at their seeded positions.
- On door entry, `S2C_ROOM_LOAD { room_index, room: RoomData, split_ms }` delivers next room's enemies (again RNG-driven, with the next N enemies consuming the next N RNG blocks in the fixed order).
- `S2C_STATE` broadcasts `EntitySnapshot`s each tick with updated X/Y and AIState.
- `S2C_ENTITY_DIED { entity_id }` sent when an enemy transitions to Dead.

## Implementation notes

- Package: `internal/game/mobai` (target).
- Enemy archetype table lives in a single Go file so adding a new archetype is a one-row change.
- Patrol path waypoint clamp: `wX = clamp(wX, minX, maxX)`, same for Y. Clamp after RNG consumption — never before — otherwise the consumption order silently depends on the clamp.
- Do not call `MobRNG` from any tick handler. All per-tick randomness must come from pre-computed `EnemyState` fields.

## Implementation status

Greenfield. Current server does not have mob AI or RNG-driven spawns — rooms are static Tiled JSON with no server-side enemies. Full pipeline is future work.

## Intentional bugs in this area

### BUG-01 Mob Seed Optimization

**Source:** The seed-driven choreography is by design; optimizing over it is a speedrunning art.

**Design choice enabling the bug:** `start_time` from `C2S_START_RUN` seeds `MobRNG`, validated only within ±30 minutes of `time.Now()`. That window admits millions of candidate seeds.

**Why it's hard (and skill-rewarding):** The seed determines **everything** — spawn positions, patrol waypoints, aggro jitter, attack delays — across all rooms. But knowing the choreography is only half the battle. The player must then **plan an optimal route through every room** for that specific choreography. This is a **specialized routing problem**:

- Which enemies to kill in what order given their specific positions.
- Optimal movement path through each room's unique spawn layout.
- Which patrol intercept windows to hit (enemies walking toward you vs. away).
- Attack timing alignment — when do favorable aggro jitter windows open.
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
