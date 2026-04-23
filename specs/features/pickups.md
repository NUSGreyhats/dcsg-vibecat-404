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
}
```

### Pickup types

| Type | Name              | Effect                                                       |
| ---- | ----------------- | ------------------------------------------------------------ |
| 1    | Health potion     | `ApplyHeal(+20)` — see [combat-weapons.md BUG-08](combat-weapons.md#bug-08-hp-integer-overflow-healing-past-max) for wraparound hazard. |
| 2    | Ammo crate        | `+{N}` ammo for current weapon.                              |
| 3    | Speed boost       | Timed movement-speed multiplier.                             |
| 4    | Damage boost      | Timed damage multiplier (KIV — see open questions).          |
| 5    | Heart (large)     | `ApplyHeal(+30000)` — boss-room pickup; wraps past MaxHP and kills a high-HP player. The [BUG-08 "avoid if HP > 2767"](combat-weapons.md#bug-08-hp-integer-overflow-healing-past-max) hazard. |

Exact heal amounts and durations TBD — see open questions in [../../SPECIFICATIONS.md](../../SPECIFICATIONS.md).

## Interact handler

```go
const interactRange = 2500   // BUG-05: should be 50

func handleInteract(p *PlayerState, r *RoomState, pkt InteractPacket) {
    pickup, ok := r.Pickups[pkt.TargetID]
    if !ok || pickup.Consumed { return }
    dx := pickup.X - p.X
    dy := pickup.Y - p.Y
    if math.Abs(dx) < interactRange && math.Abs(dy) < interactRange {
        applyPickup(p, pickup)
        pickup.Consumed = true
    }
}
```

The check is **square-axis** (`abs(dx) < r && abs(dy) < r`) rather than radial, and the constant is `2500` (it should be `50`). Both together enable [BUG-05](#bug-05-interact-range-bug).

## Data flow

- Room load: `RoomData.pickups []PickupSpawn` arrives in `S2C_RUN_STARTED` / `S2C_ROOM_LOAD` → server instantiates `PickupState` entries in `RoomState.Pickups`.
- `C2S_INTERACT { target_id }` → server looks up pickup → applies if in (buggy) range.
- On apply: pickup is marked `Consumed = true`; server broadcasts an updated `S2C_STATE` with the modified `PlayerSnapshot` (and the pickup absent from `entities`).

Per-tick collision scan also exists for pickups that auto-trigger on touch (e.g., `Coin`); that auto-trigger path is separate from the `C2S_INTERACT` buggy range. Only the `C2S_INTERACT` path carries BUG-05.

## Client interactions

- `client/src/game/entities/Coin.ts` renders coin-style pickups.
- Pickup UI: sprite + interact prompt when the player is nearby.
- Input: press "E" (or configured key) → `C2S_INTERACT { target_id: <nearest> }`.

## Implementation notes

- Package: `internal/game/pickups` (target).
- Do not change `interactRange` to a square root check or to `50` — both changes fix BUG-05.
- Respawn behavior (e.g., ammo crates respawning after 30 s) is per-type; default is non-respawning.

## Implementation status

Greenfield. Current server has a `Coins` counter on `gameState` but no pickup authoritative model; pickups exist only client-side in the current prototype.

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
- [combat-weapons.md](combat-weapons.md) — `ApplyHeal` wraparound hazard for Type 5 pickup (BUG-08).
- [rooms-doors.md](rooms-doors.md) — rooms host pickups.
