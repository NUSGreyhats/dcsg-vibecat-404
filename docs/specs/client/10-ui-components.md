# 10 — UI Components

## Purpose

Design doc for the **visual/UX layer**: every on-screen control, indicator, panel, and text element the player sees. Where earlier files ([04-scenes.md](./04-scenes.md), [05-entities-rendering.md](./05-entities-rendering.md)) describe the code structure, this one defines the *components themselves* — their appearance, states, interactions, styling tokens, and composition rules. This is the source of truth for consistent UI look-and-feel across scenes.

The game's visual identity: **monospace typography + bold accent colors + dark dungeon palette**. Every component below follows from that identity.

## Design tokens

Shared constants that define the visual language. Not currently centralized in a token module (they're inlined across scenes), but this table is the canonical set. Any new component should reuse these values rather than inventing new ones.

### Color palette

| Token               | Hex       | Usage                                                              |
| ------------------- | --------- | ------------------------------------------------------------------ |
| **Accent primary** | `#ffcc33` | Title text, hover state, section headers, highlighted menu item    |
| **Text primary**   | `#ffffff` | Default text on dark bgs; neutral button label                     |
| **Text secondary** | `#cccccc` | Subtitles, secondary info text                                     |
| **Text muted**     | `#aaaaaa` | Hints, status-line "in progress" messages, deemphasized rows       |
| **Text dim**       | `#888888` | Empty-state placeholder ("(empty)"), disabled                      |
| **Success**        | `#88ff88` | Connected status, confirmatory message                             |
| **Warning**        | `#ffaa33` | "Still connecting — hang on." style transient warnings             |
| **Error**          | `#ff6666` | Error states, disconnected, "Could not connect"                    |
| **Accent amber**   | `#e0c020` | HP bar at 25–50 %                                                  |
| **Danger red**     | `#dc3d3d` / `#cc3333` | HP bar at <25 %, enemy health bar low color             |
| **Health green**   | `#3ddc3d` / `#44cc44` | HP bar at >50 %, healthy indicator                      |
| **Dungeon bg**     | `#0d0d10` | Phaser canvas background (between scenes)                          |
| **Panel fill**     | `#111111` (α 0.95) | Modal panels (InventoryMenu)                             |
| **Panel dim**      | `#000000` (α 0.55–0.6) | Screen-dim overlay behind modals, HP bar background   |
| **Wall**           | `#0a0a0a` | Fallback perimeter walls                                           |
| **Button bg**      | `#333333` | `[ Start Run ]`, `[ Play Again ]` box                              |
| **Stroke**         | `#ffffff` / `#000000` | Rectangle outlines + text stroke for legibility         |

Room tint palette (11 entries) lives in [`roomLayout.ts`](../../../client/src/game/world/roomLayout.ts) — see [06-world-rooms.md#tintforroom--room-palette](./06-world-rooms.md#tintforroom--room-palette).

### Typography

All text is **monospace**, all at a narrow set of sizes. There is no serif / sans-serif option — the constrained font family is part of the terminal / DEF CON identity.

| Token                   | Size  | Usage                                                        |
| ----------------------- | ----- | ------------------------------------------------------------ |
| **Display (title)**     | 72 px | "DEFCON" on MainMenu                                         |
| **Title**               | 48 px | "Run Complete" on WinScreen                                  |
| **Heading**             | 28 px | "Inventory", `[ Start Run ]` button, `[ Play Again ]`        |
| **Subtitle**            | 26 px | "Total time: …" on WinScreen                                 |
| **Section header**      | 22 px | "Rank: #X" secondary info                                    |
| **Body emphasis**       | 20 px | Subtitle text, selected item, HUD labels                     |
| **Body**                | 18 px | Input label, menu item                                       |
| **Small**               | 16 px | Leaderboard rows, splits rows, status text                   |
| **Caption**             | 14 px | Hint text ("[Up/Down] select …")                             |
| **Micro**               | 12 px | Room-id debug badge                                          |

Font family string: `"monospace"` (browser-resolved; no custom font file is loaded). Stroke: `strokeThickness: 3` on HUD text against the 3D world; `strokeThickness: 2` on pickup glyphs. Stroke color is `"#000000"` unless noted.

### Spacing

Layout uses pixel values — no grid system. Consistent spacing is a convention, not enforced:

| Token                | Value    | Usage                                                                 |
| -------------------- | -------- | --------------------------------------------------------------------- |
| **Gutter small**     | 8 px     | Between a header and its first row; badge inner padding               |
| **Gutter medium**    | 12 px    | Screen-edge margins for HUD items                                     |
| **Gutter large**     | 22 px    | Row height in tables (splits, leaderboard)                            |
| **Section gap**      | 40–80 px | Between major blocks in menus                                         |
| **Modal padding**    | 16 px    | Button internal padding (`left: 16, right: 16, top: 8, bottom: 8`)    |

### Depth layers (re-stated from rendering)

See [05-entities-rendering.md#depth-layering](./05-entities-rendering.md#depth-layering):

| Depth  | Layer                                      |
| ------ | ------------------------------------------ |
| -100   | Fallback background                        |
| -50    | Fallback walls                             |
| -40    | Room-id badge                              |
| -30    | Tilemap                                    |
| 0–5    | Entities, projectiles, pickups, doors      |
| 10     | Player                                     |
| 1000   | HUD text + HP bar background               |
| 1001   | HUD HP bar fill, modal panels              |
| 1002   | Modal text, selected item                  |

## Component catalog

Every reusable UI unit in the game. Each entry notes: purpose, visual states, interactions, placement rules, and the component's current implementation file.

### 1. Text label

**Purpose:** static or lightly-updating text. The workhorse.

**States:** none — a text label is what it says.

**Variants:**
- **Plain** — fixed color, no stroke. Menus, titles.
- **HUD** — with stroke (3 px black). Stays readable against any background.
- **Status** — color-coded:
  - neutral: `#aaaaaa`
  - success: `#88ff88`
  - warning: `#ffaa33`
  - error: `#ff6666`

**Placement rules:** use `.setOrigin(0, 0)` for left-aligned, `.setOrigin(0.5)` for centered, `.setOrigin(1, 0)` for right-aligned. Origin at `(1, 0.5)` for right-edge + vertically-centered (weapon text).

**Implementation:** `scene.add.text(x, y, str, style)`. No wrapper component.

**Example:**

```ts
this.statusText = this.add.text(cx, cy + 150, "Connecting...", {
    fontFamily: "monospace",
    fontSize: "16px",
    color: "#aaaaaa",
}).setOrigin(0.5);
```

### 2. Status line

**Purpose:** scene-level feedback. MainMenu uses it for connect/auth/start progression; WinScreen could use it for "submitting run…" messages.

**States:**
| State            | Color       | Example message                                    |
| ---------------- | ----------- | -------------------------------------------------- |
| **Idle**         | `#aaaaaa`   | "Connecting..."                                    |
| **In progress**  | `#aaaaaa`   | "Authenticating...", "Starting run..."             |
| **Success**      | `#88ff88`   | "Connected. Enter a player ID and press Start."    |
| **Warning**      | `#ffaa33`   | "Still connecting — hang on."                      |
| **Error**        | `#ff6666`   | "Could not connect to the game server."            |

**Placement rules:** one status line per scene. Bottom-center is conventional for modal scenes; lower-left for gameplay overlays.

**Implementation:** single `Phaser.GameObjects.Text` that `setText` + `setColor` mutates as the scene progresses. See `MainMenu.statusText`.

### 3. Button (text-as-button)

**Purpose:** primary action. Visual metaphor: bracketed label `[ Start Run ]` with a background fill that shifts on hover.

**States:**
| State      | Visual                                                    |
| ---------- | --------------------------------------------------------- |
| **Idle**   | `color: "#ffffff"`, `backgroundColor: "#333333"`          |
| **Hover**  | `color: "#ffcc33"` (on `pointerover`)                     |
| **Pressed**| No explicit pressed state; clicking fires the action      |
| **Disabled** | Not implemented; if needed, use `color: "#888888"` and skip input registration |

**Interactions:**
- Cursor: `useHandCursor: true` on `setInteractive`.
- Event: `pointerdown` fires the action. No `pointerup` handling — activate on down.
- Event: `pointerover` / `pointerout` toggle color.
- No keyboard access currently (no Enter activation). Future improvement.

**Placement rules:** single-button scenes center horizontally; multi-button rows share a `cy` and distribute on `cx - N`, `cx`, `cx + N`.

**Implementation:** `scene.add.text(...)` with `.setInteractive({ useHandCursor: true })`, three event listeners. The label conventionally includes the surrounding brackets: `[ Start Run ]`, `[ Play Again ]`.

**Example:**

```ts
this.startButton = this.add.text(cx, cy + 80, "[ Start Run ]", {
    fontFamily: "monospace",
    fontSize: "28px",
    color: "#ffffff",
    backgroundColor: "#333333",
    padding: { left: 16, right: 16, top: 8, bottom: 8 },
})
    .setOrigin(0.5)
    .setInteractive({ useHandCursor: true });

this.startButton.on("pointerover", () => this.startButton?.setColor("#ffcc33"));
this.startButton.on("pointerout",  () => this.startButton?.setColor("#ffffff"));
this.startButton.on("pointerdown", () => this.onStart());
```

### 4. HTML text input (DOM overlay)

**Purpose:** capture free-form text from the user. Phaser has no native text input; we render an HTML `<input>` positioned over the Phaser canvas.

**States:** managed by the browser — `:focus`, `:hover`, etc. We set a monospace font to keep the input visually consistent with the surrounding Phaser text.

**Interactions:**
- Default focus on scene mount (`setTimeout(...focus(), 50)`).
- `Enter` key submits the form (calls `onStart`).
- Re-positioned on `window.resize` and `this.scale.on("resize")` so it tracks the canvas layout.
- Removed via `.remove()` on scene shutdown.

**Styling tokens:**
- Font: 18 px monospace.
- Colors: `color: #fff`, `background: #111`, `border: 1px solid #888`.
- Size: 280 px wide, auto height (6 px / 10 px padding).
- `outline: none`.
- `textAlign: center`.

**Placement rules:** pixel-positioned by computing the canvas bounding rect + scene-to-viewport scale. This is the only component that needs resize handling.

**Implementation:** see `MainMenu.mountTextInput()`. If ever needed again (chat, settings, custom player name), extract into a `TextInputOverlay` utility class.

### 5. Progress bar

**Purpose:** linear progress indicator. Used by `Preloader` (asset loading 0–100 %) and by `Player`/`Enemy` for HP.

**Variants:**
#### 5a. Preloader bar

- 468 × 32 px frame, 1 px white stroke, transparent fill.
- 4–464 px white fill inside, aligned left, grows with progress.
- 18 px monospace `"0%"` centered below, updates per progress event.

#### 5b. HUD HP bar

- 200 × 16 px background (`#000000` alpha 0.6), 1 px white stroke, left-aligned at `(12, h - 20)`.
- 196 × 12 px fill (4 px inset), color shifts:
  - `#44cc44` when `hp/max > 60 %`
  - `#ddaa22` at `30–60 %`
  - `#cc3333` below 30 %
- Pinned to screen coords (`setScrollFactor(0)`, depth 1000/1001).

#### 5c. Entity HP bar

- 32 × 4 px (player) or 28 × 3 px (enemy), anchored above the sprite (`HP_BAR_OFFSET_Y = -14` / `-18`).
- Background: `#330000` / `#200000`.
- Fill color shifts: `#3ddc3d` > 50 %, `#e0c020` > 25 %, `#dc3d3d` otherwise.
- **Hidden at full HP** (enemy only — player HP bar is always visible).

**Implementation files:** `Preloader.ts` (progress), `GameHUD.ts` (HUD HP), `Player.ts` / `Enemy.ts` (entity HP).

### 6. HUD timer

**Purpose:** show the run's elapsed ms as `MM:SS.mmm`.

**Format:** `zero-pad(minutes, 2):zero-pad(seconds, 2).zero-pad(ms, 3)` → e.g. `02:14.732`.

**Update rate:** every render frame (~60 Hz). Value polled from `state.elapsedMs`.

**Placement:** top-left, 12 px gutter. `setScrollFactor(0)`, depth 1000.

**Styling:** 20 px monospace white with 3 px black stroke.

**Implementation:** `GameHUD.timeText`. Format helper `formatTime(ms)` is duplicated in `WinScreen.ts`; consolidate into a shared util if a third caller appears.

### 7. HUD room indicator

**Purpose:** show `Room N/ROOM_COUNT`.

**Placement:** top-right, 12 px gutter.

**Styling:** same as HUD timer. `.setOrigin(1, 0)` right-aligns.

### 8. HUD weapon / ammo line

**Purpose:** show current weapon name + ammo count.

**Format:** `<WeaponName><2 spaces><ammo|—>`. Dash if no ammo data yet (initial render).

**Placement:** bottom-right, 12 px gutter.

**Data source:** `state.player?.weapon_id` → `ALL_WEAPON_IDS[clamp(id)]` → `WEAPON_STATS[id].displayName`. Ammo from `state.player?.ammo`.

### 9. Modal panel

**Purpose:** foreground a decision or overlay (inventory, future pause menu, future shop).

**Structure:**
1. **Dim layer** — full-screen rectangle, `#000000` alpha 0.55, depth 1000. Blocks pointer events on the scene below.
2. **Panel** — centered rectangle, size depends on content (480 × 360 for inventory), fill `#111111` alpha 0.95, 2 px `#ffffff` stroke, depth 1001.
3. **Title** — 28 px accent-color monospace text, centered at panel top.
4. **Hint line** — 14 px muted text, centered at panel bottom, lists shortcut keys.
5. **Content area** — varies per modal (see InventoryMenu).

**Interaction model:** the underlying scene is paused (`scene.pause()`) while the modal is launched (`scene.launch()`). Modal owns its own keyboard — does NOT share `Controls` with `Game` (see [07-input-controls.md](./07-input-controls.md)).

**Close actions:** Esc, the modal's own toggle key (e.g. `I` for inventory), or Enter after making a selection. Any close path calls `scene.resume("Game"); scene.stop();`.

**Implementation:** `InventoryMenu.ts` is the canonical example. Future `PauseMenu` / `ShopMenu` should follow this shape.

### 10. Selectable list (vertical)

**Purpose:** modal content for inventory (and future menus — shop inventory, settings).

**States:**
- **Default row:** `  item_name` (two-space prefix), color `#ffffff`.
- **Selected row:** `> item_name`, color `#ffcc33` (accent).
- **Empty state:** `"(empty)"`, color `#888888`, no navigation.

**Interactions:** Up/Down moves selection with modular wrap (`(selected + delta + len) % len`). Enter activates. Esc cancels.

**Layout:** 20 px monospace, 28 px row height, centered horizontally at `cx`, starts at `cy - 70` (panel-relative).

**Implementation:** `InventoryMenu.renderItems()`.

### 11. Two-column summary (splits + leaderboard)

**Purpose:** display end-of-run summary + live leaderboard.

**Structure:**
- Left column: "Splits" header + column-header row (`"Room   Time        Kills"`) + one row per split.
- Right column: "Leaderboard" header + column-header row (`"Rank  Name                 Time"`) + top-10 rows.

**Styling:**
- Header: 20 px `#ffcc33` accent.
- Column header: 16 px `#ffffff` body.
- Rows: 16 px `#ffffff` body, 22 px row height.

**Alignment:** use `.padStart/.padEnd` on fixed-width substrings — fits the monospace aesthetic and avoids Phaser's layout complexity:

```ts
const line = `${String(s.room_index).padEnd(6)}`
           + `${formatTime(s.time_ms).padEnd(12)}`
           + `${s.kills}`;
```

Name column truncates to 20 chars (`.slice(0, 20)`) then `.padEnd(20)`. This keeps the monospace column alignment stable across arbitrary display names.

**Implementation:** `WinScreen.renderSplits()`, `WinScreen.renderLeaderboard()`.

### 12. Room-id debug badge

**Purpose:** label the current tilemap in the fallback-rendered room, as an aid during seed-routing debug sessions. Not visible in prod Tiled rooms.

**Styling:** 12 px monospace white, stroke/alpha 0.35, anchored top-right at `(ROOM_WIDTH - 24, 22)`.

**Implementation:** `roomLayout.paintRoomBackground()`.

### 13. Entity sprites (as UI)

Briefly included here because the player reads entity state visually and the visual states are part of the UI language.

| Entity   | Idle         | Hit                                | Dying                                                         | Selected / highlighted |
| -------- | ------------ | ---------------------------------- | ------------------------------------------------------------- | ---------------------- |
| Player   | walk frame 0 | HP bar shrinks / color shifts      | n/a (run over)                                                | n/a                    |
| Enemy    | colored fill per type | HP bar appears below full | 180 ms scale 1×→1.4× + alpha fade, tint/fill flash white     | (future: aim assist glow) |
| Bullet   | `#fff23d` yellow rect | —                         | despawn (no animation; just removal)                          | n/a                    |
| Pickup   | bob 900 ms yoyo | —                              | 220 ms alpha fade + 12 px rise                                | (future: interact prompt ring) |
| Door     | red/green tint + icon | —                         | n/a (closed → open is the state change)                       | (future: "E" prompt)   |

Full detail in [05-entities-rendering.md](./05-entities-rendering.md).

## Composition: scenes as component compositions

Each scene is a specific arrangement of the components above.

### MainMenu

```
┌──────────────────────────────────────────────────────┐
│                                                      │
│                    DEFCON                            │  ← display text (72 px accent)
│                Dungeon Speedrun                      │  ← subtitle (20 px secondary)
│                                                      │
│                  Player ID                           │  ← body label (18 px primary)
│        ┌────────────────────────────┐                │
│        │ [HTML <input>]             │                │  ← HTML text input overlay
│        └────────────────────────────┘                │
│                                                      │
│                  [ Start Run ]                       │  ← button (28 px primary)
│                                                      │
│                                                      │
│          Connected. Enter a player ID...             │  ← status line
│                                                      │
└──────────────────────────────────────────────────────┘
```

Vertical rhythm: each element anchored relative to `cy` (scene-center):
- Title at `cy - 200`
- Subtitle at `cy - 130`
- Label at `cy - 40`
- Input at `cy` (via HTML overlay computation)
- Button at `cy + 80`
- Status at `cy + 150`

### Game + GameHUD (in-run)

```
┌──────────────────────────────────────────────────────┐
│ 02:14.732                             Room 3/11       │  ← timer | room indicator
│                                                      │
│             (room tiles, entities, bullets)          │
│                                                      │
│                       ☻  ≡☠                          │  ← player / enemies
│                                                      │
│                                                      │
│ HP 78/100                                            │
│ ████████████░░░░░░░░░░░                Rifle  24     │  ← HP bar | weapon / ammo
└──────────────────────────────────────────────────────┘
```

### Game + InventoryMenu (modal)

```
┌──────────────────────────────────────────────────────┐
│  (dim layer — dungeon showing through at α 0.55)     │
│        ┌──────────────────────────────┐              │
│        │        Inventory             │              │  ← modal title (28 px accent)
│        │                              │              │
│        │      health_potion           │              │
│        │    > speed_boost             │              │  ← selected (accent + >)
│        │      shield                  │              │
│        │                              │              │
│        │  [Up/Down] select  [Enter]   │              │  ← hint (14 px muted)
│        │  use  [Esc/I] close          │              │
│        └──────────────────────────────┘              │
└──────────────────────────────────────────────────────┘
```

### WinScreen

```
┌──────────────────────────────────────────────────────┐
│                   Run Complete                       │  ← title (48 px accent)
│                                                      │
│              Total time:  01:07.412                  │  ← subtitle
│                    Rank:  #3                         │
│                                                      │
│ Splits                        Leaderboard             │
│ Room   Time        Kills      Rank  Name                 Time
│ 1      00:08.123   4          1     zinc-pelican      00:52.014
│ 2      00:14.217   6          2     aluminum-fox      01:04.891
│ ...                            3     (you)              01:07.412
│                                                      │
│                                                      │
│                  [ Play Again ]                      │
└──────────────────────────────────────────────────────┘
```

## Cross-scene consistency rules

Unifying principles that every new component must obey:

1. **Monospace only.** If a component calls for a non-monospace font, it's wrong — either redesign with shorter text or use the existing Phaser renderer.
2. **Stroke on any text over world geometry.** HUD text and over-entity labels always use `stroke: #000000, strokeThickness: 3`. Without stroke, white text on the tan-tiled rooms becomes illegible in dense corners.
3. **Accent color is action / emphasis.** `#ffcc33` marks the thing the player should look at next: the current selection, a hover, a title, a rank.
4. **Red is feedback, not decoration.** Red means "you are taking damage" (HP bar low) or "this failed" (error status). Never use red for general emphasis.
5. **Depth discipline.** Any overlay above the world uses a depth ≥ 1000. Any world element — including the Player — sits below 1000. The "1000 gap" makes it cheap to add new overlays without re-layering existing code.
6. **Pointer events only on interactive components.** Don't `setInteractive` on static text. It wastes a Phaser hit-test slot and can intercept clicks meant for elements below.
7. **Every clickable component has hover feedback.** Minimum: color change. Prevents uncertainty about what's actionable.
8. **All numeric displays format via a helper.** `formatTime(ms)` is the canonical example. If a second numeric field ships (score, kills, currency), write the formatter alongside.

## Accessibility considerations

Current state is limited — it's a kiosk-at-DEF-CON game more than a general-public product. Baseline decencies:

- **Color-blind friendly HP bar:** the HP bar color alone isn't enough to communicate low health; the numeric `HP 78/100` text next to it carries the value. Keep both.
- **High-contrast text:** all menu text is white (or accent `#ffcc33`) on near-black — contrast ratio > 10:1 in every case I measured.
- **Keyboard navigation:** `MainMenu` input supports Enter. `InventoryMenu` is keyboard-only. `WinScreen`'s `[ Play Again ]` currently requires a click — future improvement: any-keypress or Enter activates.
- **Motion:** the pickup bob and enemy-death tweens are brief (900 ms yoyo, 180 ms). No long-running flashes that could cause seizures.
- **Font size floor:** 12 px for the debug badge; everything actionable is ≥ 16 px.

## Extending the component library

When adding a new component:

1. Check this catalog first. If the new thing can reuse or extend an existing pattern, do.
2. Use the design tokens. Don't introduce a new color or font size unless there's a justification — and if you do, add it to the tokens table above in the same PR.
3. Follow the depth rules — overlays ≥ 1000, world content < 1000.
4. Add a row to the component catalog here.
5. Document stateful variants (hover, pressed, disabled, empty) — don't leave interactive states implicit.
6. Keep keyboard access in mind; any clickable thing should be activatable without a mouse.

## Implementation notes

- There is no central "UIKit" module today. Components are inlined in their scenes. This is fine for the current surface area but will strain if two more modals land. Proposed future work: `client/src/game/ui/` folder with:
  - `tokens.ts` — the color/size/spacing tables.
  - `Button.ts` — text-as-button factory.
  - `StatusLine.ts` — the state-machine text label.
  - `Panel.ts` — modal-panel factory.
  - `List.ts` — selectable list factory.
  - `Bar.ts` — HP/progress bar.
- For consistency across scenes, consider centralizing `formatTime` and the stroke style object.
- DOM overlays (text input) must always be `.remove()`'d in scene `shutdown`. Leaked overlays reappear on the next scene and look broken.
- If a future component needs animation (e.g., a "skill unlocked" toast), favor Phaser tweens over CSS — the game is a single Phaser canvas and mixing DOM animations would diverge the aesthetic.

## Implementation status

- All components listed here exist across the scene files as described.
- No centralized UI module yet; each scene inlines its own styles.
- No theme switching (light/dark) — the game is dark-mode only by design.
- No settings UI — no user-configurable keybindings, volume, or display options.

## Intentional bugs in this area

None. Visual design is not exploit surface; any bug here is a UX defect. The cosmetic visibility of **gameplay bugs** (kill-log garbage in the HUD's kill counter, heal-past-max showing negative HP, etc.) is desired — do not paper over server-side effects with client-side clamping or animation smoothing.

## Cross-references

- [04-scenes.md](./04-scenes.md) — lifecycle of the scenes these components live in.
- [05-entities-rendering.md](./05-entities-rendering.md) — entity-sprite UI.
- [06-world-rooms.md](./06-world-rooms.md) — room-id badge and tint palette.
- [07-input-controls.md](./07-input-controls.md) — how inputs reach these components.
- [08-assets-pipeline.md](./08-assets-pipeline.md) — textures referenced by icon components.
- [09-spectator-mode.md](./09-spectator-mode.md) — spectator viewer uses the same component library with the input-producing pieces removed.
