# Level Design Guide

A practical reference for editing rooms in Tiled. Pairs with the spec at
[`specs/features/rooms-doors.md`](./specs/features/rooms-doors.md), which
describes the runtime model — read this one when you're authoring; that one when
you're debugging the engine.

## Where rooms live

```
client/public/assets
  tilemap.png             — the source tilesheet (Aseprite next door)
client/public/assets/rooms/
  room-1.json             — the canonical room file you edit in Tiled
  room-2.json             — Vite serves these to the client at runtime
  ...
  room-10.json
server/internal/game/tilemaps/
  room-1.json             — Mirror of the client
  ...
```

**Sync rule:** Tiled writes to `client/public/assets/rooms/`. After saving in
Tiled, copy the same files into `server/internal/game/tilemaps/`. The server
embeds those at compile time and the engine won't pick up your edits until
they're mirrored. A simple `cp` works:

```bash
cp client/public/assets/rooms/room-*.json server/internal/game/tilemaps/
```

If the two copies drift, the server will use stale walls/markers and the client
will show the new look — confusing failure mode. Always mirror.

## Layer model

Each room is a 40×24 grid (1280×768 px at 32 px tiles). Standard layer stack in
Tiled:

| Layer     | Type         | What the engine uses it for                      |
| --------- | ------------ | ------------------------------------------------ |
| `Ground`  | Tile layer   | Pure visuals. No collision, no semantics.        |
| `Walls`   | Tile layer   | **Collision + water + doors.** See below.        |
| `Decor`   | Tile layer   | Pure visuals on top of Ground. No collision.     |
| `Objects` | Object layer | Marker objects (spawn, pickups, enemies, waves). |

The client renders all tile layers in JSON declaration order, so `Decor`
overdraws `Ground`/`Walls`. The server only reads:

- `Walls` for the collision grid (`parseWallGrid`)
- `Walls` for door tile detection (`parseTilemapDoors`)
- `Objects` for spawn / pickup / enemy / wave markers

### The Walls layer in detail

Every non-zero tile on this layer is a collision cell. The server classifies
each cell as one of three kinds:

- **Solid** — blocks movement _and_ projectiles. Default for any tile on this
  layer.
- **Water** — blocks movement only; projectiles fly over. Set the custom `water`
  boolean property on the tile in the tileset (see "Custom tile properties"
  below).
- **Door** — solid + carries door semantics (sprite swap on unlock,
  proximity-trigger to next room). Detected by GID, not properties.

Tile flips (Tiled's Z/X/Y keys) are honoured — the parser strips the top 3 flag
bits before classifying, so a flipped water tile still behaves as water.

## Custom tile properties

These live on the **tileset** (`art/defcon_tileset.tsx`, edited via the Tilesets
panel in Tiled). When the tileset is re-saved, the properties are inlined into
every room JSON's `tilesets[].tiles[]` block — that's where the server reads
them from. **Re-export the tileset and re-save each room** after changing tile
properties.

| Property | Type | Where it matters                                                                                           |
| -------- | ---- | ---------------------------------------------------------------------------------------------------------- |
| `water`  | bool | Tiles flagged `water=true` placed on `Walls` block movement only — bullets fly over. Used for ponds, pits. |

(Other properties exist in the `.tsx` — `collides`, `collidesPlayer`,
`collidesObjects` — but the **engine doesn't read them**. They're metadata for
tooling that isn't wired up yet.)

## Object markers

All markers go on the `Objects` layer. Place them as **point objects** (the
green dot tool, or any object with `width=0, height=0`). The marker's `(x, y)`
is the spawn position in pixels.

| Marker name                                       | What it does                           |
| ------------------------------------------------- | -------------------------------------- |
| `PlayerSpawn`                                     | Where the player appears on room load. |
| `Health`                                          | One health pickup at this position.    |
| `Ammo`                                            | One ammo pickup at this position.      |
| `Grunt` / `Runner` / `Tank` / `Kamikaze` / `Boss` | One initial enemy at this position.    |
| `Grunt` etc + `wave` property                     | A wave-reinforcement mob (see below).  |

Set the marker's **name** AND **class/type** to the same string (`PlayerSpawn` /
`Health` / `Grunt` / etc). The server checks both fields and accepts either, so
authoring is forgiving — but the recommendation is to set both for clarity in
the Tiled UI.

### Marker reference

#### `PlayerSpawn`

Exactly one per room (rooms 1-10). Determines where the player appears on
`S2C_ROOM_LOAD`. Without a marker, the server falls back to a heuristic
placement (bottom-center, near the south wall).

#### `Health` / `Ammo`

Any number per room. Health restores HP; Ammo refills the current weapon. The
marker position is the pickup's exact world position.

#### Enemy markers (initial spawns)

Set `name` (and `class/type`) to one of: `Grunt`, `Runner`, `Tank`, `Kamikaze`,
`Boss`.

The marker position is the **base** spawn — the server adds ±50 px of RNG jitter
at room load and snaps to the nearest non-wall tile. Don't tile-perfect-align
enemies; treat the position as "roughly here."

EntityIDs are slot-based by Tiled object iteration order. Reordering objects in
Tiled changes the IDs and shifts RNG consumption, which **invalidates replays of
older runs**. If you only re-arrange existing enemies, the play experience is
identical, but a saved replay seed will diverge.

#### Wave markers

A wave mob is just an enemy marker with one extra custom property:

| Property | Type | Value                                     |
| -------- | ---- | ----------------------------------------- |
| `wave`   | int  | 1 for first wave, 2 for second wave, etc. |

The `name` is your label — `Wave1 Grunt`, `Wave2 Tank`, etc — but **the server
reads only the `wave` property** to decide which slot the mob belongs to. The
naming convention is purely for visibility in the Tiled UI; you can call it
anything.

Wave triggers are global (`server/internal/game/constants.go`):

| Slot | Threshold (fraction of initial pop)                    |
| ---- | ------------------------------------------------------ |
| 1    | 0.66 (66%) — arms when 2/3 of initial enemies are dead |
| 2    | 0.33 (33%) — arms when 1/3 remain                      |

Only 2 wave slots exist. A wave mob with `wave=3` is silently ignored. Per-wave
fire delay is `1500 ± 1500 ms` jitter; waves fire in declaration order (Wave 2
cannot fire before Wave 1).

## Doors

Doors are **tiles**, not markers. The 3 locked-door variants live at GIDs `146`,
`150`, `154` in the JSON (the Tiled UI shows these as local IDs `145`, `149`,
`153` because Tiled UI is 0-indexed but the JSON adds `firstgid=1`). The
unlocked sprite for each variant is the adjacent tile (locked + 1 = unlocked).

| Locked GID | Unlocked GID |
| ---------- | ------------ |
| 146        | 147          |
| 150        | 151          |
| 154        | 155          |

**Authoring rules:**

1. Place locked-door tiles on the **Walls** layer. Doors on other layers are not
   detected.
2. Doors implicitly target `currentRoom + 1` — rooms are linear. Don't place
   door tiles in the boss room (room 10).
3. The locked tile blocks movement (it's on the Walls layer). Same for the
   unlocked tile after the swap — players don't walk _through_ the door, they
   walk **near** it (within 56 px) and get transported.
4. You can place multiple doors per room; whichever the player approaches first
   triggers the transition.

On level clear, the server flips each door to unlocked, broadcasts
`S2C_DOOR_UNLOCKED` with the tile coords + unlocked GID, and the client swaps
the rendered tile in place. There's no separate Door GameObject on the client —
the tile _is_ the door.

## Decor scattering

The Decor layer is pure visual cover. To scatter decor tiles randomly, open
Tiled's **View → Console** and paste a script like:

```javascript
const map = tiled.activeAsset;

let decor = null;
for (let i = 0; i < map.layerCount; i++) {
  const L = map.layerAt(i);
  if (L.name === "Decor" && L.isTileLayer) {
    decor = L;
    break;
  }
}
if (!decor) throw new Error("No tile layer named 'Decor' found");

const tileset = map.tilesets[0];
const tiles = [217, 221].map((id) => tileset.tile(id));
const probability = 0.025;

const edit = decor.edit();
// Skip the outer ring (border walls) — start at 1, stop one short.
for (let y = 1; y < map.height - 1; y++) {
  for (let x = 1; x < map.width - 1; x++) {
    if (Math.random() < probability) {
      edit.setTile(x, y, tiles[Math.floor(Math.random() * tiles.length)]);
    }
  }
}
edit.apply();
```

Change the tile ids in the script as you see fit.

A few things to know:

- Tile IDs in `tileset.tile(id)` are **local** (0-indexed within the tileset).
  The Tiled status bar shows the local ID when you hover a tile in the Tilesets
  panel.
- The script picks uniformly from the array; if you want one tile rarer than
  another, run the script twice with different `probability` values.
- Re-running the script _adds_ more decor on top of whatever's already there.
  Clear the layer first if you want a fresh roll.
- The script also has the side effect of skipping the border tiles (the 1-wide
  perimeter), so you don't accidentally paint over wall tiles.

## Per-room workflow

A typical session looks like:

1. Open `client/public/assets/rooms/room-N.json` in Tiled.
2. Edit ground tiles, walls, decor.
3. Place markers on the `Objects` layer for player spawn, pickups, enemies,
   waves.
4. Place door tiles on the `Walls` layer at the top border (or wherever the room
   exits should be).
5. Save (`Ctrl+S` in Tiled).
6. Mirror to the server:
   `cp client/public/assets/rooms/room-N.json server/internal/game/tilemaps/`.
7. Restart the game server (`go run ./cmd/server` from the `server/` directory).
8. Play through the room.

## Determinism gotchas

The replay system records run seed + input. Replaying a run regenerates mob
positions/patrols by re-consuming RNG in a fixed order. Two things in the
tilemap affect that order:

- **Initial-enemy iteration order on the Objects layer.** Re-ordering enemy
  markers in Tiled changes their EntityIDs and shifts RNG consumption. Older
  replay seeds will diverge.
- **Wave-mob iteration order within each wave.** Same story — mobs within a wave
  are seeded in the order they appear in the JSON.

If you're just re-skinning a room (moving Ground/Decor tiles, adding
Health/Ammo) replays stay valid. Adding/removing/reordering enemies or waves
invalidates them.

## Common gotchas

- **Server doesn't see your edits.** You forgot to mirror the JSON to
  `server/internal/game/tilemaps/` and restart the server.
- **Door doesn't exist in the game.** Door tile is on `Decor` instead of
  `Walls`. Move it.
- **Door doesn't visually unlock.** You placed the unlocked variant (locked + 1)
  instead of the locked variant. Verify the GID — the Tiled status bar shows the
  **local** ID, the JSON stores **GID = local + firstgid (= local + 1)**. Use
  the table in the Doors section above.
- **Bullets stop in mid-pond.** You forgot to set `water=true` on the water
  tiles in the tileset, then re-export the tileset, then re-save the room. The
  custom property is inlined into the room JSON on save — the server reads it
  from there, not from the standalone `.tsx`.
- **Wave never fires / fires in the wrong order.** The `wave` property must be a
  positive int (1-based). Waves fire in slot order; only the first 2 slots
  actually fire (others are silently dropped).
- **Replay diverges from original run.** You re-ordered or added/removed
  enemies. Expected — mark the seed as stale.
