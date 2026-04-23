# 05 — Server Runtime

## Purpose

The game server's concurrency model: per-player goroutines, the 20 Hz game loop, channel sizes, the semaphore that caps concurrent players, the `ts` validation policy, the post-door settle window, and the kill-log cleanup goroutines. Three intentional bugs live here because they are all artifacts of *how data moves between goroutines and across a single tick*.

## Goroutine architecture

```mermaid
graph TD
    subgraph "Game Server Process"
        ACC[Accept Loop<br/>ws.Upgrade per conn]
        SEM["Semaphore<br/>chan struct{} size MAX_CONCURRENT"]

        subgraph "Player 1"
            RD1[connReader] -->|"inCh (buf 64)"| GL1[gameLoop 20Hz]
            GL1 -->|"outCh (buf 64)"| WR1[connWriter]
        end

        subgraph "Player N"
            RDN[connReader] -->|inCh| GLN[gameLoop 20Hz]
            GLN -->|outCh| WRN[connWriter]
        end

        LS[Leaderboard Service<br/>single goroutine]
        GDB[(game.db)]

        ACC -->|"acquire semaphore"| RD1 & GL1
        ACC -->|"acquire semaphore"| RDN & GLN
        GL1 & GLN -->|"completedRunCh (buf 128)"| LS
        LS --> GDB
    end

    style GL1 fill:#2ecc71,color:#fff
    style GLN fill:#2ecc71,color:#fff
    style LS fill:#3498db,color:#fff
    style SEM fill:#e74c3c,color:#fff
```

## Goroutine responsibilities

| Goroutine       | Owns                                           | Lifecycle       |
| --------------- | ---------------------------------------------- | --------------- |
| **connReader**  | Nothing — stateless decoder                    | Per connection  |
| **gameLoop**    | All player + room state (sole owner, no mutex) | Per connection  |
| **connWriter**  | Nothing — stateless encoder                    | Per connection  |
| **leaderboard** | Rankings, DB writes, broadcast                 | Server lifetime |

No shared game state between players. The only cross-player touchpoint is `completedRunCh` → leaderboard.

The **semaphore** (`chan struct{}` sized `MAX_CONCURRENT`) gates the accept loop — unlimited in dev/test, `1` in prod.

## Channels & sizes

| Channel           | Direction                    | Size |
| ----------------- | ---------------------------- | ---- |
| `inCh`            | connReader → gameLoop        | 64   |
| `outCh`           | gameLoop → connWriter        | 64   |
| `completedRunCh`  | gameLoop(s) → leaderboard    | 128  |

## Game loop tick (20 Hz)

```mermaid
graph TD
    TICK["Ticker fires every 50ms"] --> DRAIN["Drain inCh — process all queued client packets"]
    DRAIN --> PHYSICS["Physics step: move player, projectiles, mob AI"]
    PHYSICS --> COLLISION["Collision: projectile↔enemy, player↔pickup"]
    COLLISION --> DEATH["Death check: remove dead, check room clear"]
    DEATH --> DOOR["Door unlock: all enemies dead → Unlocked"]
    DOOR --> TIMER["Update elapsed: time.Now() − runStart"]
    TIMER --> SEND["Build S2C_STATE → outCh"]

    style DRAIN fill:#e74c3c,color:#fff
    style SEND fill:#2ecc71,color:#fff
```

- 50 ms ticker fires → drain `inCh` of all queued C2S packets → physics → collision → death → door → timer → send.
- All logic runs on a single goroutine per player. No mutex guards player/room state — the goroutine is the sole owner.

## `ts` validation

Each incoming C2S packet has a `ts` field (client wallclock ms). The game server:

- Validates `|ts − time.Now()| ≤ 2 s` per packet (absolute window).
- Uses `ts − prev_ts` as the physics `deltaTime` integration step.
- **Does NOT track cumulative drift** — i.e., there's no rate-of-progression check that compares the sum of `deltaTime` values against wallclock.

This is the design choice that enables BUG-02.

## Post-door settle window

When a player enters a door and the server transitions the room:

- Spawn the new room's enemies at their base positions.
- Run a **2-tick settle** during which the new enemy positions stabilize (AI initializes, patrol/aggro jitter applied from RNG).
- **`connReader` does NOT block during the settle** — packets continue to flow into `inCh` and get drained on the next tick.

This is the design choice that enables BUG-06.

## Kill log ownership (cleanup goroutines)

When multiple projectiles kill enemies in the same tick, the death processing **spawns per-enemy cleanup goroutines** for visual effects (particle systems, loot drops). These goroutines append to a shared `KillLog` slice **without synchronization**:

```go
// Called from collision pass — one goroutine per dead enemy
func (r *RoomState) processEnemyDeath(entityID uint32) {
    // ... cleanup, spawn particles, etc.

    // BUG: concurrent append to shared slice — no mutex
    r.KillLog = append(r.KillLog, KillRecord{
        EntityID:  entityID,
        Timestamp: time.Now(),
    })
}
```

The server also runs a **reconciliation pass** every 10 ticks (500 ms) that recounts dead enemies and force-unlocks doors if the kill log diverged from reality.

This is the design choice that enables BUG-11.

**IMPORTANT:** Rule from [06-game-state-conventions.md](./06-game-state-conventions.md) says `gameLoop` is the sole owner of player+room state — any cross-goroutine access must go through a channel. The cleanup-goroutine append **intentionally violates** this rule.

## Implementation notes

- Accept loop in `cmd/server/main.go` performs `ws.Upgrade`, then `sem <- struct{}{}` to claim a slot; on disconnect releases.
- Per-connection wiring: `go connReader(conn, inCh)` / `go connWriter(conn, outCh)` / `go gameLoop(inCh, outCh, completedRunCh)`.
- Ticker: `time.NewTicker(50 * time.Millisecond)` in `gameLoop`.
- `MAX_CONCURRENT` read from env at startup; default 1 in prod, unlimited (e.g. 64) in dev/test.

## Implementation status

Current `server/main.go`:

- Uses a single `handleGameWS` goroutine per connection with `session.validate(...)` called inline — **not** the three-goroutine split described here.
- No explicit 20 Hz ticker — the loop is purely reactive to incoming packets.
- `playerSession` holds a `sync.Mutex` — whereas the spec says gameLoop owns state with no mutex.
- No `completedRunCh` — leaderboard writes via `appendWinner()` with `leaderMu`.

Migration to the goroutine model is later work. For now, the spec is the target.

## Intentional bugs in this area

### BUG-02 Drift Tick Stretching

**Design choice enabling the bug:** per-packet absolute `ts` validation with no cumulative drift check, and `ts` used directly as physics `deltaTime`.

**Root cause sketch:** `deltaTime := ts - prev_ts` fed into the physics integrator. Because the check is `|ts − wallclock| ≤ 2 s` per packet and nothing else, a client can advance `ts` slightly faster than real time — say 10–15 % faster — and the server's physics will run that much faster.

**Exploit path:** Client smoothly inflates `ts` increments. Over the run, player movement, projectile travel, and mob AI all step faster.

**Bound:** Cumulative drift > 2 s invalidates the run (the absolute window catches over-eager stretching). Sustainable zone is ~15–20 %.

**Detection:** Leaderboard replay re-runs the InputLog with server-authoritative time. Any detectable mismatch between InputLog and recorded `total_ms` fails verification. Do NOT add cumulative-drift checks to the live path — the exploit is the point.

**Synergies:** Acts as a multiplier on every other time savings (BUG-01, BUG-03, BUG-06). See [../bugs.md](../bugs.md#synergy-map).

**Savings:** ~15–20 % of total run time.

**See also:** [BUG-06](#bug-06-concurrent-room-settle-race), [BUG-11](#bug-11-kill-log-append-race).

---

### BUG-06 Concurrent Room Settle Race

**Design choice enabling the bug:** `connReader` does not block during the 2-tick settle window after a room transition. Packets queued during settle are drained on the next tick and processed against already-spawned enemies at their **initial** (pre-jitter) positions.

**Root cause sketch:** Between door-enter and settle-complete, enemies exist at their base positions and `inCh` is still accepting C2S packets. Any `C2S_SHOOT` fired while the player still sees the previous room arrives at the new room's handler with no guard.

**Exploit path:** Client knows (from BUG-01 seed planning) where enemies will spawn in the new room. Immediately after sending `C2S_ENTER_DOOR`, the client bursts `C2S_SHOOT` packets aimed at the predicted initial positions. Those shots resolve against the freshly-spawned enemies before the player sprite has even visually moved.

**Bound:** 2-tick window (100 ms). Only the first few shots land; subsequent fire-rate cooldown kicks in. Most useful when seed + routing (BUG-01) predicts tight clusters near doorways.

**Detection:** The InputLog replay will reproduce the same behavior — this bug is deterministic given seed + InputLog, so it does NOT fail replay verification. This is intentional.

**Synergies:** Requires seed knowledge ([BUG-01](../features/mob-ai.md#bug-01-mob-seed-optimization)) for reliable target prediction. Composes with same-tick damage batching ([BUG-07](../features/combat-weapons.md#bug-07-damage-batch-overflow)) for higher kills per burst.

**Savings:** ~3–5 s per room.

---

### BUG-11 Kill Log Append Race

**Source:** [100 Go Mistakes #69 — Creating data races with append](https://100go.co/#creating-data-races-with-append-69). In Go, concurrent `append` to a shared slice is **not safe** — it causes silent data loss (not a panic), where elements are overwritten or lost.

**Design choice enabling the bug:**

1. Per-enemy cleanup goroutines spawned from the collision pass append to `r.KillLog` without synchronization.
2. Room-clear check uses `len(r.KillLog) >= len(r.Enemies)`.
3. Reconciliation pass runs every 10 ticks (500 ms) to force-unlock doors if the count diverges from actual dead enemies.
4. Room bonus is computed from `KillLog[0].Timestamp` vs `KillLog[len-1].Timestamp`.

**Root cause sketch:**

```go
// Room clear check
func (r *RoomState) checkRoomClear() bool {
    return len(r.KillLog) >= len(r.Enemies)
}

// Reconciliation (runs every 10 ticks)
func (r *RoomState) reconcileKillLog() {
    actualDead := 0
    for _, e := range r.Enemies {
        if e.HP <= 0 {
            actualDead++
        }
    }
    if actualDead >= len(r.Enemies) && !r.AllEnemiesDead {
        r.AllEnemiesDead = true
        // force unlock doors
    }
}

// Room bonus (time savings from fast clears)
func computeRoomBonus(log []KillRecord) int64 {
    if len(log) == 0 {
        return 0
    }
    duration := log[len(log)-1].Timestamp.Sub(log[0].Timestamp)
    if duration < 2*time.Second {
        return -500  // 500ms bonus (subtracted from room time)
    }
    return 0
}
```

Append in Go is three operations: read length → write element at index → update length. Two goroutines can read the same length, write the same index (one clobbers the other), and both increment length → lost kill record + duplicate/garbage entry.

**Exploit path:**

1. **Happy-path softlock avoidance:** If the player kills enemies one at a time, the log stays accurate and the door unlocks immediately.
2. **Race-for-negative-bonus:** Using [BUG-07](../features/combat-weapons.md#bug-07-damage-batch-overflow) to kill 3+ enemies on the same tick triggers the race. A garbage entry with zero-value `time.Time{}` can land in the last position, producing a **negative** duration in `computeRoomBonus` → underflow → massive time subtraction.

**Bound:**

- Non-deterministic. Players who understand Go's slice internals can bias timing to improve odds.
- Softlock recovery: reconciliation pass always unlocks within 500 ms, so worst case is a 500 ms delay (not a hung run).
- Replay system must tolerate the non-determinism — see [08-leaderboard-replay.md](./08-leaderboard-replay.md).

**Detection:** CI `go test -race` is intentionally disabled for the file containing `processEnemyDeath` via `//go:build !norace` build tag. See [09-ci-cd-deploy.md](./09-ci-cd-deploy.md) for the build-tag strategy. Do NOT add a mutex.

**Synergies:** Requires simultaneous kills ([BUG-07](../features/combat-weapons.md#bug-07-damage-batch-overflow) damage burst) and favorable spawn positions ([BUG-01](../features/mob-ai.md#bug-01-mob-seed-optimization)). See [../bugs.md](../bugs.md#synergy-map).

**Savings:** ~5–8 s total across a run (when corruption is favorable).

## Cross-references

- [02-protocol.md](./02-protocol.md) — envelope, dispatch, drain-phase inputs.
- [06-game-state-conventions.md](./06-game-state-conventions.md) — `RoomState.KillLog`, ownership rules.
- [07-determinism-seeding.md](./07-determinism-seeding.md) — RNG use during settle.
- [08-leaderboard-replay.md](./08-leaderboard-replay.md) — `completedRunCh` consumer, replay semantics for non-deterministic bugs.
- [09-ci-cd-deploy.md](./09-ci-cd-deploy.md) — build tags that exclude the race file from `-race`.
- [../features/combat-weapons.md](../features/combat-weapons.md) — same-tick damage batching (BUG-07).
- [../features/mob-ai.md](../features/mob-ai.md) — seed-driven spawn positions (BUG-01).
