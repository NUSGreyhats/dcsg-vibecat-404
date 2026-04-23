# DEFCON Dungeon Speedrun — Specifications

A single-player dungeon-crawler speedrun with a shared leaderboard. Players clear rooms of enemies, traverse doors to new single-screen maps, and post completion times. The **real competition** is exploiting protocol-level bugs to shave seconds off a run. Each bug is a small, bounded advantage — stacking them is the path to #1. Every intentional bug is **a load-bearing design requirement, not a mistake**. If you find one, don't fix it without coordinating first.

## Layout

```
specs/
├── README.md              ← you are here
├── bugs.md                ← central index of all 11 intentional bugs
├── foundations/           ← cross-cutting invariants (rarely change)
│   ├── 01-architecture.md            — service topology + deployment
│   ├── 02-protocol.md                — WS envelope + packet registry + BUG-09 parser half
│   ├── 03-identity-auth.md           — player_id, bcrypt, AES, token allowlist
│   ├── 04-queue-service.md           — queue admin service
│   ├── 05-server-runtime.md          — goroutines, tick loop, BUG-02 / BUG-06 / BUG-11
│   ├── 06-game-state-conventions.md  — state blocks + ownership rules
│   ├── 07-determinism-seeding.md     — MobRNG + consumption invariant
│   ├── 08-leaderboard-replay.md      — CompletedRun, checksum, replay engine
│   ├── 09-ci-cd-deploy.md            — pipelines, source stripping, build tags
│   └── 10-client-architecture.md     — Phaser scenes, net sync
└── features/              ← self-contained gameplay features
    ├── combat-weapons.md             — BUG-04, BUG-07, BUG-08
    ├── mob-ai.md                     — BUG-01
    ├── rooms-doors.md                — BUG-03
    ├── pickups.md                    — BUG-05
    └── inventory-items.md            — BUG-10, BUG-09 dispatch half
```

## Reading order for a new implementer

1. [foundations/01-architecture.md](foundations/01-architecture.md) — what the two services are.
2. [foundations/02-protocol.md](foundations/02-protocol.md) — how they talk.
3. [foundations/03-identity-auth.md](foundations/03-identity-auth.md) — how players authenticate.
4. [foundations/04-queue-service.md](foundations/04-queue-service.md) — prod-only queue machinery.
5. [foundations/05-server-runtime.md](foundations/05-server-runtime.md) — goroutine model + tick loop.
6. [foundations/06-game-state-conventions.md](foundations/06-game-state-conventions.md) — the data the runtime owns.
7. [foundations/07-determinism-seeding.md](foundations/07-determinism-seeding.md) — RNG + replay invariants.
8. [foundations/08-leaderboard-replay.md](foundations/08-leaderboard-replay.md) — how runs are persisted + verified.
9. [foundations/10-client-architecture.md](foundations/10-client-architecture.md) — the Phaser client.
10. `features/*.md` — any order; each file is self-contained. See [combat-weapons.md](features/combat-weapons.md), [mob-ai.md](features/mob-ai.md), [rooms-doors.md](features/rooms-doors.md), [pickups.md](features/pickups.md), [inventory-items.md](features/inventory-items.md).
11. [bugs.md](bugs.md) — the index that ties it all together.
12. [foundations/09-ci-cd-deploy.md](foundations/09-ci-cd-deploy.md) — read last; understand the build-tag strategy that ships BUG-11.

## How to add a new feature

The layout is designed so adding a feature is mostly additive — one new file + small appends. Follow this recipe.

### 1. Create `specs/features/<kebab-name>.md`

Use the standard skeleton (every foundation/feature file uses this):

```
# Feature — <Name>

## Purpose

## Data model

## Data flow (IMPORTANT)

## Client interactions

## Implementation notes

## Implementation status

## Intentional bugs in this area
(or "None." if the feature is anticheat-adjacent)

## Packets used

## Cross-references
```

Keep feature state local to the feature file where possible. If you must add to existing state blocks, follow step 3.

### 2. If the feature introduces a new packet type

Append one row to the C2S or S2C table in [foundations/02-protocol.md](foundations/02-protocol.md). Use the next unused type code (C2S appends below `0x08`, S2C below `0x89`). Define the `data` shape in that row, then mirror in `client/src/net/packet.ts`.

### 3. If the feature owns persistent state

- Prefer feature-local state (a new struct referenced by a thin pointer on `PlayerState` / `RunState` / `RoomState`).
- If you must add a field to an existing block, append it to the matching block in [foundations/06-game-state-conventions.md](foundations/06-game-state-conventions.md) and cite the feature:
  ```go
  PlayerState {
      ...existing fields...
      Coins  int32    // owned by features/shop.md
  }
  ```
- If the field has load-bearing type semantics (integer width that wraps, slice semantics, etc.), call it out in the state-conventions spec AND in the feature spec's "Intentional bugs in this area" section.

### 4. If the feature adds an intentional bug

- Append one row to the index table in [bugs.md](bugs.md) with a link to the body.
- Write the body in the feature spec's "Intentional bugs in this area" section. Use the standard sub-headers: **Source**, **Design choice enabling the bug**, **Root cause sketch**, **Exploit path**, **Bound**, **Detection**, **Synergies**, **Savings**.
- Update the stacking / synergy diagrams in [bugs.md](bugs.md) if the bug interacts with existing ones.

### 5. If the feature adds a Phaser scene

Append one row to the scene table in [foundations/10-client-architecture.md](foundations/10-client-architecture.md) and register the scene in `client/src/game/main.ts`.

### 6. Cross-link from adjacent features

If the new feature's data flow touches another feature (e.g. a shop reads inventory or spawns pickups), add cross-references in both directions.

## Worked example — adding an in-game shop

Suppose you're adding a shop that lets players spend coins on weapons, items, or consumables between rooms.

**Step 1 — `specs/features/shop.md`:**

```markdown
# Feature — Shop

## Purpose
Between-room shop where the player trades Coins for items, weapons, or consumables.

## Data model
Lives on PlayerState (via features/shop.md registration):
  Coins    int32
  ShopInventory ShopInventory  // feature-local struct

## Data flow
C2S_SHOP_OPEN → server validates (between-rooms only) → S2C_SHOP_STATE
C2S_SHOP_PURCHASE { item_id } → server debits Coins, adds to inventory → S2C_STATE

## Intentional bugs in this area
(decide — e.g. BUG-12 price underflow if going to add one)

## Packets used
C2S_SHOP_OPEN (0x09), C2S_SHOP_PURCHASE (0x0a), S2C_SHOP_STATE (0x8a)

## Cross-references
- features/inventory-items.md — purchased items enter Inventory
- features/combat-weapons.md — purchased weapons unlock UnlockedWeapons
```

**Step 2 — append to `foundations/02-protocol.md`:**

Append to the C2S table:

| 0x09 | `C2S_SHOP_OPEN`     | `{}`                             |
| 0x0a | `C2S_SHOP_PURCHASE` | `{ item_id: uint32 }`            |

Append to the S2C table:

| 0x8a | `S2C_SHOP_STATE`    | `{ coins, items: []ShopOffer }`  |

Mirror in `client/src/net/packet.ts` (append to `C2SType` / `S2CType` const + the discriminated-union `C2SDataFor` / `S2CDataFor`).

**Step 3 — append to `foundations/06-game-state-conventions.md`:**

In the `PlayerState` block:

```go
PlayerState {
    ...existing fields...
    Coins          int32           // owned by features/shop.md
    ShopInventory  *ShopInventory  // owned by features/shop.md
}
```

**Step 4 — new bug (if any):**

If the shop introduces a new intentional bug (e.g. price arithmetic underflow), add it to [bugs.md](bugs.md) index and write the body in `features/shop.md`.

**Step 5 — scene:**

Append to scene table in [foundations/10-client-architecture.md](foundations/10-client-architecture.md):

| `ShopMenu` | `client/src/game/scenes/ShopMenu.ts` | Shop overlay between rooms |

(The file already exists in the repo as a scaffold.)

**Step 6 — cross-links:**

Add cross-refs in [features/inventory-items.md](features/inventory-items.md) and [features/combat-weapons.md](features/combat-weapons.md) pointing at the new `features/shop.md`.

**Total changes:** 1 new file + ~4 small appends. No restructure.

## Current implementation status

This spec describes the **target** design. The codebase has meaningful deviations that are tracked in each spec's "Implementation status" section — most notably the current server uses a single-blob JSON sync rather than the typed-envelope protocol. Reconciliation is future work that should happen one feature at a time, driven by these specs.
