# 02 — Protocol

## Purpose

Defines the WebSocket wire protocol: packet envelope shape, the C2S / S2C packet registry, sub-structures passed inside `data` payloads, JSON parser semantics (including the intentional parser differential that enables BUG-09), and the dispatch pipeline on the server.

## Envelope

All traffic over the per-player WebSocket is JSON. Every packet has a typed envelope.

### Client → Server (C2S)

```
{ "type": uint8, "seq": uint32, "ts": int64, "data": { ... } }
```

- `type` — one of the C2S type codes (0x01–0x08).
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

### C2S — append new rows below `0x08` when adding features

| Type | Name                | `data` Fields                  |
| ---- | ------------------- | ------------------------------ |
| 0x01 | `C2S_AUTH`          | `{ player_id: string }`        |
| 0x02 | `C2S_START_RUN`     | `{ start_time: int64 }`        |
| 0x03 | `C2S_INPUT`         | `{ dx: float32, dy: float32 }` |
| 0x04 | `C2S_SHOOT`         | `{ aim_angle: float32 }`       |
| 0x05 | `C2S_SWITCH_WEAPON` | `{ weapon_id: uint8 }`         |
| 0x06 | `C2S_INTERACT`      | `{ target_id: uint32 }`        |
| 0x07 | `C2S_ENTER_DOOR`    | `{ door_id: uint32 }`          |
| 0x08 | `C2S_USE_ITEM`      | `{ item_name: string }`        |

### S2C — append new rows below `0x89` when adding features

| Type | Name                | `data` Fields                                                           |
| ---- | ------------------- | ----------------------------------------------------------------------- |
| 0x81 | `S2C_AUTH_OK`       | `{ display_name }`                                                      |
| 0x82 | `S2C_RUN_STARTED`   | `{ seed: int64, room: RoomData }`                                       |
| 0x83 | `S2C_STATE`         | `{ elapsed_ms, player: PlayerSnapshot, entities: [], projectiles: [] }` |
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
| `PlayerSnapshot`   | `{ x, y, hp, weapon_id, ammo, inventory: []string }`                                          |
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
[pre-processing / audit middleware]      ← parser P1 (may be manual / different)
    │                                       logs e.g. item usage
    ▼
encoding/json.Unmarshal(envelope)        ← parser P2 (stdlib)
    │
    ▼
dispatch by type  ──► handler per type
    │
    ▼
mutates game state (owned by gameLoop goroutine)
```

**Critical:** parser P1 (the logging/audit middleware) and parser P2 (`encoding/json` used for dispatch) can disagree on the same raw bytes. The game keeps this disagreement intentionally — it's the core mechanism of BUG-09 (below).

## JSON parser semantics (footguns)

Go's `encoding/json`:

1. Unmarshals struct fields **case-insensitively** by default. `{"Weapon_ID": 5}`, `{"weapon_id": 5}`, and `{"WEAPON_ID": 5}` all match the same struct field.
2. Silently accepts **duplicate keys** — last one wins.

Source: [Trail of Bits — Unexpected security footguns in Go's parsers](https://blog.trailofbits.com/2025/06/17/unexpected-security-footguns-in-gos-parsers/).

Both behaviors must be preserved — they are the parser half of BUG-09.

## Spectator protocol

Spectators connect to the game server on a **separate WS path** from players. The envelope is identical to the player S2C envelope — same `type` codes, same `data` shapes — but the connection is read-only and omits the auth handshake.

### Connection

- **Path:** `/ws/spectate?player_id=<hex>`
- **Handshake:** none. The accept loop inspects the query param; if the target player is actively running (and caps allow — see [04-lobby.md#caps-and-abuse-resistance](./04-lobby.md#caps-and-abuse-resistance) and [05-server-runtime.md#accept-path](./05-server-runtime.md#accept-path)), the upgrade succeeds. Otherwise HTTP `503` with `Retry-After`.
- **Auth:** none. Live spectating is intentionally public; the `player_id` is effectively public once the run is listed on `/api/live`.
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

- **Client:** typed envelope + discriminated unions fully defined in `client/src/net/packet.ts`.
- **Server:** current server (`server/main.go`) does NOT yet use this envelope — it accepts a single monolithic `gameState` JSON every ~1.5 s with server-side validation. Migration to the typed envelope is later work.

## Intentional bugs in this area

### BUG-09 JSON Case-Insensitive Field Smuggling

**Source:** [Trail of Bits — Unexpected security footguns in Go's parsers](https://blog.trailofbits.com/2025/06/17/unexpected-security-footguns-in-gos-parsers/).

**Design choices enabling the bug:**

1. `encoding/json` unmarshals case-insensitively.
2. `encoding/json` takes the last of duplicate keys.
3. A pre-processing middleware logs item usage using a **different** parser (or a manual string search) that is case-sensitive and/or uses first-key-wins semantics.
4. A fallback/debug item dispatch path still exists in the item handler (see [../features/inventory-items.md](../features/inventory-items.md)).

**Root cause sketch:**

```go
type UseItemPacket struct {
    ItemName string `json:"item_name"`
}

var allowedItems = map[string]ItemDef{
    "health_potion":  { ... },
    "speed_boost":    { ... },
    "shield":         { ... },
}

var debugItems = map[string]ItemDef{
    "instakill":     { damage: 9999 },
    "teleport":      { ... },
    "timewarp":      { duration: -5000 },  // subtracts 5s from elapsed
}

func handleUseItem(pkt UseItemPacket, player *PlayerState) {
    item, ok := allowedItems[pkt.ItemName]
    if !ok {
        // fall through to debugItems as a left-in fallback
        return
    }
    applyItem(item, player)
}
```

The dispatcher front-end calls `strings.EqualFold` against an allowlist built from `allowedItems` keys only; the logging middleware parses the raw JSON with a different parser that is case-sensitive and takes the **first** `item_name` key.

**Exploit path:**

Client sends a `C2S_USE_ITEM` with duplicate `item_name` keys:

```json
{
  "type": 8,
  "seq": 1,
  "ts": 123,
  "data": {"item_name": "HEALTH_POTION", "item_name": "timewarp"}
}
```

- Logging middleware (parser P1, first-key-wins) sees `"HEALTH_POTION"` → logs legitimate item use.
- `encoding/json` (parser P2, last-key-wins) produces `ItemName = "timewarp"`.
- `strings.EqualFold` against the allowlist fails — BUT the fallback debug dispatch path fires and `timewarp` subtracts 5 s from elapsed time, or `instakill` clears a tough enemy.

**Bound:**
- `timewarp` is limited to one use per room (server tracks usage).
- `instakill` only works on non-boss enemies.
- Debug item names (`timewarp`, `instakill`, `teleport`) are discoverable in the stripped source but not hinted in-game.

**Detection:**
- Audit log records the benign first `item_name`; leaderboard replay will mismatch if the log is cross-referenced with the effective game state.
- Do NOT add a "duplicate key check" in `encoding/json` — that kills the bug.

**Synergies:**
- `timewarp` (−5 s) stacks with seed-planned routing — see [BUG-01](../features/mob-ai.md#bug-01-mob-seed-optimization).
- `instakill` can shorten boss setup — see [BUG-08](../features/combat-weapons.md#bug-08-hp-integer-overflow-healing-past-max).

**See also:** BUG-10 at [../features/inventory-items.md#bug-10-rune-vs-byte-item-length-bypass](../features/inventory-items.md#bug-10-rune-vs-byte-item-length-bypass) (dispatch-side details of item handling).

## Cross-references

- [05-server-runtime.md](./05-server-runtime.md) — dispatch/drain, tick loop, `ts` validation.
- [06-game-state-conventions.md](./06-game-state-conventions.md) — state types handlers mutate.
- [../features/inventory-items.md](../features/inventory-items.md) — BUG-09 dispatch-side details.
- [../bugs.md](../bugs.md) — central bug index.
