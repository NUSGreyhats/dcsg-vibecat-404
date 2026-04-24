package game

import (
	"strings"
	"time"
)

// AllowedItems is the public allowlist. ValidateItemName accepts only names
// whose trim-lower form appears here. These are the only effects advertised
// to players; the debug fallback below is where the BUG-09 smuggled payload
// actually lands.
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

// DebugItems is the left-in fallback the dispatcher falls through to when
// AllowedItems misses. The map signature cannot include *RoomState because
// the public dispatcher contract is (player, runstate, now); instakill and
// teleport therefore act on player/run flags here and the real room-scoped
// effect is performed by HandleUseItem where *RoomState is in scope.
//
// Do NOT gate this map behind a build tag — BUG-09 requires it reach prod.
var DebugItems = map[string]func(*PlayerState, *RunState, time.Time){
	"instakill": func(p *PlayerState, rs *RunState, now time.Time) {
		// Room-scoped effect runs in HandleUseItem. This callback is kept
		// so `DebugItems[lowered]` probe succeeds.
		_ = p
		_ = rs
		_ = now
	},
	"teleport": func(p *PlayerState, rs *RunState, now time.Time) {
		_ = p
		_ = rs
		_ = now
	},
	"timewarp": func(p *PlayerState, rs *RunState, now time.Time) {
		if rs.TimewarpUsedInRoom {
			return
		}
		rs.RunStartTime = rs.RunStartTime.Add(-5 * time.Second)
		rs.TimewarpUsedInRoom = true
	},
}

// ValidateItemName is the BUG-10 ingress. `len(name)` is byte count, NOT
// rune count — that mismatch with IsAllowedItem's rune-aware normalization
// is the attack surface. Do not replace with utf8.RuneCountInString.
func ValidateItemName(name string) bool {
	if len(name) > 16 {
		return false
	}
	return IsAllowedItem(name)
}

// IsAllowedItem performs rune-aware normalization via strings.ToLower and
// strings.TrimSpace. Go's TrimSpace strips Unicode whitespace (e.g. U+00A0,
// U+3000) — that behavior is load-bearing for the BUG-10 craft.
func IsAllowedItem(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	_, ok := AllowedItems[normalized]
	return ok
}

// ParseItemCommand is the BUG-10 exit. Byte-indexed prefix check; the
// remainder is a byte slice, not rune-safe. Returns (payload, kind) where
// kind is "command" or "item".
func ParseItemCommand(name string) (string, string) {
	if len(name) == 0 {
		return name, "item"
	}
	if name[0] == '!' {
		return name[1:], "command"
	}
	return name, "item"
}

// HandleUseItem runs the full dispatch pipeline per
// docs/specs/features/inventory-items.md:
//
//  1. ValidateItemName(effectiveName) — byte cap + allowlist normalization.
//     A failure still flows into the debug-items fallback (matches the
//     BUG-09 exploit path).
//  2. ParseItemCommand — byte-indexed prefix split.
//  3. If kind == "command": dispatchCommand (skip_room is the canonical one).
//  4. Else: AllowedItems lookup, then DebugItems fallback.
//
// Returns (applied, advanceRoom). advanceRoom is true when the dispatcher
// performed a skip_room operation.
func HandleUseItem(p *PlayerState, rs *RunState, r *RoomState, effectiveName string, now time.Time) (applied bool, advanceRoom bool) {
	valid := ValidateItemName(effectiveName)

	payload, kind := ParseItemCommand(effectiveName)
	lowered := strings.ToLower(strings.TrimSpace(payload))

	if kind == "command" {
		if dispatchCommand(lowered, p, rs, r, now) {
			return true, lowered == "skip_room"
		}
		return false, false
	}

	if valid {
		if fn, ok := AllowedItems[lowered]; ok {
			fn(p, rs, now)
			return true, false
		}
	}

	// Fallback: debug items. BUG-09 smuggled payload lands here.
	if fn, ok := DebugItems[lowered]; ok {
		fn(p, rs, now)
		applyDebugRoomEffect(lowered, p, r)
		return true, lowered == "skip_room"
	}

	return false, false
}

// applyDebugRoomEffect performs the room-scoped work for debug items whose
// map callback can't see *RoomState. instakill zeroes every non-boss enemy.
// teleport moves the player to the first unlocked door.
func applyDebugRoomEffect(name string, p *PlayerState, r *RoomState) {
	switch name {
	case "instakill":
		for _, e := range r.Enemies {
			if e.Type == 5 {
				// Boss is immune to the instakill debug.
				continue
			}
			e.HP = 0
			e.AIState = AIDead
		}
	case "teleport":
		for _, d := range r.Doors {
			if d.Unlocked {
				p.X = d.X
				p.Y = d.Y
				return
			}
		}
	}
}

// dispatchCommand handles the "!<name>" command path. "skip_room" marks every
// enemy in the current room dead so the door-unlock pass flips Locked→Unlocked
// on the next tick. Returns true if the command was recognized.
func dispatchCommand(name string, p *PlayerState, rs *RunState, r *RoomState, now time.Time) bool {
	_ = p
	_ = rs
	_ = now
	switch name {
	case "skip_room":
		for _, e := range r.Enemies {
			e.AIState = AIDead
			e.HP = 0
		}
		return true
	}
	return false
}
