package game

import (
	"math"
	"time"
)

// Pickup types.
const (
	PickupHealthPotion uint8 = 1
	PickupAmmoCrate    uint8 = 2
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

func applyPickup(p *PlayerState, pk *PickupState, _ time.Time) {
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
	}
}
