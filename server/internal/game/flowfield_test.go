package game

import (
	"testing"
	"time"
)

// setWallGridForTest installs a synthetic wall grid at roomIndex for the
// duration of the test, then restores the prior grid via t.Cleanup. Tests
// must NOT use roomIndex 1..10 — those are loaded by init() from the
// embedded tilemaps and shared across every test in this package.
func setWallGridForTest(t *testing.T, roomIndex uint8, walls map[[2]int]cellKind) {
	t.Helper()
	if roomIndex >= 1 && roomIndex <= 10 {
		t.Fatalf("setWallGridForTest: roomIndex %d collides with init-loaded tilemap; use 0 or 11", roomIndex)
	}
	prev := wallGrids[roomIndex]
	g := &wallGrid{}
	for rc, kind := range walls {
		g[rc[0]][rc[1]] = kind
	}
	wallGrids[roomIndex] = g
	t.Cleanup(func() { wallGrids[roomIndex] = prev })
}

// abs is provided in walls.go.

// manhattan computes the expected tile-step distance for an empty grid
// (no walls), as a 4-neighbourhood BFS would yield.
func manhattan(ar, ac, br, bc int) int {
	d := ar - br
	if d < 0 {
		d = -d
	}
	e := ac - bc
	if e < 0 {
		e = -e
	}
	return d + e
}

// TestBFS_EmptyRoom_ManhattanDistances asserts that on an empty grid
// (room 11, no tilemap), BFS distances equal Manhattan distances on a
// 4-neighbourhood — the algorithm reduces to plain Manhattan when no
// walls are present.
func TestBFS_EmptyRoom_ManhattanDistances(t *testing.T) {
	const room uint8 = 11 // nil grid → cellEmpty everywhere; no tilemap loaded
	const sr, sc = 12, 20
	f := BFSFromTile(room, sr, sc)
	for r := 0; r < GridRows; r++ {
		for c := 0; c < GridCols; c++ {
			want := uint16(manhattan(r, c, sr, sc))
			if got := f[r][c]; got != want {
				t.Fatalf("empty BFS: f[%d][%d]=%d, want %d", r, c, got, want)
			}
		}
	}
}

// TestBFS_UShape_RoutesAround places a U-shaped wall opening upward and
// asserts that a mob outside the U gets a strictly longer path than the
// straight-line Manhattan distance — the BFS routes around the U rather
// than tunnelling through it. This is the core defect from issue #19.
//
// Layout (cells in row,col): U is built at rows 5..10, cols 10 and 14
// (vertical sides) plus row 10 cols 10..14 (bottom). The opening faces
// north. The player sits inside the U at (7, 12). A mob at (12, 12) is
// south of the U bottom, must walk around either side.
func TestBFS_UShape_RoutesAround(t *testing.T) {
	const room uint8 = 0
	walls := map[[2]int]cellKind{}
	for r := 5; r <= 10; r++ {
		walls[[2]int{r, 10}] = cellSolid
		walls[[2]int{r, 14}] = cellSolid
	}
	for c := 10; c <= 14; c++ {
		walls[[2]int{10, c}] = cellSolid
	}
	setWallGridForTest(t, room, walls)

	const playerR, playerC = 7, 12 // inside the U
	f := BFSFromTile(room, playerR, playerC)

	// Mob is below the U bottom, dead-centre. Manhattan distance is 5
	// (rows 12 → 7), but the direct path is blocked by row 10 cols 10..14.
	// The BFS must route around — minimum extra distance is 4 tiles
	// (down to row 10, around col 9 or 15, back up).
	const mobR, mobC = 12, 12
	mh := uint16(manhattan(mobR, mobC, playerR, playerC))
	got := f[mobR][mobC]
	if got <= mh {
		t.Fatalf("U-shape: expected BFS distance > manhattan(%d), got %d (mob trapped or impossibly short)", mh, got)
	}
	// Sanity-bound: shouldn't be absurdly long for a 40×24 grid.
	if got >= FlowFieldSentinel {
		t.Fatalf("U-shape: mob should still be reachable around the U, got sentinel")
	}

	// Walk the gradient and verify it terminates at the player tile —
	// no infinite loops, no parking. This is the property that issue
	// #19 demands.
	r, c := mobR, mobC
	for step := 0; step < 200; step++ {
		nr, nc := NextStepTile(f, r, c)
		if nr == r && nc == c {
			break
		}
		// Never pick a wall tile.
		if tileIsWall(room, nr, nc) {
			t.Fatalf("U-shape: gradient walked into wall at (%d,%d)", nr, nc)
		}
		// Distance must strictly decrease at each gradient step.
		if f[nr][nc] >= f[r][c] {
			t.Fatalf("U-shape: gradient did not strictly decrease at step %d, (%d,%d)d=%d → (%d,%d)d=%d",
				step, r, c, f[r][c], nr, nc, f[nr][nc])
		}
		r, c = nr, nc
	}
	if r != playerR || c != playerC {
		t.Fatalf("U-shape: gradient walk ended at (%d,%d), want player (%d,%d)", r, c, playerR, playerC)
	}
}

// TestNextStepTile_TieBreakingNESW asserts that when multiple neighbours
// have equal lowest distance, the picker prefers N, then E, then S, then
// W. This is a determinism invariant — replays depend on it.
func TestNextStepTile_TieBreakingNESW(t *testing.T) {
	// Hand-built field: mob at (5, 5), neighbours all at distance 1
	// except west which is sentinel (wall). Expected pick: N.
	var f FlowField
	for r := 0; r < GridRows; r++ {
		for c := 0; c < GridCols; c++ {
			f[r][c] = FlowFieldSentinel
		}
	}
	f[5][5] = 2
	f[4][5] = 1 // N
	f[5][6] = 1 // E
	f[6][5] = 1 // S
	// W = sentinel
	r, c := NextStepTile(&f, 5, 5)
	if r != 4 || c != 5 {
		t.Fatalf("tie N/E/S: expected N=(4,5), got (%d,%d)", r, c)
	}

	// N out, E and S tied → expect E.
	f[4][5] = FlowFieldSentinel
	r, c = NextStepTile(&f, 5, 5)
	if r != 5 || c != 6 {
		t.Fatalf("tie E/S: expected E=(5,6), got (%d,%d)", r, c)
	}

	// E out, S and W tied → expect S.
	f[5][6] = FlowFieldSentinel
	f[5][4] = 1
	r, c = NextStepTile(&f, 5, 5)
	if r != 6 || c != 5 {
		t.Fatalf("tie S/W: expected S=(6,5), got (%d,%d)", r, c)
	}

	// S out, only W reachable.
	f[6][5] = FlowFieldSentinel
	r, c = NextStepTile(&f, 5, 5)
	if r != 5 || c != 4 {
		t.Fatalf("only W: expected W=(5,4), got (%d,%d)", r, c)
	}
}

// TestNextStepTile_LocalMinimumHolds asserts that a mob already on the
// source tile (or in an unreachable pocket where no neighbour is closer)
// holds position rather than moving randomly.
func TestNextStepTile_LocalMinimumHolds(t *testing.T) {
	const room uint8 = 11
	const sr, sc = 6, 6
	f := BFSFromTile(room, sr, sc)
	// Mob is on the source tile.
	r, c := NextStepTile(f, sr, sc)
	if r != sr || c != sc {
		t.Fatalf("on source: expected hold (%d,%d), got (%d,%d)", sr, sc, r, c)
	}
}

// TestNextStepTile_UnreachablePocketHolds asserts that when a mob is
// stranded on the wrong side of a fully-enclosing wall, it holds position
// (no wraparound, no panic). This is the spec's "no path exists" case.
func TestNextStepTile_UnreachablePocketHolds(t *testing.T) {
	const room uint8 = 0
	walls := map[[2]int]cellKind{}
	// Box around the player at (5, 5): walls at row 4 cols 4..6, row 6
	// cols 4..6, col 4 rows 4..6, col 6 rows 4..6. Player is enclosed.
	for c := 4; c <= 6; c++ {
		walls[[2]int{4, c}] = cellSolid
		walls[[2]int{6, c}] = cellSolid
	}
	for r := 4; r <= 6; r++ {
		walls[[2]int{r, 4}] = cellSolid
		walls[[2]int{r, 6}] = cellSolid
	}
	setWallGridForTest(t, room, walls)

	f := BFSFromTile(room, 5, 5)
	// A mob outside the box at (10, 10) sees only sentinel.
	const mobR, mobC = 10, 10
	if f[mobR][mobC] != FlowFieldSentinel {
		t.Fatalf("unreachable pocket: expected sentinel at mob, got %d", f[mobR][mobC])
	}
	r, c := NextStepTile(f, mobR, mobC)
	if r != mobR || c != mobC {
		t.Fatalf("unreachable: expected hold, got step to (%d,%d)", r, c)
	}
}

// TestBFS_Determinism asserts byte-identical output across repeated calls
// with identical inputs. The BFS must be a pure function of (room, src);
// the replay engine depends on this.
func TestBFS_Determinism(t *testing.T) {
	const room uint8 = 0
	walls := map[[2]int]cellKind{}
	for r := 5; r <= 12; r++ {
		walls[[2]int{r, 8}] = cellSolid
	}
	for c := 8; c <= 20; c++ {
		walls[[2]int{12, c}] = cellSolid
	}
	setWallGridForTest(t, room, walls)

	a := BFSFromTile(room, 3, 3)
	b := BFSFromTile(room, 3, 3)
	if *a != *b {
		t.Fatal("BFS not deterministic: two calls produced different fields")
	}
}

// TestBFS_WaterIsImpassable confirms water tiles block movement (mirrors
// IsWallAt — both cellSolid and cellWater are walls for movement). This
// guards against a future refactor that accidentally lets mobs walk into
// water.
func TestBFS_WaterIsImpassable(t *testing.T) {
	const room uint8 = 0
	walls := map[[2]int]cellKind{}
	// Vertical water wall at col 10, rows 0..23 (full divider).
	for r := 0; r < GridRows; r++ {
		walls[[2]int{r, 10}] = cellWater
	}
	setWallGridForTest(t, room, walls)

	f := BFSFromTile(room, 5, 5)
	if f[5][20] != FlowFieldSentinel {
		t.Fatalf("water divider: tile (5,20) on the far side should be unreachable, got %d", f[5][20])
	}
}

// TestBiasedPlayerTile_DeterministicMapping locks the jitter→tile-bias
// mapping. The thresholds 0.25 and 0.75 are spec invariants — changing
// them shifts the BUG-01 search surface and invalidates seeds.
func TestBiasedPlayerTile_DeterministicMapping(t *testing.T) {
	const room uint8 = 11
	const pr, pc = 12, 20
	cases := []struct {
		jx, jy   float32
		wantR    int
		wantC    int
		describe string
	}{
		{0.10, 0.50, pr, pc - 1, "jx<0.25 → col-1"},
		{0.90, 0.50, pr, pc + 1, "jx>0.75 → col+1"},
		{0.50, 0.10, pr - 1, pc, "jy<0.25 → row-1"},
		{0.50, 0.90, pr + 1, pc, "jy>0.75 → row+1"},
		{0.10, 0.10, pr - 1, pc - 1, "both low → row-1, col-1"},
		{0.90, 0.90, pr + 1, pc + 1, "both high → row+1, col+1"},
		{0.50, 0.50, pr, pc, "neutral → no bias"},
		{0.25, 0.75, pr, pc, "boundary inclusive — 0.25 and 0.75 → no bias"},
	}
	for _, tc := range cases {
		gotR, gotC := BiasedPlayerTile(pr, pc, tc.jx, tc.jy, room)
		if gotR != tc.wantR || gotC != tc.wantC {
			t.Errorf("%s: BiasedPlayerTile(%d,%d,%v,%v) = (%d,%d), want (%d,%d)",
				tc.describe, pr, pc, tc.jx, tc.jy, gotR, gotC, tc.wantR, tc.wantC)
		}
	}
}

// TestBiasedPlayerTile_SnapsOutOfWall asserts that a bias landing on a
// wall snaps deterministically to the nearest non-wall tile via the
// Chebyshev spiral. This guarantees the BFS source is always reachable.
func TestBiasedPlayerTile_SnapsOutOfWall(t *testing.T) {
	const room uint8 = 0
	walls := map[[2]int]cellKind{}
	walls[[2]int{5, 6}] = cellSolid // bias target
	setWallGridForTest(t, room, walls)

	// Player at (5, 5), jitter pushes col +1 → (5, 6) which is wall.
	r, c := BiasedPlayerTile(5, 5, 0.9, 0.5, room)
	if tileIsWall(room, r, c) {
		t.Fatalf("snap: result (%d,%d) should be non-wall", r, c)
	}
	// Same call should always return the same snap target (deterministic).
	r2, c2 := BiasedPlayerTile(5, 5, 0.9, 0.5, room)
	if r != r2 || c != c2 {
		t.Fatalf("snap: non-deterministic, got (%d,%d) then (%d,%d)", r, c, r2, c2)
	}
}

// TestTileOf_AndTileCenter asserts the pixel↔tile mapping is consistent.
// TileCenter must produce a pixel that TileOf rounds back to the same
// tile.
func TestTileOf_AndTileCenter(t *testing.T) {
	cases := []struct{ r, c int }{
		{0, 0}, {12, 20}, {GridRows - 1, GridCols - 1}, {5, 5},
	}
	for _, tc := range cases {
		x, y := TileCenter(tc.r, tc.c)
		gotR, gotC := TileOf(x, y)
		if gotR != tc.r || gotC != tc.c {
			t.Errorf("round-trip TileCenter→TileOf at (%d,%d): pixel (%v,%v) → (%d,%d)",
				tc.r, tc.c, x, y, gotR, gotC)
		}
	}
}

// TestTileOf_ClampsOutOfBounds asserts negative or oversized pixel coords
// clamp to the grid edge rather than producing negative tile indices that
// would be misinterpreted by downstream BFS lookups.
func TestTileOf_ClampsOutOfBounds(t *testing.T) {
	r, c := TileOf(-100, -100)
	if r != 0 || c != 0 {
		t.Errorf("clamp negative: got (%d,%d), want (0,0)", r, c)
	}
	r, c = TileOf(99999, 99999)
	if r != GridRows-1 || c != GridCols-1 {
		t.Errorf("clamp huge: got (%d,%d), want (%d,%d)", r, c, GridRows-1, GridCols-1)
	}
}

// TestFlowFieldCache_Reuse asserts the per-tick cache returns the SAME
// field instance for repeated requests with identical sources, and
// distinct instances for distinct sources.
func TestFlowFieldCache_Reuse(t *testing.T) {
	const room uint8 = 11
	cache := NewFlowFieldCache(room)
	a := cache.Get(5, 5)
	b := cache.Get(5, 5)
	if a != b {
		t.Errorf("cache: two Get(5,5) returned different instances")
	}
	c := cache.Get(5, 6)
	if a == c {
		t.Errorf("cache: distinct sources returned same instance")
	}
}

// TestIssue19_Regression_BoundaryAnchoredAlcove constructs the exact
// failure mode from issue #19: an L-shape attached to the room boundary
// such that the L's concave pocket is enclosed and the player sits inside
// it. With the legacy greedy chase the mob would park against the L's
// vertical bar; with the flow field it must route around the bottom of
// the L and reach the player.
//
// Layout:
//   - Vertical bar at col 5, rows 1..10 (top arm of L touches row 0
//     boundary at the top of the room).
//   - Horizontal bar at row 10, cols 5..9 (bottom of the L).
//   - Player at (3, 7) — inside the L's pocket.
//   - Mob at (3, 1) — outside, west of the L.
//
// The legacy chase would park the mob at (~3, 4) against the L's
// vertical bar, both axis components blocked. The flow field must yield
// a finite distance and a NextStepTile that points south (toward the
// route around the L bottom).
func TestIssue19_Regression_BoundaryAnchoredAlcove(t *testing.T) {
	const room uint8 = 0
	walls := map[[2]int]cellKind{}
	// Top boundary row 0 — the L's top arm joins this so going over the
	// top is blocked.
	for c := 0; c < GridCols; c++ {
		walls[[2]int{0, c}] = cellSolid
	}
	// L vertical bar at col 5, rows 1..10 (touches top boundary).
	for r := 1; r <= 10; r++ {
		walls[[2]int{r, 5}] = cellSolid
	}
	// L horizontal bar at row 10, cols 5..9.
	for c := 5; c <= 9; c++ {
		walls[[2]int{10, c}] = cellSolid
	}
	setWallGridForTest(t, room, walls)

	const playerR, playerC = 3, 7 // inside the L pocket
	const mobR, mobC = 3, 1       // outside the L, west

	f := BFSFromTile(room, playerR, playerC)
	if f[mobR][mobC] == FlowFieldSentinel {
		t.Fatalf("issue #19 regression: mob should be reachable around the L, got sentinel")
	}

	// Walk the gradient; assert distance strictly decreases each step
	// (no infinite loops, no parking).
	r, c := mobR, mobC
	prevDist := f[r][c]
	for step := 0; step < 200; step++ {
		nr, nc := NextStepTile(f, r, c)
		if nr == r && nc == c {
			break // reached source (or local minimum at source)
		}
		d := f[nr][nc]
		if d >= prevDist {
			t.Fatalf("issue #19 regression: gradient walk did not strictly decrease at step %d, prev=%d, next=%d at (%d,%d)→(%d,%d)",
				step, prevDist, d, r, c, nr, nc)
		}
		prevDist = d
		r, c = nr, nc
	}
	if r != playerR || c != playerC {
		t.Fatalf("issue #19 regression: gradient walk ended at (%d,%d), want player tile (%d,%d)",
			r, c, playerR, playerC)
	}
}

// TestUpdateEnemy_AggroRoutesAroundUShape is an end-to-end integration
// test: a real EnemyState in AIAggro is repeatedly ticked through a
// U-shape wall layout and must reach (within melee range of) the player.
// This is the integrated behaviour issue #19 demands and the change that
// would have failed under the legacy direct-vector chase.
func TestUpdateEnemy_AggroRoutesAroundUShape(t *testing.T) {
	const room uint8 = 0
	walls := map[[2]int]cellKind{}
	// U-shape opening south: vertical sides cols 10 and 14 rows 5..14,
	// top closed at row 5 cols 10..14. Player sits inside the U at the
	// closed end; mob is north of the U opening.
	for r := 5; r <= 14; r++ {
		walls[[2]int{r, 10}] = cellSolid
		walls[[2]int{r, 14}] = cellSolid
	}
	for c := 10; c <= 14; c++ {
		walls[[2]int{5, c}] = cellSolid
	}
	setWallGridForTest(t, room, walls)

	// Player inside the U pocket at tile (12, 12) → pixel (400, 400).
	playerX, playerY := TileCenter(12, 12)
	p := &PlayerState{X: playerX, Y: playerY, HP: 100, MaxHP: 100}

	// Mob at tile (2, 12) — north of the U opening, on the direct line
	// to the player but blocked by the U's closed top.
	mobX, mobY := TileCenter(2, 12)
	e := &EnemyState{
		EntityID:   1,
		Type:       EnemyGrunt,
		X:          mobX,
		Y:          mobY,
		HP:         3,
		MaxHP:      3,
		AIState:    AIAggro,           // start aggroed so the test isolates the chase
		AggroRange: 2000,              // way larger than the room — stays aggroed
		Speed:      Archetypes[EnemyGrunt].Speed,
		JitterX:    0.5,               // no bias — common case
		JitterY:    0.5,
	}

	dt := float32(TickDuration.Seconds())
	now := time.Unix(1_700_000_000, 0)

	// 200 ticks at 95 px/sec ≈ 950 px of travel — more than enough to
	// route around a 4-tile U.
	const maxTicks = 200
	for tick := 0; tick < maxTicks; tick++ {
		fields := NewFlowFieldCache(room)
		UpdateEnemy(e, p, room, dt, now, fields)

		// Mob must never end up in a wall after a tick.
		if BlockedAABB(room, e.X, e.Y, 12, 12) {
			t.Fatalf("tick %d: mob ended in wall at (%v,%v)", tick, e.X, e.Y)
		}

		dx := p.X - e.X
		dy := p.Y - e.Y
		if dx*dx+dy*dy < 34*34 {
			// Reached melee range — chase succeeded.
			return
		}
		now = now.Add(TickDuration)
	}
	t.Fatalf("mob did not reach melee range after %d ticks; final pos (%v,%v), player (%v,%v)",
		maxTicks, e.X, e.Y, p.X, p.Y)
}

// TestUpdateEnemy_AggroJitterBias asserts that two mobs with different
// jitter values can produce different next-step targets — the BUG-01
// seed-search surface is preserved (the spec calls for jitter to remain
// a routing dimension).
func TestUpdateEnemy_AggroJitterBias(t *testing.T) {
	const room uint8 = 11 // empty, no walls — bias effects are observable
	playerX, playerY := TileCenter(12, 20)
	p := &PlayerState{X: playerX, Y: playerY, HP: 100, MaxHP: 100}

	makeMob := func(jx, jy float32) *EnemyState {
		mx, my := TileCenter(8, 20) // 4 tiles north of player
		return &EnemyState{
			EntityID: 1, Type: EnemyGrunt,
			X: mx, Y: my,
			HP: 3, MaxHP: 3,
			AIState:    AIAggro,
			AggroRange: 2000,
			Speed:      0, // freeze movement; we only care about the chosen aim tile
			JitterX:    jx, JitterY: jy,
		}
	}

	dt := float32(TickDuration.Seconds())
	now := time.Unix(1_700_000_000, 0)

	// Mob A: no bias. Source = (12, 20). Best step from (8, 20) = south
	// toward (9, 20).
	mobA := makeMob(0.5, 0.5)
	// Mob B: jitter biases the source by (-1, -1) → (11, 19). Best step
	// from (8, 20) toward (11, 19) — could differ from mobA's path.
	mobB := makeMob(0.1, 0.1)

	// Run a single tick on each. Speed is 0 so position doesn't move
	// — but we can verify the BFS source via BiasedPlayerTile directly.
	cache := NewFlowFieldCache(room)
	UpdateEnemy(mobA, p, room, dt, now, cache)
	UpdateEnemy(mobB, p, room, dt, now, cache)

	// Direct check: BiasedPlayerTile must return distinct sources.
	pr, pc := TileOf(p.X, p.Y)
	srcA_r, srcA_c := BiasedPlayerTile(pr, pc, mobA.JitterX, mobA.JitterY, room)
	srcB_r, srcB_c := BiasedPlayerTile(pr, pc, mobB.JitterX, mobB.JitterY, room)
	if srcA_r == srcB_r && srcA_c == srcB_c {
		t.Fatalf("jitter bias should differentiate sources: A=(%d,%d), B=(%d,%d)",
			srcA_r, srcA_c, srcB_r, srcB_c)
	}
}

// TestBFS_StraightDivider_NoBypass — a wall that fully bisects the room
// (col 10, all rows) must yield a sentinel for any tile on the far side.
// This is intentional: the spec says fully separated regions are correctly
// "no path", not "park forever".
func TestBFS_StraightDivider_NoBypass(t *testing.T) {
	const room uint8 = 0
	walls := map[[2]int]cellKind{}
	for r := 0; r < GridRows; r++ {
		walls[[2]int{r, 10}] = cellSolid
	}
	setWallGridForTest(t, room, walls)

	f := BFSFromTile(room, 5, 5)
	for r := 0; r < GridRows; r++ {
		for c := 11; c < GridCols; c++ {
			if f[r][c] != FlowFieldSentinel {
				t.Fatalf("straight divider: (%d,%d) should be unreachable, got %d", r, c, f[r][c])
			}
		}
	}
}
