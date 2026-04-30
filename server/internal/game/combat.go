package game

import (
	"math"
	"time"

	"github.com/greyhats/defcon-game/server/internal/metrics"
)

// ApplyHeal mutates p.HP in place. BUG-08: no overflow check — Go wraps
// int16 silently, and the `> MaxHP` clamp never fires on wrap-to-negative.
func ApplyHeal(p *PlayerState, amount int16) {
	p.HP += amount
	if p.HP > p.MaxHP {
		p.HP = p.MaxHP
	}
}

// TakeDamage returns true when the enemy's HP drops to zero or below.
func TakeDamage(e *EnemyState, amount int16) bool {
	e.HP -= amount
	return e.HP <= 0
}

// HandleSwitchWeapon is called during drain with the tick-shared `now`.
// BUG-04 substrate: both the SwapCooldownEnd write (here) and the SHOOT
// cooldown read share that tickNow, so a same-tick SWITCH→SHOOT sequence
// can bypass the swap delay.
func HandleSwitchWeapon(p *PlayerState, weaponID uint8, now time.Time) {
	if !contains(p.UnlockedWeapons, weaponID) {
		return
	}
	if p.CurrentWeapon == weaponID {
		return
	}
	p.CurrentWeapon = weaponID
	p.SwapCooldownEnd = now.Add(SwapCooldownMs * time.Millisecond)
}

// HandleShoot spawns a projectile if the weapon's cooldown and the player's
// swap cooldown permit it. `now` is the tick-shared timestamp captured at
// tick start (load-bearing for BUG-04 and BUG-07).
//
// `replayMode` opts the gating checks (swap cooldown, weapon cooldown,
// ammo) out of rejection: the recording's existence is itself proof
// the live engine accepted the SHOOT, so replay must too. Without
// these bypasses, tiny live-vs-synthetic timing drifts (wall-clock
// jitter pushes a live tick past a cooldown boundary the synthetic
// tick lands exactly on) reject shots in replay that fired in the
// recording, and the resulting kill shortfall cascades into the coin
// economy and ammo shop. Ammo still decrements visually (clamped at 0)
// so HUD trackers don't lie. This trade is documented in
// docs/specs/foundations/08-leaderboard-replay.md.
func HandleShoot(p *PlayerState, r *RoomState, aimAngle float32, now time.Time, replayMode bool) {
	wid := p.CurrentWeapon
	spec, ok := WeaponSpecs[wid]
	if !ok {
		return
	}

	cd := p.WeaponCooldown[wid]
	if !replayMode {
		// Check against the swap cooldown as seen *before* the current handler's
		// potential update. BUG-04 hinges on this ordering.
		if now.Before(p.SwapCooldownEnd) && !sameTickSwap(p.SwapCooldownEnd, now) {
			return
		}
		if now.Before(cd) {
			return
		}
		if p.WeaponAmmo[wid] <= 0 {
			return
		}
	}

	// Decrement ammo for HUD parity, but clamp at 0 — replay never blocks
	// a recorded SHOOT on ammo exhaustion.
	if ammo := p.WeaponAmmo[wid]; ammo > 0 {
		p.WeaponAmmo[wid] = ammo - 1
	}
	metrics.WeaponShots.WithLabelValues(spec.Name).Inc()
	cooldown := time.Duration(spec.CooldownMs) * time.Millisecond
	if p.FireRateStacks > 0 {
		cooldown = time.Duration(float64(cooldown) / (1 + 0.1*float64(p.FireRateStacks)))
	}
	p.WeaponCooldown[wid] = now.Add(cooldown)

	dmg := int16(float32(spec.Damage) * EffectiveDamageMult(p))

	if wid == 3 {
		// shotgun: 3 pellets in a ±10° spread.
		for _, deg := range []float64{-10, 0, 10} {
			spawnProjectile(r, p, wid, aimAngle+float32(deg*math.Pi/180), dmg, now)
		}
		return
	}
	spawnProjectile(r, p, wid, aimAngle, dmg, now)
}

func spawnProjectile(r *RoomState, p *PlayerState, wid uint8, angle float32, dmg int16, now time.Time) {
	id := r.NextProjID
	r.NextProjID++
	r.Projectiles[id] = &ProjectileState{
		ProjID:    id,
		X:         p.X,
		Y:         p.Y,
		VelX:      ProjectileSpeed * float32(math.Cos(float64(angle))),
		VelY:      ProjectileSpeed * float32(math.Sin(float64(angle))),
		Damage:    dmg,
		OwnerID:   0,
		WeaponID:  wid,
		SpawnedAt: now,
	}
}

// sameTickSwap lets the shoot handler pass even when a switch landed in the
// same tick. The substrate: if SwapCooldownEnd is exactly tickNow+SwapCooldownMs
// (set this tick), we treat the check as having been evaluated against the
// prior zero value. This is BUG-04 — a 400ms swap delay is skipped for any
// SWITCH→SHOOT pair delivered in the same 50ms drain window.
func sameTickSwap(end time.Time, now time.Time) bool {
	diff := end.Sub(now)
	return diff == SwapCooldownMs*time.Millisecond
}

// EffectiveDamageMult is the permanent shop-stack multiplier: (1 + 0.1*stacks).
// See features/shop.md.
func EffectiveDamageMult(p *PlayerState) float32 {
	return 1 + 0.1*float32(p.DamageStacks)
}

// EffectiveSpeedMult is the permanent shop-stack multiplier: (1 + 0.1*stacks).
func EffectiveSpeedMult(p *PlayerState) float32 {
	return 1 + 0.1*float32(p.SpeedStacks)
}


// AdvanceProjectiles steps every projectile, culls expired, and returns
// the list of (projectile, enemy) collisions for the collision pass to
// resolve. Resolution deliberately happens AFTER the full collision scan
// for BUG-07 (damage batch).
type collisionHit struct {
	proj   *ProjectileState
	enemy  *EnemyState
	projID uint32
}

func AdvanceProjectiles(r *RoomState, dt float32, now time.Time) []collisionHit {
	hits := []collisionHit{}
	for id, pr := range r.Projectiles {
		pr.X += pr.VelX * dt
		pr.Y += pr.VelY * dt
		age := now.Sub(pr.SpawnedAt).Seconds()
		life := float64(ProjectileLifeSec)
		if spec, ok := WeaponSpecs[pr.WeaponID]; ok && spec.LifeSec > 0 {
			life = float64(spec.LifeSec)
		}
		if age > life ||
			pr.X < 0 || pr.X > RoomWidth ||
			pr.Y < 0 || pr.Y > RoomHeight ||
			BlocksProjectileAt(r.RoomIndex, pr.X, pr.Y) {
			delete(r.Projectiles, id)
			continue
		}
		for _, e := range r.Enemies {
			if e.AIState == AIDead {
				continue
			}
			if collides(pr.X, pr.Y, e.X, e.Y, 24) {
				hits = append(hits, collisionHit{proj: pr, enemy: e, projID: id})
			}
		}
	}
	return hits
}

// ResolveCollisions applies all damage before any death pass. This is the
// BUG-07 substrate: overkill on an enemy that would have died on shot 2 still
// lets shot 3's damage apply in the bookkeeping.
func ResolveCollisions(r *RoomState, hits []collisionHit, now time.Time) (deaths []uint32) {
	// Apply damage pass — all hits, no early death culling.
	for _, h := range hits {
		if h.enemy.AIState == AIDead {
			continue
		}
		TakeDamage(h.enemy, h.proj.Damage)
		delete(r.Projectiles, h.projID)
		// Pack aggro: any damage to a mob alerts nearby non-aggro mobs.
		// See docs/specs/features/harder-mobs.md. Fires on every hit,
		// not just HP-changing ones — "they heard you" is the signal.
		BroadcastPackAggro(r, h.enemy.X, h.enemy.Y, h.enemy.EntityID)
	}
	// Death pass — one entry per dead enemy. Each dead enemy drops a coin
	// at its last (X, Y); see features/shop.md.
	for id, e := range r.Enemies {
		if e.AIState != AIDead && e.HP <= 0 {
			e.AIState = AIDead
			deaths = append(deaths, id)
			SpawnCoinDrop(r, e.X, e.Y, now)
		}
	}
	return deaths
}

func collides(ax, ay, bx, by, r float32) bool {
	dx := ax - bx
	dy := ay - by
	return dx*dx+dy*dy < r*r
}

func contains(s []uint8, v uint8) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
