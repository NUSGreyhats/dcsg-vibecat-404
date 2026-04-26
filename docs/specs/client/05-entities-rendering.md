# 05 — Entities & Rendering

## Purpose

Defines how server snapshots become on-screen Phaser `GameObject`s. Covers the five entity classes (`Player`, `Enemy`, `Bullet`, `Pickup`, `Door`), the snapshot-to-target-position pattern, the lerp-based smoothing loop, the fallback texture strategy that keeps the game playable when assets fail, depth layering, and the death/consumption animations. Rendering is **cosmetic only** — no collision, no damage, no AI ticks. All authority lives on the server; these classes only paint.

## Entity inventory

| Class     | File                                     | Base                                      | Tracks                     | Driven by              |
| --------- | ---------------------------------------- | ----------------------------------------- | -------------------------- | ---------------------- |
| `Player`  | `client/src/game/entities/Player.ts`     | `Phaser.GameObjects.Container`            | One per scene (self)       | `PlayerSnapshot` via `Game.onServerState` |
| `Enemy`   | `client/src/game/entities/Enemy.ts`      | `Phaser.GameObjects.Container`            | `Map<entity_id, Enemy>`    | `EntitySnapshot[]`      |
| `Bullet`  | `client/src/game/entities/Bullet.ts`     | `Phaser.GameObjects.Rectangle`            | `Map<proj_id, Bullet>`     | `ProjSnapshot[]`        |
| `Pickup`  | `client/src/game/entities/Pickup.ts`     | `Phaser.GameObjects.Container`            | `Map<entity_id, Pickup>`   | `PickupSpawn` (static until consumed) |
| `Door`    | `client/src/game/entities/Door.ts`       | `Phaser.GameObjects.Container`            | `Map<door_id, Door>`       | `DoorDef` + `S2C_DOOR_UNLOCKED` |

All five follow the same contract: **construct from initial data, mutate target state on snapshot, lerp in `tick()`, destroy on server death event.** Scene code owns the maps; the entity owns its Phaser children.

## Common rendering pattern

Every moving entity (`Player`, `Enemy`, `Bullet`) has four relevant fields:

```ts
x, y: number;                 // current rendered position (inherited from Phaser.GameObjects.Container)
targetX, targetY: number;     // last server-reported position; updated in applySnapshot()
```

And two methods:

```ts
applySnapshot(snap): void { this.targetX = snap.x; this.targetY = snap.y; /* ... */ }
tick(_dt: number): void {
    this.x = Phaser.Math.Linear(this.x, this.targetX, LERP_FACTOR);
    this.y = Phaser.Math.Linear(this.y, this.targetY, LERP_FACTOR);
}
```

Per-class `LERP_FACTOR` is tuned so the final position reaches the target before the next snapshot (20 Hz = 50 ms) without looking laggy:

| Class    | `LERP_FACTOR` | Rationale                                                                           |
| -------- | ------------- | ----------------------------------------------------------------------------------- |
| `Player` | 0.3           | Fastest-responding entity — the locally-controlled avatar needs snappy feedback.    |
| `Enemy`  | 0.3           | Matches `Player` so chase / flee reads naturally.                                   |
| `Bullet` | 0.4           | Slightly snappier; projectiles are small and fast, blur looks worse than pop.       |

`Pickup` and `Door` do **not** move, so they don't lerp. `Pickup` has a gentle bob tween for juice (not physics); `Door` is stationary.

## Snapshot reconciliation (scene-side)

The scene owns the maps. Per-tick reconciliation happens in `Game.onServerState(payload)`:

```
for each snap in payload.entities:
    if snap.state === 3 (AIDead): continue        ← defensive; server already filters
    if !enemies.has(snap.entity_id):
        enemy = new Enemy(scene, snap); scene.add.existing(enemy); enemies.set(id, enemy);
    enemies.get(snap.entity_id).applySnapshot(snap)

for each p in payload.projectiles:
    (same pattern with Bullet + projectiles Map)

// Despawn phase — anything in the projectiles Map that's not in the new frame is gone.
for each (id, bullet) in projectiles:
    if !seenProjIds.has(id): bullet.destroy(); projectiles.delete(id)

// Death / consumption sweep — the deadEntities Set accumulates ENTITY_DIED ids across ticks.
for id in state.deadEntities:
    enemies.get(id)?.markDead(); enemies.delete(id)
    pickups.get(id)?.setConsumed(); pickups.delete(id)
```

### Why entities are created lazily from `S2C_STATE` snapshots

`EnemySpawn` in `S2C_RUN_STARTED` / `S2C_ROOM_LOAD` delivers initial enemy metadata, but we **also** create enemies on first-sight in `S2C_STATE` — defensive coding against either:

- The server adds a new enemy mid-room (a future "waves" feature).
- A frame drop loses the `S2C_ROOM_LOAD` but the next `S2C_STATE` still references the new enemies.

So `buildRoom()` (see [06-world-rooms.md](./06-world-rooms.md)) seeds the initial maps from `RoomData.enemies`, but `onServerState()` treats the `EntitySnapshot` list as canonical and fills in gaps. If a future feature introduces transient enemies, this path already handles them.

### Why projectiles despawn from snapshot diff but enemies don't

- Projectiles live only while in-flight; they're routinely removed from the server's `Projectiles` map when they hit something or leave the room. A snapshot-diff despawn is the natural "disappear when the server stops reporting them" signal.
- Enemies can be legitimately absent from one `S2C_STATE` (packet loss) without being dead, so they're only removed when an explicit `S2C_ENTITY_DIED` arrives (captured in `deadEntities`). This keeps spurious flickers off-screen during packet loss.

## Depth layering

Phaser uses an integer depth to sort same-scene GameObjects. The layering below is deliberate — getting it wrong causes the tilemap to hide the player (a bug that actually bit us; see the comment in `Player.ts`).

| Depth   | Layer                                                                       |
| ------- | --------------------------------------------------------------------------- |
| `-100`  | Fallback room background rectangle (`paintRoomBackground`).                 |
| `-50`   | Fallback perimeter walls.                                                   |
| `-40`   | Room-id badge (debug overlay).                                              |
| `-30`   | Tilemap layers (when the Tiled tilemap loads successfully).                 |
| `0`     | Default — enemies, pickups, doors, bullets (they don't set explicit depth). |
| `10`    | `Player` — **above tilemaps** so `room-X.json` tiles don't hide the avatar. |
| `1000+` | HUD overlays (`GameHUD`, `InventoryMenu`, pause dim).                       |

`Player.ts`:

```ts
// Keep the player above tilemaps (depth 0) and entities (depth ≈ 0–5).
// Without this, a room tilemap loaded on C2S_ROOM_LOAD renders on top
// of the player and visually hides them.
this.setDepth(10);
```

## Fallback texture strategy

The client must still be playable if individual art assets fail to load. Two mechanisms:

### 1. Preloader is non-fatal

`Preloader.preload()` hooks `load.on("loaderror", ...)` to log and continue. If `player.png` 404s, the load still completes — individual entities fall back to colored rectangles.

### 2. Per-entity fallbacks

Each class checks `scene.textures.exists(...)` at construction:

| Class     | If asset missing …                                                                                     |
| --------- | ------------------------------------------------------------------------------------------------------ |
| `Player`  | Generates `__player_fallback_tex` once (a solid teal 20×20 rect drawn into a `Graphics` → `generateTexture`), then uses it so the rest of the class still treats `this.sprite` as a real `Sprite`. |
| `Enemy`   | Falls back to `scene.add.rectangle(0, 0, size, size, tint)` — no texture needed. `sprite` is typed as `Sprite \| Rectangle` and `markDead` branches on `instanceof`. |
| `Bullet`  | Always uses a `Rectangle` (no texture path in the first place).                                        |
| `Pickup`  | Falls back to a small colored rect with a glyph label above it; different icon keys for `heart` vs `coin`. |
| `Door`    | Always uses a `Rectangle` + a text icon (`▲` / `⚿`).                                                   |

Key detail: **`Player` uses `generateTexture` once** so the fallback is a real texture, which means animations, tints, and display-size scaling still behave. Other classes don't need this because they don't depend on texture operations.

Combined, these two layers mean the game can run with zero art assets: rectangles everywhere, labels for doors/pickups, no tilemaps (flat tinted background, rectangular walls — see [06-world-rooms.md](./06-world-rooms.md)). This is the dev-mode default when someone clones the repo without the art assets.

## `Player` — the self avatar

```ts
class Player extends Phaser.GameObjects.Container {
    readonly sprite: Phaser.GameObjects.Sprite;
    private readonly hpBar: Phaser.GameObjects.Graphics;
    private lastHp = PLAYER_MAX_HEALTH;
    private targetX: number;
    private targetY: number;

    applySnapshot(snap: { x, y, hp }): void { /* set targets; redraw HP bar if changed */ }
    tick(_dt): void { /* lerp */ }
}
```

- Single instance per `Game` scene, held at `Game.player`.
- HP bar is a 32×4 graphics drawn into the container; color shifts green→yellow→red at 50 %/25 % of `PLAYER_MAX_HEALTH = 100`.
- `applySnapshot` is called from `Game.onServerState` with `payload.player`. There is no separate per-frame HP interpolation — HP jumps immediately.
- `PLAYER_MAX_HEALTH` is a **client constant** (`constants.ts`) and must stay in sync with the server's authoritative cap. The HUD also reads from this constant rather than from `snap`, so if the server ever ships HP above this (e.g. via [BUG-08 overflow-heal](../features/combat-weapons.md#bug-08-hp-integer-overflow-healing-past-max)) the bar pins at 100% while the numeric field `snap.hp` shows the true value.

## `Enemy` — server-driven actors

```ts
class Enemy extends Phaser.GameObjects.Container {
    readonly entityId: number;
    readonly enemyType: number;
    sprite: Phaser.GameObjects.Sprite | Phaser.GameObjects.Rectangle;
    // ...
}
```

### Type → visual mapping

Matches `features/mob-ai.md`:

| Type | Name       | Color      | Size |
| ---- | ---------- | ---------- | ---- |
| 1    | GRUNT      | `#6b8e23`  | 24   |
| 2    | RUNNER     | `#ff8c00`  | 24   |
| 3    | TANK       | `#4169e1`  | 36   |
| 4    | KAMIKAZE   | `#dc143c`  | 14   |
| 5    | BOSS       | `#8b0000`  | 36   |
| else | (unknown)  | `#808080`  | 24   |

If the `"enemy"` texture atlas is loaded, `snap.type` is used as the frame index and the sprite is `setTint(color)`. Otherwise the entity is a colored rectangle.

### `markDead()`

Starts a brief tween: `scaleX/Y: 1.4, alpha: 0, 180 ms ease Cubic.easeOut`. Works uniformly for `Sprite` (via `setTintFill(0xffffff)`) and `Rectangle` (via `setFillStyle(0xffffff)`). Guarded by `this.dying` to avoid double-triggering if `S2C_ENTITY_DIED` and the server's next `S2C_STATE` (which omits the entity) arrive in the same frame.

### HP bar

3-pixel-tall bar above the sprite, hidden at full HP or after death. Color shifts green→yellow→red at 50%/25% of the per-entity `MaxHP` captured at construction time. A cheap way to telegraph "how close is this thing to dying" without needing a HUD refactor.

## `Bullet` — projectile sprites

Minimal: a 6×3 yellow rectangle, rotated to `snap.angle`. Two scalar targets for smoothing. Despawned by `Game.onServerState()` diff logic when the server stops reporting the projectile.

`ownerId: snap.owner` is captured but not currently used for visual differentiation — future work could color enemy projectiles differently.

## `Pickup` — floor items

### Type → visual mapping

| Type | Name    | Color        | Size | Glyph | Uses texture if loaded |
| ---- | ------- | ------------ | ---- | ----- | ---------------------- |
| 1    | HEALTH  | `#dc2d2d`    | 16   | ♥     | `heart`                |
| 2    | AMMO    | `COIN_GOLD`  | 16   | ■     | `coin`                 |
| 3    | COIN    | `COIN_GOLD`  | 12   | $     | `coin`                 |
| else | unknown | `#cccccc`    | 12   | ?     | —                      |

Type 3 was previously SPEED; recycled to COIN by [features/shop.md](../features/shop.md). Types 4 (DAMAGE) and 5 (LARGE_HEART) were removed when their underlying pickups were retired — see [features/pickups.md](../features/pickups.md).

### Bob animation

At construction:

```ts
scene.tweens.add({ targets: this, y: spawn.y - 3, duration: 900, yoyo: true, repeat: -1, ease: "Sine.easeInOut" });
```

Purely cosmetic — tells the player "this is interactable." The server doesn't know or care about the bob.

### `setConsumed()`

Fades up + alpha 0 over 220 ms, then `destroy()`. `scene.tweens.killTweensOf(this)` first, otherwise the bob fights the fade. Guarded by `this.consumed`.

Note: pickups are consumed either via `C2S_INTERACT` (manual press) or a server-side auto-touch check, but the **visual** disappearance is always driven by `S2C_ENTITY_DIED` → `Game.onServerState` sweep → `setConsumed()`. See [features/pickups.md](../features/pickups.md) for the server pipeline and [BUG-05](../features/pickups.md#bug-05-interact-range-bug).

## `Door` — interactables

```ts
class Door extends Phaser.GameObjects.Container {
    readonly doorId: number;
    readonly targetRoom: number;
    unlocked: boolean;
    contains(px, py): boolean { /* AABB hit-test */ }
    setUnlocked(unlocked): void { /* update color + icon */ }
}
```

### Visual states

| State     | Rect color    | Stroke        | Icon |
| --------- | ------------- | ------------- | ---- |
| Locked    | `#6b1818`     | `#2a0808`     | `⚿`  |
| Unlocked  | `#2d8b2d`     | `#0d3a0d`     | `▲`  |

### State transitions

- Initial state is `!def.locked` from `DoorDef.locked`.
- `Game.onDoorUnlocked(doorId)` calls `doors.get(doorId)?.setUnlocked(true)`, which also updates the icon.
- There is no "re-lock" path — doors only go from locked → unlocked, and the next `S2C_ROOM_LOAD` destroys and recreates them.

`Door.contains()` exists for prompt UI that isn't currently wired up (a future "Press E to enter" tooltip). The actual interaction check in `Game.handleInput()` uses a simple radial search over the `doors` map (`nearestUnlockedDoorId`), not `contains()` — see [07-input-controls.md](./07-input-controls.md).

## Why entities are `Container`s (for most classes)

Three reasons:

1. **Multi-GameObject entities.** `Player` has a sprite + an HP bar; `Enemy` has a sprite/rect + HP bar; `Pickup` has a body + glyph label; `Door` has a rect + icon text. Wrapping them in a `Container` means positioning, destroying, and scene-adding all work as one unit.
2. **Uniform `destroy()`.** `destroy()` cascades into children, so the scene's teardown code stays simple: one `entity.destroy()` disposes everything.
3. **Cheap hierarchy for tweens.** `Enemy.markDead` tweens `scaleX/Y` on the container and the whole unit (sprite + HP bar) scales together.

`Bullet` is an exception — it's a plain `Rectangle` because it has no children and no HP bar.

## Edge cases and gotchas

- **Entity id collisions across entity types:** `Enemy` and `Pickup` live in different maps but share the 32-bit `entity_id` space on the server. The current scheme assumes the server hands out non-overlapping ids per room. If a collision ever happened, `Game.onEntityDied` calling both `enemies.get(id)?.markDead()` and `pickups.get(id)?.setConsumed()` would hit both — safe no-ops because each is guarded by its own dying/consumed flag.
- **AIState === 3 (Dead) in snapshots:** server's `EntitySnapshot.state` is an enum (`Idle/Patrol/Aggro/Dead`). The scene skips state=3 snaps defensively, even though the server already filters them. Never rely on the client to enforce the filter.
- **Despawn vs. destroy:** `markDead()` plays an animation then `destroy`s. `destroy()` alone (as used for projectile despawn) skips the animation. Don't use `destroy()` for enemies — the visual cue of their death is necessary feedback for combat.
- **Fallback pixel:** Boot loads a `__fallback_pixel` 1x1 transparent PNG so `Preloader` always has at least one valid texture before moving on. If ever a class needs "any valid texture as a placeholder," that's the key.

## Client-side prediction: none

The client does not predict:

- Player movement (server sends position back on `S2C_STATE`; the lerp is toward that, not extrapolated).
- Projectile flight (target position comes from the server; the client doesn't simulate velocity).
- Enemy AI (no local pathfinding, no local aggression).
- Damage (no local HP decrement on a visible hit).

Every visible motion is the result of a server snapshot. The 50 ms snapshot cadence + the per-frame lerp is what makes motion look smooth despite the low snapshot rate. Players with unstable latency will see it as more abrupt snaps (the lerp can't help if `targetX` jumps a large delta), which is correct — adding prediction would mask server desync and create exploit surface.

## Implementation notes

- New entity classes should follow the existing pattern: construct from initial data, `applySnapshot`, optional `tick`, explicit destroy method.
- Always check `scene.textures.exists(key)` before attempting to use a texture — the fallback path is part of the contract, not an emergency brake.
- Keep per-entity size / color tables co-located with the class (`colorForType`, `sizeForType`, `styleFor`). This is the one place where gameplay-type ids are translated into visual choices; centralizing them avoids drift.
- When an entity needs to participate in tweens, prefer `scene.tweens.add({ targets: this, ... })` over `this.scene.tweens.add(...)` — both work, but the former is easier to grep.
- Do not add physics bodies to entities for collision; the scene uses its own simple distance checks (`nearestPickupId`, `nearestUnlockedDoorId`) because collisions matter only for UX proximity, not for gameplay.

## Implementation status

- All five classes are implemented and used in `Game`.
- Animation/frame playback for `Player` (walking frames from `player.json`) is **not wired up** — `player` is loaded as an atlas but the current entity uses a single frame. A future `WALK_ANIM_FRAMERATE`-driven animation (constant exists at `constants.ts`) is planned.
- `Bullet` does not differentiate player-owned vs. enemy-owned projectiles visually; `ownerId` is captured for future use.
- `Enemy.applyAnimation` (different frame per AIState, e.g. patrol vs aggro pose) — not implemented; current rendering only varies color and size by `Type`.

## Intentional bugs in this area

None. Rendering is cosmetic. Any "bug" here (wrong fallback, missed `destroy`, lerp that lags behind snapshots) is simply a presentation issue and must be fixed — it would give no competitive advantage and only worsens the experience.

Adjacent: the combat / mob / pickup / door **server** bugs (BUG-04/05/06/07/08/11) are rendered faithfully on the client — the visual quirks of BUG-11 kill-log corruption, BUG-06 settle race shots, etc. should all be visible. See [foundations/05-server-runtime.md#interaction-with-intentional-bugs](../foundations/05-server-runtime.md#interaction-with-intentional-bugs).

## Cross-references

- [03-client-state.md](./03-client-state.md) — `GameState` event bus the reconciler listens to.
- [04-scenes.md](./04-scenes.md) — `Game` scene that hosts the entity maps.
- [06-world-rooms.md](./06-world-rooms.md) — `buildRoom()` seeds the initial entity set.
- [07-input-controls.md](./07-input-controls.md) — proximity-based `nearestPickup` / `nearestUnlockedDoor` lookups.
- [foundations/02-protocol.md](../foundations/02-protocol.md) — `PlayerSnapshot`, `EntitySnapshot`, `ProjSnapshot`, `DoorDef`, `PickupSpawn` shapes.
- [features/mob-ai.md](../features/mob-ai.md) — enemy type numeric mapping + archetype balance.
- [features/combat-weapons.md](../features/combat-weapons.md) — weapon stats mirrored in `weapons.ts`.
- [features/pickups.md](../features/pickups.md) — pickup type semantics + BUG-05.
- [features/rooms-doors.md](../features/rooms-doors.md) — door state machine + BUG-03.
