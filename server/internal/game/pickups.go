package game

import (
	"math"
	"time"
)

// Pickup types.
const (
	PickupHealthPotion uint8 = 1
	PickupAmmoCrate    uint8 = 2
	PickupSpeedBoost   uint8 = 3
	PickupDamageBoost  uint8 = 4
	PickupHeart        uint8 = 5
)

// HandleInteract implements the buggy interact — square-axis check with the
// comically-wrong constant. This is BUG-05: the check should be radial with
// a 50-unit range; it's square-axis with 2500.
func HandleInteract(p *PlayerState, r *RoomState, targetID uint32, now time.Time) (consumed *PickupState) {
	pickup, ok := r.Pickups[targetID]
	if !ok || pickup.Consumed {
		return nil
	}
	dx := pickup.X - p.X
	dy := pickup.Y - p.Y
	if math.Abs(float64(dx)) < InteractRange && math.Abs(float64(dy)) < InteractRange {
		applyPickup(p, pickup, now)
		pickup.Consumed = true
		return pickup
	}
	return nil
}

func applyPickup(p *PlayerState, pk *PickupState, now time.Time) {
	switch pk.Type {
	case PickupHealthPotion:
		ApplyHeal(p, 20)
	case PickupAmmoCrate:
		spec, ok := WeaponSpecs[p.CurrentWeapon]
		if ok {
			cur := p.WeaponAmmo[p.CurrentWeapon]
			cur += 20
			if cur > spec.AmmoMax {
				cur = spec.AmmoMax
			}
			p.WeaponAmmo[p.CurrentWeapon] = cur
		}
	case PickupSpeedBoost:
		p.SpeedMult = 1.5
		p.SpeedMultUntil = now.Add(5 * time.Second)
	case PickupDamageBoost:
		p.DamageMult = 1.5
		p.DamageMultUntil = now.Add(5 * time.Second)
	case PickupHeart:
		// BUG-08 trap: raw +30000 heal wraps past 32767 for a high-HP player.
		ApplyHeal(p, 30000)
	}
}
