# Feature — Shop

## Purpose

In-run shop where the player spends Coins on **permanent stackable traits**: ammo capacity, speed, fire rate, damage. Each purchase increments a stack counter; effects compound for the rest of the run (zeroed on death). Coins drop from defeated enemies as world pickups. The shop is opened via a global keybind ("B") at any time during a run — there is no shop room or shop NPC. Hosts [BUG-12 — Pickup Type Confusion](#bug-12-pickup-type-confusion), a Protocol-tier client-trusted-discriminator bug that lets the player declare what type a pickup is at collection time (e.g. claim an ammo crate as a coin).

## Data model

### `PlayerState` additions

Shop items are **permanent stackable traits**, not timed buffs. Each purchase increments a stack counter on `PlayerState`; effective multipliers/caps are derived from the counters. There are no `*Until` expiry fields for shop traits — they persist for the entire run.

```go
PlayerState {
    ...existing fields...
    Coins           int32   // owned by features/shop.md
    SpeedStacks     uint8   // owned by features/shop.md
    FireRateStacks  uint8   // owned by features/shop.md
    DamageStacks    uint8   // owned by features/shop.md
    AmmoStacks      uint8   // owned by features/shop.md
}
```

`int32` is wide enough on `Coins` to absorb BUG-12 abuse without wrapping. Integer-overflow on coins is **not** an intended bug class for this feature.

`uint8` is plenty for the stack counters — coin cost is the natural rate-limit; nobody is stacking 256 traits in a 2-minute run.

The existing `SpeedMult` / `SpeedMultUntil` / `DamageMult` / `DamageMultUntil` timed-buff fields (used by the inventory `speed_boost` consumable) are **unchanged** and orthogonal to shop traits — temporary buffs from inventory items and permanent traits from the shop compose multiplicatively, and shop traits do not introduce new timer machinery.

Effective multipliers and caps are **derived**, not stored:

| Trait      | Derivation                                                                              |
| ---------- | --------------------------------------------------------------------------------------- |
| Speed      | `(1.0 + 0.1 × SpeedStacks) × tempSpeedMult(p, now)` — composes with temp buff           |
| Fire rate  | cooldown divided by `(1.0 + 0.1 × FireRateStacks)`                                      |
| Damage     | `(1.0 + 0.1 × DamageStacks) × tempDamageMult(p, now)` — composes with temp buff         |
| Max ammo   | `WeaponSpec.AmmoMax + 5 × AmmoStacks` per non-unlimited weapon (pistol unaffected)      |

Stacks are reset to zero at run start (on death / new run). They persist across rooms within a run. There is no per-stack diminishing return in the initial design — keep the math linear and tune the per-stack delta if a single trait dominates.

### New pickup type — Coin

Append to the pickup-type enum in `server/internal/game/pickups.go`:

| Type | Name | Effect                       |
| ---- | ---- | ---------------------------- |
| 3    | Coin | `+coinValue` to `p.Coins`    |

Type 3 was previously assigned to "Speed boost" before its removal — the constant is being recycled. See [pickups.md — pickup types](pickups.md#pickup-types).

### Shop catalog (static, hardcoded server + client)

```go
type ShopOffer struct {
    ID       uint8
    Name     string
    Cost     int32
}

var ShopCatalog = []ShopOffer{
    {1, "ammo",      5, /* AmmoStacks++ ; refill all non-unlimited weapons to new cap */},
    {2, "speed",     5, /* SpeedStacks++ */},
    {3, "fire_rate", 5, /* FireRateStacks++ */},
    {4, "damage",    5, /* DamageStacks++ */},
}
```

Catalog is fixed at server startup; client mirrors the same IDs and costs. Dispatch is by `item_id uint8`, **not** by string name — this deliberately keeps the shop path off the `AllowedItems` string-keyed pipeline (which historically hosted BUG-09 / BUG-10).

### Coin drop policy

When an enemy dies (in `internal/game/combat.go`'s enemy-death branch), spawn a `PickupCoin` at the enemy's `(x, y)` with a fresh `entity_id`, registered in the room's `RoomState.Pickups` map identically to any other pickup.

- Drop chance: 100%, 1 coin per enemy. Tune from there.
- Despawn after 30 s of being unclaimed (a new time-bounded cleanup pass on `RoomState.Pickups`; or reuse generalized `RespawnAt` logic if [pickups.md — implementation notes](pickups.md#implementation-notes) factors it out).
- Because coins live in the same `Pickups` registry and use the same `C2S_INTERACT` path as ammo / health, they are subject to BUG-05 *and* BUG-12.

## Data flow

### Coin collection (the buggy path)

```
C2S_INTERACT { target_id, claimed_type }    ← claimed_type added for shop
    │
    ▼
handleInteract:
    pk, ok := r.Pickups[target_id]
    if !ok || pk.Consumed { return }
    if !inRange(p, pk) { return }            // still subject to BUG-05
    applyPickup(p, pk, claimed_type, now)
    pk.Consumed = true
    │
    ▼
applyPickup switches on CLAIMED_TYPE — see BUG-12
    │
    ▼
S2C_STATE broadcasts updated PlayerSnapshot (Coins, ammo, buff fields, etc.)
```

The pickup is consumed regardless of which switch case ran — claiming an ammo crate as a coin still consumes the ammo crate. This is intentional ([BUG-12](#bug-12-pickup-type-confusion)).

### Shop purchase (server-authoritative)

```
C2S_SHOP_PURCHASE { item_id: uint8 }
    │
    ▼
handleShopPurchase:
    if item_id == 0 || int(item_id) > len(ShopCatalog) { return }
    offer := ShopCatalog[item_id - 1]
    if p.Coins < offer.Cost { return }
    p.Coins -= offer.Cost
    applyShopEffect(p, offer.ID)        // increments the stack counter
    │
    ▼
S2C_STATE broadcasts updated PlayerSnapshot
```

`applyShopEffect` is a small `switch offer.ID` — integer dispatch, no string-name lookup, no fallback table. The body is one line per case: `p.SpeedStacks++`, `p.DamageStacks++`, etc. The `ammo` case also iterates `WeaponSpecs` and refills `WeaponAmmo[w]` to the post-stack cap.

### Stack semantics

All four shop items are **permanent for the duration of the run** and **stack additively**. Each purchase increments the corresponding `*Stacks` counter; the effective multiplier or cap is derived per the table in the data-model section. There is no expiry, no decay, no overwrite — purchases compound until the run ends (death / new run zeroes them).

`ammo` is special-cased: in addition to incrementing `AmmoStacks`, the purchase **refills** every non-unlimited weapon's ammo to its new cap. Without the refill, "I bought ammo and got nothing visible" would be a footgun.

There is no purchase cooldown. Stack growth is gated by coin supply — at 5 coins per purchase and ~1 coin per enemy kill, a run yields ~4–6 stacks total honestly. BUG-12 lifts that ceiling toward "every pickup converts to a stack", which is intentional.

## Client interactions

- **New scene `ShopMenu`** at `client/src/game/scenes/ShopMenu.ts` — modal overlay following the [`InventoryMenu`](../client/04-scenes.md) pattern: pause-on-launch, depth-stacked above `Game`, keyboard navigation, panel styling from [client/10-ui-components.md](../client/10-ui-components.md).
- **Open**: global "B" keybind in the `GameHUD` input layer (see [client/07-input-controls.md](../client/07-input-controls.md)). The shop is fully client-side until purchase — no `C2S_SHOP_OPEN` is sent.
- **Catalog**: client mirrors `ShopCatalog` (same IDs, costs, display names). Items where `coins < cost` render greyed.
- **Selection**: Enter → `sendShopPurchase(item_id)`. Server reply lands in the next `S2C_STATE`.
- **HUD**: coin counter rendered in `GameHUD.ts` (top-right, gold tone).
- **Coin pickup sprite**: new asset registered in `Preloader.ts`; rendered by the existing pickup pipeline in [client/05-entities-rendering.md](../client/05-entities-rendering.md).
- **Honest behavior**: client always sends `claimed_type = pickup.type` (the type it sees rendered). The buggy path is invisible without packet manipulation.

## Implementation notes

- Package: `internal/game/shop` (new).
- Do **not** route shop purchases through `AllowedItems` (the string-keyed map in `items.go`). Shop dispatch is integer-keyed by design — adding it to the string map would re-expose BUG-09 / BUG-10 surfaces (currently disabled) on the shop path.
- Effective multipliers/caps are **derived per read**, not cached on `PlayerState`. The combat / movement / ammo paths each compute `1.0 + 0.1 × *Stacks` (or the ammo equivalent) at the point of use. Don't memoize — the stack counter is the source of truth and the arithmetic is trivial.
- Stack counters are zeroed at run start (alongside the existing run-state reset). They do **not** persist across deaths.
- The temporary `SpeedMult` / `DamageMult` fields used by inventory `speed_boost` are unrelated to shop stacks. Effective speed at a tick is `(1 + 0.1 × p.SpeedStacks) × nonZeroSpeedMult(p, now)` — **keep** the `nonZero*Mult(p, now)` helper pattern for the temp-buff side; the shop trait reads do not need expiry-checking.
- `ammo` purchase iterates `WeaponSpecs` once and writes `WeaponAmmo[w] = newCap` for every non-unlimited weapon (skip pistol). This is the only shop case that mutates anything besides a stack counter.
- Coin drops use the existing `RoomState.Pickups` registry. A despawn-by-time pass needs to be added (or generalized from any future `RespawnAt` logic in pickups.md).
- The `Coins` int32 field is **not** narrowed for "fun" — see Data model note.
- Replay engine must persist `claimed_type` alongside `target_id` in the per-tick input log; otherwise BUG-12-stacked runs won't reproduce. See [foundations/08-leaderboard-replay.md](../foundations/08-leaderboard-replay.md).

## Implementation status

Greenfield. Nothing in the current server or client implements shop, coins, trait stacks, or `claimed_type`. The `Coins` counter mentioned in [pickups.md — implementation status](pickups.md#implementation-status) is a leftover scaffold and is redefined by this feature.

When implementing, the following appends are also required:

- [foundations/02-protocol.md](../foundations/02-protocol.md): extend `C2S_INTERACT` payload with `claimed_type uint8`; add `C2S_SHOP_PURCHASE` (suggested code `0x09`) row.
- [foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md): append `Coins`, `SpeedStacks`, `FireRateStacks`, `DamageStacks`, `AmmoStacks` to the `PlayerState` block, citing this spec; note that stacks zero on run reset.
- [features/pickups.md](pickups.md): add Coin (Type 3) row to the pickup-types table; cross-link BUG-12; correct the existing "auto-trigger Coin" mention in its data-flow paragraph (coins use the manual `C2S_INTERACT` path so BUG-12 applies).
- [bugs.md](../bugs.md): append BUG-12 row to the index; update the synergy diagram with a `BUG-05 → BUG-12` edge.
- [client/04-scenes.md](../client/04-scenes.md): append `ShopMenu` row.
- [foundations/08-leaderboard-replay.md](../foundations/08-leaderboard-replay.md): replay engine records `claimed_type` per interact.

## Intentional bugs in this area

### BUG-12 Pickup Type Confusion

**Source:** OWASP A04:2021 — Insecure Design (client-trusted discriminator). Same anti-pattern family as Mass Assignment / Parameter Tampering: the server accepts a client-supplied tag and uses it to dispatch into authoritative state, even though the server already has the canonical tag.

**Design choice enabling the bug:** `C2S_INTERACT` carries a `claimed_type uint8` field, and `applyPickup` switches on `claimed_type` rather than on the server-stored `pk.Type`.

**Root cause sketch:**

```go
type InteractPacket struct {
    TargetID    uint32
    ClaimedType uint8   // added for shop; should not be trusted
}

func handleInteract(p *PlayerState, r *RoomState, pkt InteractPacket, now time.Time) {
    pk, ok := r.Pickups[pkt.TargetID]
    if !ok || pk.Consumed { return }
    if !inRange(p, pk) { return }
    applyPickup(p, pk, pkt.ClaimedType, now)   // ← takes client-declared type
    pk.Consumed = true
}

func applyPickup(p *PlayerState, pk *PickupState, claimedType uint8, now time.Time) {
    switch claimedType {     // ← NOT pk.Type
    case PickupHealthPotion: ApplyHeal(p, 20)
    case PickupAmmoCrate:    /* +20 ammo to current weapon */
    case PickupCoin:         p.Coins += 1
    }
}
```

The server validates that the pickup *exists*, that it's in range (subject to BUG-05's loose check), and that it's unconsumed. It does **not** validate that `claimedType == pk.Type`.

**Exploit path:** Modify the outgoing `C2S_INTERACT` packet to set `claimed_type = PickupCoin` while interacting with any pickup. Each ammo crate or health potion the player walks past becomes `+1` coin instead of its honest effect, bypassing the need to grind enemies for coin drops.

A weaker reverse exploit also exists — claim a coin as `PickupHealthPotion` to gain `+20 HP`. Less interesting because health potions are plentiful and HP is rarely the binding constraint.

**Bound:** Limited per pickup — each pickup is consumed once via the `pk.Consumed` flag. The exploit's economic ceiling is `(number of pickups in the run) × 1 coin`. Pickup count is room-bounded; the player cannot mint coins from thin air. Combined with shop costs (5–20 coins per item), the practical upside is roughly one extra shop purchase per run-without-grinding-enemies.

**Detection:** Replay reproduces identically — `(seed, InputLog)` must include the recorded `claimed_type` per interact. Do **not** "fix" `applyPickup` to switch on `pk.Type`, and do **not** drop the `claimed_type` field from the packet.

**Synergies:**
- Strong synergy with [BUG-05](pickups.md#bug-05-interact-range-bug): grab pickups across the room *and* choose what they decode as → multiplies the type-confusion abuse rate.
- Indirect synergy with [BUG-04](combat-weapons.md#bug-04-swap-cancel) and [BUG-07](combat-weapons.md#bug-07-damage-batch-overflow) via the shop's `damage_boost` and `fire_rate` items — converting incidental ammo crates into shop currency funds the buffs that turbo-charge those DPS bugs.

**Savings:** ~10–20 s per run. Larger than for a timed-buff economy because shop traits are **permanent and stack additively** — every minted coin converts to a stack that compounds for the whole run, not just a 10s window. The most exploitative target is `damage` (each stack permanently boosts DPS through every subsequent fight, including the boss). Concrete back-of-envelope: ~10-15 pickups per run, BUG-12 converts each into a coin, ~2-3 extra trait stacks beyond honest play, ~20-30% effective DPS / mobility uplift for the run's tail.

## Packets used

- `C2S_INTERACT` — payload extended with `ClaimedType uint8`. **This breaks the existing payload shape**; coordinate with [foundations/02-protocol.md](../foundations/02-protocol.md) and the client's `packet.ts`.
- `C2S_SHOP_PURCHASE` (new, suggested type code `0x09`) — `{ item_id: uint8 }`.
- `S2C_STATE` — already broadcasts `PlayerSnapshot`; the snapshot is extended with `Coins`, `SpeedStacks`, `FireRateStacks`, `DamageStacks`, `AmmoStacks`.

A `C2S_SHOP_OPEN` / `S2C_SHOP_STATE` pair is **not** introduced — the shop UI is fully client-side until purchase. The README's "How to add a feature" worked example shows a heavier shape (with open packet and between-rooms gating); this spec deviates intentionally because the shop opens via global keybind anywhere and the catalog is static.

## Cross-references

- [features/pickups.md](pickups.md) — `applyPickup` lives there; BUG-05 stacks with BUG-12; Coin (Type 3) added to its pickup-types table.
- [features/inventory-items.md](inventory-items.md) — confirms shop dispatch is intentionally *not* routed through `AllowedItems`.
- [features/combat-weapons.md](combat-weapons.md) — damage and fire-rate buff multipliers consumed by the shoot pipeline; `nonZero*Mult` helper pattern.
- [features/mob-ai.md](mob-ai.md) — enemy death is the coin drop trigger.
- [foundations/02-protocol.md](../foundations/02-protocol.md) — `InteractPacket.ClaimedType` field and `C2S_SHOP_PURCHASE` row.
- [foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md) — `Coins`, `FireRateMult`, `FireRateMultUntil` on `PlayerState`.
- [foundations/08-leaderboard-replay.md](../foundations/08-leaderboard-replay.md) — replay must record `claimed_type`.
- [client/04-scenes.md](../client/04-scenes.md) — `ShopMenu` scene.
- [client/07-input-controls.md](../client/07-input-controls.md) — "B" → open ShopMenu binding.
- [client/10-ui-components.md](../client/10-ui-components.md) — panel styling tokens reused by `ShopMenu`.
- [bugs.md](../bugs.md) — BUG-12 index row.
