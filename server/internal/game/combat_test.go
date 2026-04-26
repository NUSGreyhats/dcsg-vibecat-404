package game

import (
	"testing"
	"time"
)

// TestHandleShoot_HoldFireCooldownPenalty pins the issue #62 contract: a
// SHOOT with released=false costs HoldFireCooldownMult× the normal weapon
// cooldown, while released=true keeps the full weapon rate.
func TestHandleShoot_HoldFireCooldownPenalty(t *testing.T) {
	p := NewPlayerState()
	r := NewRoomState(0)
	now := time.Unix(1_000_000, 0)
	wid := p.CurrentWeapon
	baseCd := time.Duration(WeaponSpecs[wid].CooldownMs) * time.Millisecond

	HandleShoot(p, r, 0, true, now)
	gotReleasedCd := p.WeaponCooldown[wid].Sub(now)
	if gotReleasedCd != baseCd {
		t.Fatalf("released=true: cooldown = %v, want %v", gotReleasedCd, baseCd)
	}

	p.WeaponCooldown[wid] = time.Time{}
	HandleShoot(p, r, 0, false, now)
	gotHeldCd := p.WeaponCooldown[wid].Sub(now)
	wantHeldCd := baseCd * HoldFireCooldownMult
	if gotHeldCd != wantHeldCd {
		t.Fatalf("released=false: cooldown = %v, want %v (= base %v × %d)",
			gotHeldCd, wantHeldCd, baseCd, HoldFireCooldownMult)
	}
}
