# Feature — Inventory & Items

## Purpose

Player inventory (string-named items) and item-name validation pipeline. Formerly also hosted a command-prefix parser and a "debug items" fallback (`instakill`, `teleport`, `timewarp`, `!skip_room`). Those enabled [BUG-09](#bug-09-json-smuggling-dispatch-side-detail) and [BUG-10](#bug-10-rune-vs-byte-item-length-bypass) — both are **currently disabled**, see [bugs.md](../bugs.md#currently-disabled). The specs for the disabled bugs are preserved below for when they return.

## Data model

Inventory lives on `PlayerState` — see [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md):

```go
Inventory []string   // item names, size-capped; see validateItemName
```

### Allowed items map

```go
var AllowedItems = map[string]func(*PlayerState, *RunState, time.Time){
    "health_potion": func(p *PlayerState, rs *RunState, now time.Time) { ApplyHeal(p, 20) },
    "speed_boost":   func(p *PlayerState, rs *RunState, now time.Time) { p.SpeedMult = 1.5; p.SpeedMultUntil = now.Add(5 * time.Second) },
    "shield":        func(p *PlayerState, rs *RunState, now time.Time) { p.ShieldUntil = now.Add(5 * time.Second) },
}
```

This is the complete set of usable items in the shipping code. Nothing else dispatches.

### Debug items map — REMOVED

Formerly `DebugItems` held `instakill` / `teleport` / `timewarp`, and a separate command path dispatched `!skip_room`. These have been removed from `server/internal/game/items.go`. See [bugs.md](../bugs.md#currently-disabled) for the rationale and the possibility of reintroducing them.

## Validation + dispatch pipeline

### 1. Length validation

```go
func ValidateItemName(name string) bool {
    if len(name) > 16 {
        return false
    }
    return IsAllowedItem(name)
}
```

### 2. Rune-aware allow check

```go
func IsAllowedItem(name string) bool {
    normalized := strings.ToLower(strings.TrimSpace(name))
    _, ok := AllowedItems[normalized]
    return ok
}
```

Historically the mismatch between byte-counting `len()` here and rune-aware `strings.ToLower`/`strings.TrimSpace` below was the surface for BUG-10. Without a debug-items fallback (or a command prefix) there is nothing useful to reach via a bypass, so the mismatch is currently benign. The mismatch itself is intentionally preserved so that reintroducing BUG-10 later is a one-line change (re-add the command path).

### 3. Dispatch

```go
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
```

No command-prefix parsing, no fallback into a debug map. The `advanceRoom` return value is retained for signature stability but is always `false` now (it previously signalled that `!skip_room` had cleared the room).

## Data flow

```
C2S_USE_ITEM { item_name }
    │
    ▼
encoding/json → UseItemPacket { ItemName }
    │
    ▼
ValidateItemName (byte length → bool, + IsAllowedItem)
    │
    ▼ (valid items only)
AllowedItems lookup → effect
```

## Client interactions

- `client/src/game/scenes/InventoryMenu.ts` renders the inventory overlay and lets the player select an item.
- Selecting an item → `C2S_USE_ITEM { item_name }`.
- Client does NOT validate item names — the server is authoritative (and intentionally buggy).

## Implementation notes

- Package: `internal/game/items`.
- `strings.TrimSpace` specifically: Go's implementation trims Unicode whitespace. Do not replace with an ASCII-only trim without thinking first — that change would close off the BUG-10 ingress if/when the debug-items path returns.

## Implementation status

Active. Current server ships the shape above — `AllowedItems` only.

## Intentional bugs in this area

All item-hosted bugs are currently **disabled**. Specs preserved.

### BUG-10 Rune vs Byte Item Length Bypass — DISABLED

**Source:** [100 Go Mistakes #36 — Not understanding the concept of a rune](https://100go.co/#not-understanding-the-concept-of-rune-36). In Go, `len(string)` returns **byte count**, not character count. Multi-byte UTF-8 characters (emoji, CJK, etc.) count as 1 rune but 2–4 bytes.

**Design choice enabling the bug:**

1. `validateItemName` uses `len(name) > 16` — byte-count check.
2. `isAllowedItem` uses `strings.ToLower` + `strings.TrimSpace` — rune-aware.
3. `parseItemCommand` uses `name[0] == '!'` and `name[1:]` — byte-indexed.

These three stages see the same bytes but different effective strings.

**Root cause sketch:** See the three functions above — the mismatch between byte-indexed command parsing and rune-aware normalization is the attack surface.

**Exploit path:**

Craft an item name that:
- Starts with a multi-byte UTF-8 character whose byte sequence is **not** `0x21` ('!') but whose trimmed/normalized form *is* a legal-looking name.
- After a `strings.TrimSpace` applied to certain Unicode whitespace prefixes (U+00A0 no-break space, U+3000 ideographic space), the effective string for `isAllowedItem` differs from the byte view the command parser sees.

Practical craft: the raw bytes `"\xE3\x80\x80!skip_room"` (U+3000 ideographic space + `!skip_room`).
- `len()` = 14 bytes (passes 16-byte cap).
- `strings.TrimSpace` strips U+3000 → `isAllowedItem` sees `"!skip_room"` → not in `allowedItems` → `validateItemName` returns `false`.

So that specific example fails at the allowlist. A working craft needs a bytestring that:
1. Has `len() ≤ 16`.
2. Passes `isAllowedItem` (trim + lowercase form is in `allowedItems`).
3. `name[0] == '!'` after byte-indexing.

E.g.: a legal item name that *also* begins with `!` after a specific rune sequence shifts byte positions. Exact craft is left to participants to discover — the bug is that such a craft exists.

**Bound:** Single-room skip only. Player must construct a carefully encoded UTF-8 string. The room is marked as skipped on the leaderboard replay (visible, but the time stands).

**Detection:** Replay reproduces identically (given the exact bytes). Do NOT switch `len()` to `utf8.RuneCountInString()` or replace byte indexing with rune iteration — both fix the bug.

**Synergies:** Limited to 1–2 uses per run (server tracks). Stacks additively with all time-savings bugs.

**Savings:** ~3–5 s per room where used.

**Status:** Disabled. The byte-vs-rune asymmetry still exists in `ValidateItemName`, but the command-prefix parser and `DebugItems` map it used to reach are gone. To reintroduce: re-add `ParseItemCommand` + `dispatchCommand` + a `DebugItems` map in `server/internal/game/items.go` and route them from `HandleUseItem`. Discuss the room cap first — uncapped `!skip_room` skips the entire run.

---

### BUG-09 JSON Smuggling (dispatch-side detail) — DISABLED

The parser-differential body of this bug lives in [../foundations/02-protocol.md#bug-09-json-case-insensitive-field-smuggling](../foundations/02-protocol.md#bug-09-json-case-insensitive-field-smuggling). The dispatch fallback (`debugItems`) that the smuggled `timewarp` / `instakill` payload used to land in no longer exists.

**Former design choice (this side):** The dispatcher had a fallback path into `debugItems` when the name was not in `allowedItems`. That fallback has been removed from `server/internal/game/items.go` — `HandleUseItem` now rejects anything not in `AllowedItems`.

**To reintroduce:** restore `DebugItems` + the fallback branch in `HandleUseItem`. The JSON parser-side asymmetry still exists in the protocol spec — nothing has changed there. See the BUG-09 note in the protocol doc.

## Packets used

- `C2S_USE_ITEM`.
- `S2C_STATE` (reflects inventory + effect changes).

## Cross-references

- [../foundations/02-protocol.md](../foundations/02-protocol.md) — JSON parser semantics + BUG-09 parser half.
- [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md) — `PlayerState.Inventory`.
- [combat-weapons.md](combat-weapons.md) — when BUG-09 returns, `instakill` and `timewarp` stack with BUG-08 boss setup (also disabled).
- [../bugs.md](../bugs.md) — central index, including the disabled-bugs list.
