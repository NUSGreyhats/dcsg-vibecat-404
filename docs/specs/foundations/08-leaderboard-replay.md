# 08 — Leaderboard & Replay

## Purpose

Server-authoritative leaderboard + tamper-proof replay verification. Every completed run stores its full `InputLog`; the replay engine can re-run `(seed, InputLog)` and must produce the same final time. The leaderboard is the anticheat backstop that makes all the intentional bugs safe to ship — a "cheat" only counts if it reproduces from replay.

## Deployment toggle

`LEADERBOARD_ENABLED` (default `true`) gates this entire pipeline. When
set to `false`:

- **Game server** does not open `game.db`, does not start the
  leaderboard goroutine, and passes `nil` submit callbacks into the
  engine. Completed runs therefore produce no database row and no
  `S2C_LEADERBOARD` broadcast when the leaderboard is disabled. The
  `/ws/replay` route is not registered (404 on upgrade rather than
  503).
- **Lobby** does not open `game.db`, does not run admin schema
  bootstrap, and returns `503 leaderboard disabled` on every public
  and admin route that reads `completed_runs` / `flagged_runs`. See
  the enumeration in [04-lobby.md](./04-lobby.md#disabled-leaderboard-deployments).
- **Client** reads `/api/config` once at boot. When
  `leaderboard_enabled: false`, the win screen replaces the right-hand
  leaderboard pane with a configurable booth CTA whose text comes from
  `leaderboard_disabled_message` (env var
  `LEADERBOARD_DISABLED_MESSAGE`, default `"Please visit our booth to
  record an official attempt"`).

This posture targets event deployments that record official attempts in
person, training environments without prizes, and any deploy where
collecting `InputLog` blobs is not desirable. `LEADERBOARD_ENABLED=true`
keeps the original behavior described below.

## `CompletedRun`

```go
CompletedRun {
    ReplayID        string
    PlayerID        string
    DisplayName     string
    TotalMs         int64
    Splits          []SplitData
    Seed            int64
    InputLog        []InputRecord
    Checksum        [32]byte
    SubmittedAt     time.Time
}
```

- `ReplayID` is a server-generated UUID.
- `Checksum = SHA256(seed || serialize(InputLog))`. Computed at submission and stored; the replay endpoint recomputes to detect tampering on `game.db` at rest.
- `InputLog` is the full ordered sequence of C2S packets (after drain, within each tick). Every C2S type code is recorded verbatim — including `C2S_INTERACT` (whose payload carries `claimed_type`, load-bearing for [BUG-12](../features/shop.md#bug-12-pickup-type-confusion)) and `C2S_SHOP_PURCHASE` (`item_id`).

## Replay engine

Given `(seed, InputLog)`:

1. Construct a fresh `RunState` with `Seed` and `MobRNG` per [07-determinism-seeding.md](./07-determinism-seeding.md).
2. Replay each `InputRecord` in order, running the same 20 Hz tick loop as live gameplay (see [05-server-runtime.md](./05-server-runtime.md)).
3. Record the reproduced final `total_ms`. It must match `CompletedRun.TotalMs` to within replay tolerance (nanosecond-level — they should be exactly equal for deterministic bugs).

**Non-deterministic bugs** (specifically [BUG-11](./05-server-runtime.md#bug-11-kill-log-append-race)) violate byte-exact reproduction. The replay engine accommodates this by accepting runs whose reproduced time is within a tolerance window derived from known race bounds. The exact tolerance is TBD — see open questions in [../../../SPECIFICATIONS.md](../../../SPECIFICATIONS.md).

**Coin pickup ID permutation** (multi-kill ticks, see [../features/shop.md](../features/shop.md)) is observationally a no-op for replay: every coin's `applyPickup` effect is identical (`+1 Coin`) and dispatch ignores the underlying `pk.Type` (BUG-12), so swapping which `EntityID` the death pass assigns to which dropped coin does not change player state. This invariant relies on coins being interchangeable in dispatch — if a future feature differentiates coins (e.g., per-coin value), the replay engine will need to enforce a deterministic iteration order in the death pass at `combat.go`.

**Wall-clock vs. synthetic time.** The live engine drives `stepTick` with real `time.Now()`, which means recorded `total_ms` (and `Splits[*].time_ms`) reflect tick-cadence jitter. Replay should advance `now` synthetically by `TickDuration` per tick anchored on a synthetic `RunStartTime`, and skip the `±2s` `ts` window check in `dispatch` — the recorded `Ts` values come from the original session's wall-clock and would otherwise reject every packet. All time math in the runtime is relative (`now.Add(...)`, `now.Sub(RunStartTime)`, `now.After(...)`), so this is sound.

## Data flow

```
gameLoop (per player)
    │ appends to InputLog each tick (post-drain)
    │ on run completion, builds CompletedRun + checksum
    ▼
completedRunCh (buf 128)
    ▼
leaderboard goroutine (single, server-lifetime)
    │ INSERT INTO completed_runs
    │ recompute rank
    ▼
game.db (SQLite)
    ▼ (broadcast)
S2C_LEADERBOARD to all connected players
```

Only runs with `Status == RunCompleted` are submitted to `completedRunCh`. `RunFailed` runs (triggered by the [player-death pass](./05-server-runtime.md#player-death-pass) on `HP <= 0`) skip the leaderboard entirely — the `CompletedRun` struct is never constructed, no DB row is written, and replay verification never runs on a failed run. This preserves the "only boss-clear times are leaderboard-eligible" invariant and keeps the verification path simple: every row the replay engine sees is a successful completion.

Lobby reads `game.db` read-only for its public leaderboard endpoint and the moderators-only replay-download endpoint (`/admin/replay/:replay_id`). See [04-lobby.md](./04-lobby.md).

## `game.db` schema (target)

```sql
CREATE TABLE completed_runs (
    replay_id      TEXT PRIMARY KEY,
    player_id      TEXT NOT NULL,
    display_name   TEXT NOT NULL,
    total_ms       INTEGER NOT NULL,
    seed           INTEGER NOT NULL,
    splits_json    TEXT NOT NULL,
    input_log      BLOB NOT NULL,
    checksum       BLOB NOT NULL,
    submitted_at   DATETIME NOT NULL
);
CREATE INDEX idx_runs_total_ms ON completed_runs(total_ms);
CREATE INDEX idx_runs_player   ON completed_runs(player_id);
```

`InputLog` stored as a compact binary blob (msgpack or length-prefixed JSON — TBD).

## `S2C_LEADERBOARD`

```
{
  entries: [
    { rank, display_name, total_ms, splits, seed, replay_id }
  ]
}
```

Broadcast to connected players on every new submission that changes the top N. Full rankings also available via `/api/leaderboard` on the lobby service.

## Implementation notes

- Leaderboard goroutine is spawned once at server startup and owns `game.db`. All inserts go through `completedRunCh`.
- Broadcast to connected players: the leaderboard goroutine holds a registry of per-player `outCh`s populated at `C2S_AUTH` time. Non-blocking sends (drop if `outCh` is full — it's OK, the next broadcast will catch up).
- Replay endpoint (on lobby, admin-gated) streams `InputLog` as-is. Optional: replay verification run on-demand from admin panel.

## Implementation status

Current server (`server/main.go`) persists winners to `winners.csv` with only `{name, timestamp}` — no `InputLog`, no checksum, no replay. Migration to `game.db` and the full replay pipeline is later work.

## Intentional bugs in this area

None — this is the anticheat backstop. Bugs found here (e.g., weak checksum, missing validation of replay length) must be **fixed**. Intentional bugs in the game loop rely on the replay system being correct, so they can be trusted not to invalidate legitimate runs.

## Streaming replay engine

The verification engine described above produces a final `total_ms` and never emits packets — it's a pure correctness check. The **streaming replay engine** is a sibling that drives the same tick loop but fans out the resulting `S2CEnvelope`s to a single connected viewer over `/ws/replay`. It is the backend for the replay viewer client documented in [../client/11-replay-viewer.md](../client/11-replay-viewer.md).

### Engine extensions

`game.Engine` gains a single boolean field, `ReplayMode`. When set:

- The `±30 min` `SeedWindow` check in `initRun` is skipped (replays are old by definition).
- The `±2 s` `Ts` validation in `dispatch` is skipped (recorded `Ts` is from a different wall-clock era).
- `Submit` is bypassed even if non-nil (a replay must never write a duplicate row to `completed_runs`).
- `Engines.Register` is skipped — a replayed run is not visible to `/ws/spectate`. This isolates replay viewers from the spectator surface; spectators can't accidentally attach to a replay session, and replays don't show up in `/api/live`.
- `publishLiveSummary` is skipped for the same reason.

A second exported entry point, `Engine.RunReplay(ctx, advance)`, mirrors `Engine.Run` but uses a caller-supplied `advance func(prev time.Time) (time.Time, bool)` instead of an internal `time.Ticker`. The caller (`replay.Player`) drives synthetic time at the playback speed:

```go
func (e *Engine) RunReplay(ctx context.Context, advance func(time.Time) (time.Time, bool)) {
    if !e.handshake(ctx) { return }
    now := e.run.RunStartTime
    for {
        select { case <-ctx.Done(): return; default: }
        next, ok := advance(now)
        if !ok { return }
        now = next
        e.stepTick(now)
        if e.run.Status.IsTerminal() { return }
    }
}
```

`advance` is the playback clock. It is also the pause/seek/speed surface — when the player sends `pause`, `advance` blocks indefinitely (with cancellation). When the player sends `set_speed`, the inter-tick sleep shortens or lengthens accordingly.

### `replay.Player`

A new package, `server/internal/replay/`, wraps the engine into a viewer-facing player:

```go
type Player struct {
    PlayerID    string
    DisplayName string
    Seed        int64
    Log         []game.InputRecord

    OutCh chan protocol.S2CEnvelope  // S2C stream the viewer reads from
    cmdCh chan command               // pause / play / set_speed / restart
}

func (p *Player) Run(ctx context.Context) { /* drives Engine.RunReplay */ }
func (p *Player) Pause()                  { /* enqueue command */ }
func (p *Player) Play()                   { /* enqueue command */ }
func (p *Player) SetSpeed(s float32)      { /* enqueue command, clamp [0.25, 8.0] */ }
func (p *Player) Restart()                { /* enqueue command */ }
```

Internally, `Run` constructs an Engine in `ReplayMode`, feeds synthetic `C2S_AUTH` and `C2S_START_RUN` envelopes onto its `inCh` to satisfy the handshake, then for each tick:

1. Look up `InputRecord`s where `record.Tick == nextTick` and push them onto `inCh` as raw envelope bytes.
2. Sleep `TickDuration / speed` (or block while paused), waking on `cmdCh` or `ctx.Done()`.
3. Call `advance` with the next synthetic `now = prev + TickDuration`, returning to `RunReplay`'s caller.

`inCh` for a replay session is sized to `max(InChBuf, maxBurstPerTick(InputLog) + 8)` rather than the default `InChBuf=64`. Live recording can produce single ticks with more than `InChBuf` records — `drainInput` runs in a tight loop while the WS reader concurrently refills, so the per-tick burst is bounded only by client send-rate. Step 1 above front-loads every record for `nextTick` before `stepTick` gets a chance to drain, so a 64-deep channel silently drops the tail of a heavy burst (e.g. an `ENTER_DOOR` colocated with movement and shoot frames in a sprint), and the rest of the run replays in the wrong room. Sizing to the worst observed burst keeps the drop path as defence-in-depth only.

`Restart` resets the player by cancelling the inner context, allocating a fresh Engine + InputLog cursor, and starting again. The viewer sees a fresh `S2C_RUN_STARTED` from `T0`.

### `/ws/replay` endpoint

Mounted on the game server, alongside `/ws` and `/ws/spectate`. Replays are **admin-only** (replay viewing is effectively reading the exploit recipe — see [../client/11-replay-viewer.md#why-admin-only](../client/11-replay-viewer.md#why-admin-only)). The endpoint is gated by a short-lived ticket that the lobby mints from the admin session cookie:

```
/ws/replay?replay_id=<uuid>&ticket=<replay_id>.<exp_unix>.<base64url_hmac>
```

Ticket format:

```
HMAC-SHA256(ADMIN_SESSION_SECRET, "replay:" + replay_id + ":" + exp_unix)
```

The literal `"replay:"` prefix is a domain separator so the same secret cannot mint anything else (admin session cookies have no prefix). TTL: 30 minutes.

The game server reads `ADMIN_SESSION_SECRET` from env (the same secret the lobby uses to sign admin session cookies; both services share it via docker-compose). No DB read for ticket validation — pure HMAC compare on the hot path.

Upgrade flow:

1. Validate `replay_id` shape (UUID).
2. Validate `ticket` HMAC and expiry; ticket's embedded `replay_id` must equal the query `replay_id` (binds the ticket to a specific replay so an old ticket can't be reused for a different run). On failure → `401` with `WWW-Authenticate: Replay`.
3. Acquire a global slot (cap = `REPLAY_GLOBAL`, default 16 — small because admin-only). On `503`, set `Retry-After: 5`.
4. Per-IP rate-limit (5 new connects/min, mirrors spectator).
5. Load `(seed, input_log, display_name)` from `game.db` via `leaderboard.GetReplay` — same checksum verification path as `/admin/replay/:id`. On `ErrNotFound` → `404`. On `ErrChecksumMismatch` → `500` with code `REPLAY_INTEGRITY_FAILED`.
6. Construct `replay.Player`, spawn `Player.Run(ctx)`, spawn a writer goroutine that drains `Player.OutCh` and writes JSON envelopes to the WS, spawn a reader goroutine that:
   - Accepts `C2S_REPLAY_CONTROL` and forwards to `Player.{Pause,Play,SetSpeed,Restart}`.
   - Closes the WS with `S2C_ERROR { code: "REPLAY_READ_ONLY" }` on any other C2S type.

Idle keepalive uses the same 30 s ping cadence as spectator. On run termination (the player emits `S2C_RUN_COMPLETE`), the writer enters a 500 ms grace window then closes the WS — same shape as `SpectatorCatchupGrace`.

If `ADMIN_SESSION_SECRET` is unset on the game server (dev deploy without admin), `/ws/replay` returns `503 admin disabled` — the viewer is unreachable in dev, exactly as `/admin/*` is unreachable on the lobby. To watch replays in dev, configure admin auth.

### `C2S_REPLAY_CONTROL`

New C2S type, registered as `0x0A` in [02-protocol.md](./02-protocol.md). Payload:

```json
{ "action": "play" | "pause" | "set_speed" | "restart", "speed": 1.0 }
```

- `speed` is required when `action == "set_speed"`. Server clamps to `[0.25, 8.0]`. A non-zero `speed` does not implicitly play; clients should always send `play` after a pause if they want to resume.
- `pause` and `play` are idempotent.
- `restart` resets the engine to `T0` and emits a fresh `RUN_STARTED` immediately.

This is the **only** C2S type accepted on `/ws/replay`. Any other type closes the WS with `S2C_ERROR { code: "REPLAY_READ_ONLY" }`.

### Lobby endpoints (admin-only)

All three new endpoints sit under `/admin/replay/...` and are gated by the existing `authSvc.Wrap` (admin session cookie):

| Method | Path                                | Purpose                                                                  |
| ------ | ----------------------------------- | ------------------------------------------------------------------------ |
| GET    | `/admin/replay/:id`                 | (Existing) Raw `Replay` JSON download — `replay_id`, `seed`, `input_log`, etc. Used for forensic export and verification scripts. |
| GET    | `/admin/replay/:id/meta`            | (New) Lightweight metadata — no `input_log`. Used by `ReplayConnecting` for the loading screen and `ReplayEnd` for splits. |
| POST   | `/admin/replay/:id/ticket`          | (New) Mints a 30-min HMAC ticket bound to `replay_id`. Returns `{ticket, expires_at}`. |
| GET    | `/admin/replay/:id/view`            | (New) Serves the replay viewer host HTML (`replay.html` from the client bundle). |

`/admin/replay/:id/meta` response:

```json
{
  "replay_id": "…",
  "display_name": "aluminum-fox",
  "total_ms": 247813,
  "seed": 1735689600000,
  "splits": [{ "room_index": 1, "time_ms": 12000, "kills": 4 }, ...],
  "submitted_at": "2025-12-31T22:40:00Z"
}
```

`input_log` and `checksum` are **not** returned by `/meta` — those stay on the `/admin/replay/:id` JSON download so a typo in admin-panel JS can't accidentally exfiltrate input logs into the browser DOM of every page that fetches the metadata.

`/admin/replay/:id/meta` calls `leaderboard.GetReplayMeta` and only maps `ErrNotFound` to 404; it does **not** verify the replay checksum (the metadata projection is read-only display fodder, not the integrity-bearing payload). `/admin/replay/:id/ticket` and the raw `/admin/replay/:id` JSON download call `leaderboard.GetReplay`, enforce checksum verification, and therefore can return either `ErrNotFound` (404) or `ErrChecksumMismatch` (500 with `REPLAY_INTEGRITY_FAILED`).

### Caps and configuration

| Env var                   | Default | Purpose                                                |
| ------------------------- | ------- | ------------------------------------------------------ |
| `REPLAY_GLOBAL`           | `16`    | Global concurrent replay viewers on the game server. Small by design — admin-only, no public scaling pressure. |
| `REPLAY_PER_IP`           | `5`     | Per-IP new-connect rate per minute.                    |
| `ADMIN_SESSION_SECRET`    | (env)   | Shared with the lobby. Game server uses it to validate ticket HMACs; lobby uses it to sign session cookies and mint tickets. Empty on the game server disables `/ws/replay` entirely (`503 admin disabled`). |

Per-replay cap is **none** — a popular run can be watched by many moderators in parallel because each viewer gets its own cheap synthetic engine.

### Non-determinism and tolerance

[BUG-11 kill-log append race](./05-server-runtime.md#bug-11-kill-log-append-race) is the one source of non-determinism in the live engine. Replay also has to contend with finer wall-clock-vs-synthetic timing drifts (a tick boundary that lands exactly on a cooldown expiry, the live concurrent reader pushing a SHOOT mid-`stepTick` that replay's sequential `advance` would assign to a different settle state, etc.). The combined effect on dense speedruns is a kill shortfall in early rooms that cascades through the coin/shop economy: missed kills → missing coin pickups → shop purchases that were affordable in the recording fail in replay → fewer damage stacks → even more enemies survive.

`ReplayMode` therefore takes the recording itself as ground truth and bypasses every server-side check that the live engine demonstrably already accepted at recording time:

1. **Deterministic kill log.** Per-kill goroutine spawn (`go ProcessEnemyDeathAsync(...)`) is replaced with a synchronous append on the same goroutine that drives the death pass. Append order is deterministic (collision-pass order) and the timestamp is the synthetic per-tick `now`, so no zero-value `time.Time` ever lands in the tail.
2. **Force-unlock doors on recorded `ENTER_DOOR`, then sweep-kill remaining enemies.** `tryEnterDoor` force-unlocks the named door (gated on existence in the current `RoomState`) before delegating to `HandleEnterDoor`. The recording's `ENTER_DOOR` packet at tick N is proof the door was effectively unlocked then. *In the same step*, any enemies still alive in the leaving room are marked `AIDead` and an `S2C_ENTITY_DIED` is emitted for each — without this, the recording's BUG-11 phantom-cleared rooms render as the player walking past mobs that are still chasing them, which is the visible "looks terrible" failure mode. The room is about to be discarded by `transitionToRoom` so the kill-log mutation is a side-effect that costs nothing.
3. **`HandleShoot` skips cooldown + ammo gates.** Swap-cooldown, weapon-cooldown, and ammo-zero rejection paths all return early only when `!replayMode`. Ammo still decrements (clamped at 0) so HUD trackers stay informative. Without this, a synthetic-time tick that lands exactly on a cooldown boundary rejects shots that wall-clock jitter let through in the recording.
4. **`HandleShopPurchase` skips the coin-balance gate.** `if !replayMode && p.Coins < offer.Cost { return false }`. Coins still decrement (clamped at 0). Without this, a single missed kill earlier in the run starves the player out of upgrades and the cascade compounds.
5. **Player invulnerability.** The player-death pass (`HP<=0` → `RunFailed`) is skipped in `ReplayMode`. HP still decrements (and may go negative) so damage taken is visible to the moderator, but the run does not terminate as failed.
6. **Force-finish on log exhaustion.** If the recorded `InputLog` is exhausted without the engine reaching a terminal state, `replay.Player` calls `Engine.ForceFinishReplay(recordedTotalMs)` after a 40-tick (~2 s) post-log grace window — the engine emits `S2C_RUN_COMPLETE` carrying the recording's canonical `total_ms`. With #3 and #4 in place this rarely fires; most runs reach natural boss-kill termination.

All six changes are scoped strictly to `e.ReplayMode == true`. The live tick loop, leaderboard verification path (when implemented as a separate engine instance), and on-disk run format are unaffected. The wire `RUN_COMPLETE.total_ms` is the recording's canonical value; the viewer surfaces both numbers so any engine-vs-metadata discrepancy is visible.

The shape of the trade: replay reproduces **player progression** at high fidelity — same kills in the same rooms at approximately the same times, same stack accumulation, same boss kill — at the cost of replay's `Coins`/`Ammo`/`HP` tracking being informational rather than authoritative. This is the right balance for a moderation viewer; the verification engine, when it exists as a separate path, can keep the live gates in place because its purpose is anti-cheat, not playback.

## Cross-references

- [02-protocol.md](./02-protocol.md) — `S2C_LEADERBOARD`, `S2C_RUN_COMPLETE`, `C2S_INTERACT.claimed_type`, `C2S_SHOP_PURCHASE`, `C2S_REPLAY_CONTROL`.
- [05-server-runtime.md](./05-server-runtime.md) — `completedRunCh`, leaderboard goroutine.
- [06-game-state-conventions.md](./06-game-state-conventions.md) — `InputLog`, `Splits`, `Seed`.
- [07-determinism-seeding.md](./07-determinism-seeding.md) — RNG consumption invariant replay depends on.
- [04-lobby.md](./04-lobby.md) — public `/api/leaderboard` endpoint + admin-only `/admin/replay/:replay_id`, `/admin/replay/:id/meta`, and `/admin/replay/:id/ticket`.
- [../client/11-replay-viewer.md](../client/11-replay-viewer.md) — replay viewer client (third Phaser bundle).
- [../features/shop.md](../features/shop.md) — coin drops, BUG-12 `claimed_type` reproducibility.
