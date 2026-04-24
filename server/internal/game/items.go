package game

import (
	"strings"
	"time"
)

// AllowedItems is the exhaustive list of usable items. HandleUseItem only
// dispatches through this map — there is no debug/command fallback.
var AllowedItems = map[string]func(*PlayerState, *RunState, time.Time){
	"health_potion": func(p *PlayerState, rs *RunState, now time.Time) {
		ApplyHeal(p, 20)
	},
	"speed_boost": func(p *PlayerState, rs *RunState, now time.Time) {
		p.SpeedMult = 1.5
		p.SpeedMultUntil = now.Add(5 * time.Second)
	},
	"shield": func(p *PlayerState, rs *RunState, now time.Time) {
		p.ShieldUntil = now.Add(5 * time.Second)
	},
}

func ValidateItemName(name string) bool {
	if len(name) > 16 {
		return false
	}
	return IsAllowedItem(name)
}

func IsAllowedItem(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	_, ok := AllowedItems[normalized]
	return ok
}

// HandleUseItem resolves an item-use request. Validated names run their
// effect; anything else (including "!<command>" payloads) is ignored.
func HandleUseItem(p *PlayerState, rs *RunState, r *RoomState, effectiveName string, now time.Time) (applied bool, advanceRoom bool) {
	_ = r
	if !ValidateItemName(effectiveName) {
		return false, false
	}
	normalized := strings.ToLower(strings.TrimSpace(effectiveName))
	fn, ok := AllowedItems[normalized]
	if !ok {
		return false, false
	}
	fn(p, rs, now)
	return true, false
}
