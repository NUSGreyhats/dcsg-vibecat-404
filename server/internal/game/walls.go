package game

import (
	"embed"
	"encoding/json"
	"fmt"
)

// tilemapsFS holds the Tiled JSON exports for rooms 1..10. Room 11 (boss)
// has no tilemap — it falls back to an open room with outer bounds only.
//
//go:embed tilemaps/*.json
var tilemapsFS embed.FS

const (
	// Tile dimensions match the Tiled exports at client/public/assets/rooms/.
	TileSize   float32 = 32
	GridCols   int     = 40
	GridRows   int     = 24
	solidRoom0 uint8   = 0 // sentinel for "no walls"
)

// cellKind classifies what a wall-layer tile blocks. Movement is blocked
// by both kinds; projectiles pass over water but are absorbed by solids.
type cellKind uint8

const (
	cellEmpty cellKind = iota
	cellSolid          // brick wall — blocks movement and projectiles
	cellWater          // water tile — blocks movement, lets projectiles fly
)

// wallGrid is a 40×24 grid of cell classifications.
type wallGrid [GridRows][GridCols]cellKind

// wallGrids is keyed by room index (1..11). Rooms without a tilemap get
// an empty grid (outer bounds only).
var wallGrids [12]*wallGrid

// roomWaterGIDs is the per-room set of GIDs (already firstgid-adjusted)
// whose embedded tileset entry carries `water=true`. Tiles in this set
// still live on the "Walls" layer and block movement, but projectiles
// fly over them. Populated by parseWaterGIDs from each room JSON's
// inline tileset definition.
var roomWaterGIDs [12]map[int]struct{}

// parseWaterGIDs walks the room's embedded tilesets and returns the set
// of GIDs whose tile entry has `water=true` in its properties block.
// Tiled inlines the tileset (including custom properties) on export
// when the tileset is embedded, so this is the authoritative source —
// the standalone .tsx file can lag.
func parseWaterGIDs(m *tiledMap) map[int]struct{} {
	out := map[int]struct{}{}
	for _, ts := range m.Tilesets {
		first := ts.FirstGID
		if first <= 0 {
			first = 1
		}
		for _, t := range ts.Tiles {
			for _, p := range t.Properties {
				if p.Name != "water" || p.Type != "bool" {
					continue
				}
				if v, ok := p.Value.(bool); ok && v {
					out[t.ID+first] = struct{}{}
					break
				}
			}
		}
	}
	return out
}

// tiledLayer / tiledObject / tiledMap mirror the subset of the Tiled JSON
// schema we care about: a "Walls" tile layer whose non-zero cells are
// solid, and an "Objects" object layer carrying a PlayerSpawn marker.
type tiledLayer struct {
	Name    string        `json:"name"`
	Type    string        `json:"type"`
	Width   int           `json:"width"`
	Height  int           `json:"height"`
	Data    []int         `json:"data"`
	Objects []tiledObject `json:"objects"`
}

type tiledObject struct {
	Name string `json:"name"`
	// Tiled emits `type` (1.8 and earlier) or `class` (1.9+) for the
	// object's class field. Read both — exporters can pick either.
	Type       string              `json:"type"`
	Class      string              `json:"class"`
	X          float32             `json:"x"`
	Y          float32             `json:"y"`
	Properties []tiledTileProperty `json:"properties"`
}

type tiledMap struct {
	Width    int             `json:"width"`
	Height   int             `json:"height"`
	Layers   []tiledLayer    `json:"layers"`
	Tilesets []tiledTilesetR `json:"tilesets"`
}

// tiledTilesetR captures the room's inline tileset definition. Tiled
// inlines per-tile properties (including the `water` flag this code
// keys off) when the tileset is exported as embedded — see
// parseWaterGIDs.
type tiledTilesetR struct {
	FirstGID int                `json:"firstgid"`
	Tiles    []tiledTilesetTile `json:"tiles"`
}

type tiledTilesetTile struct {
	ID         int                `json:"id"`
	Properties []tiledTileProperty `json:"properties"`
}

type tiledTileProperty struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// Value is `any` because Tiled exports it polymorphically per property
	// type (bool / float / string / int). Callers narrow with a type
	// assertion. JSON-Unmarshal yields bool / float64 / string here.
	Value any `json:"value"`
}

// playerSpawn captures the authored PlayerSpawn marker for a room. `ok`
// is false when no marker exists or the room has no tilemap; callers
// fall back to the heuristic in placePlayerSpawn.
type playerSpawn struct {
	X, Y float32
	ok   bool
}

// playerSpawns is keyed by room index (1..11), parallel to wallGrids.
var playerSpawns [12]playerSpawn

// tilemapPickups holds Health/Ammo pickups authored as object-layer
// markers in the room's Tiled JSON. Keyed by room index (1..11).
var tilemapPickups [12][]PickupSpawnDef

// tilemapEnemies holds initial enemy spawns authored as object-layer
// markers in the room's Tiled JSON. Keyed by room index (1..11). The
// slice is sorted ascending by EntityID before LoadRoom consumes it,
// so RNG consumption order stays deterministic across replays.
var tilemapEnemies [12][]EnemySpawnDef

// tilemapWaves holds reinforcement-wave mobs authored as object-layer
// markers. Wave membership comes from the marker name's "WaveN" prefix:
// "Wave1 Grunt" → slot 0, "Wave2 Tank" → slot 1, etc. Each room's slice
// is dense (slot index = wave-slot) and is consumed by LoadRoom in
// declaration order, matching the seedWaves contract.
var tilemapWaves [12][]WaveDef

// tilemapDoors holds doors detected as locked-door tiles on the Walls
// layer. Each entry carries the tile position so the client can swap
// the sprite to the matching unlocked variant when the room clears.
var tilemapDoors [12][]DoorDef

// lockedToUnlockedDoorGID maps each locked-door GID (as it appears in
// the room JSON) to its unlocked variant. Tiled's UI shows local tile
// IDs (0-indexed), but JSON exports add firstgid=1, so the locked tiles
// the artist sees in Tiled as 145/149/153 are 146/150/154 in the JSON.
// The unlocked sprite sits one slot past the locked one.
var lockedToUnlockedDoorGID = map[int]int{
	146: 147,
	150: 151,
	154: 155,
}

// init parses the embedded tilemaps at process start. A failure on
// any room is fatal — the binary shouldn't boot with missing
// collision data.
func init() {
	for i := 1; i <= 10; i++ {
		name := fmt.Sprintf("tilemaps/room-%d.json", i)
		raw, err := tilemapsFS.ReadFile(name)
		if err != nil {
			panic(fmt.Sprintf("walls: read %s: %v", name, err))
		}
		var m tiledMap
		if err := json.Unmarshal(raw, &m); err != nil {
			panic(fmt.Sprintf("walls: parse %s: %v", name, err))
		}
		// Water set must be parsed before parseWallGrid so the grid
		// can classify cellWater vs cellSolid using the per-room map.
		roomWaterGIDs[i] = parseWaterGIDs(&m)
		wallGrids[i] = parseWallGrid(uint8(i), &m)
		playerSpawns[i] = parsePlayerSpawn(&m)
		tilemapPickups[i] = parseTilemapPickups(uint8(i), &m)
		tilemapEnemies[i], tilemapWaves[i] = parseTilemapEnemiesAndWaves(uint8(i), &m)
		tilemapDoors[i] = parseTilemapDoors(uint8(i), &m)
	}
}

// parsePlayerSpawn returns the room's authored PlayerSpawn marker, if
// any. Tiled exports point objects with width/height = 0 and a "type"
// field set in the object editor; we accept either `type` or `name`
// equal to "PlayerSpawn" so author conventions can drift slightly
// without breaking the load.
func parsePlayerSpawn(m *tiledMap) playerSpawn {
	for _, L := range m.Layers {
		if L.Type != "objectgroup" {
			continue
		}
		for _, o := range L.Objects {
			if o.Type == "PlayerSpawn" || o.Name == "PlayerSpawn" {
				return playerSpawn{X: o.X, Y: o.Y, ok: true}
			}
		}
	}
	return playerSpawn{}
}

// PlayerSpawnAt returns the authored PlayerSpawn position for the room,
// if a marker exists. Callers fall back to the heuristic placement when
// `ok` is false (no marker, or no tilemap).
func PlayerSpawnAt(roomIndex uint8) (x, y float32, ok bool) {
	if roomIndex >= uint8(len(playerSpawns)) {
		return 0, 0, false
	}
	s := playerSpawns[roomIndex]
	return s.X, s.Y, s.ok
}

// parseTilemapPickups walks every objectgroup layer for point objects
// whose Tiled `type` (or `name`) is "Health" or "Ammo" and returns them
// as PickupSpawnDef values. Object order in Tiled determines slot index,
// which feeds a deterministic EntityID — replays remain byte-stable as
// long as the tilemap isn't re-authored.
func parseTilemapPickups(roomIndex uint8, m *tiledMap) []PickupSpawnDef {
	var out []PickupSpawnDef
	for _, L := range m.Layers {
		if L.Type != "objectgroup" {
			continue
		}
		for _, o := range L.Objects {
			var t uint8
			switch {
			case o.Type == "Health" || o.Name == "Health":
				t = PickupHealthPotion
			case o.Type == "Ammo" || o.Name == "Ammo":
				t = PickupAmmoCrate
			default:
				continue
			}
			id := uint32(roomIndex)*100 + 50 + uint32(len(out)+1)
			out = append(out, PickupSpawnDef{
				EntityID: id,
				Type:     t,
				X:        o.X,
				Y:        o.Y,
			})
		}
	}
	return out
}

// TilemapPickups returns the Health/Ammo pickups authored in the room's
// Tiled JSON, or nil if the room has none (or no tilemap).
func TilemapPickups(roomIndex uint8) []PickupSpawnDef {
	if int(roomIndex) >= len(tilemapPickups) {
		return nil
	}
	return tilemapPickups[roomIndex]
}

// archetypeFromString maps a single string (a Tiled `class`, `type`, or
// `name` field) to its enemy archetype ID. Returns (0, false) when the
// string isn't a known archetype.
func archetypeFromString(s string) (uint8, bool) {
	switch s {
	case "Grunt":
		return EnemyGrunt, true
	case "Runner":
		return EnemyRunner, true
	case "Tank":
		return EnemyTank, true
	case "Kamikaze":
		return EnemyKamikaze, true
	case "Boss":
		return EnemyBoss, true
	}
	return 0, false
}

// archetypeFromMarker resolves an enemy archetype from a Tiled object,
// preferring `class` (Tiled 1.9+) then `type` (older), then `name` (so
// initial-enemy markers without a class still work). Returns (0, false)
// when the marker isn't an enemy.
func archetypeFromMarker(o tiledObject) (uint8, bool) {
	for _, s := range [3]string{o.Class, o.Type, o.Name} {
		if t, ok := archetypeFromString(s); ok {
			return t, true
		}
	}
	return 0, false
}

// waveSlotFromObject reads the 1-based "wave" int property authored on
// the Tiled object. Returns (0, false) when the property is absent or
// non-positive — caller treats those as initial-enemy markers.
func waveSlotFromObject(o tiledObject) (int, bool) {
	for _, p := range o.Properties {
		if p.Name != "wave" {
			continue
		}
		// Tiled int properties unmarshal as float64 in JSON. Accept
		// both float64 and int just in case an author uses string
		// inputs that the JSON layer pre-coerces.
		switch v := p.Value.(type) {
		case float64:
			n := int(v)
			if n >= 1 {
				return n, true
			}
		case int:
			if v >= 1 {
				return v, true
			}
		}
		return 0, false
	}
	return 0, false
}

// parseTilemapEnemiesAndWaves walks every objectgroup layer once and
// splits enemy markers into initial spawns vs. wave reinforcements.
//
//   - Initial: marker class/type matches an archetype (e.g. "Grunt") and
//     no `wave` property is set.
//   - Wave:    marker has a `wave` int property (1-based slot). The
//     archetype still comes from class/type — the name typically
//     carries a "Wave1 Grunt" label for visibility in Tiled but
//     isn't parsed by the server.
//
// Initial-enemy EntityIDs are slot-based (`roomIndex*100 + slot`) in the
// 1..49 band; pickups occupy 50..89 and doors 91..99. Wave mobs get IDs
// at fire time from RoomState.NextEnemyID, like before.
//
// Tiled object iteration order is the slot order — re-ordering objects
// in Tiled changes RNG consumption and therefore invalidates older
// replays.
func parseTilemapEnemiesAndWaves(roomIndex uint8, m *tiledMap) ([]EnemySpawnDef, []WaveDef) {
	var initial []EnemySpawnDef
	waveBuckets := map[int][]WaveMobDef{}
	maxSlot := -1
	for _, L := range m.Layers {
		if L.Type != "objectgroup" {
			continue
		}
		for _, o := range L.Objects {
			arch, ok := archetypeFromMarker(o)
			if !ok {
				continue
			}
			if slot1, isWave := waveSlotFromObject(o); isWave {
				slot := slot1 - 1
				waveBuckets[slot] = append(waveBuckets[slot], WaveMobDef{
					Type: arch,
					X:    o.X,
					Y:    o.Y,
				})
				if slot > maxSlot {
					maxSlot = slot
				}
				continue
			}
			id := uint32(roomIndex)*100 + uint32(len(initial)+1)
			initial = append(initial, EnemySpawnDef{
				EntityID: id,
				Type:     arch,
				X:        o.X,
				Y:        o.Y,
			})
		}
	}
	if maxSlot < 0 {
		return initial, nil
	}
	waves := make([]WaveDef, maxSlot+1)
	for slot, mobs := range waveBuckets {
		waves[slot] = WaveDef{Mobs: mobs}
	}
	return initial, waves
}

// TilemapEnemies returns the initial-enemy spawns authored in the room's
// Tiled JSON, or nil if the room has none.
func TilemapEnemies(roomIndex uint8) []EnemySpawnDef {
	if int(roomIndex) >= len(tilemapEnemies) {
		return nil
	}
	return tilemapEnemies[roomIndex]
}

// TilemapWaves returns the reinforcement waves authored in the room's
// Tiled JSON. Slots are dense (slot 0 = "Wave1", slot 1 = "Wave2"); a
// gap (e.g. only "Wave2" markers) yields an empty WaveDef in the lower
// slot. Returns nil if the room has no wave markers.
func TilemapWaves(roomIndex uint8) []WaveDef {
	if int(roomIndex) >= len(tilemapWaves) {
		return nil
	}
	return tilemapWaves[roomIndex]
}

// parseTilemapDoors scans the Walls tile layer for locked-door tiles
// (GIDs in lockedToUnlockedDoorGID) and emits one DoorDef per match.
//
//   - Position is the tile center (col*32+16, row*32+16).
//   - TargetRoom is `roomIndex + 1` — rooms are linear; the boss room
//     should not declare any door tiles.
//   - DoorID is slot-based (`roomIndex*100 + 90 + slot`) in the 91..99
//     band, parallel to the existing pickup/enemy ID layout.
//   - Tiles are walked in row-major order so DoorIDs stay stable across
//     loads of the same tilemap.
//
// Door tiles also classify as cellSolid via parseWallGrid (locked or
// unlocked, the player walks up to but not through them — entering is a
// proximity trigger, not a walk-through).
func parseTilemapDoors(roomIndex uint8, m *tiledMap) []DoorDef {
	var out []DoorDef
	for _, L := range m.Layers {
		if L.Type != "tilelayer" || L.Name != "Walls" {
			continue
		}
		if L.Width != GridCols || L.Height != GridRows {
			return nil
		}
		for i, raw := range L.Data {
			if raw == 0 {
				continue
			}
			gid := raw & 0x1FFFFFFF // strip flip bits
			unlocked, ok := lockedToUnlockedDoorGID[gid]
			if !ok {
				continue
			}
			col := i % GridCols
			row := i / GridCols
			id := uint32(roomIndex)*100 + 90 + uint32(len(out)+1)
			out = append(out, DoorDef{
				DoorID:      id,
				X:           float32(col)*TileSize + TileSize/2,
				Y:           float32(row)*TileSize + TileSize/2,
				TargetRoom:  roomIndex + 1,
				TileX:       col,
				TileY:       row,
				UnlockedGID: unlocked,
			})
		}
	}
	return out
}

// TilemapDoors returns the doors detected in the room's Walls layer, or
// nil if the room has none.
func TilemapDoors(roomIndex uint8) []DoorDef {
	if int(roomIndex) >= len(tilemapDoors) {
		return nil
	}
	return tilemapDoors[roomIndex]
}

func parseWallGrid(roomIndex uint8, m *tiledMap) *wallGrid {
	var g wallGrid
	water := roomWaterGIDs[roomIndex]
	for _, L := range m.Layers {
		if L.Type != "tilelayer" {
			continue
		}
		if L.Name != "Walls" {
			continue
		}
		if L.Width != GridCols || L.Height != GridRows {
			// Out-of-spec room — leave grid empty rather than panic.
			return &g
		}
		for i, tileID := range L.Data {
			if tileID == 0 {
				continue
			}
			// Tiled encodes per-tile flip flags in the top 3 bits of the
			// GID (H=0x80000000, V=0x40000000, D=0x20000000). Mask them
			// off before lookup, otherwise a flipped water tile reads as
			// an unknown GID and gets classified as cellSolid — which
			// makes projectiles bounce off it.
			gid := tileID & 0x1FFFFFFF
			r := i / GridCols
			c := i % GridCols
			if _, isWater := water[gid]; isWater {
				g[r][c] = cellWater
			} else {
				g[r][c] = cellSolid
			}
		}
	}
	return &g
}

// IsWallAt reports whether (x, y) is impassable for movement. Both solid
// walls and water count — entities can't walk into either. Out-of-bounds
// coordinates return true so callers treat "past the edge" as a wall.
func IsWallAt(roomIndex uint8, x, y float32) bool {
	return cellAt(roomIndex, x, y) != cellEmpty
}

// BlocksProjectileAt reports whether (x, y) absorbs a projectile. Solid
// walls do; water does not. Out-of-bounds coordinates return true so
// projectiles that fly off the map are culled.
func BlocksProjectileAt(roomIndex uint8, x, y float32) bool {
	return cellAt(roomIndex, x, y) == cellSolid
}

// cellAt returns the cell classification at (x, y). Out-of-bounds maps to
// cellSolid so callers see "past the edge" as a hard stop for both
// movement and projectiles.
func cellAt(roomIndex uint8, x, y float32) cellKind {
	if roomIndex >= uint8(len(wallGrids)) {
		return cellSolid
	}
	g := wallGrids[roomIndex]
	if g == nil {
		return cellEmpty
	}
	c := int(x / TileSize)
	r := int(y / TileSize)
	if c < 0 || c >= GridCols || r < 0 || r >= GridRows {
		return cellSolid
	}
	return g[r][c]
}

// NearestNonWall returns a (non-wall) world-space point close to (x, y),
// searching outward in a deterministic spiral. Consumes no RNG so it is
// safe to call after the RNG-consumption invariant in spec 07 has run.
// Returns the original point if it is already free, or if no non-wall
// tile is found within the search radius (caller should treat this as a
// stuck enemy — rare in practice for hand-authored maps).
func NearestNonWall(roomIndex uint8, x, y float32) (float32, float32) {
	const hw, hh float32 = 12, 12
	if !BlockedAABB(roomIndex, x, y, hw, hh) {
		return x, y
	}
	// Deterministic spiral over tile centers, increasing chebyshev radius.
	startC := int(x / TileSize)
	startR := int(y / TileSize)
	for radius := 1; radius <= 12; radius++ {
		for dr := -radius; dr <= radius; dr++ {
			for dc := -radius; dc <= radius; dc++ {
				// Only probe the ring at this radius.
				if abs(dr) != radius && abs(dc) != radius {
					continue
				}
				r := startR + dr
				c := startC + dc
				if r < 0 || r >= GridRows || c < 0 || c >= GridCols {
					continue
				}
				cx := float32(c)*TileSize + TileSize/2
				cy := float32(r)*TileSize + TileSize/2
				if !BlockedAABB(roomIndex, cx, cy, hw, hh) {
					return cx, cy
				}
			}
		}
	}
	return x, y
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// BlockedAABB reports whether an axis-aligned bounding box centered on
// (cx, cy) with half-extent hw/hh overlaps any wall tile. Sample the four
// corners — sufficient for tile-sized bodies.
func BlockedAABB(roomIndex uint8, cx, cy, hw, hh float32) bool {
	if IsWallAt(roomIndex, cx-hw, cy-hh) {
		return true
	}
	if IsWallAt(roomIndex, cx+hw, cy-hh) {
		return true
	}
	if IsWallAt(roomIndex, cx-hw, cy+hh) {
		return true
	}
	if IsWallAt(roomIndex, cx+hw, cy+hh) {
		return true
	}
	return false
}
