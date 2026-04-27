# 08 — Leaderboard & Replay

## Purpose

Server-authoritative leaderboard + tamper-proof replay verification. Every completed run stores its full `InputLog`; the replay engine can re-run `(seed, InputLog)` and must produce the same final time. The leaderboard is the anticheat backstop that makes all the intentional bugs safe to ship — a "cheat" only counts if it reproduces from replay.

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

## Cross-references

- [02-protocol.md](./02-protocol.md) — `S2C_LEADERBOARD`, `S2C_RUN_COMPLETE`, `C2S_INTERACT.claimed_type`, `C2S_SHOP_PURCHASE`.
- [05-server-runtime.md](./05-server-runtime.md) — `completedRunCh`, leaderboard goroutine.
- [06-game-state-conventions.md](./06-game-state-conventions.md) — `InputLog`, `Splits`, `Seed`.
- [07-determinism-seeding.md](./07-determinism-seeding.md) — RNG consumption invariant replay depends on.
- [04-lobby.md](./04-lobby.md) — public `/api/leaderboard` endpoint + admin-only `/admin/replay/:replay_id`.
- [../features/shop.md](../features/shop.md) — coin drops, BUG-12 `claimed_type` reproducibility.
