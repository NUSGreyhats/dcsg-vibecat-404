package game

import (
	"math"
	"time"
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
// BUG-08 (enemy side): int16 arithmetic on `amount - reduction` can underflow.
func TakeDamage(e *EnemyState, amount int16) bool {
	reduction := e.Armor
	effectiveDmg := amount - reduction
	e.HP -= effectiveDmg
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
func HandleShoot(p *PlayerState, r *RoomState, aimAngle float32, now time.Time) {
	wid := p.CurrentWeapon
	spec, ok := WeaponSpecs[wid]
	if !ok {
		return
	}

	cd := p.WeaponCooldown[wid]
	// Check against the swap cooldown as seen *before* the current handler's
	// potential update. BUG-04 hinges on this ordering.
	if now.Before(p.SwapCooldownEnd) && !sameTickSwap(p.SwapCooldownEnd, now) {
		return
	}
	if now.Before(cd) {
		return
	}

	ammo := p.WeaponAmmo[wid]
	if ammo <= 0 {
		return
	}

	// Charge cooldown + ammo. Fire-rate stacks shorten the per-shot cooldown
	// linearly: cooldown / (1 + 0.1 * stacks). See features/shop.md.
	p.WeaponAmmo[wid] = ammo - 1
	cooldown := time.Duration(spec.CooldownMs) * time.Millisecond
	if p.FireRateStacks > 0 {
		cooldown = time.Duration(float64(cooldown) / (1 + 0.1*float64(p.FireRateStacks)))
	}
	p.WeaponCooldown[wid] = now.Add(cooldown)

	dmg := int16(float32(spec.Damage) * EffectiveDamageMult(p, now))

	if wid == 3 {
		// shotgun: 3 pellets in a ±10° spread.
		for _, deg := range []float64{-10, 0, 10} {
			spawnProjectile(r, p, aimAngle+float32(deg*math.Pi/180), dmg, now)
		}
		return
	}
	spawnProjectile(r, p, aimAngle, dmg, now)
}

func spawnProjectile(r *RoomState, p *PlayerState, angle float32, dmg int16, now time.Time) {
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

func nonZeroDamageMult(p *PlayerState, now time.Time) float32 {
	if p.DamageMult > 0 && now.Before(p.DamageMultUntil) {
		return p.DamageMult
	}
	return 1
}

// EffectiveDamageMult composes the timed buff with the permanent shop
// damage stacks: (1 + 0.1*stacks) * tempBuff. See features/shop.md.
func EffectiveDamageMult(p *PlayerState, now time.Time) float32 {
	return (1 + 0.1*float32(p.DamageStacks)) * nonZeroDamageMult(p, now)
}

// EffectiveSpeedMult composes the timed speed buff with the permanent
// shop speed stacks: (1 + 0.1*stacks) * tempBuff.
func EffectiveSpeedMult(p *PlayerState, now time.Time) float32 {
	temp := float32(1)
	if p.SpeedMult > 0 && now.Before(p.SpeedMultUntil) {
		temp = p.SpeedMult
	}
	return (1 + 0.1*float32(p.SpeedStacks)) * temp
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
		if age > float64(ProjectileLifeSec) ||
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
