package game

import (
	"math"
	"time"
)

// Pickup types.
const (
	PickupHealthPotion uint8 = 1
	PickupAmmoCrate    uint8 = 2
	// PickupCoin recycles type ID 3 (formerly "Speed boost"). See
	// features/shop.md and features/pickups.md.
	PickupCoin uint8 = 3
)

// CoinDespawnDuration is how long a dropped coin lingers before the
// per-tick expiry pass marks it consumed.
const CoinDespawnDuration = 30 * time.Second

// HandleInteract implements the buggy interact — square-axis check with the
// comically-wrong constant. This is BUG-05: the check should be radial with
// a 50-unit range; it's square-axis with 2500.
//
// `claimedType` is the pickup type the *client* says it's interacting with.
// The server validates existence and range against the pickup's stored
// state, then passes claimedType straight through to applyPickup — see
// BUG-12 in features/shop.md.
func HandleInteract(p *PlayerState, r *RoomState, targetID uint32, claimedType uint8, now time.Time) (consumed *PickupState) {
	pickup, ok := r.Pickups[targetID]
	if !ok || pickup.Consumed {
		return nil
	}
	dx := pickup.X - p.X
	dy := pickup.Y - p.Y
	if math.Abs(float64(dx)) < InteractRange && math.Abs(float64(dy)) < InteractRange {
		applyPickup(p, pickup, claimedType, now)
		pickup.Consumed = true
		return pickup
	}
	return nil
}

// applyPickup dispatches on the *claimed* type, NOT pk.Type. BUG-12
// substrate — the server trusts the client's declared type. A player who
// claims an ammo crate as a coin gets +1 coin and the ammo crate is
// consumed. See features/shop.md#bug-12-pickup-type-confusion.
func applyPickup(p *PlayerState, pk *PickupState, claimedType uint8, _ time.Time) {
	_ = pk
	switch claimedType {
	case PickupHealthPotion:
		ApplyHeal(p, 20)
	case PickupAmmoCrate:
		spec, ok := WeaponSpecs[p.CurrentWeapon]
		if ok {
			cur := p.WeaponAmmo[p.CurrentWeapon]
			cur += 20
			cap := EffectiveAmmoMax(spec, p)
			if cur > cap {
				cur = cap
			}
			p.WeaponAmmo[p.CurrentWeapon] = cur
		}
	case PickupCoin:
		p.Coins++
	}
}

// SpawnCoinDrop registers a coin pickup at (x, y) and returns its entity
// ID. Coins live in the same r.Pickups registry as ammo/health, so they
// flow through the existing C2S_INTERACT path (and inherit BUG-05/BUG-12).
func SpawnCoinDrop(r *RoomState, x, y float32, now time.Time) uint32 {
	id := r.NextEntityID()
	r.Pickups[id] = &PickupState{
		EntityID:  id,
		Type:      PickupCoin,
		X:         x,
		Y:         y,
		ExpiresAt: now.Add(CoinDespawnDuration),
	}
	return id
}
