# Feature — Pickups

## Purpose

World-placed items the player can interact with to gain health, ammo, speed boosts, or other temporary buffs. Hosts [BUG-05 — Interact Range Bug](#bug-05-interact-range-bug), a Surface-tier off-by-units in the interact hitbox.

## Data model

### Pickup spawn (delivered via protocol)

`PickupSpawn` from `RoomData`:

```
PickupSpawn {
    entity_id   uint32
    type        uint8
    x, y        float32
}
```

### Pickup state (server-side)

Lives in `RoomState.Pickups map[uint32]*PickupState`:

```go
PickupState {
    EntityID     uint32
    Type         uint8
    X, Y         float32
    Consumed     bool
    RespawnAt    time.Time   // zero for non-respawning
    ExpiresAt    time.Time   // zero for never expires; non-zero = despawn deadline (see shop.md)
}
```

`ExpiresAt` is set by runtime-spawned pickups (currently coin drops — see [shop.md](shop.md)) to `now.Add(30s)` at spawn. The per-tick pickup pass marks any unconsumed pickup with `now.After(ExpiresAt)` as `Consumed = true`. Room-templated pickups leave `ExpiresAt` as the zero value and never auto-despawn.

### Pickup types

Current shipping set:

| Type | Name              | Effect                                                       |
| ---- | ----------------- | ------------------------------------------------------------ |
| 1    | Health potion     | `ApplyHeal(+20)`.                                            |
| 2    | Ammo crate        | `+20` ammo for current weapon (clamped to `AmmoMax`).        |
| 3    | Coin              | `+1` to `PlayerState.Coins`. See [shop.md](shop.md).         |

Type 3 is **recycled** from the formerly-removed Speed boost — see [shop.md](shop.md) for the rationale. Removed (formerly types 4–5): Damage boost and Heart (large, `+30000 HP`). See [bugs.md — currently disabled](../bugs.md#currently-disabled): the Heart pickup was the payload for [BUG-08](combat-weapons.md#bug-08-hp-integer-overflow-healing-past-max), and the boost pickups were judged low-value.

## Interact handler

```go
const interactRange = 2500   // BUG-05: should be 50

func handleInteract(p *PlayerState, r *RoomState, pkt InteractPacket) {
    pickup, ok := r.Pickups[pkt.TargetID]
    if !ok || pickup.Consumed { return }
    dx := pickup.X - p.X
    dy := pickup.Y - p.Y
    if math.Abs(dx) < interactRange && math.Abs(dy) < interactRange {
        applyPickup(p, pickup, pkt.ClaimedType, now)   // dispatch on ClaimedType, NOT pk.Type — BUG-12
        pickup.Consumed = true
    }
}
```

The check is **square-axis** (`abs(dx) < r && abs(dy) < r`) rather than radial, and the constant is `2500` (it should be `50`). Both together enable [BUG-05](#bug-05-interact-range-bug).

## Data flow

- Room load: `RoomData.pickups []PickupSpawn` arrives in `S2C_RUN_STARTED` / `S2C_ROOM_LOAD` → server instantiates `PickupState` entries in `RoomState.Pickups`.
- Runtime spawns (currently only coin drops on enemy death) append directly to `RoomState.Pickups` and surface in the next `S2C_STATE.pickups`.
- `C2S_INTERACT { target_id, claimed_type }` → server looks up pickup → checks (buggy) range → calls `applyPickup` dispatching on **`claimed_type`, not `pk.Type`** (this is [BUG-12](shop.md#bug-12-pickup-type-confusion)).
- On apply: pickup is marked `Consumed = true`; server broadcasts an updated `S2C_STATE` with the modified `PlayerSnapshot` (and the pickup absent from `pickups`).
- Despawn: the per-tick pickup pass marks any unconsumed pickup with `now.After(ExpiresAt)` as consumed and emits `S2C_ENTITY_DIED` so the client tears down the sprite.

All pickup types (including Coin) flow through the manual `C2S_INTERACT` path, so [BUG-05](#bug-05-interact-range-bug) and [BUG-12](shop.md#bug-12-pickup-type-confusion) both apply uniformly. There is **no** auto-on-touch pickup pipeline.

## Client interactions

- `client/src/game/entities/Coin.ts` renders coin-style pickups.
- Pickup UI: sprite + interact prompt when the player is nearby.
- Input: press "E" (or configured key) → `C2S_INTERACT { target_id: <nearest> }`.

## Implementation notes

- Package: `internal/game/pickups` (target).
- Do not change `interactRange` to a square root check or to `50` — both changes fix BUG-05.
- Respawn behavior (e.g., ammo crates respawning after 30 s) is per-type; default is non-respawning.

## Implementation status

Implemented. `RoomState.Pickups` and `applyPickup` (`server/internal/game/pickups.go`) own pickup state; runtime coin drops live in the same registry (see [shop.md](shop.md)). The despawn pass in `server/internal/game/loop.go` (`stepTick`) handles `ExpiresAt` expiry.

## Intentional bugs in this area

### BUG-05 Interact Range Bug

**Design choice enabling the bug:** The interact handler uses axis-separated absolute-distance checks (square hitbox) with `range = 2500` instead of `50`.

**Root cause sketch:**

```go
if math.Abs(dx) < 2500 && math.Abs(dy) < 2500 {
    applyPickup(p, pickup)
}
```

Two design flaws stacked:
1. Square hitbox (`abs(dx) && abs(dy)`) rather than radial (`dx*dx + dy*dy < r*r`).
2. Off-by-50× constant (2500 vs 50).

**Exploit path:** Press interact while standing anywhere in the room (rooms are 1280×768, so `2500 > max dimension`). Pickups can be grabbed from across the room without walking to them.

**Bound:** Limited per pickup — each is consumed once. Only applies to manually-interacted pickups, not auto-touch pickups.

**Detection:** Replay reproduces identically. Do NOT tighten the range constant or switch to radial.

**Synergies:** Multiplied by [BUG-02 drift](../foundations/05-server-runtime.md#bug-02-drift-tick-stretching). Stacks with [BUG-03 door skip](rooms-doors.md#bug-03-door-skip-no-proximity) when rooms require both pickup + door work.

**Savings:** ~1–2 s per pickup-heavy room.

## Packets used

- `C2S_INTERACT`.
- `S2C_STATE` (reflects consumed pickup).

## Cross-references

- [../foundations/02-protocol.md](../foundations/02-protocol.md) — `C2S_INTERACT`, `PickupSpawn`.
- [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md) — `RoomState.Pickups`.
- [combat-weapons.md](combat-weapons.md) — `ApplyHeal` semantics. The Type 5 Heart pickup that triggered BUG-08 has been removed.
- [rooms-doors.md](rooms-doors.md) — rooms host pickups.
