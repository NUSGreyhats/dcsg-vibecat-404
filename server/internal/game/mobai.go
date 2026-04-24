package game

import (
	"math"
	"math/rand"
	"time"
)

type ArchetypeSpec struct {
	Name             string
	HP               int16
	Speed            float32
	AggroRange       float32
	AttackCooldownMs int
	Armor            int16
}

var Archetypes = map[uint8]ArchetypeSpec{
	1: {Name: "grunt", HP: 3, Speed: 80, AggroRange: 200, AttackCooldownMs: 800},
	2: {Name: "runner", HP: 2, Speed: 140, AggroRange: 260, AttackCooldownMs: 600},
	3: {Name: "tank", HP: 12, Speed: 50, AggroRange: 160, AttackCooldownMs: 1200, Armor: 1},
	4: {Name: "kamikaze", HP: 1, Speed: 120, AggroRange: 220, AttackCooldownMs: 0},
	5: {Name: "boss", HP: 120, Speed: 60, AggroRange: 360, AttackCooldownMs: 900, Armor: 3},
}

// SpawnEnemy consumes MobRNG in the exact order mandated by
// docs/specs/foundations/07-determinism-seeding.md. Any change to the order
// invalidates every pre-computed seed — do not reorder.
//
// After the RNG block completes, spawn and waypoint positions are nudged to
// the nearest non-wall tile via NearestNonWall. That step consumes no RNG,
// so the invariant holds.
func SpawnEnemy(rng *rand.Rand, roomIndex uint8, baseID uint32, etype uint8, baseX, baseY float32, now time.Time) *EnemyState {
	spec := Archetypes[etype]

	offsetX := float32(rng.Intn(100) - 50)
	offsetY := float32(rng.Intn(100) - 50)

	waypointCount := rng.Intn(3) + 2
	path := make([]Vec2, waypointCount)
	for i := range waypointCount {
		wx := clampFloat(float32(rng.Intn(RoomWidth)), 40, RoomWidth-40)
		wy := clampFloat(float32(rng.Intn(RoomHeight)), 40, RoomHeight-40)
		path[i] = Vec2{X: wx, Y: wy}
	}

	jitterX := rng.Float32()
	jitterY := rng.Float32()

	firstAttackDelay := rng.Intn(500)

	// --- RNG block complete. Post-processing with the wall grid below. ---

	spawnX, spawnY := NearestNonWall(roomIndex, baseX+offsetX, baseY+offsetY)
	for i, wp := range path {
		nx, ny := NearestNonWall(roomIndex, wp.X, wp.Y)
		path[i] = Vec2{X: nx, Y: ny}
	}

	return &EnemyState{
		EntityID:       baseID,
		Type:           etype,
		X:              spawnX,
		Y:              spawnY,
		HP:             spec.HP,
		MaxHP:          spec.HP,
		AIState:        AIPatrol,
		PatrolPath:     path,
		AggroRange:     spec.AggroRange,
		AttackCooldown: now.Add(time.Duration(firstAttackDelay) * time.Millisecond),
		Speed:          spec.Speed,
		JitterX:        jitterX,
		JitterY:        jitterY,
		Armor:          spec.Armor,
	}
}

// UpdateEnemy runs one tick of AI. No RNG is consumed here — all randomness
// was baked in at spawn time (determinism invariant). roomIndex is used to
// look up the wall grid so enemies don't walk through tilemap walls.
func UpdateEnemy(e *EnemyState, p *PlayerState, roomIndex uint8, dt float32, now time.Time) (didAttack bool) {
	if e.AIState == AIDead {
		return false
	}

	dx := p.X - e.X
	dy := p.Y - e.Y
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))

	if e.AIState != AIAggro && dist < e.AggroRange {
		e.AIState = AIAggro
	}

	switch e.AIState {
	case AIIdle:
		e.AIState = AIPatrol
	case AIPatrol:
		if len(e.PatrolPath) == 0 {
			return false
		}
		tgt := e.PatrolPath[e.PatrolIndex]
		moveTowardWithWalls(e, tgt.X, tgt.Y, dt, roomIndex)
		if near(e.X, e.Y, tgt.X, tgt.Y, 8) {
			e.PatrolIndex = (e.PatrolIndex + 1) % uint8(len(e.PatrolPath))
		}
	case AIAggro:
		aimX := p.X + (e.JitterX-0.5)*20
		aimY := p.Y + (e.JitterY-0.5)*20
		moveTowardWithWalls(e, aimX, aimY, dt, roomIndex)

		if dist < 34 && now.After(e.AttackCooldown) {
			didAttack = true
			cd := Archetypes[e.Type].AttackCooldownMs
			e.AttackCooldown = now.Add(time.Duration(cd) * time.Millisecond)
		}
	}
	return didAttack
}

func moveToward(e *EnemyState, tx, ty, dt float32) {
	dx := tx - e.X
	dy := ty - e.Y
	mag := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if mag < 0.001 {
		return
	}
	step := e.Speed * dt
	e.X += dx / mag * step
	e.Y += dy / mag * step
}

// moveTowardWithWalls is moveToward with axis-separated wall checks so
// enemies slide along walls instead of tunneling through them.
func moveTowardWithWalls(e *EnemyState, tx, ty, dt float32, roomIndex uint8) {
	dx := tx - e.X
	dy := ty - e.Y
	mag := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if mag < 0.001 {
		return
	}
	step := e.Speed * dt
	const hw float32 = 12
	const hh float32 = 12
	newX := e.X + dx/mag*step
	if !BlockedAABB(roomIndex, newX, e.Y, hw, hh) {
		e.X = newX
	}
	newY := e.Y + dy/mag*step
	if !BlockedAABB(roomIndex, e.X, newY, hw, hh) {
		e.Y = newY
	}
}

func near(ax, ay, bx, by, eps float32) bool {
	dx := ax - bx
	dy := ay - by
	return dx*dx+dy*dy < eps*eps
}

func clampFloat(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
