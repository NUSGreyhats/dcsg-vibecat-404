package game

// ShopOffer is one row in the static shop catalog. Dispatch is by integer
// ID, not string name. See features/shop.md.
type ShopOffer struct {
	ID   uint8
	Name string
	Cost int32
}

// Shop item IDs. Stable wire constants — the client mirrors these.
const (
	ShopItemAmmo     uint8 = 1
	ShopItemSpeed    uint8 = 2
	ShopItemFireRate uint8 = 3
	ShopItemDamage   uint8 = 4
)

// ShopCatalog is the fixed offer list. Tunable but not hot-reloaded —
// server and client mirror it at startup.
var ShopCatalog = []ShopOffer{
	{ID: ShopItemAmmo, Name: "ammo", Cost: 5},
	{ID: ShopItemSpeed, Name: "speed", Cost: 10},
	{ID: ShopItemFireRate, Name: "fire_rate", Cost: 15},
	{ID: ShopItemDamage, Name: "damage", Cost: 20},
}

// HandleShopPurchase is the C2S_SHOP_PURCHASE handler. Validates the item
// ID, debits coins, and applies the matching effect. The `ammo` case
// refills every non-unlimited weapon to its static ammo cap.
//
// `replayMode` waives the coin-balance gate: the recording's
// SHOP_PURCHASE packet is proof the live engine accepted it (the
// player had the coins at the time), and rejecting it in replay
// because of an earlier kill cascade would compound the divergence.
// Coins still decrement (clamped at 0) so the HUD tracks something
// reasonable.
func HandleShopPurchase(p *PlayerState, itemID uint8, replayMode bool) bool {
	if itemID == 0 || int(itemID) > len(ShopCatalog) {
		return false
	}
	offer := ShopCatalog[itemID-1]
	if !replayMode && p.Coins < offer.Cost {
		return false
	}
	p.Coins -= offer.Cost
	if p.Coins < 0 {
		p.Coins = 0
	}

	switch offer.ID {
	case ShopItemAmmo:
		// Refill: top up every non-pistol weapon to its static cap. The
		// purchase is a consumable-style refill, NOT a permanent stack —
		// max capacity is unchanged.
		for w, spec := range WeaponSpecs {
			if spec.AmmoMax >= 9999 {
				continue
			}
			p.WeaponAmmo[w] = spec.AmmoMax
		}
	case ShopItemSpeed:
		p.SpeedStacks++
	case ShopItemFireRate:
		p.FireRateStacks++
	case ShopItemDamage:
		p.DamageStacks++
	default:
		// Unknown ID after bounds-check — refund.
		p.Coins += offer.Cost
		return false
	}
	return true
}
