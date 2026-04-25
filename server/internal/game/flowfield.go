package game

// Flow-field pathfinding for mob aggro chase.
//
// See docs/specs/features/mob-ai.md § "Aggro chase: flow field" for the
// authoritative spec. Summary:
//
//   - Per tick, BFS from the player's tile over the static wall grid.
//     Output is a tile-distance field consumed by every mob in Aggro state.
//   - A mob picks the 4-neighbour with the lowest distance; ties broken
//     by fixed enumeration order N, E, S, W.
//   - Per-mob aggro jitter is reinterpreted as a deterministic ±1 tile
//     bias on the BFS source — preserves BUG-01 seed-search depth without
//     per-tick MobRNG.
//   - The BFS is a pure function of (roomIndex, sourceTile). Determinism
//     invariant is documented in docs/specs/foundations/07-determinism-seeding.md.

// FlowFieldSentinel is the "unreachable" distance marker. Tiles that the BFS
// never visits (walls, or open tiles fenced off from the source) keep this
// value, which sorts above any reachable distance.
const FlowFieldSentinel uint16 = 0xFFFF

// FlowField holds tile-step distances from a single BFS source over a
// 40×24 room grid. Index order: [row][col]. Walls and unreachable tiles
// hold FlowFieldSentinel.
type FlowField [GridRows][GridCols]uint16

// Neighbour enumeration order — fixed for determinism. Used by both the
// BFS queue and NextStepTile. See spec § Determinism invariants.
//
// Order: N (row-1), E (col+1), S (row+1), W (col-1).
var flowNeighbours = [4]struct{ dr, dc int }{
	{-1, 0}, // N
	{0, 1},  // E
	{1, 0},  // S
	{0, -1}, // W
}

// TileOf converts a pixel-space (x, y) to (row, col) tile coordinates.
// Out-of-bounds inputs are clamped to the grid; callers should not rely on
// this for "is the position in a wall" queries — use BlockedAABB for that.
func TileOf(x, y float32) (int, int) {
	c := int(x / TileSize)
	r := int(y / TileSize)
	if c < 0 {
		c = 0
	} else if c >= GridCols {
		c = GridCols - 1
	}
	if r < 0 {
		r = 0
	} else if r >= GridRows {
		r = GridRows - 1
	}
	return r, c
}

// TileCenter returns the pixel-space center of tile (r, c).
func TileCenter(r, c int) (x, y float32) {
	return float32(c)*TileSize + TileSize/2, float32(r)*TileSize + TileSize/2
}

// tileIsWall reports whether tile (r, c) blocks movement. Mirrors the
// movement-blocking check in walls.go (cellSolid + cellWater both block).
// Out-of-bounds is treated as wall.
func tileIsWall(roomIndex uint8, r, c int) bool {
	if r < 0 || r >= GridRows || c < 0 || c >= GridCols {
		return true
	}
	if roomIndex >= uint8(len(wallGrids)) {
		return true
	}
	g := wallGrids[roomIndex]
	if g == nil {
		return false
	}
	return g[r][c] != cellEmpty
}

// BFSFromTile returns a flow field rooted at (sr, sc) for the given room.
// Walls (solid + water) are impassable. Distances are measured in tile
// steps over a 4-neighbourhood.
//
// Determinism: the BFS is a pure function of (roomIndex, sr, sc). Neighbour
// enumeration order is fixed at N, E, S, W. The wall grid is static per
// room. Two invocations with identical inputs produce byte-identical
// FlowField output.
//
// If the source tile itself is a wall (e.g. corrupt or boundary-clamped
// state), the field is all sentinel. Callers should snap to the nearest
// non-wall tile via NearestNonWallTile beforehand.
func BFSFromTile(roomIndex uint8, sr, sc int) *FlowField {
	f := &FlowField{}
	for r := 0; r < GridRows; r++ {
		for c := 0; c < GridCols; c++ {
			f[r][c] = FlowFieldSentinel
		}
	}
	if sr < 0 || sr >= GridRows || sc < 0 || sc >= GridCols {
		return f
	}
	if tileIsWall(roomIndex, sr, sc) {
		return f
	}
	// Ring-buffer queue sized to the grid — every tile enqueued at most once.
	type cell struct{ r, c int }
	queue := make([]cell, 0, GridRows*GridCols)
	queue = append(queue, cell{sr, sc})
	f[sr][sc] = 0
	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		d := f[cur.r][cur.c]
		for _, n := range flowNeighbours {
			nr, nc := cur.r+n.dr, cur.c+n.dc
			if nr < 0 || nr >= GridRows || nc < 0 || nc >= GridCols {
				continue
			}
			if tileIsWall(roomIndex, nr, nc) {
				continue
			}
			if f[nr][nc] != FlowFieldSentinel {
				continue
			}
			f[nr][nc] = d + 1
			queue = append(queue, cell{nr, nc})
		}
	}
	return f
}

// NextStepTile returns the 4-neighbour of (mr, mc) with the lowest
// distance in the field. Ties are broken by enumeration order N, E, S, W.
// If no neighbour is strictly closer than the mob's own tile (mob is at a
// local minimum — already on the source tile, or stranded with no path),
// the mob's own tile is returned, signalling "hold position."
//
// Out-of-bounds (mr, mc) is treated as a wall and yields the same hold
// behaviour.
func NextStepTile(f *FlowField, mr, mc int) (int, int) {
	if f == nil || mr < 0 || mr >= GridRows || mc < 0 || mc >= GridCols {
		return mr, mc
	}
	best := f[mr][mc]
	br, bc := mr, mc
	for _, n := range flowNeighbours {
		nr, nc := mr+n.dr, mc+n.dc
		if nr < 0 || nr >= GridRows || nc < 0 || nc >= GridCols {
			continue
		}
		d := f[nr][nc]
		if d < best {
			best = d
			br, bc = nr, nc
		}
	}
	return br, bc
}

// BiasedPlayerTile applies the deterministic per-mob jitter bias to the
// player's tile, then snaps the result to the nearest non-wall tile.
//
// Jitter is interpreted as ±1 tile per axis according to the spec:
//
//	jitter <  0.25 → -1 tile
//	jitter >  0.75 → +1 tile
//	otherwise      →  0 (no bias)
//
// Mobs whose jitter falls in [0.25, 0.75] on both axes share the unbiased
// flow field, the common case. The snap step matches NearestNonWall's
// deterministic spiral so a biased target inside a wall (or out of bounds)
// resolves to a stable nearby tile and the BFS source is always reachable.
func BiasedPlayerTile(playerR, playerC int, jX, jY float32, roomIndex uint8) (int, int) {
	dr, dc := 0, 0
	if jX < 0.25 {
		dc = -1
	} else if jX > 0.75 {
		dc = 1
	}
	if jY < 0.25 {
		dr = -1
	} else if jY > 0.75 {
		dr = 1
	}
	r, c := playerR+dr, playerC+dc
	return nearestNonWallTile(roomIndex, r, c)
}

// nearestNonWallTile snaps (r, c) to the nearest non-wall tile via a
// deterministic Chebyshev-radius spiral. Mirrors NearestNonWall's pixel
// version but operates directly on tile coordinates so callers don't have
// to round-trip through pixels. Consumes no RNG.
//
// Returns (r, c) unchanged if the tile is already non-wall, or if no
// non-wall tile is found within radius 12 (the room is degenerate — the
// caller should treat this as "hold position").
func nearestNonWallTile(roomIndex uint8, r, c int) (int, int) {
	if !tileIsWall(roomIndex, r, c) {
		return r, c
	}
	for radius := 1; radius <= 12; radius++ {
		for dr := -radius; dr <= radius; dr++ {
			for dc := -radius; dc <= radius; dc++ {
				if abs(dr) != radius && abs(dc) != radius {
					continue
				}
				nr, nc := r+dr, c+dc
				if !tileIsWall(roomIndex, nr, nc) {
					return nr, nc
				}
			}
		}
	}
	return r, c
}

// FlowFieldCache lazily computes per-tick BFS fields for the small set of
// distinct (row, col) BFS sources requested by mobs in this room this tick.
// In the common case (no jitter beyond ±1 tile) at most 9 sources can be
// requested; cost is bounded by 9 × O(GridRows*GridCols) ≈ 8.6k ops.
//
// Not safe for concurrent use. The tick loop builds a fresh cache each
// tick and discards it after advanceEnemies.
type FlowFieldCache struct {
	roomIndex uint8
	fields    map[int]*FlowField
}

// NewFlowFieldCache constructs a fresh cache for the given room. The
// caller is responsible for calling Get with bias-adjusted source tiles.
func NewFlowFieldCache(roomIndex uint8) *FlowFieldCache {
	return &FlowFieldCache{
		roomIndex: roomIndex,
		fields:    map[int]*FlowField{},
	}
}

// Get returns the flow field rooted at (sr, sc), computing it on first
// request and caching for subsequent reads in the same tick. Identical
// (sr, sc) inputs always return the same field instance for the cache's
// lifetime.
func (c *FlowFieldCache) Get(sr, sc int) *FlowField {
	if c == nil {
		return BFSFromTile(0, sr, sc) // fallback — only hit in tests
	}
	key := sr*GridCols + sc
	if f, ok := c.fields[key]; ok {
		return f
	}
	f := BFSFromTile(c.roomIndex, sr, sc)
	c.fields[key] = f
	return f
}
