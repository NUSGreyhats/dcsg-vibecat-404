# 02 — Net Sync

## Purpose

Client side of the WebSocket wire protocol. Defines the typed envelope mirror, the outbound `C2S` send pipeline (serialization, `seq` bookkeeping, pre-open buffering), the inbound `S2C` dispatch pipeline, connection-lifecycle semantics, error handling, and how the client stays 1:1 with the server registry in [foundations/02-protocol.md](../foundations/02-protocol.md). The net layer is the **only** mutable bridge between the browser and the game — every exploit-relevant bit passes through it.

## Module surface

Two files carry the net layer:

- `client/src/net/packet.ts` — pure types. No logic, no imports other than itself. Defines `C2SType`, `S2CType`, the `C2SEnvelope<T>` / `S2CEnvelope<T>` shapes, and the `C2SDataFor<T>` / `S2CDataFor<T>` discriminated unions. **This file is the source of truth for envelope shapes on the client** — changes to [foundations/02-protocol.md](../foundations/02-protocol.md) must land here in the same PR.
- `client/src/game/net/GameStateSync.ts` — runtime. Owns one `WebSocket`, one monotonic `seq` counter, one outbound buffer (for messages sent before `onopen`), and one `GameState` mirror. Exposes a typed `send*` method per C2S packet and dispatches S2C envelopes into `GameState.apply()`.

The client-side `GameState` that `apply()` mutates is documented separately in [03-client-state.md](./03-client-state.md).

## Envelope shape (mirror of [foundations/02-protocol.md](../foundations/02-protocol.md))

```ts
type C2SEnvelope<T extends C2SType = C2SType> = {
  type: T;        // 0x01–0x08, uint8 range
  seq: number;    // uint32 monotonic per connection, starts at 1
  ts: number;    // Date.now() — client wallclock ms
  data: C2SDataFor<T>;
};

type S2CEnvelope<T extends S2CType = S2CType> = {
  type: T;        // 0x81–0x89
  seq: number;    // uint32, server-monotonic
  server_ts: number;
  data: S2CDataFor<T>;
};
```

All frames are UTF-8 JSON strings. The client does not use binary WebSocket frames; incoming binary data is coerced to `String(ev.data)` and parsed via `JSON.parse` — malformed frames are dropped silently (see [Error handling](#error-handling)).

### Client → Server packet registry

| Type  | Name                | `send*` helper                          | Emits at                      |
| ----- | ------------------- | --------------------------------------- | ----------------------------- |
| 0x01  | `C2S_AUTH`          | `sendAuth(playerId)`                    | `MainMenu.onStart()`          |
| 0x02  | `C2S_START_RUN`     | `sendStartRun(startTime?)`              | `MainMenu.onAuthOk()`         |
| 0x03  | `C2S_INPUT`         | `sendInput(dx, dy)`                     | `Game.handleInput()`, throttled to `INPUT_SEND_INTERVAL_MS = 50 ms` |
| 0x04  | `C2S_SHOOT`         | `sendShoot(aimAngle)`                   | `Game.handleInput()` on edge-triggered left-click |
| 0x05  | `C2S_SWITCH_WEAPON` | `sendSwitchWeapon(slot)`                | `Game.handleInput()` on `1/2/3` keys |
| 0x06  | `C2S_INTERACT`      | `sendInteract(targetId)`                | `Game.handleInput()` when `E` pressed near a pickup |
| 0x07  | `C2S_ENTER_DOOR`    | `sendEnterDoor(doorId)`                 | `Game.handleInput()` when `E` pressed near an unlocked door |
| 0x08  | `C2S_USE_ITEM`      | `sendUseItem(itemName)`                 | `InventoryMenu.useSelected()` |

### Server → Client packet registry

| Type  | Name               | Routed in `GameState.apply()` to event | Consumers                                      |
| ----- | ------------------ | -------------------------------------- | ---------------------------------------------- |
| 0x81  | `S2C_AUTH_OK`      | `authOk`                               | `MainMenu`                                     |
| 0x82  | `S2C_RUN_STARTED`  | `runStarted`                           | `MainMenu` → hands off to `Game`               |
| 0x83  | `S2C_STATE`        | `state`                                | `Game` (entity reconcile), `GameHUD` (polls state each frame) |
| 0x84  | `S2C_ROOM_LOAD`    | `roomLoad`                             | `Game` (tears down old room, builds new)       |
| 0x85  | `S2C_RUN_COMPLETE` | `runComplete`                          | `Game` (stops HUD, transitions to WinScreen)   |
| 0x86  | `S2C_LEADERBOARD`  | `leaderboard`                          | `WinScreen`                                    |
| 0x87  | `S2C_ERROR`        | `error`                                | `MainMenu` (surfaces to status line), others   |
| 0x88  | `S2C_DOOR_UNLOCKED`| `doorUnlocked`                         | `Game` (flips the `Door` entity state)         |
| 0x89  | `S2C_ENTITY_DIED`  | `entityDied`                           | `Game` (plays death animation, drops from map) |

Every `S2CType` has exactly one branch in `GameState.apply()` (plus a `default: break` that swallows unknown future types — adding a new S2C type does not crash the client, it just no-ops until the branch is added).

### Sub-structure mirrors

1:1 with [foundations/02-protocol.md](../foundations/02-protocol.md):

| Type               | File                    |
| ------------------ | ----------------------- |
| `RoomData`         | `src/net/packet.ts`     |
| `EnemySpawn`       | `src/net/packet.ts`     |
| `PickupSpawn`      | `src/net/packet.ts`     |
| `DoorDef`          | `src/net/packet.ts`     |
| `PlayerSnapshot`   | `src/net/packet.ts`     |
| `EntitySnapshot`   | `src/net/packet.ts`     |
| `ProjSnapshot`     | `src/net/packet.ts`     |
| `SplitData`        | `src/net/packet.ts`     |
| `LeaderboardEntry` | `src/net/packet.ts`     |

Field-for-field match on names and types (TS `number` covers both server `float32` and `int64` at wire level; any width-sensitive semantics stay server-side).

## Outbound pipeline — C2S

```mermaid
graph LR
    UI["Scene calls<br/>sync.sendShoot(angle)"] --> BUILD["build envelope<br/>{ type, seq=++this.seq, ts=Date.now(), data }"]
    BUILD --> JSON["JSON.stringify"]
    JSON --> GATE{"ws.readyState<br/>=== OPEN?"}
    GATE -->|yes| SEND["ws.send(payload)"]
    GATE -->|no|  BUF["this.queued.push(payload)"]
    BUF --> FLUSH["onopen: drain queued[] in order"]
    FLUSH --> SEND

    style BUILD fill:#3498db,color:#fff
    style SEND fill:#2ecc71,color:#fff
```

- `seq` increments **before** the envelope is built (`seq: ++this.seq`), so the first C2S ever sent is `seq: 1`. Server spec (see [foundations/02-protocol.md](../foundations/02-protocol.md)) requires `seq` to be uint32 monotonic per connection starting at 1 — the `++this.seq` idiom satisfies this.
- `ts` is `Date.now()` at the moment of envelope construction. Server validates `|ts − time.Now()| ≤ 2 s`. The client does **not** track or reshape `ts`; anyone who wants to exploit [BUG-02 drift tick stretching](../foundations/05-server-runtime.md#bug-02-drift-tick-stretching) has to write a custom client — this is the intended tooling surface.
- `JSON.stringify(envelope)` preserves camelCase / snake_case on the envelope fields as defined in `packet.ts`. `type`, `seq`, `ts`, `server_ts`, `data` — snake_case matches the Go server's JSON tags.
- The pre-open buffer (`queued: string[]`) exists because `MainMenu.onStart()` can call `sendAuth()` between `new WebSocket()` and `onopen`. Buffer order is FIFO; flush happens once, synchronously, inside `onopen`.

### Duplicate-key and case-insensitive payloads (BUG-09)

The stock client uses `JSON.stringify`, which **cannot** emit duplicate JSON keys (last write wins at the JS object level) and always emits field names exactly as they appear in the TS source. Triggering [BUG-09 — JSON Case-Insensitive Field Smuggling](../foundations/02-protocol.md#bug-09-json-case-insensitive-field-smuggling) therefore requires a custom client — e.g. a WebSocket script that hand-builds frame text like `'{"type":8,"seq":1,"ts":0,"data":{"item_name":"HEALTH_POTION","item_name":"timewarp"}}'`. This is by design: exploiting packet-level bugs is supposed to be a coding exercise, not a menu option.

The stock client must never grow a "developer mode" that lets users craft arbitrary JSON — that would short-circuit the challenge. Stick to the typed helpers.

## Inbound pipeline — S2C

```mermaid
graph LR
    WS["WebSocket onmessage"] --> TEXT["coerce ev.data to string"]
    TEXT --> PARSE["JSON.parse"]
    PARSE -->|fails| DROP["swallow"]
    PARSE -->|ok|    APPLY["GameState.apply(envelope)"]
    APPLY --> SWITCH{"switch on<br/>envelope.type"}
    SWITCH --> MUTATE["mutate matching GameState fields"]
    MUTATE --> EMIT["emit typed event<br/>(authOk / runStarted / state / ...)"]
    EMIT --> SCENE["scene listener reacts"]

    style APPLY fill:#9b59b6,color:#fff
    style DROP fill:#7f8c8d,color:#fff
```

Concretely (`GameStateSync.onMessage`):

```ts
private onMessage(raw: string): void {
    let env: S2CEnvelope;
    try {
        env = JSON.parse(raw) as S2CEnvelope;
    } catch {
        // Malformed frame — drop it. The server is authoritative; we just
        // wait for the next well-formed snapshot.
        return;
    }
    this.state.apply(env);
}
```

The `apply()` switch (in `GameState.ts`) is a single exhaustive match; each branch narrows `envelope.data` through the discriminated union and emits a typed event. See [03-client-state.md](./03-client-state.md) for the full field list each branch mutates.

### Ordering & drop tolerance

The server emits packets in the order shown in [foundations/05-server-runtime.md](../foundations/05-server-runtime.md): drain → physics → collision → enemy-death → player-death → door → timer → send. The client trusts that ordering and does not reorder or buffer inbound frames.

- `S2C_STATE` is idempotent — each frame contains the **full** entity / projectile list, so a dropped frame costs at most 50 ms of smoothing and self-heals on the next tick.
- `S2C_ENTITY_DIED` and `S2C_DOOR_UNLOCKED` are sticky side-effect packets — the next `S2C_STATE` would make up for a lost death (the entity is absent from `entities[]`) but the explosion/animation cue and the unlocked-door indicator both rely on the event firing. In practice the WebSocket TCP stream doesn't drop mid-connection, so we don't design around it. If spectators see transient desyncs (see [09-spectator-mode.md](./09-spectator-mode.md)) the next `S2C_STATE` resyncs them.

## Connection lifecycle

`GameStateSync.connect(url)` returns a `Promise<void>` that:

- **resolves** on the first `onopen` event;
- **rejects** on `onerror` if the error arrives before `onopen` (i.e., the upgrade itself failed).

```ts
connect(url: string): Promise<void> {
    // constructs WebSocket(url)
    // hooks onopen: resolve + flush queued
    // hooks onerror: reject if not yet settled
    // hooks onmessage: onMessage
    // hooks onclose: mark disconnected, emit 'error' with code 'disconnected'
}
```

| Event      | Behaviour                                                                                                                                        |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `onopen`   | `connected = true`; flush `queued[]` FIFO; resolve the connect promise.                                                                          |
| `onmessage`| Parse → `state.apply()`.                                                                                                                         |
| `onerror`  | If the connect promise is still pending, reject it (`MainMenu` surfaces an error to the status line).                                            |
| `onclose`  | `connected = false`; set `state.lastError = { code: "disconnected", message: "ws closed" }` and emit `"error"` on the `GameState`. No auto-reconnect. |

`close()` is idempotent and called from `resetSync()` (see [01-architecture.md](./01-architecture.md)). It tears down the WS, clears the pre-open buffer, and marks `connected = false`. The `seq` counter resets with the fresh `GameStateSync` instance a subsequent `getSync()` creates — **every run gets seq:1..N**, matching the server's per-connection expectation.

### No auto-reconnect

If the WS drops mid-run, the run is lost. Rationale:

- The server has a 15 s disconnect grace (see [foundations/04-lobby.md#queue-rules](../foundations/04-lobby.md#queue-rules)); a fast auto-reconnect on the client would masquerade a real disconnect as healthy, which breaks queue fairness.
- Replay integrity requires a contiguous `seq` stream. A reconnect would restart `seq` at 1 and the server's `InputLog` would not deserialize cleanly.
- Failure is explicit: `state.lastError` bubbles to the scene, and the user is invited to refresh and re-queue.

If future work introduces mid-run resume, it must be a new server-sanctioned packet (`C2S_RESUME { run_token }`) with server-side `InputLog` stitching, not a client-local reconnect.

## Error handling

Three failure modes, handled by dropping rather than crashing:

| Failure                                                 | Handling                                                                                     |
| ------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Outbound send before the WS is open                     | Buffer in `queued[]`; flush on `onopen`.                                                     |
| Outbound send after the WS is closed                    | Buffer in `queued[]`; never flushes. The next user action that depends on server response (e.g. `sendAuth` awaiting `S2C_AUTH_OK`) stalls — `MainMenu` shows a status message on the `"error"` event from `onclose`. |
| Malformed inbound frame (invalid JSON)                  | Swallowed in the `try/catch` around `JSON.parse`. Next well-formed `S2C_STATE` resyncs.      |
| Unknown `type` field in inbound frame                   | Falls through to `default: break` in `GameState.apply()`. Forwards-compatible with a server that adds a new type before the client redeploys. |
| `S2C_ERROR { code, message }`                           | Routed to `state.lastError` + `"error"` event. Scene code decides how to surface it (`MainMenu` shows the message inline; `Game` has no specific branch — current code relies on the server closing the connection if the error is fatal). |

No frame is ever "applied" twice; duplicates (which the current server does not emit) would double-apply state deltas, but since almost every S2C packet is idempotent or sticky-set, this is a latent non-issue rather than an active bug.

## Non-authority guarantees

The client is strictly a presentation layer for server-authoritative state. Concretely:

- `GameStateSync` does not compute `elapsed_ms`, HP, ammo, cooldowns, enemy positions, or anything gameplay-semantic. Every field in `GameState` is either a direct copy of a server field or a derived Set-of-IDs (e.g. `unlockedDoors`) maintained from server-side events.
- The client never **suppresses** or **rewrites** a C2S packet to enforce client-side gameplay invariants (e.g. "don't send `C2S_SHOOT` if cooldown hasn't expired"). Any gate-keeping the stock client does is UX polish — the server treats all C2S as untrusted.
- The Phaser Arcade physics engine runs only for position smoothing between snapshots (see [05-entities-rendering.md](./05-entities-rendering.md)). There is no movement simulation that could diverge from the server.

## Implementation notes

- Keep `src/net/packet.ts` in sync with [foundations/02-protocol.md](../foundations/02-protocol.md). If a new packet type is added to the server registry, update both — PR review must verify 1:1 shape match.
- Every new `send*` helper goes on `GameStateSync` as a typed wrapper around `this.send(...)`; scenes should never build envelopes directly. This keeps `seq`/`ts` bookkeeping in one place.
- Every new S2C type goes into the `S2CType` const, the `S2CDataFor` union, and an `apply()` branch in `GameState.ts`. The exhaustive-switch on `envelope.type` plus the discriminated union catches missed branches at compile time.
- Do not add client-side prediction. It would introduce a second source of truth and make replay verification pointless.
- Do not add client-side validation of outbound packets. The server is the only validator; mirroring validation rules in the client leaks them to anyone who reads the published source and tempts future maintainers to trust the client.

## Implementation status

- **Typed envelope + discriminated unions** are fully defined in `src/net/packet.ts`, 1:1 with [foundations/02-protocol.md](../foundations/02-protocol.md).
- **`GameStateSync`** implements: lazy WebSocket construction, `seq` monotonicity, pre-open buffer, every `send*` helper listed above, JSON-parse guard, full `GameState.apply()` dispatch.
- **Server side is the lag**: the current `server/main.go` still emits a legacy single-blob `gameState` JSON roughly every 1.5 s rather than the typed envelope. Until the server migrates, the `GameStateSync` code above will in practice receive an object whose `type` field is absent, fall through to `default: break`, and leave the UI empty. Reconciliation is tracked in [foundations/02-protocol.md#implementation-status](../foundations/02-protocol.md#implementation-status).
- **No reconnect, no heartbeat, no ping** — intentional as above.

## Intentional bugs in this area

None. The net layer is a pass-through. Any bug here (duplicate frame handling, incorrect seq increment, silent drop of a needed packet) invalidates replay or hides server-side exploits, so this file must stay correct. Every intentional bug surfaces on the **other side** of the pipeline — either the server's JSON parser differential (BUG-09) or the server's state mutation (BUG-07, BUG-08, etc.).

## Cross-references

- [01-architecture.md](./01-architecture.md) — runtime singletons, bootstrap chain.
- [03-client-state.md](./03-client-state.md) — the `GameState` class that `apply()` mutates + its event contract.
- [04-scenes.md](./04-scenes.md) — scenes that subscribe to state events.
- [07-input-controls.md](./07-input-controls.md) — where each C2S helper is called from.
- [foundations/02-protocol.md](../foundations/02-protocol.md) — authoritative packet registry + sub-structure definitions + BUG-09.
- [foundations/05-server-runtime.md](../foundations/05-server-runtime.md) — server send ordering.
