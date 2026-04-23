# 10 — Client Architecture

## Purpose

Defines the Phaser 3 client's structure: scene graph, packet layer, how client-side state mirrors the server authoritative state, and the asset pipeline. The client is deliberately "dumb" — it renders what the server says and serializes user intent back as C2S packets. All exploit surface lives at the packet boundary, not in the scene code.

## Scene graph

```
Boot → Preloader → MainMenu → Game (+ overlays)
```

### Scene table — append new rows when adding scenes

| Scene             | File                                      | Role                                   |
| ----------------- | ----------------------------------------- | -------------------------------------- |
| `Boot`            | `client/src/game/scenes/Boot.ts`          | Phaser bootstrap                       |
| `Preloader`       | `client/src/game/scenes/Preloader.ts`     | Asset loading                          |
| `MainMenu`        | `client/src/game/scenes/MainMenu.ts`      | Player ID entry, start screen          |
| `Game`            | `client/src/game/scenes/Game.ts`          | Main gameplay loop, physics, rendering |
| `GameHUD`         | `client/src/game/scenes/GameHUD.ts`       | Overlay: HP, ammo, timer, splits       |
| `PauseMenu`       | `client/src/game/scenes/PauseMenu.ts`     | Pause/resume overlay                   |
| `InventoryMenu`   | `client/src/game/scenes/InventoryMenu.ts` | Inventory overlay                      |
| `ShopMenu`        | `client/src/game/scenes/ShopMenu.ts`      | Shop overlay                           |
| `WinScreen`       | `client/src/game/scenes/WinScreen.ts`     | Run complete / leaderboard             |

Game + overlays run concurrently via `scene.launch()`. Overlays receive `S2C_STATE` snapshots via the shared `GameState` object.

## Packet layer

- **Envelope + types:** `client/src/net/packet.ts`. Defines `C2SType`, `S2CType`, discriminated unions `C2SDataFor<T>` / `S2CDataFor<T>`, and sub-structures (`RoomData`, `EnemySpawn`, etc.). Matches [02-protocol.md](./02-protocol.md) 1:1.
- **Net sync:** `client/src/game/net/GameStateSync.ts`. Owns the WebSocket, serializes outbound C2S, deserializes and routes inbound S2C.
- **Client state:** `client/src/game/net/GameState.ts`. Mirrors server snapshots into objects the scenes render. **Non-authoritative** — client does NOT compute elapsed time, HP, or game rules locally. It only interpolates positions between `S2C_STATE` frames for smooth rendering.

## Entity / world modules

- `client/src/game/entities/Player.ts` — player sprite, input binding, movement interpolation.
- `client/src/game/entities/Enemy.ts` — enemy sprites + AI-state visualization.
- `client/src/game/entities/Bullet.ts` — projectile rendering.
- `client/src/game/entities/Coin.ts` — pickup rendering.
- `client/src/game/world/roomLayout.ts` — room/door/spawn definitions as consumed from `S2C_ROOM_LOAD`.
- `client/src/game/weapons.ts` — weapon metadata (must match `server/main.go` `weaponSpecs`, see [../features/combat-weapons.md](../features/combat-weapons.md)).
- `client/src/game/shop.ts` — shop-side scaffolding (future feature hookup).
- `client/src/game/constants.ts` — `ROOM_WIDTH=1280`, `ROOM_HEIGHT=768`, `WS_URL`, etc.

## Data flow

```
keyboard / mouse (Phaser input)
    │
    ▼
Game scene → Controls.ts maps input → C2SEnvelope<INPUT|SHOOT|...>
    │
    ▼
GameStateSync.send(envelope)
    │
    ▼ WebSocket
server gameLoop (authoritative) → S2C_STATE / S2C_*
    │
    ▼ WebSocket
GameStateSync.onMessage → GameState.apply(snapshot)
    │
    ▼
Game + overlay scenes read GameState and re-render
```

The client's Phaser Arcade physics runs independently for smooth interpolation only. Server state always overrides client state on each `S2C_STATE` received.

## Asset pipeline

- **Tilemaps:** Tiled JSON exports — `client/public/assets/rooms/room-1.json` … `room-10.json`. Keyed by `tilemap_id` in `RoomData`.
- **Spritesheets:** Aseprite-authored PNG + JSON atlases — `player.png/json`, `enemy.png`, `bullets.png/json`, `coin.png`, `heart.png`, weapon_* variants.
- **UI:** `title.png`, `logo.png`, `bg.png`.
- **Tileset image:** `tilemap.png`, `topdown-tilesheet-extruded.png`.

Assets stay in `client/public/assets/` so Vite serves them directly; raw source (`.aseprite`) lives in the repo's top-level `art/` directory.

## Implementation notes

- Phaser version: 3.90.0 (see `client/package.json`).
- Build tool: Vite.
- Add new scenes by registering them in `client/src/game/main.ts` and updating the scene table above.
- Add new packet types: append in `client/src/net/packet.ts` (discriminated union) and in [02-protocol.md](./02-protocol.md) packet registry.

## Implementation status

- `client/src/net/packet.ts` — typed envelope fully defined (1:1 with spec).
- `client/src/game/net/GameStateSync.ts` — currently speaks the *legacy* single-blob `gameState` protocol, not the typed envelope. Migration is later work.
- All scene files listed above exist. Some are scaffolds pending server-side features.

## Intentional bugs in this area

None. All exploits happen at the packet boundary. The client is the tool competitors use to build custom clients that fire crafted packets — don't put logic here that they'd need to recreate.

## Cross-references

- [02-protocol.md](./02-protocol.md) — envelope, packet types, sub-structures.
- [05-server-runtime.md](./05-server-runtime.md) — server side of the WS.
- [06-game-state-conventions.md](./06-game-state-conventions.md) — snapshot shapes mirrored in the client.
- [../features/combat-weapons.md](../features/combat-weapons.md) — `weapons.ts` source of truth.
- [../features/rooms-doors.md](../features/rooms-doors.md) — `roomLayout.ts` and room tilemaps.
