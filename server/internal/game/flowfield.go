package game

// Flow-field pathfinding for mob aggro chase.
//
// See docs/specs/features/mob-ai.md § "Aggro chase: flow field" for the
// authoritative spec. Summary:
//
//   - Per tick, BFS from the player's tile over the static wall grid.
//     Output is a tile-distance field consumed by every mob in Aggro state.
//   - A mob picks the 8-neighbour with the lowest distance; ties broken
//     by fixed clockwise enumeration order N, NE, E, SE, S, SW, W, NW.
//   - Diagonal steps are forbidden when either axis-aligned neighbour is a
//     wall (no corner-cutting), so a mob never visually clips through a
//     wall corner.
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
// 8-neighbourhood, clockwise from N. Diagonals carry distance 1 (unweighted
// BFS → Chebyshev distance in open space), but are gated on corner-cut
// prevention: a diagonal step is rejected when either axis-aligned tile
// adjacent to the corner is a wall.
var flowNeighbours = [8]struct{ dr, dc int }{
	{-1, 0},  // N
	{-1, 1},  // NE
	{0, 1},   // E
	{1, 1},   // SE
	{1, 0},   // S
	{1, -1},  // SW
	{0, -1},  // W
	{-1, -1}, // NW
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
// steps over an 8-neighbourhood; diagonal steps cost 1 (Chebyshev metric)
// and are forbidden when either axis-aligned tile bracketing the corner is
// a wall, preventing mobs from visually clipping through wall corners.
//
// Determinism: the BFS is a pure function of (roomIndex, sr, sc). Neighbour
// enumeration order is fixed (clockwise from N: N, NE, E, SE, S, SW, W,
// NW). The wall grid is static per room. Two invocations with identical
// inputs produce byte-identical FlowField output.
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
			if n.dr != 0 && n.dc != 0 {
				if tileIsWall(roomIndex, cur.r+n.dr, cur.c) || tileIsWall(roomIndex, cur.r, cur.c+n.dc) {
					continue
				}
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

// NextStepTile returns the 8-neighbour of (mr, mc) with the lowest distance
// in the field. Ties are broken by enumeration order (clockwise from N: N,
// NE, E, SE, S, SW, W, NW). Diagonal candidates are skipped when either
// axis-aligned bracketing tile is a wall — same corner-cut rule the BFS
// applies, so the gradient never aims through a wall corner.
//
// If no neighbour is strictly closer than the mob's own tile (mob is at a
// local minimum — already on the source tile, or stranded with no path),
// the mob's own tile is returned, signalling "hold position."
//
// Out-of-bounds (mr, mc) is treated as a wall and yields the same hold
// behaviour.
func NextStepTile(f *FlowField, roomIndex uint8, mr, mc int) (int, int) {
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
		if n.dr != 0 && n.dc != 0 {
			if tileIsWall(roomIndex, mr+n.dr, mc) || tileIsWall(roomIndex, mr, mc+n.dc) {
				continue
			}
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
