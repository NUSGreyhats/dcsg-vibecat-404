# Feature — Tilemap-Driven Room Content

## Purpose

Specification for the `feat/level-redesign` refactor that moved every
piece of authored room content out of Go code (`rooms_data.go`) and
into the per-room Tiled JSON. The runtime engine still owns
gameplay-mechanical behaviour (RNG seeding, wave triggers, door
unlock state); the tilemap is now the single source of truth for
*placement*.

This spec describes the post-refactor wire format, parser contracts,
and authoring conventions. For the runtime room/door state machine,
see [`rooms-doors.md`](./rooms-doors.md). For the dev-facing
how-to-author-a-level, see [`../../level-design.md`](../../level-design.md).

## Scope

Pre-refactor, `rooms_data.go` declared per room:

- `Enemies   []EnemySpawnDef`  — initial spawns
- `Pickups   []PickupSpawnDef` — Health/Ammo positions
- `Waves     []WaveDef`        — reinforcement-mob positions
- `Doors     []DoorDef`        — door positions + target rooms

Post-refactor, all four are derived from the tilemap. `RoomTemplate`
collapses to:

```go
type RoomTemplate struct {
    TilemapID string
}
```

Doors are inferred from special tiles on the `Walls` tile layer; the
other three come from point objects on the `Objects` object layer.

The boss room (former room 11) was merged into room 10 in the same
branch — `FinalRoom` is now `10`, `RoomTemplates` is `[11]` (index 0
unused, 1–10 active), and the post-room-9 door targets the new room
10 instead of a separate boss arena.

## Data model

### Server-side caches

`server/internal/game/walls.go` parses every embedded room JSON at
process start (`init()`) and populates these parallel arrays, all
keyed by `roomIndex` 1..10:

```go
var wallGrids       [12]*wallGrid                    // collision grid
var roomWaterGIDs   [12]map[int]struct{}             // water-tile GIDs (per-room, includes firstgid)
var playerSpawns    [12]playerSpawn                  // PlayerSpawn marker
var tilemapPickups  [12][]PickupSpawnDef             // Health/Ammo markers
var tilemapEnemies  [12][]EnemySpawnDef              // initial-enemy markers
var tilemapWaves    [12][]WaveDef                    // wave-reinforcement markers
var tilemapDoors    [12][]DoorDef                    // door-tile detections
```

The `[12]` size is index 0 (unused) plus 1..11 (slot 11 reserved for
the historical boss-room slot — currently unused but kept so old
constants don't underflow).

Each cache has a public accessor (`PlayerSpawnAt`, `TilemapPickups`,
`TilemapEnemies`, `TilemapWaves`, `TilemapDoors`). `LoadRoom` consumes
all of them when constructing a `RoomState`.

### Wire protocol changes

`protocol.DoorDef` and `protocol.DoorUnlockedData` grew three fields
to support tile-based door rendering on the client:

```go
type DoorDef struct {
    DoorID      uint32  `json:"door_id"`
    X           float32 `json:"x"`
    Y           float32 `json:"y"`
    TargetRoom  uint8   `json:"target_room"`
    Locked      bool    `json:"locked"`
    TileX       int     `json:"tile_x"`        // NEW
    TileY       int     `json:"tile_y"`        // NEW
    UnlockedGID int     `json:"unlocked_gid"`  // NEW
}

type DoorUnlockedData struct {
    DoorID      uint32  `json:"door_id"`
    TileX       int     `json:"tile_x"`        // NEW
    TileY       int     `json:"tile_y"`        // NEW
    UnlockedGID int     `json:"unlocked_gid"`  // NEW
}
```

`RoomData` itself is unchanged on the wire — pickups, enemies, and
doors still arrive on `S2C_RUN_STARTED` / `S2C_ROOM_LOAD`. Waves are
not exposed in `RoomData` (server-only, same as before).

No new packet types were added.

## Tilemap layer contract

Each room JSON is 40×24 tiles. The server reads:

| Layer name | Layer type    | Purpose                                                   |
| ---------- | ------------- | --------------------------------------------------------- |
| `Walls`    | `tilelayer`   | Collision + water classification + door-tile detection.   |
| `Objects`  | `objectgroup` | All point markers (PlayerSpawn / Health / Ammo / enemies). |

Other tile layers (`Ground`, `Decor`) exist for the client renderer
but are ignored by the server.

Layer name matching is case-sensitive. A typo (`walls` vs `Walls`)
silently produces an empty grid.

### Tile flip flags

Tiled encodes per-tile flip orientation in the top 3 bits of the GID
in `data` arrays:

| Bit          | Flag             |
| ------------ | ---------------- |
| `0x80000000` | horizontal flip  |
| `0x40000000` | vertical flip    |
| `0x20000000` | diagonal flip    |

`parseWallGrid` and `parseTilemapDoors` mask with `& 0x1FFFFFFF`
before any GID lookup, so flipped water/door tiles classify correctly.

### `firstgid` offset

Tiled UI displays local tile IDs (0-indexed within the tileset);
exported JSON adds `firstgid=1` to every GID in tile layer `data`
arrays. The server reads `tilesets[0].firstgid` from the room JSON
and applies the offset where the distinction matters (notably in
`parseWaterGIDs`, which keys its output map by GID rather than local
ID). Door GIDs in `lockedToUnlockedDoorGID` are stored as
post-firstgid values, so the locked variants the artist sees in
Tiled as `145`, `149`, `153` are referenced in code as `146`, `150`,
`154`.

## Walls layer details

### Cell classification

Every non-zero cell on the `Walls` layer becomes one of:

```go
const (
    cellEmpty cellKind = iota
    cellSolid          // brick wall — blocks movement and projectiles
    cellWater          // water tile — blocks movement, lets projectiles fly
)
```

`parseWallGrid(roomIndex, m)` walks the layer and:

1. For each cell: `gid = raw & 0x1FFFFFFF`.
2. Skip if zero.
3. Look up `gid` in the per-room `roomWaterGIDs[roomIndex]` set; if
   present → `cellWater`.
4. Otherwise → `cellSolid`.

### Water property

Water tiles are flagged in the **tileset** (per-tile custom property
`water` = bool). When the tileset is embedded in the room JSON
export, the property travels inline:

```json
{
  "id": 24,
  "properties": [
    {"name": "water", "type": "bool", "value": true}
  ]
}
```

`parseWaterGIDs(m)` walks `m.Tilesets[].Tiles[]` and emits the set of
GIDs (`tile.id + tileset.firstgid`) where any property satisfies
`name == "water" && type == "bool" && value == true`.

The set is per-room because each room JSON carries its own embedded
copy of the tileset (Tiled's "Embed Tileset" export option). If a
room is exported without embedding, the water set is empty for that
room and water tiles silently classify as `cellSolid`.

### Door tiles

The `Walls` layer doubles as the door layer. `parseTilemapDoors`
walks the layer once and emits one `DoorDef` per cell whose masked
GID matches the locked-door table:

```go
var lockedToUnlockedDoorGID = map[int]int{
    146: 147,
    150: 151,
    154: 155,
}
```

The unlocked variant for each locked tile is the immediately-adjacent
tile slot (`locked + 1`).

Each detected door produces:

```go
DoorDef{
    DoorID:      roomIndex*100 + 90 + slot,  // slot is 1-based, walk order
    X, Y:        tile center (col*32+16, row*32+16),
    TargetRoom:  roomIndex + 1,
    TileX, TileY: grid coords,
    UnlockedGID: lockedToUnlockedDoorGID[gid],
}
```

DoorIDs sit in the `91..99` band (parallel to the existing
EntityID-band convention: 1..49 enemies, 50..89 pickups, 91..99
doors). Walk order is row-major (left-to-right, top-to-bottom),
deterministic across loads of the same JSON.

`TargetRoom` is hardcoded to `roomIndex + 1` — rooms are linear.
Authors must not place door tiles in the boss room (room 10).

Door tiles also classify as `cellSolid` via `parseWallGrid` (locked
or unlocked, the player walks up to but not through them — entry is
proximity-triggered, not walk-through).

## Objects layer markers

All markers are point objects (`width=0, height=0`). The server
checks both the marker's `name` and `type` fields, accepting either
— the migration script writes both for clarity in the Tiled UI.

For Tiled 1.9+, the `class` field replaces `type`. The parser reads
all three (`o.Class`, `o.Type`, `o.Name`) so authoring is forgiving:

```go
type tiledObject struct {
    Name       string              `json:"name"`
    Type       string              `json:"type"`        // Tiled 1.8 and earlier
    Class      string              `json:"class"`       // Tiled 1.9+
    X          float32             `json:"x"`
    Y          float32             `json:"y"`
    Properties []tiledTileProperty `json:"properties"`
}
```

### `PlayerSpawn`

At most one per room. `parsePlayerSpawn` returns the first object
whose `name` or `type` is `"PlayerSpawn"`. When absent, callers
(`placePlayerSpawn`) fall back to a heuristic placement near the
south wall.

### `Health` / `Ammo`

Any number per room. `parseTilemapPickups(roomIndex, m)` walks every
object layer and emits one `PickupSpawnDef` per matching marker:

```go
PickupSpawnDef{
    EntityID: roomIndex*100 + 50 + slot,  // slot 1-based
    Type:     PickupHealthPotion or PickupAmmoCrate,
    X, Y:     marker position,
}
```

Slot derives from object iteration order in the JSON, which is
deterministic per export. EntityIDs sit in the `50..89` band.

### Enemy markers (initial spawns + waves)

A single parser, `parseTilemapEnemiesAndWaves(roomIndex, m)`, splits
markers into initial spawns vs. wave reinforcements based on a
custom `wave` property:

```go
func parseTilemapEnemiesAndWaves(roomIndex uint8, m *tiledMap) ([]EnemySpawnDef, []WaveDef)
```

For each object whose class/type/name resolves to a known archetype
(`Grunt`, `Runner`, `Tank`, `Kamikaze`, `Boss`):

- If `properties[]` contains `{name: "wave", type: "int", value: N}`
  with `N >= 1` → wave mob, slotted into `waveBuckets[N-1]`.
- Otherwise → initial enemy.

Wave mobs become `WaveMobDef{Type, X, Y}` (no EntityID — IDs are
allocated at fire time from `RoomState.NextEnemyID`).

Initial enemies become `EnemySpawnDef{EntityID, Type, X, Y}` with
`EntityID = roomIndex*100 + slot` (1..49 band).

Wave buckets are densified into `[]WaveDef` indexed by slot. A gap
in slot numbers (e.g. only `wave=2` markers) yields an empty
`WaveDef` in the lower slot — `seedWaves` then consumes RNG for the
empty slot but spawns nothing.

Wave triggers remain in `constants.go`:

```go
var WaveTriggers = []float32{0.66, 0.33}
```

A mob with `wave >= 3` is silently dropped because `seedWaves`
breaks at `slot >= len(WaveTriggers)`.

The `name` field on a wave marker is human-only ("Wave1 Grunt",
"Wave2 Tank") — the server reads only the `wave` property to
determine slot.

### Wave fire ordering

`AdvanceWaves` (in `waves.go`) gates the fire pass on prior-slot
completion: a wave is held back until every earlier slot has fired,
even if its own `FireAt` has elapsed. This prevents a +1500 / -1500
ms jitter combination from making Wave 2 spawn before Wave 1 when
both arm in the same tick (e.g., a fast multi-kill).

```go
for i := range r.PendingWaves {
    w := &r.PendingWaves[i]
    if !w.Armed || w.Fired { continue }
    if now.Before(w.FireAt) { continue }
    if i > 0 && !r.PendingWaves[i-1].Fired { continue }   // NEW: order gate
    // ... fire ...
}
```

## File mirroring contract

The same room JSON exists in two places:

```
client/public/assets/rooms/room-N.json   — Tiled writes here, Vite serves here
server/internal/game/tilemaps/room-N.json — server embeds via go:embed
```

These must be byte-identical. The server's `init()` panics on parse
errors but does NOT cross-check against the client copy; drift
manifests as silent gameplay/visual divergence (server uses old
walls/markers, client renders new look).

The boss room previously had no tilemap (`room-11.json` did not
exist). After the merge, the boss arena is room 10 with its existing
tilemap; no `room-11.json` files exist.

## Determinism implications

The replay system records the run seed and input log; replaying
re-consumes RNG in a fixed order (see
[`../foundations/07-determinism-seeding.md`](../foundations/07-determinism-seeding.md)).

Two authoring actions affect RNG consumption order and therefore
invalidate older replay seeds:

1. **Reordering objects within the Objects layer.** Initial-enemy
   EntityIDs are slot-based; reordering shifts every enemy's ID and
   therefore the RNG-consumption sequence. Wave mobs within a slot
   are seeded in object iteration order; reordering them shifts the
   seed of every later wave mob.

2. **Adding/removing initial enemies or wave mobs.** Same root cause
   — the slot-based numbering shifts for everything past the
   insertion point.

Authoring actions that do NOT affect determinism:

- Editing Ground / Decor / Walls visuals (tile swaps, decor
  scattering, wall reshaping).
- Adding/removing/moving Health/Ammo markers (pickups don't consume
  RNG).
- Adding/removing/moving doors (door detection consumes no RNG).
- Adding/removing PlayerSpawn marker (deterministic resolution to a
  fixed position).

## ID-band conventions

EntityIDs and DoorIDs all derive from the room index, with bands
chosen so the four marker types never collide:

| Band             | What                          | Formula                                     |
| ---------------- | ----------------------------- | ------------------------------------------- |
| `roomIndex*100 + 1..49`  | Initial enemies      | `roomIndex*100 + slot` (slot 1-based)       |
| `roomIndex*100 + 50..89` | Pickups              | `roomIndex*100 + 50 + slot`                 |
| `roomIndex*100 + 91..99` | Doors                | `roomIndex*100 + 90 + slot`                 |
| `WaveEntityIDBase + …`   | Wave-spawned mobs    | Per-room counter from `RoomState.NextEnemyID` |

Wave-mob IDs are allocated at fire time, not at parse time, because
the wave-fire ordering and live enemy state at trigger time
determine when each mob lands. They do NOT collide with the
slot-based IDs because `WaveEntityIDBase = 100000` is well above any
plausible `roomIndex*100 + slot` value.

## Migration notes

The `feat/level-redesign` branch contains four sub-migrations,
landed in this order:

1. **Pickups → tilemap** — `parseTilemapPickups`, `Health` / `Ammo`
   markers. `Pickups` field removed from `RoomTemplate`. (Commit
   `4b2fe05`.)

2. **Initial enemies → tilemap** — `parseTilemapEnemies`, archetype
   markers. `Enemies` field removed. (Commit `2b671a0`.)

3. **Boss room merge** — `FinalRoom = 10`, `RoomTemplates` shrunk to
   `[11]`. Boss + 3 grunts authored as markers in `room-10.json`,
   waves replaced with the boss-themed runner / kamikaze waves.
   (Commit `e7b32e2`.)

4. **Wave mobs → tilemap** — `parseTilemapEnemiesAndWaves` (parsers
   merged), `wave` int property routes mobs to slots.
   `parseTilemapEnemies` deleted. `Waves` field removed. (Commit
   `3c260f4`.)

5. **Tile-based doors** — `parseTilemapDoors`, `DoorDef` extended,
   `Door` GameObject removed from client (the tile *is* the door).
   `Doors` field removed. (Commit `5d49f60`.)

After all five, `RoomTemplate` is just `{TilemapID string}` — at
this point it could collapse to a `map[uint8]string` or a derived
constant if no other per-room scalar is needed.

## Server initialization order

`walls.go init()` runs at process start:

```go
for i := 1; i <= 10; i++ {
    raw := embedded room-i.json
    var m tiledMap; json.Unmarshal(raw, &m)

    roomWaterGIDs[i]  = parseWaterGIDs(&m)            // must precede parseWallGrid
    wallGrids[i]      = parseWallGrid(uint8(i), &m)   // reads roomWaterGIDs
    playerSpawns[i]   = parsePlayerSpawn(&m)
    tilemapPickups[i] = parseTilemapPickups(uint8(i), &m)
    tilemapEnemies[i], tilemapWaves[i] = parseTilemapEnemiesAndWaves(uint8(i), &m)
    tilemapDoors[i]   = parseTilemapDoors(uint8(i), &m)
}
```

`parseWaterGIDs` must run before `parseWallGrid` because the latter
classifies cells as `cellWater` only when their GID appears in the
per-room water set.

A panic in any room's parse aborts startup — the binary does not
boot with malformed walls/markers.

## `LoadRoom` contract

`LoadRoom` (in `rooms.go`) takes the room template + caller-supplied
enemy/wave slices and produces a fully-populated `RoomState` plus
the `protocol.RoomData` for the client:

```go
func LoadRoom(
    r *RoomState,
    rng *rand.Rand,
    tmpl *RoomTemplate,
    enemies []EnemySpawnDef,
    waves []WaveDef,
    now time.Time,
) protocol.RoomData
```

Production callers pass `TilemapEnemies(roomIndex)` and
`TilemapWaves(roomIndex)` directly. Tests can pass hand-built slices
to exercise determinism without depending on the embedded tilemaps
(see `waves_test.go: TestLoadRoomRNGConsumptionInvariant`).

Doors come from `TilemapDoors(roomIndex)` — `LoadRoom` calls the
accessor inline because doors don't participate in the
RNG-consumption invariant.

`RoomTemplate` is currently used only for `TilemapID` (passed
through to `protocol.RoomData.TilemapID`). The argument position
remains reserved for future per-room scalars.

## Client-side rendering

### Door tile swap

The client renders door tiles as part of the Walls tile layer (no
separate Door GameObject). `paintRoomBackground` returns the
constructed `Phaser.Tilemaps.TilemapLayer` for the Walls layer
specifically, in addition to the flat list of created GameObjects:

```ts
type PaintedBackground = {
  created: Phaser.GameObjects.GameObject[];
  wallsLayer?: Phaser.Tilemaps.TilemapLayer;
};
```

The scene captures `wallsLayer` and on `S2C_DOOR_UNLOCKED` calls
`wallsLayer.putTileAt(unlockedGid, tileX, tileY, true)` — atomic
sprite swap, no entity teardown.

A lightweight `DoorRef` lookup map (`Map<doorId, {x, y, tileX, …}>`)
holds enough state for the proximity check
(`nearestUnlockedDoorId`). The previous `Door` GameObject and
`entities/Door.ts` were removed.

### Proximity radius

`nearestUnlockedDoorId` is called every frame with a 56 px radius
(was 28 px pre-refactor). The wider radius accommodates
horizontal-axis offset when the player approaches the top-border
door (door tile center at y=16, player can only reach y≈44 against
the wall, so the strict 28 px required pixel-perfect column
alignment).

## Constants moved or removed

| Constant                       | Before                | After                                |
| ------------------------------ | --------------------- | ------------------------------------ |
| `FinalRoom`                    | `11`                  | `10`                                 |
| `BossRoom`                     | `11`                  | `10`                                 |
| `client.constants.ROOM_COUNT`  | `11`                  | `10`                                 |
| `client.constants.WIN_ROOM`    | `11`                  | `10`                                 |
| `RoomTemplates` array size     | `[12]*RoomTemplate`   | `[11]*RoomTemplate`                  |

Existing replay seeds from before the merge are invalidated — both
because the boss-room merge changed RNG consumption, and because all
EntityIDs were re-derived from object iteration order rather than
hand-assigned.
