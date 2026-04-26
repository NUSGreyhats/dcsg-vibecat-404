package game

import "time"

type WeaponSpec struct {
	Damage     int16
	CooldownMs int
	Name       string
	AmmoMax    int
	AmmoStart  int
	// LifeSec caps how long the weapon's projectile lives before it's
	// culled. Shorter values turn weapons into close-range tools without
	// changing the projectile speed. 0 falls back to ProjectileLifeSec.
	LifeSec float32
}

// Mirrors client/src/game/weapons.ts. IDs are stable.
var WeaponSpecs = map[uint8]WeaponSpec{
	1: {Damage: 1, CooldownMs: 150, Name: "pistol", AmmoMax: 9999, AmmoStart: 9999, LifeSec: 1.5},
	2: {Damage: 2, CooldownMs: 75, Name: "rifle", AmmoMax: 30, AmmoStart: 30, LifeSec: 1.5},
	// Shotgun is intentionally short-range — pellets cover ~210 px at the
	// shared 600 px/s ProjectileSpeed before being culled.
	3: {Damage: 6, CooldownMs: 520, Name: "shotgun", AmmoMax: 8, AmmoStart: 8, LifeSec: 0.35},
}

func WeaponCooldown(id uint8) time.Duration {
	spec, ok := WeaponSpecs[id]
	if !ok {
		return 0
	}
	return time.Duration(spec.CooldownMs) * time.Millisecond
}

func WeaponDamage(id uint8) int16 {
	spec, ok := WeaponSpecs[id]
	if !ok {
		return 0
	}
	return spec.Damage
}
