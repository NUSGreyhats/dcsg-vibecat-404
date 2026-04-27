# 02 — Protocol

## Purpose

Defines the WebSocket wire protocol: packet envelope shape, the C2S / S2C packet registry, sub-structures passed inside `data` payloads, and the dispatch pipeline on the server.

## Envelope

All traffic over the per-player WebSocket is JSON. Every packet has a typed envelope.

### Client → Server (C2S)

```
{ "type": uint8, "seq": uint32, "ts": int64, "data": { ... } }
```

- `type` — one of the C2S type codes (0x01–0x09).
- `seq` — uint32, monotonic per-connection, starts at 1.
- `ts` — client wallclock ms (`Date.now()`). Used by the server for physics `deltaTime`. Validated ±2 s of `time.Now()` per packet. **No rate-of-progression check** — see [05-server-runtime.md#bug-02-drift-tick-stretching](./05-server-runtime.md#bug-02-drift-tick-stretching).
- `data` — packet-specific payload (see registry below).

### Server → Client (S2C)

```
{ "type": uint8, "seq": uint32, "server_ts": int64, "data": { ... } }
```

- `type` — one of the S2C type codes (0x81–0x89).
- `seq` — uint32, server-monotonic.
- `server_ts` — server wallclock ms at send time.
- `data` — packet-specific payload (see registry below).

## Packet registry

### C2S — append new rows below `0x09` when adding features

| Type | Name                | `data` Fields                                       |
| ---- | ------------------- | --------------------------------------------------- |
| 0x01 | `C2S_AUTH`          | `{ player_id: string }`                             |
| 0x02 | `C2S_START_RUN`     | `{ start_time: int64 }`                             |
| 0x03 | `C2S_INPUT`         | `{ dx: float32, dy: float32 }`                      |
| 0x04 | `C2S_SHOOT`         | `{ aim_angle: float32 }`                            |
| 0x05 | `C2S_SWITCH_WEAPON` | `{ weapon_id: uint8 }`                              |
| 0x06 | `C2S_INTERACT`      | `{ target_id: uint32, claimed_type: uint8 }`        |
| 0x07 | `C2S_ENTER_DOOR`    | `{ door_id: uint32 }`                               |
| 0x09 | `C2S_SHOP_PURCHASE` | `{ item_id: uint8 }`                                |
| 0x0A | `C2S_REPLAY_CONTROL`| `{ action: "play"\|"pause"\|"set_speed"\|"restart", speed?: float }` |

`claimed_type` on `C2S_INTERACT` is the pickup type the *client* says it's interacting with. The server does **not** validate `claimed_type == pk.Type` — see [../features/shop.md#bug-12-pickup-type-confusion](../features/shop.md#bug-12-pickup-type-confusion).

`C2S_REPLAY_CONTROL` is **only** valid on `/ws/replay`. The player and spectator endpoints reject it (any unrecognised C2S type is a bad-envelope error on `/ws`; spectator closes the WS on any frame). The replay endpoint, conversely, rejects every C2S type *except* `C2S_REPLAY_CONTROL` with `S2C_ERROR { code: "REPLAY_READ_ONLY" }`. See [08-leaderboard-replay.md#c2s_replay_control](./08-leaderboard-replay.md#c2s_replay_control).

### S2C — append new rows below `0x89` when adding features

| Type | Name                | `data` Fields                                                           |
| ---- | ------------------- | ----------------------------------------------------------------------- |
| 0x81 | `S2C_AUTH_OK`       | `{ display_name }`                                                      |
| 0x82 | `S2C_RUN_STARTED`   | `{ seed: int64, room: RoomData }`                                       |
| 0x83 | `S2C_STATE`         | `{ elapsed_ms, player: PlayerSnapshot, entities: [], projectiles: [], pickups: [] }` |
| 0x84 | `S2C_ROOM_LOAD`     | `{ room_index, room: RoomData, split_ms }`                              |
| 0x85 | `S2C_RUN_COMPLETE`  | `{ outcome: "completed" \| "failed", total_ms, rank, splits: [] }`      |
| 0x86 | `S2C_LEADERBOARD`   | `{ entries: [] }`                                                       |
| 0x87 | `S2C_ERROR`         | `{ code, message }` — codes include `SPECTATOR_READ_ONLY` (closes conn). |
| 0x88 | `S2C_DOOR_UNLOCKED` | `{ door_id }`                                                           |
| 0x89 | `S2C_ENTITY_DIED`   | `{ entity_id }`                                                         |

## Sub-structures

| Structure          | Fields                                                                                        |
| ------------------ | --------------------------------------------------------------------------------------------- |
| `RoomData`         | `{ room_index, tilemap_id, enemies: []EnemySpawn, pickups: []PickupSpawn, doors: []DoorDef }` |
| `EnemySpawn`       | `{ entity_id, type, x, y, hp }`                                                               |
| `PickupSpawn`      | `{ entity_id, type, x, y }`                                                                   |
| `DoorDef`          | `{ door_id, x, y, target_room, locked }`                                                      |
| `PlayerSnapshot`   | `{ x, y, hp, weapon_id, ammo, coins: int32, speed_stacks: uint8, fire_rate_stacks: uint8, damage_stacks: uint8, aim_angle: float32 }`. `aim_angle` is the radians angle of the player's most recent `C2S_SHOOT` (zero until the first shot). The player client ignores it (their local cursor read is more responsive); spectators (no local cursor) render the gun rotation from this field. See [04-lobby.md#live-spectating](./04-lobby.md#live-spectating). |
| `EntitySnapshot`   | `{ entity_id, type, x, y, hp, state }`                                                        |
| `ProjSnapshot`     | `{ proj_id, x, y, angle, owner }`                                                             |
| `SplitData`        | `{ room_index, time_ms, kills }`                                                              |
| `LeaderboardEntry` | `{ rank, display_name, total_ms, splits, seed, replay_id }`                                   |

### S2C_RUN_COMPLETE outcomes

| Outcome     | Trigger                                        | `rank`                    | Leaderboard submit |
| ----------- | ---------------------------------------------- | ------------------------- | ------------------ |
| `completed` | Boss room `AllEnemiesDead` while Running       | server-assigned rank      | yes                |
| `failed`    | Player-death pass: `HP <= 0` while Running     | `0` (sentinel)            | no                 |

`splits` includes the final split in both cases — the slot the player was in when the run terminated. A failed run's replay is still deterministic (same InputLog, same seed), but it is not leaderboard-eligible and is not written to the DB. See [05-server-runtime.md#player-death-pass](./05-server-runtime.md#player-death-pass).

## Data flow (server side)

```
raw bytes on WS
    │
    ▼
encoding/json.Unmarshal(envelope)
    │
    ▼
dispatch by type  ──► handler per type
    │
    ▼
mutates game state (owned by gameLoop goroutine)
```

## Spectator protocol

Spectators connect to the game server on a **separate WS path** from players. The envelope is identical to the player S2C envelope — same `type` codes, same `data` shapes — but the connection is read-only and omits the auth handshake.

### Connection

- **Path:** `/ws/spectate?run_id=<uuid>`
- **Handshake:** none. The accept loop inspects the query param; if a live run with that `run_id` is registered (and caps allow — see [04-lobby.md#caps-and-abuse-resistance](./04-lobby.md#caps-and-abuse-resistance) and [05-server-runtime.md#accept-path](./05-server-runtime.md#accept-path)), the upgrade succeeds. Otherwise HTTP `503` with `Retry-After`.
- **Auth:** none. Live spectating is intentionally public; the `run_id` is the per-run public handle minted by the engine in `initRun`. `player_id` is the WS bearer credential and is **not** exposed on the spectator surface or on `/api/live` — see issue #71 #1.
- **No `C2S_AUTH` / `C2S_START_RUN`** — those are player-only.

### Packet flow to spectators

Spectators receive the player's S2C stream with the following filter:

| S2C packet          | Sent to spectator? | Notes                                       |
| ------------------- | ------------------ | ------------------------------------------- |
| `S2C_AUTH_OK`       | **No**             | Auth is scoped to the player connection.    |
| `S2C_RUN_STARTED`   | Yes — as catchup   | Sent on spectator attach if run is mid-flight (so the viewer has the seed + starting room). |
| `S2C_STATE`         | Yes                | Every tick. This is the main feed. Drop-on-full at spectator outCh. |
| `S2C_ROOM_LOAD`     | Yes                | Including the latest one on attach, as catchup. |
| `S2C_RUN_COMPLETE`  | Yes                | Triggers spectator WS close after `SPECTATOR_CATCHUP_MS`. |
| `S2C_LEADERBOARD`   | Yes                | Spectators see leaderboard updates the same as the player. |
| `S2C_ERROR`         | Yes                | If the player errors out, spectators see it and are closed. |
| `S2C_DOOR_UNLOCKED` | Yes                | Rendering depends on it.                    |
| `S2C_ENTITY_DIED`   | Yes                | Rendering depends on it.                    |

The `seq` field on spectator-bound packets is **reset per-spectator** (starts at 1 on attach) — spectators cannot see the player's `seq` because they may attach mid-run.

### Read-only enforcement

Any WebSocket frame received from a spectator (binary or text of any type code) causes the server to:

1. Send an `S2C_ERROR` with code `SPECTATOR_READ_ONLY`, `message: "spectator connections are read-only"`.
2. Close the WebSocket with code `1008` (policy violation).

No spectator frames are parsed beyond the WS control layer; we don't dispatch them into the gameLoop's drain phase, so there is no attack surface from malformed spectator JSON.

### Catchup snapshot on attach

Because spectators can attach at any point during a run, on register the gameLoop sends an immediate catchup sequence to the new spectator's outCh **before** the next regular `S2C_STATE`:

1. `S2C_RUN_STARTED` with the run's `seed` and initial `room`.
2. The most recent `S2C_ROOM_LOAD` (so the viewer has the current room's layout, not the first room's).
3. The most recent `S2C_STATE` (so player/enemy/projectile positions are current).

This gives the viewer everything it needs to render from the first frame without waiting for the next tick.

## Implementation notes

- Package: `internal/protocol` (target).
- Envelope decode: one `Envelope` struct with `type`, `seq`, `ts`/`server_ts`, and `data json.RawMessage`. Dispatch on `type`, then unmarshal the raw message into the type-specific struct.
- Handlers live in `internal/game` and are dispatched from the `gameLoop` goroutine during its drain phase. See [05-server-runtime.md](./05-server-runtime.md).
- TypeScript counterpart: `client/src/net/packet.ts` already defines the envelope + typed `C2SDataFor<T>` / `S2CDataFor<T>` unions that match this spec 1:1.

## Implementation status

- **Client:** typed envelope + discriminated unions in `client/src/net/packet.ts`.
- **Server:** typed envelope live across `server/internal/protocol/` (decode) and `server/internal/game/loop.go` (dispatch). Single `gameLoop` goroutine drains `inCh`, runs the 20 Hz tick, and emits `S2C_*` envelopes via `outCh`.
- **Spectator protocol:** server-side fan-out (per-spectator outCh + catchup) shipped in #23. Client-side bundle (`client/src/spectate-main.ts` + `SpectatorSync` + `SpectateGame`) shipped in the same PR.

## Cross-references

- [05-server-runtime.md](./05-server-runtime.md) — dispatch/drain, tick loop, `ts` validation.
- [06-game-state-conventions.md](./06-game-state-conventions.md) — state types handlers mutate.
- [../bugs.md](../bugs.md) — central bug index.
