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
	MeleeDamage      int16 // Damage dealt to the player on a successful melee hit.
}

var Archetypes = map[uint8]ArchetypeSpec{
	1: {Name: "grunt", HP: 3, Speed: 95, AggroRange: 200, AttackCooldownMs: 800, MeleeDamage: 5},
	2: {Name: "runner", HP: 2, Speed: 165, AggroRange: 260, AttackCooldownMs: 600, MeleeDamage: 4},
	3: {Name: "tank", HP: 12, Speed: 60, AggroRange: 160, AttackCooldownMs: 1200, Armor: 1, MeleeDamage: 8},
	4: {Name: "kamikaze", HP: 1, Speed: 145, AggroRange: 220, AttackCooldownMs: 0, MeleeDamage: 15},
	5: {Name: "boss", HP: 120, Speed: 70, AggroRange: 360, AttackCooldownMs: 900, Armor: 3, MeleeDamage: 20},
}

// roomTier returns the HP and Speed multipliers applied to mobs spawned in a
// given room, for both initial enemies and wave reinforcements. Scaling
// happens at InstantiateEnemy time and consumes no RNG, so the BUG-01 seed
// invariant is preserved. See docs/specs/features/harder-mobs.md.
func roomTier(roomIndex uint8) (hpMult, speedMult float32) {
	switch {
	case roomIndex >= 9:
		return 1.6, 1.2
	case roomIndex >= 6:
		return 1.3, 1.1
	default:
		return 1.0, 1.0
	}
}

// SpawnEnemy consumes MobRNG in the exact order mandated by
// docs/specs/foundations/07-determinism-seeding.md. Any change to the order
// invalidates every pre-computed seed — do not reorder.
//
// After the RNG block completes, spawn and waypoint positions are nudged to
// the nearest non-wall tile via NearestNonWall. That step consumes no RNG,
// so the invariant holds.
func SpawnEnemy(rng *rand.Rand, roomIndex uint8, baseID uint32, etype uint8, baseX, baseY float32, now time.Time) *EnemyState {
	seed := SeedMob(rng, roomIndex, baseX, baseY)
	return InstantiateEnemy(roomIndex, baseID, etype, seed, now)
}

// SeedMob performs the RNG consumption block for a single mob. It does not
// allocate an EntityID and does not build an EnemyState — callers pass the
// returned MobSeed to InstantiateEnemy when they're ready to put the mob
// into the world. This split lets reinforcement waves pre-seed all their
// mobs at room-load time (keeping RNG consumption deterministic) and then
// instantiate them later on fire, without touching MobRNG.
func SeedMob(rng *rand.Rand, roomIndex uint8, baseX, baseY float32) MobSeed {
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

	return MobSeed{
		SpawnX:           spawnX,
		SpawnY:           spawnY,
		PatrolPath:       path,
		JitterX:          jitterX,
		JitterY:          jitterY,
		FirstAttackDelay: firstAttackDelay,
	}
}

// InstantiateEnemy turns a seeded mob into a live EnemyState. Consumes no RNG.
// `now` anchors the first-attack cooldown — for initial enemies this is room
// load time; for wave mobs it is wave fire time (so attack delay starts from
// the moment the mob actually appears, not from room load).
//
// Per-room HP and Speed scaling (roomTier) is applied here. Scaling depends
// only on roomIndex + archetype — both deterministic — so replays remain
// byte-stable.
func InstantiateEnemy(roomIndex uint8, id uint32, etype uint8, seed MobSeed, now time.Time) *EnemyState {
	spec := Archetypes[etype]
	hpMult, speedMult := roomTier(roomIndex)
	hp := int16(float32(spec.HP) * hpMult)
	if hp < 1 {
		hp = 1
	}
	return &EnemyState{
		EntityID:       id,
		Type:           etype,
		X:              seed.SpawnX,
		Y:              seed.SpawnY,
		HP:             hp,
		MaxHP:          hp,
		AIState:        AIPatrol,
		PatrolPath:     seed.PatrolPath,
		AggroRange:     spec.AggroRange,
		AttackCooldown: now.Add(time.Duration(seed.FirstAttackDelay) * time.Millisecond),
		Speed:          spec.Speed * speedMult,
		JitterX:        seed.JitterX,
		JitterY:        seed.JitterY,
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

// BroadcastPackAggro flips every non-aggro, non-dead enemy within
// PackAggroRadius of (victimX, victimY) to AIAggro — including the damaged
// mob itself (its distance to itself is 0, always inside the radius). No
// LOS check — "they heard you."
//
// Including the victim is load-bearing: a mob shot from beyond its own
// AggroRange would otherwise stay on patrol because the per-tick aggro flip
// gates on `dist < AggroRange`. Pack-aggro on damage is the only path that
// guarantees a damaged mob always reacts.
//
// Consumes no RNG. Called from the single damage site in ResolveCollisions.
// If further damage sources are added (melee, AoE, etc.), they must call
// this helper for the mechanic to remain consistent.
func BroadcastPackAggro(r *RoomState, victimX, victimY float32, victimID uint32) {
	_ = victimID // kept for callers; victim is included in the sweep.
	rSq := PackAggroRadius * PackAggroRadius
	for _, other := range r.Enemies {
		if other.AIState == AIDead || other.AIState == AIAggro {
			continue
		}
		dx := other.X - victimX
		dy := other.Y - victimY
		if dx*dx+dy*dy <= rSq {
			other.AIState = AIAggro
		}
	}
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
