# Feature — Inventory & Items

## Purpose

Player inventory (string-named items), item-name validation pipeline, command-prefix parsing on item names, and the dispatch between "allowed" items and a left-in "debug" item set. Hosts [BUG-10 — Rune vs Byte Item Length Bypass](#bug-10-rune-vs-byte-item-length-bypass) and the item-dispatch half of [BUG-09 — JSON Smuggling](../foundations/02-protocol.md#bug-09-json-case-insensitive-field-smuggling) (the parser half lives in the protocol spec).

## Data model

Inventory lives on `PlayerState` — see [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md):

```go
Inventory []string   // item names, size-capped; see validateItemName
```

### Allowed items map

```go
var allowedItems = map[string]ItemDef{
    "health_potion":  { effect: heal(20) },
    "speed_boost":    { effect: speedMult(1.5, 5*time.Second) },
    "shield":         { effect: damageReduction(0.5, 5*time.Second) },
}
```

### Debug items map (left-in fallback)

```go
var debugItems = map[string]ItemDef{
    "instakill":     { damage: 9999 },
    "teleport":      { effect: teleportToDoor() },
    "timewarp":      { duration: -5000 },  // subtracts 5s from elapsed
}
```

`ItemDef` is a union of possible effects; the shape doesn't matter for the bug — what matters is that debug items exist as a fallback the dispatcher falls through to when the name doesn't match `allowedItems`.

## Validation + parse pipeline

### 1. Length validation (byte-count based — BUG-10 ingress)

```go
func validateItemName(name string) bool {
    if len(name) > 16 {    // len() counts BYTES, not characters
        return false
    }
    return isAllowedItem(name)
}
```

### 2. Rune-aware allow check

```go
func isAllowedItem(name string) bool {
    normalized := strings.ToLower(strings.TrimSpace(name))
    _, exists := allowedItems[normalized]
    return exists
}
```

Note `strings.TrimSpace` strips certain Unicode whitespace characters as well as ASCII — the trim can shift the effective string in ways that byte-index operations below don't see.

### 3. Command-prefix parsing (byte-index based — BUG-10 exit)

```go
func parseItemCommand(name string) (string, string) {
    // Check for command prefix "!" at byte position 0
    if name[0] == '!' {
        return name[1:], "command"      // byte slice, not rune-safe
    }
    return name, "item"
}
```

If `parseItemCommand` returns `("skip_room", "command")`, a command dispatcher is invoked that triggers debug commands (e.g. `skip_room` instantly clears the current room).

## Dispatch

```go
func handleUseItem(pkt UseItemPacket, player *PlayerState) {
    name := pkt.ItemName
    if !validateItemName(name) { return }       // byte-length check + allow check

    payload, kind := parseItemCommand(name)
    switch kind {
    case "command":
        dispatchCommand(payload, player)         // includes skip_room etc.
    case "item":
        if item, ok := allowedItems[strings.ToLower(payload)]; ok {
            applyItem(item, player); return
        }
        // fallback — debug items (meant to be disabled, still present)
        if item, ok := debugItems[strings.ToLower(payload)]; ok {
            applyItem(item, player); return
        }
    }
}
```

This is the dispatch-side half of [BUG-09](../foundations/02-protocol.md#bug-09-json-case-insensitive-field-smuggling) — the debug-items fallback is the payload the JSON parser differential delivers.

## Data flow

```
C2S_USE_ITEM { item_name }
    │
    ▼
[audit / logging middleware — parser P1, case-sensitive, first-key-wins]
    │ logs e.g. "HEALTH_POTION"
    ▼
encoding/json → UseItemPacket { ItemName: "timewarp" }    ← parser P2 (last-key-wins)
    │
    ▼
validateItemName (byte length → bool)                     ← BUG-10 ingress
    │
    ▼
parseItemCommand (byte-indexed prefix check)              ← BUG-10 exit
    │       │
    │       └─── "command" path → dispatchCommand (skip_room, …)
    │
    ▼ "item" path
allowedItems lookup → applyItem
    │
    ▼ fallback
debugItems lookup → applyItem                             ← BUG-09 payload lands here
```

## Client interactions

- `client/src/game/scenes/InventoryMenu.ts` renders the inventory overlay and lets the player select an item.
- Selecting an item → `C2S_USE_ITEM { item_name }`.
- Client does NOT validate item names — the server is authoritative (and intentionally buggy).

## Implementation notes

- Package: `internal/game/items` (target).
- Keep all three stages (validate → parseCommand → dispatch) as separate functions. BUG-10 requires that the length/normalization stage and the command-parse stage see different effective strings.
- `strings.TrimSpace` specifically: Go's implementation trims Unicode whitespace. Do not replace with an ASCII-only trim — that changes the attack surface.
- `debugItems` must remain reachable via the fallback. Do not delete the map.

## Implementation status

Greenfield. Current server has no item system; only a `Coins` counter. The current `client/src/game/shop.ts` hints at a future shop flow that'll overlap with inventory.

## Intentional bugs in this area

### BUG-10 Rune vs Byte Item Length Bypass

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

---

### BUG-09 JSON Smuggling (dispatch-side detail)

The parser-differential body of this bug lives in [../foundations/02-protocol.md#bug-09-json-case-insensitive-field-smuggling](../foundations/02-protocol.md#bug-09-json-case-insensitive-field-smuggling). The dispatch fallback (`debugItems`) that the smuggled `timewarp` / `instakill` payload lands in lives here.

**Design choice (this side):** The dispatcher has a fallback path into `debugItems` when the name is not in `allowedItems`. This fallback was "meant to be disabled" but is left in — a realistic code-review miss.

**What participants need from this file:** knowledge that the `debugItems` map exists and what its entries do. This is sourced from the stripped public source.

**Do NOT:**
- Remove the `debugItems` fallback.
- Gate it behind a build tag (that would allow CI to catch it).

## Packets used

- `C2S_USE_ITEM`.
- `S2C_STATE` (reflects inventory + effect changes).

## Cross-references

- [../foundations/02-protocol.md](../foundations/02-protocol.md) — JSON parser semantics + BUG-09 parser half.
- [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md) — `PlayerState.Inventory`.
- [combat-weapons.md](combat-weapons.md) — `instakill` and `timewarp` stack with BUG-08 boss setup.
- [../bugs.md](../bugs.md) — central index.
