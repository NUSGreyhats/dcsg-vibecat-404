# 03 — Client State

## Purpose

The client-side mirror of the server's authoritative run state. Defines the `GameState` class (fields, event emitter contract, lifecycle), the ownership rule that keeps the client non-authoritative, and how scenes read the mirror vs. subscribe to mutation events. This is the client-side analogue of [foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md) — but where the server owns everything, the client **copies** everything from S2C packets.

## Ownership rule (critical)

**`GameState` is a passive mirror. It is never the source of truth for any gameplay fact.**

- Every field is either copied directly from an `S2C_*` `data` payload or derived from a sequence of such payloads (e.g., `deadEntities` is the set of entity IDs that have ever appeared in an `S2C_ENTITY_DIED`).
- Scenes MUST NOT write to `GameState` fields from input handlers or physics ticks. The only writer is `GameState.apply(envelope)` in the net layer (see [02-net-sync.md](./02-net-sync.md)).
- Scenes MAY attach/detach listeners via `on(event, fn)` / `off(event, fn)` and MAY read fields during render.

This rule exists because any client-side computed gameplay state would either:
1. diverge from the server when a packet is dropped or delayed, or
2. become a target for cheaters who'd exploit the divergence.

Keep the client dumb and the exploit surface exactly at the packet boundary.

## State block

```ts
class GameState extends Phaser.Events.EventEmitter {
    displayName?: string;                 // from S2C_AUTH_OK
    seed?: number;                        // from S2C_RUN_STARTED
    runStarted = false;                   // set true on S2C_RUN_STARTED, false on apply() reset at next run
    runFinished = false;                  // set true on S2C_RUN_COMPLETE
    runOutcome?: "completed" | "failed";  // from S2C_RUN_COMPLETE.outcome
    elapsedMs = 0;                        // from S2C_STATE.elapsed_ms
    currentRoom?: number;                 // from S2C_RUN_STARTED / S2C_ROOM_LOAD
    roomData?: RoomData;                  // from S2C_RUN_STARTED / S2C_ROOM_LOAD
    player?: PlayerSnapshot;              // from S2C_STATE.player
    entities: EntitySnapshot[] = [];      // from S2C_STATE.entities (replaces wholesale each frame)
    projectiles: ProjSnapshot[] = [];     // from S2C_STATE.projectiles (replaces wholesale each frame)
    unlockedDoors = new Set<number>();    // grows on S2C_DOOR_UNLOCKED; reset on room load
    deadEntities = new Set<number>();     // grows on S2C_ENTITY_DIED; reset on room load
    totalMs?: number;                     // from S2C_RUN_COMPLETE
    rank?: number;                        // from S2C_RUN_COMPLETE
    splits: SplitData[] = [];             // from S2C_RUN_COMPLETE (or accumulated from S2C_ROOM_LOAD in future)
    leaderboard: LeaderboardEntry[] = []; // from S2C_LEADERBOARD
    lastError?: GameStateError;           // from S2C_ERROR or WS onclose
}
```

Field notes:

- **`displayName`** — not the same as `player_id`. `player_id` never lives on the client; only the display name (chosen at registration) is returned. Used in `MainMenu`, `GameHUD` title bar (future), and `WinScreen`.
- **`runStarted` / `runFinished`** — explicit booleans rather than a single enum so scenes can react to both edges independently. Note: `S2C_RUN_STARTED` sets `runStarted=true, runFinished=false` (resurrection on "Play Again" after a fresh `GameStateSync`).
- **`entities` / `projectiles`** — replaced wholesale on every `S2C_STATE`, not merged. Idempotent; dropping a frame costs only 50 ms of smoothing. Keeping these as arrays (not maps) because the scene turns them into maps keyed by server id during reconciliation; see [05-entities-rendering.md](./05-entities-rendering.md).
- **`unlockedDoors` / `deadEntities`** — `Set<number>` grown monotonically within a room and cleared on `S2C_ROOM_LOAD`. Used by `Game` to drive door visuals and despawn stale entity sprites on the next tick.
- **`lastError`** — the most recent error (S2C-reported or WS-close). Cleared only when overwritten. Scenes that need "clear on retry" semantics should do so locally after reading.

### Inventory display

`PlayerSnapshot.inventory: string[]` is the authoritative inventory list. `InventoryMenu` reads it at open time:

```ts
this.items = getSync().state.player?.inventory?.slice() ?? [];
```

The slice is deliberate: `InventoryMenu` uses a local copy so a live `S2C_STATE` mid-selection doesn't shift the highlighted index. When the user picks an item and presses Enter, the menu sends `C2S_USE_ITEM { item_name }` and the server-side effect reaches the client on the next `S2C_STATE` snapshot.

## Event emitter contract

`GameState` extends `Phaser.Events.EventEmitter`. Every branch of `apply()` emits exactly one event **after** mutating state, so listeners always see a consistent snapshot.

| Event           | Payload                                                                                        | Typical subscriber      |
| --------------- | ---------------------------------------------------------------------------------------------- | ----------------------- |
| `authOk`        | `(displayName: string)`                                                                        | `MainMenu`              |
| `runStarted`    | `(seed: number, room: RoomData)`                                                               | `MainMenu` → starts `Game` |
| `state`         | `(data: { elapsed_ms, player, entities, projectiles })`                                        | `Game` (entity reconcile) |
| `roomLoad`      | `(roomIndex: number, room: RoomData, splitMs: number)`                                         | `Game` (rebuild world)  |
| `doorUnlocked`  | `(doorId: number)`                                                                             | `Game` (flip door sprite) |
| `entityDied`    | `(entityId: number)`                                                                           | `Game` (play death anim) |
| `runComplete`   | `(outcome: "completed" \| "failed", totalMs: number, rank: number, splits: SplitData[])`        | `Game` → `WinScreen`    |
| `leaderboard`   | `(entries: LeaderboardEntry[])`                                                                | `WinScreen`             |
| `error`         | `({ code: string, message: string })`                                                          | `MainMenu`, others (future) |

### Subscription lifecycle

Scenes attach in `create()` and detach in a `shutdown` handler. The pattern used throughout:

```ts
// Game.create()
sync.state.on("state", this.onServerState, this);
sync.state.on("roomLoad", this.onRoomLoad, this);
// ...
this.events.on("shutdown", this.cleanup, this);

// Game.cleanup()
sync.state.off("state", this.onServerState, this);
sync.state.off("roomLoad", this.onRoomLoad, this);
```

Why detach: Phaser scenes can be stopped and restarted, and a stopped scene would otherwise still receive events, keep references alive, and attempt to touch destroyed `GameObjects`. Every scene that subscribes to `sync.state` must `off()` the same handlers in its shutdown hook.

There is one nuanced case: `WinScreen.onPlayAgain()` calls `resetSync()`, which **throws away the entire `GameStateSync`**, including its `state` emitter. Listeners on the old emitter are effectively unreachable after reset — but `WinScreen` still detaches before the reset (via `resetHandlersAttached = false`) so its later `shutdown` cleanup doesn't try to `off()` a handler on the freshly-constructed sync. See `WinScreen.onPlayAgain()`.

### Why an event bus (rather than polling)

`GameHUD.update()` polls `getSync().state` every frame — it wants the latest values for its text layout. That works because HUD fields (HP, elapsed ms, room index) change cheaply and don't trigger cascading side effects.

`Game.onServerState()` subscribes because each `S2C_STATE` requires a meaningful amount of work (entity reconcile, projectile despawn, dead-entity sweep) that should run **once per snapshot**, not every render frame.

So both patterns coexist: *polling* for read-only HUD text, *events* for mutation-sensitive reconciliation. Neither bypasses `GameState` as the mirror.

## Data flow

### Incoming (from server)

```
S2C frame arrives (WebSocket)
    │
    ▼
GameStateSync.onMessage → JSON.parse → GameState.apply(envelope)
    │
    ▼
switch(envelope.type):
    case AUTH_OK:       displayName := data.display_name
    case RUN_STARTED:   seed / roomData / currentRoom / runStarted / reset sets
    case STATE:         elapsedMs / player / entities / projectiles
    case ROOM_LOAD:     currentRoom / roomData / clear entities/projectiles/unlockedDoors/deadEntities
    case DOOR_UNLOCKED: unlockedDoors.add(door_id)
    case ENTITY_DIED:   deadEntities.add(entity_id)
    case RUN_COMPLETE:  runFinished / runOutcome / totalMs / rank / splits
    case LEADERBOARD:   leaderboard
    case ERROR:         lastError
    │
    ▼
emit typed event
    │
    ▼
subscribed scenes react (enemy reconcile, door flip, HUD text refresh, scene transition)
```

### Outgoing (to server)

There is **no path from scene code that writes to `GameState`**. Outgoing always goes through `sync.send*(...)`; the resulting state change comes back via an inbound `S2C_STATE` (or sticky event) and re-enters through `apply()`. This is the invariant that keeps the mirror clean.

### Reset semantics

There are three kinds of reset:

| Trigger                                                       | Scope                                                                         |
| ------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `S2C_RUN_STARTED` (new run on same connection)                | Inside `apply()` — zero out `entities`, `projectiles`, `splits`, `unlockedDoors`, `deadEntities`, `totalMs`, `rank`. Keep `displayName`, `leaderboard`. |
| `S2C_ROOM_LOAD` (room transition within a run)                | Zero out `entities`, `projectiles`, `unlockedDoors`, `deadEntities`. Keep `seed`, `player`, `leaderboard`, run-level totals. |
| `resetSync()` (from `WinScreen` "Play Again")                  | Destroy the whole `GameStateSync` + `GameState`; next `getSync()` builds fresh. `seq` resets to 0 (first C2S will be 1). MainMenu may auto-auth with the cached `player_id` only when queueing is disabled. |

No partial reset is ever triggered by scene code directly; all three paths are mechanical consequences of server packets or explicit "Play Again" intent.

## Implementation notes

- `GameState` is constructed once by `GameStateSync` (not directly by scenes). Don't instantiate it elsewhere — there must only ever be one mirror per WebSocket.
- Field reads from scenes should use optional chaining (`sync.state.player?.hp`) because `GameState` may be pre-run (fields `undefined`). Do not add default values that could be confused with real zero states — absence is not "0 hp".
- When adding a new S2C type, update `GameState.apply()` in the same PR as the [foundations/02-protocol.md](../foundations/02-protocol.md) append and the `src/net/packet.ts` type union. The exhaustive-switch will keep typing honest; the `default: break` catches runtime frames for future types so the client is forwards-compatible with an updated server during staged rollouts.
- `entities` / `projectiles` replaced wholesale each `S2C_STATE`: do not mutate them in-place from scene code, or you'll fight the next snapshot. If a scene needs derived caches (e.g. `Map<number, Enemy>`), hold them locally and reconcile against the new arrays on each `state` event. See [05-entities-rendering.md](./05-entities-rendering.md).
- Keep `GameState` pure data + emitter; do not co-locate rendering code, Phaser scene references, or timers here. It must work without Phaser being initialised (e.g. in a future headless test harness).

## Implementation status

- All fields and event branches above exist in `client/src/game/net/GameState.ts`.
- `leaderboard` is currently only populated by `S2C_LEADERBOARD`; pre-run seeding from a dedicated REST endpoint is future work (currently the standalone `/leaderboard` HTML page polls the server independently — see `client/public/leaderboard.html`).
- `splits` is currently filled only at `S2C_RUN_COMPLETE`. Per-room splits collected from `S2C_ROOM_LOAD.split_ms` would let the HUD show a live splits column — future work once server emits real split times.
- The "current room index for HUD display" is driven by `currentRoom` but not yet reset on `S2C_RUN_COMPLETE`. Not wrong, just redundant with the `runFinished` flag.

## Intentional bugs in this area

None. This layer is pure presentation state; bugs here (missed event, stale reset, wrong field copy) either hide server-side exploits from the legitimate player or cause UI desyncs. Must stay correct.

## Cross-references

- [02-net-sync.md](./02-net-sync.md) — the net layer that calls `apply()` and owns the singleton.
- [04-scenes.md](./04-scenes.md) — every subscriber of the events above.
- [05-entities-rendering.md](./05-entities-rendering.md) — how `entities` / `projectiles` arrays become Phaser objects.
- [foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md) — server-side state the client mirrors.
- [foundations/02-protocol.md](../foundations/02-protocol.md) — envelope + field names mirrored here.
