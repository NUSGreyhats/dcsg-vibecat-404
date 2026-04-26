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

// waterGIDs are the tileset GIDs that render as water. They live in the
// "Walls" layer because they block movement, but bullets fly over them.
// Range: tileset rows 2..6 × cols 0..11 (0-indexed) at firstgid=1, 12 cols.
var waterGIDs = func() map[int]struct{} {
	const cols = 12
	m := make(map[int]struct{}, 60)
	for r := 2; r <= 6; r++ {
		for c := 0; c <= 11; c++ {
			m[r*cols+c+1] = struct{}{}
		}
	}
	return m
}()

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
	Name string  `json:"name"`
	Type string  `json:"type"`
	X    float32 `json:"x"`
	Y    float32 `json:"y"`
}

type tiledMap struct {
	Width  int          `json:"width"`
	Height int          `json:"height"`
	Layers []tiledLayer `json:"layers"`
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

// init parses the embedded tilemaps at process start. A failure on one
// room is logged via panic — the binary shouldn't boot with missing walls.
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
		grid := parseWallGrid(&m)
		wallGrids[i] = grid
		playerSpawns[i] = parsePlayerSpawn(&m)
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

func parseWallGrid(m *tiledMap) *wallGrid {
	var g wallGrid
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
			r := i / GridCols
			c := i % GridCols
			if _, isWater := waterGIDs[tileID]; isWater {
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
