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
// Range: tileset rows 2..5 × cols 0..7 (0-indexed) at firstgid=1, 12 cols.
var waterGIDs = func() map[int]struct{} {
	const cols = 12
	m := make(map[int]struct{}, 32)
	for r := 2; r <= 5; r++ {
		for c := 0; c <= 7; c++ {
			m[r*cols+c+1] = struct{}{}
		}
	}
	return m
}()

// tiledLayer / tiledMap mirror the subset of the Tiled JSON schema we care
// about: a "Walls" tile layer whose non-zero cells are solid.
type tiledLayer struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Data   []int  `json:"data"`
}

type tiledMap struct {
	Width  int          `json:"width"`
	Height int          `json:"height"`
	Layers []tiledLayer `json:"layers"`
}

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
	}
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
