package game

import "time"

type WeaponSpec struct {
	Damage     int16
	CooldownMs int
	Name       string
	AmmoMax    int
	AmmoStart  int
}

// Mirrors client/src/game/weapons.ts.
var WeaponSpecs = map[uint8]WeaponSpec{
	1: {Damage: 1, CooldownMs: 150, Name: "pistol", AmmoMax: 9999, AmmoStart: 9999},
	2: {Damage: 2, CooldownMs: 220, Name: "rifle", AmmoMax: 120, AmmoStart: 30},
	3: {Damage: 6, CooldownMs: 520, Name: "shotgun", AmmoMax: 40, AmmoStart: 8},
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
