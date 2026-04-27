# Feature — Shop

## Purpose

In-run shop where the player spends Coins on **permanent stackable traits**: ammo capacity, speed, fire rate, damage. Each purchase increments a stack counter on `PlayerState`; effects compound for the rest of the run (zeroed on death). Coins drop from defeated enemies as world pickups. The shop is opened via a global keybind ("B") at any time during a run — there is no shop room or shop NPC. Hosts [BUG-12 — Pickup Type Confusion](#bug-12-pickup-type-confusion), a Protocol-tier bug where the server uses a client-supplied `claimed_type` field on `C2S_INTERACT` to dispatch the pickup effect, instead of looking up the type stored on the pickup. A modified client can claim an ammo crate as a coin and mint currency.

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
}
```

`Coins` is `int32`; integer-overflow on coins is **not** an intended bug class. Stack counters are `uint8` (coin cost rate-limits stack growth far below 255). The `ammo` shop item is a **one-shot consumable refill** — it does not increment a stack counter and has no `AmmoStacks` field.

Effective multipliers and caps are **derived**, not stored:

| Trait      | Derivation                                                                              |
| ---------- | --------------------------------------------------------------------------------------- |
| Speed      | `(1.0 + 0.1 × SpeedStacks)`                                                              |
| Fire rate  | cooldown divided by `(1.0 + 0.1 × FireRateStacks)`                                      |
| Damage     | `(1.0 + 0.1 × DamageStacks)`                                                             |
| Ammo       | `ammo` purchase refills every non-unlimited weapon to `WeaponSpec.AmmoMax` (static cap) |

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
    {1, "ammo",      5,  /* one-shot refill to WeaponSpec.AmmoMax for every non-unlimited weapon */},
    {2, "speed",     10, /* SpeedStacks++ */},
    {3, "fire_rate", 15, /* FireRateStacks++ */},
    {4, "damage",    20, /* DamageStacks++ */},
}
```

Catalog is fixed at server startup; client mirrors the same IDs and costs. Dispatch is by `item_id uint8`, not by string name.

### Coin drop policy

When an enemy's HP reaches zero **in the projectile-damage death pass** (`ResolveCollisions` in `combat.go`), register a new `PickupState` in `r.Pickups` with `Type=PickupCoin`, the enemy's last `(X, Y)`, a fresh `EntityID`, and `ExpiresAt = now.Add(30 * time.Second)`. `ExpiresAt` is a new field on `PickupState` (see [pickups.md](pickups.md) — `RespawnAt` is the existing time field; `ExpiresAt` is added for the despawn-on-timeout behavior coins need).

Kamikaze contact-deaths do **not** drop coins. `advanceEnemies` flips kamikazes to `AIDead` before the projectile death pass runs, so the `AIState != AIDead && HP <= 0` guard skips them. This is intentional — coins are a player-skill reward; self-destruct deaths cost the player HP and shouldn't also be funded.

- Drop chance: 100%, 1 coin per enemy. Tunable.
- Despawn at `ExpiresAt`: in the per-tick pickup pass, set `pk.Consumed = true` for any `pk` where `!pk.Consumed && !pk.ExpiresAt.IsZero() && now.After(pk.ExpiresAt)`. The existing pickup-broadcast logic drops consumed pickups from the next snapshot.
- Because coins live in the same `Pickups` registry and use the same `C2S_INTERACT` path as ammo / health, they are subject to [BUG-05](pickups.md#bug-05-interact-range-bug) *and* [BUG-12](#bug-12-pickup-type-confusion).

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
S2C_STATE broadcasts updated PlayerSnapshot (Coins, *Stacks, WeaponAmmo, ...)
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

`applyShopEffect` is a small `switch offer.ID` — integer dispatch, no string-name lookup, no fallback table. The body is one line per case: `p.SpeedStacks++`, `p.DamageStacks++`, etc. The `ammo` case iterates `WeaponSpecs` and refills `WeaponAmmo[w]` to the static `spec.AmmoMax` cap — it does **not** increment any stack counter.

### Stack semantics

Each `speed`, `fire_rate`, and `damage` purchase increments the corresponding `*Stacks` counter on `PlayerState`. Stacks persist for the rest of the run and zero on death / new run. There is no expiry, no decay, no overwrite, no purchase cooldown.

`ammo` is a **one-shot consumable refill** — it does not increment any stack counter. The handler writes `p.WeaponAmmo[w] = spec.AmmoMax` for every non-unlimited weapon, resetting current ammo to the static cap. Max ammo capacity is unchanged.

Stack growth is gated by coin supply, not by a cooldown. With costs of 10/15/20 for speed/fire_rate/damage and ~1 coin per enemy kill, an honest run yields ~2–3 stacks total; BUG-12 lifts that ceiling toward "every pickup converts to coins".

## Client interactions

- **`ShopMenu`** at `client/src/game/scenes/ShopMenu.ts` — modal overlay: pause-on-launch, depth-stacked above `Game`, keyboard navigation, panel styling from [client/10-ui-components.md](../client/10-ui-components.md).
- **Open**: global "B" keybind in the `GameHUD` input layer (see [client/07-input-controls.md](../client/07-input-controls.md)). The shop is fully client-side until purchase — no `C2S_SHOP_OPEN` is sent.
- **Catalog**: client mirrors `ShopCatalog` (same IDs, costs, display names). Items where `coins < cost` render greyed.
- **Selection**: Enter → `sendShopPurchase(item_id)`. Server reply lands in the next `S2C_STATE`.
- **HUD**: coin counter rendered in `GameHUD.ts` (top-right, gold tone).
- **Coin pickup sprite**: new asset registered in `Preloader.ts`; rendered by the existing pickup pipeline in [client/05-entities-rendering.md](../client/05-entities-rendering.md).
- **Honest behavior**: client always sends `claimed_type = pickup.type` (the type it sees rendered). The buggy path is invisible without packet manipulation.

## Implementation notes

### Server

- **`internal/game/state.go`** — append `Coins int32` and `SpeedStacks / FireRateStacks / DamageStacks uint8` to `PlayerState`. Zero all four in the per-run reset path (death / new run).
- **`internal/game/pickups.go`** — add the constant `PickupCoin = 3` (recycled from the removed Speed boost type). Add `ExpiresAt time.Time` to `PickupState`. Change `applyPickup`'s signature to `applyPickup(p *PlayerState, pk *PickupState, claimedType uint8, now time.Time)` and switch on `claimedType` (see [BUG-12](#bug-12-pickup-type-confusion)). Add a `case PickupCoin: p.Coins++` arm.
- **`internal/game/combat.go`** — at the point where an enemy's HP reaches zero in the damage path, register a new `PickupState{Type: PickupCoin, X, Y, EntityID: r.NextEntityID(), ExpiresAt: now.Add(30*time.Second)}` in `r.Pickups`.
- **`internal/game/shop.go`** — define `ShopOffer`, `ShopCatalog` (the slice from the data-model section), and `HandleShopPurchase(p *PlayerState, itemID uint8) (applied bool)`. Body: bounds-check `itemID` against `len(ShopCatalog)`, debit `p.Coins`, switch on `offer.ID` to increment the matching `*Stacks` counter. The `ammo` case walks `WeaponSpecs` and writes `p.WeaponAmmo[w] = spec.AmmoMax` for every non-unlimited weapon — a one-shot refill to the static cap.
- **`internal/protocol/packets.go`** — extend `InteractPacket` with `ClaimedType uint8` (json tag `claimed_type`); add `ShopPurchasePacket` with one field `ItemID uint8` (json tag `item_id`); register `C2S_SHOP_PURCHASE` at type code `0x09`.
- **C2S dispatcher** — register `C2S_SHOP_PURCHASE` → `HandleShopPurchase` next to the existing `C2S_INTERACT` registration.
- **Tick loop** (in the same place the existing pickup pass runs) — for each `pk` in `r.Pickups`, if `!pk.Consumed && !pk.ExpiresAt.IsZero() && now.After(pk.ExpiresAt)`, set `pk.Consumed = true`. The existing snapshot diff drops consumed pickups.
- **`PlayerSnapshot`** — append `coins`, `speed_stacks`, `fire_rate_stacks`, `damage_stacks` so the client can render the HUD coin counter and per-trait stack counts in the shop UI.

### Trait reads (find every call site)

Effective values are derived at the point of use — **don't cache on `PlayerState`**. The reads:

| Read point                                                  | Implementation                                                     |
| ----------------------------------------------------------- | ------------------------------------------------------------------ |
| Movement integration (`advancePlayer`)                      | `EffectiveSpeedMult(p)` → `(1 + 0.1*float32(p.SpeedStacks))`       |
| `HandleShoot` damage (`combat.go`)                          | `EffectiveDamageMult(p)` → `(1 + 0.1*float32(p.DamageStacks))`     |
| `HandleShoot` cooldown                                      | `cooldown / (1 + 0.1*float32(p.FireRateStacks))` (Duration math)   |

### Client

- **`client/src/game/net/packet.ts`** — extend the `C2S_INTERACT` payload type with `claimed_type: number`. Add `C2S_SHOP_PURCHASE` to the `C2SType` union and a `sendShopPurchase(itemId: number)` helper. Update the `sendInteract` call to set `claimed_type` to the rendered pickup's type (the type stored client-side from the room load).
- **`client/src/game/scenes/ShopMenu.ts`** — `create()` builds a 4-row list from a hardcoded catalog mirror, panel styling from [client/10-ui-components.md](../client/10-ui-components.md). `update()` reads `getSync().state.player.coins`; render rows where `cost > coins` greyed out. Input: ↑/↓ navigate, Enter → `sendShopPurchase(catalog[selected].id)`, Esc → close. Launch with `this.scene.launch('ShopMenu'); this.scene.pause('Game')`.
- **`client/src/game/scenes/GameHUD.ts`** — add a coin-counter `Text` (top-right, gold). Bind "B" to launch `ShopMenu` and pause the `Game` scene. Render an active-stacks readout in the bottom-right just above the weapon widget — a single text line in the form `SPD×n  ROF×n  DMG×n` listing only non-zero stacks (hidden entirely when all zero). Reads `speed_stacks` / `fire_rate_stacks` / `damage_stacks` from `PlayerSnapshot`.
- **`client/src/game/scenes/Preloader.ts`** — register a `coin` sprite asset.
- **`client/src/game/main.ts`** — register the `ShopMenu` scene.
- **Coin pickup rendering** — coins are pickups, so they go through the existing pickup render path in [client/05-entities-rendering.md](../client/05-entities-rendering.md). Add a `coin` case wherever the renderer switches on pickup type.

### Replay

`InputLog` entries for `C2S_INTERACT` must include `claimed_type` alongside `target_id`. Old logs without the field decode `claimed_type = 0`, which falls through `applyPickup`'s switch with no effect — those logs still verify but don't reproduce BUG-12-exploited runs. See [foundations/08-leaderboard-replay.md](../foundations/08-leaderboard-replay.md).

## Implementation status

Implemented end-to-end:

- Server: `ShopOffer` / `ShopCatalog` / `HandleShopPurchase` in `server/internal/game/shop.go`; coin pickup type and `applyPickup` claimed-type dispatch in `server/internal/game/pickups.go`; coin drop on death in `server/internal/game/combat.go` (`ResolveCollisions`); coin despawn pass in `server/internal/game/loop.go` (`stepTick`). `PickupState.ExpiresAt`, `PlayerState.Coins`, and the three `*Stacks` fields are declared in `server/internal/game/state.go`. Effective-multiplier helpers are in `combat.go` (`EffectiveDamageMult`, `EffectiveSpeedMult`, plus the inline fire-rate divisor in `HandleShoot`).
- Protocol: `InteractPacket.ClaimedType`, `ShopPurchasePacket`, and `C2SShopPurchase = 0x09` in `server/internal/protocol/packets.go` and `envelope.go`. `PlayerSnapshot` carries `coins / speed_stacks / fire_rate_stacks / damage_stacks`.
- Client: `client/src/game/scenes/ShopMenu.ts` (modal), HUD coin counter, active-stacks readout, and "B" keybind in `GameHUD.ts`, scene registered in `main.ts`. Coin sprite in `Preloader.ts`.

Cross-doc appends made when this feature landed (record kept here so the spec history stays readable):

- [foundations/02-protocol.md](../foundations/02-protocol.md): `C2S_INTERACT` payload extended with `claimed_type`; `C2S_SHOP_PURCHASE` (`0x09`) row added; `PlayerSnapshot` sub-structure extended.
- [foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md): `Coins`, `SpeedStacks`, `FireRateStacks`, `DamageStacks` listed on `PlayerState`; reset-on-run-end note added.
- [features/pickups.md](pickups.md): Coin (Type 3) row, `ExpiresAt` field, BUG-12 cross-link, auto-trigger sentence corrected.
- [bugs.md](../bugs.md): BUG-12 row added; synergy diagram extended with `BUG-05 → BUG-12`.
- [client/04-scenes.md](../client/04-scenes.md): `ShopMenu` row added.
- [foundations/08-leaderboard-replay.md](../foundations/08-leaderboard-replay.md): replay engine records `claimed_type` per interact.

## Intentional bugs in this area

### BUG-12 Pickup Type Confusion

**Design choice enabling the bug:** `C2S_INTERACT` carries a `claimed_type uint8` field. The server validates that the pickup exists and is in range, then passes the client-declared type into `applyPickup`'s switch — the server-stored `pk.Type` is never read for dispatch.

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

**Bound:** Each pickup is consumed once via `pk.Consumed`. Economic ceiling: `(number of pickups in the run) × 1 coin`. Pickup count is room-bounded — the player cannot mint coins from thin air. At 5 coins per shop item, the practical upside is ~2–3 extra trait stacks per run beyond honest play.

**Detection:** Replay reproduces identically — `(seed, InputLog)` must include the recorded `claimed_type` per interact. Do **not** "fix" `applyPickup` to switch on `pk.Type`, and do **not** drop the `claimed_type` field from the packet.

**Synergies:**
- [BUG-05](pickups.md#bug-05-interact-range-bug) lets the player interact with pickups from across the room. Combined with BUG-12, every visible pickup becomes a coin without needing to walk to it.
- The `damage` and `fire_rate` traits compound with [BUG-04](combat-weapons.md#bug-04-swap-cancel) and [BUG-07](combat-weapons.md#bug-07-damage-batch-overflow). BUG-12 funds those traits without enemy-grinding.

**Savings:** ~10–20 s per run. With ~10–15 pickups in a typical run, an exploiter mints ~10–15 extra coins beyond the ~1-per-kill honest rate, buying ~2–3 extra permanent stacks. Each `damage` stack adds 10% damage for the rest of the run (including the boss).

## Packets used

- `C2S_INTERACT` — payload extended with `ClaimedType uint8`. **This breaks the existing payload shape**; coordinate with [foundations/02-protocol.md](../foundations/02-protocol.md) and the client's `packet.ts`.
- `C2S_SHOP_PURCHASE` (new, suggested type code `0x09`) — `{ item_id: uint8 }`.
- `S2C_STATE` — already broadcasts `PlayerSnapshot`; the snapshot is extended with `Coins`, `SpeedStacks`, `FireRateStacks`, `DamageStacks`.

A `C2S_SHOP_OPEN` / `S2C_SHOP_STATE` pair is **not** introduced — the shop UI is fully client-side until purchase. The README's "How to add a feature" worked example shows a heavier shape (with open packet and between-rooms gating); this spec deviates intentionally because the shop opens via global keybind anywhere and the catalog is static.

## Cross-references

- [features/pickups.md](pickups.md) — `applyPickup` lives there; BUG-05 stacks with BUG-12; Coin (Type 3) added to its pickup-types table.
- [features/combat-weapons.md](combat-weapons.md) — damage and fire-rate stack multipliers consumed by the shoot pipeline.
- [features/mob-ai.md](mob-ai.md) — enemy death is the coin drop trigger.
- [foundations/02-protocol.md](../foundations/02-protocol.md) — `InteractPacket.ClaimedType` field and `C2S_SHOP_PURCHASE` row.
- [foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md) — `Coins` and the four `*Stacks` fields on `PlayerState`.
- [foundations/08-leaderboard-replay.md](../foundations/08-leaderboard-replay.md) — replay must record `claimed_type`.
- [client/04-scenes.md](../client/04-scenes.md) — `ShopMenu` scene.
- [client/07-input-controls.md](../client/07-input-controls.md) — "B" → open ShopMenu binding.
- [client/10-ui-components.md](../client/10-ui-components.md) — panel styling tokens reused by `ShopMenu`.
- [bugs.md](../bugs.md) — BUG-12 index row.
