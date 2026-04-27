# 10 — UI Components

## Purpose

Design doc for the **visual/UX layer** of the Phaser client. Defines the art-grounded identity of *VIBECAT 404* (the game's on-brand name, per `art/title.png`), the color palette and typography derived from the authored pixel art, the full component catalog, cross-scene consistency rules, and an honest inventory of which art assets are wired up vs. still sitting un-loaded in `art/`. This is the source of truth for "how should a new screen or component look" — any new visual should flow from the tokens below rather than inventing its own palette.

The reference artifacts this doc is grounded in:

| Asset                            | Role                                       | Where                             |
| -------------------------------- | ------------------------------------------ | --------------------------------- |
| `art/title.png` (1920×1080)      | Splash / main-menu hero                    | Authored, not yet wired           |
| `art/player.aseprite` + `.png`   | Protagonist: black fedora-wearing cat detective, 32×32, 8 directions × (idle + move) | `.aseprite` authored; `.png` exported to `client/public/assets/player.png` and loaded |
| `art/enemy.aseprite` + `.png`    | Antagonist: white masked kitsune-cats with red markings, 32×32, 8 dir × (idle + move + attack) | Authored, **not wired** |
| `art/weapon_*.aseprite`          | Pistol, rifle, shotgun sprites (32×16, idle + shoot) | Authored, PNG/JSON in `client/public/assets/`, **not loaded in Preloader** |
| `art/bullets.png` / `.json`      | Per-weapon bullet atlas (8×8)              | Authored, **not wired**           |
| `art/coin.png`                   | Coin pickup icon (16×16)                   | Authored, wired in `Preloader.ts` as image key `"coin"`; rendered by `Pickup.ts` for type 3 |
| `art/heart.png`                  | Health-pickup icon (16×16)                 | Authored, **not wired**           |
| `art/tilemap.png`                | 32×32 tileset: grass/dirt/stone/water/walls/buildings/props | Wired (used by every room tilemap) |
| `art/poster.jpg`                 | NUS Greyhats franchise poster (blue + orange tiger-cat + mouse) | Authored, out-of-game branding; not in the client bundle |

See [Asset integration status](#asset-integration-status) below for the wire-up roadmap.

## Visual identity — what *VIBECAT 404* actually looks like

The art establishes **a pixel-art noir set in a dusk/dawn cyberpunk cityscape**. The protagonist is a black cat in a grey fedora carrying a pistol; the antagonists are kitsune-masked white cats with red marking across their fur; bullets streak yellow; the sky is a saturated two-tone blue. The vibe is "cute sprites, serious-looking gunfight" — the tension between playful pixel cats and the noir framing *is* the aesthetic.

This drives three rules the rest of this doc enforces:

1. **All world art is pixel-art at a 32-px base tile.** No anti-aliasing, no sub-pixel positions for sprites, no vector UI icons inside the playfield.
2. **Functional UI (HUD, menus) uses a different register:** a legible monospace terminal layer that sits *above* the world. The contrast of "terminal overlay on pixel-art world" is deliberate — it reads as the detective's HUD rather than as part of the world.
3. **Colors come from the title art, not from generic game-design palettes.** The blues, the blood-red, the fedora grey, the bullet yellow — all have screen-grabbed hex values below.

### Tone

- **Noir-plus-cute.** Violence is stylized (white cats, red blood, cartoon pistol). Nothing is gritty or realistic. The humor lives in the sprite work; menu copy stays functional and terse.
- **Terminal overlay on pixel world.** HUD text and scoreboards are legible monospace in white / accent colors — they read like the detective's internal "HUD" layered on top of the scene, not a seamless diegetic UI.
- **No skeuomorphism.** Panels are flat rectangles with simple outlines. No bevels, no drop shadows (except black text strokes for legibility), no glassmorphism.

## Design tokens

Shared constants defining the visual language. Not currently centralized in a code module (they're inlined across scenes); every hex below is either sampled from the authored art or from the current Phaser scene code. Any new component should reuse these.

### Color palette

Sampled from `title.png` + the authored sprites. Roles are assigned, not invented.

#### Brand blues (from the title splash)

| Token               | Hex       | Sampled from                  | Usage                                                  |
| ------------------- | --------- | ----------------------------- | ------------------------------------------------------ |
| **Sky blue**        | `#9ec5e9` | Title sky + title-letter fill | Splash gradient top, active-state highlights, accent   |
| **Mid blue**        | `#5c8fbf` | Title mid buildings           | Splash gradient middle, primary brand fill             |
| **Navy**            | `#2b3d5c` | Title far buildings           | Splash gradient bottom, panel strokes                  |
| **Deep navy**       | `#1a2338` | Title near-building shadows   | Scene backgrounds, menu gutters                        |
| **Midnight**        | `#0d1222` | Near-black city silhouette    | Phaser canvas fill (replaces the old `#0d0d10`)        |

#### Cast palette (sampled from sprites)

| Token                | Hex       | Sampled from                  | Usage                                                  |
| -------------------- | --------- | ----------------------------- | ------------------------------------------------------ |
| **Fedora grey**      | `#8a8a8a` | Protagonist's hat (`sprite.png`) | Secondary accents, neutral metal, the rifle/shotgun body |
| **Fedora shadow**    | `#4a4a4a` | Hat-band / pistol body        | Secondary on dark, small-text borders                  |
| **Cat black**        | `#1c1c1c` | Protagonist body              | Near-black fills, enemy mask outlines                  |
| **Bone white**       | `#f5f0e6` | Enemy cat body                | Body text, primary-on-dark                             |
| **Blood red**        | `#c4303b` | Enemy markings + wound dots   | HP critical, damage indicators, error status           |
| **Mask red**         | `#e64a55` | Enemy mask highlight          | Interactive hover on destructive actions               |
| **Tracer yellow**    | `#ffdd33` | Muzzle flash + bullet streaks | Active/selected item highlight, coin pickup            |
| **Coin gold**        | `#f5c03a` | `coin.png`                    | Currency text, score numerics                          |

#### HUD/functional palette

| Token                | Hex       | Role                                                              |
| -------------------- | --------- | ----------------------------------------------------------------- |
| **Text primary**     | `#f5f0e6` (Bone white)  | Default HUD/menu text on dark                       |
| **Text secondary**   | `#c8c0ae`  | Subtitles, de-emphasized info                                    |
| **Text muted**       | `#8a8679`  | Hints, placeholder text                                          |
| **Text dim**         | `#5a564c`  | Empty-state ("(empty)"), disabled                                |
| **Accent primary**   | `#ffdd33` (Tracer yellow) | Selected item, hover, menu highlight              |
| **Accent warm**      | `#e64a55` (Mask red) | Destructive hover, critical HP warning                |
| **Success**          | `#8de77a`  | "Connected" status (keep a saturated green; not in the sprite work but needs to read as distinct from tracer yellow) |
| **Warning**          | `#f5c03a` (Coin gold) | Transient "hang on" messages, almost-empty-HP     |
| **Error**            | `#c4303b` (Blood red) | Disconnected, load failure                        |
| **HP bar green**     | `#5aa94a`  | HP > 60 %                                                        |
| **HP bar amber**     | `#f5c03a` (Coin gold) | HP 30–60 %                                        |
| **HP bar red**       | `#c4303b` (Blood red) | HP < 30 %                                         |

#### Structural palette

| Token               | Hex                  | Role                                                 |
| ------------------- | -------------------- | ---------------------------------------------------- |
| **Canvas bg**       | `#0d1222` (Midnight) | Phaser `backgroundColor`                             |
| **Panel fill**      | `#1a2338` α 0.95     | Modal panel body                                     |
| **Panel stroke**    | `#5c8fbf` (Mid blue) | Modal panel border                                   |
| **Panel dim**       | `#0d1222` α 0.55     | Full-screen dim behind modals                        |
| **Button bg**       | `#2b3d5c` (Navy)     | Text-button background                               |
| **Button bg hover** | `#5c8fbf` (Mid blue) | Text-button hover                                    |
| **Text stroke**     | `#0d1222` (Midnight) | 3 px stroke on all HUD / over-world text             |

#### Migration note

The prior palette (yellow `#ffcc33` accent on near-black `#0d0d10`) was a reasonable first pass but disconnected from the authored art. Scenes currently hardcode those legacy hexes inline (see `MainMenu.ts`, `GameHUD.ts`, etc.). Re-tinting to the palette above is a cosmetic sweep; no gameplay code changes. Track under "wire up art" work — do it in the same PR that loads `title.png` into the Preloader.

### Typography

VIBECAT 404 has **two typographic registers**:

1. **Pixel display face** — used on the title splash, nowhere else. The "VIBECAT 404" logo in `title.png` is a **hand-drawn bubble-pixel font** with 2 px black outline, 1 px white inner border, and a vertical gradient fill that mirrors the sky (Sky blue top → Mid blue bottom). There is no font file — the title is a **baked PNG**. Do not try to reproduce this face programmatically; when new marquee text is needed, author it in Aseprite and export as PNG.
2. **Monospace terminal face** — used for every other text element. Browser-resolved `monospace` (system monospace). The contrast between "baked pixel logo" and "live monospace text" is load-bearing; don't try to unify.

Size ladder (all monospace except where the "display" row is a baked PNG):

| Token               | Size           | Usage                                                              |
| ------------------- | -------------- | ------------------------------------------------------------------ |
| **Logo (baked)**    | 1920×1080 PNG  | Splash screen hero — positioned top-right of MainMenu              |
| **Title**           | 48 px          | "Run Complete" on WinScreen                                        |
| **Heading**         | 28 px          | "Shop", `[ Start Run ]`, `[ Play Again ]`                          |
| **Subtitle**        | 26 px          | "Total time: …" on WinScreen                                       |
| **Section header**  | 22 px          | "Rank: #X"                                                         |
| **Body emphasis**   | 20 px          | HUD timer, HUD room indicator, selected item                       |
| **Body**            | 18 px          | Input label, menu items                                            |
| **Small**           | 16 px          | Leaderboard rows, splits rows, status text                         |
| **Caption**         | 14 px          | Hint text ("[Up/Down] select …")                                   |
| **Micro**           | 12 px          | Room-id debug badge                                                |

All HUD and over-world text: `stroke: #0d1222` (Midnight), `strokeThickness: 3`. Pickup glyphs: `strokeThickness: 2`. Stroke is load-bearing — the pixel-art world has saturated blues and greens that drown unsroked white text.

### Spacing

Layout is pixel-tuned; there's no grid system. Consistent spacing is a convention.

| Token                 | Value      | Usage                                                           |
| --------------------- | ---------- | --------------------------------------------------------------- |
| **Gutter small**      | 8 px       | Badge inner padding, small separators                           |
| **Gutter medium**     | 12 px      | Screen-edge margins for HUD elements                            |
| **Gutter large**      | 22 px      | Row height in tables (splits, leaderboard)                      |
| **Section gap**       | 40–80 px   | Between major blocks in menus                                   |
| **Button padding**    | `16/16/8/8` | `left/right/top/bottom` on text-as-button                      |
| **Modal padding**     | 24 px      | Inside modal panel, between border and content                  |
| **Pixel grid**        | 32 px      | Tile size, sprite size for cats, base grid for any new world art |

### Depth layers (restated from rendering)

See [05-entities-rendering.md#depth-layering](./05-entities-rendering.md#depth-layering):

| Depth   | Layer                                                           |
| ------- | --------------------------------------------------------------- |
| -100    | Fallback background                                             |
| -50     | Fallback walls                                                  |
| -40     | Room-id debug badge                                             |
| -30     | Tilemap                                                         |
| 0–5     | Entities, projectiles, pickups, doors                           |
| 10      | Player                                                          |
| 20      | World-space FX (e.g., pickup toasts)                            |
| 1000    | HUD text + bar backgrounds                                      |
| 1001    | HUD bar fills, modal panels                                     |
| 1002    | Modal text, selected item                                       |

## Cast visual states

The sprites themselves are UI — players read enemy and pickup state visually before they read any text. These are the states the art library needs to supply.

### Protagonist (black cat detective)

`art/player.aseprite` — 32×32, 8 directional poses × 2 animations (idle + move). Aseprite tags group the frames.

- **Idle (8 dirs):** standing, tail curled. Fedora brim visible from top-down.
- **Walk (8 dirs):** alternating leg frames.
- **Missing from the authored art today:** shoot pose, hit flash, death. When those ship, they become states 3/4/5.

Client integration: `client/public/assets/player.png` + `player.json` are loaded as an atlas; the current `Player.ts` renders a single frame. Directional facing + walk anims are future work (`WALK_ANIM_FRAMERATE = 8` constant is waiting in `constants.ts`).

### Antagonist (masked white kitsune-cats)

`art/enemy.aseprite` — 32×32, 8 directional poses × 3 animations (idle + move + attack). White body, red markings and wound dots, red-masked face with pointed ears. Based on the title art two variants appear: one with a full kitsune mask and red glare, one with a simpler red forehead marking. Client archetype table (grunt/runner/tank/kamikaze/boss) picks tints to differentiate types, but **all share the base enemy silhouette** — archetypes are color/size variants, not different sprites.

Current client renders a colored rectangle via fallback; wiring the atlas is a one-line Preloader change plus a color-tint map instead of a color-fill map.

### Weapons

`art/weapon_pistol.aseprite`, `weapon_rifle.aseprite`, `weapon_shotgun.aseprite` — 32×16, 2 frames each (idle + shoot/muzzle-flash). Currently these sprites are not rendered on the player; combat is represented only by projectiles.

The pistol sprite is the one visible in `title.png` in the protagonist's paw. When the weapon render is wired, the player container should composite the weapon sprite above the body at the paw anchor.

### Projectiles

`art/bullets.png` + `bullets.json` — 8×8 atlas, tagged by weapon. Current client renders a `Rectangle` in `#fff23d` (Tracer yellow) — visually close to the bullet tracer streaks on the title, so the fallback is not a regression.

### Pickups

- `art/coin.png` (16×16) — Coin gold orb, wired only as the fallback texture key `"coin"` on `Pickup.ts`, not currently loaded.
- `art/heart.png` (16×16) — red-pink heart, wired only as the fallback texture key `"heart"`, not currently loaded.

### Tilemap

`art/tilemap.png` — 12×11 grid of 32×32 tiles. Three terrain families (grass, dirt, stone), a water/moat tile set, wall/building tiles, and prop tiles (torch, key, sign, bush, etc.). This tileset is **already wired** and used by every room tilemap under `client/public/assets/rooms/`.

Visually the tileset reads as **medieval-adventure topdown** — a notable stylistic tension with the noir-urban splash. Acceptable framing: the splash is the detective's origin, and each room's aesthetic can vary (the poster shows a more feral NUS Greyhats tiger-cat — the franchise stretches across tones). If consistency becomes a goal, a future urban-noir tileset (`tilemap_urban.png`) could ship alongside, selected by `tilemap_id`.

### Splash

`art/title.png` (1920×1080). The detective pointing his pistol at the two kitsune-cats, city skyline behind, "VIBECAT 404" logo in the top-right. **Not currently loaded.** Intended role: the MainMenu background. When wired, all MainMenu text (title string, player-id label, status line) moves over to the bottom-left corner so the logo + scene art aren't obscured.

### Poster

`art/poster.jpg`. Out-of-game promo art for NUS Greyhats — blue + orange tiger-cat in a fedora chewing on a field mouse. Distinct style (brushstroke illustration vs. pixel art) and **not** part of the client bundle. Keep it in `art/` for marketing; don't load it into Phaser.

### Unused / legacy

- `art/sprite.png` (the standing cat with rifle), `art/sprite_top.png` (top-down fedora silhouette), `art/gemini_sprite.png`, `art/references/` — early explorations, not part of the shipping cast.
- `client/public/assets/bg.png` (cyan gradient from Phaser template) and `client/public/assets/logo.png` (Phaser logo) — **template leftovers**. Should be removed once `title.png` ships.
- `client/public/assets/character.png` — old Phaser tutorial sprite. Legacy; delete when safe.
- `client/public/assets/sprite.png` — appears to be a prior export of a different top-down pose; also superseded.

## Component catalog

Every reusable UI unit in the game. Each entry: purpose, visual states (using the palette above), interactions, placement rules, and the file where it's implemented.

### 1. Text label

**Purpose:** static or lightly-updating text. The workhorse.

**States:** none — a label is what it says.

**Variants:**
- **Plain** — fixed color, no stroke. Menus over solid panels.
- **HUD** — 3 px Midnight stroke. Readable against any world tile.
- **Status** (color-coded):

  | State        | Color                       | Example                                           |
  | ------------ | --------------------------- | ------------------------------------------------- |
  | Idle         | Text muted `#8a8679`        | "Connecting..."                                   |
  | In progress  | Text muted                  | "Authenticating...", "Starting run..."            |
  | Success      | Success `#8de77a`           | "Connected. Enter a player ID and press Start."   |
  | Warning      | Warning `#f5c03a`           | "Still connecting — hang on."                     |
  | Error        | Error `#c4303b` (Blood red) | "Could not connect to the game server."           |

**Placement:** `.setOrigin(0, 0)` left-aligned, `.setOrigin(0.5)` centered, `.setOrigin(1, 0)` right-aligned, `.setOrigin(1, 0.5)` right+vcenter (weapon line).

**Implementation:** `scene.add.text(x, y, str, style)`. Inlined; no wrapper.

### 2. Status line

**Purpose:** scene-level feedback line. MainMenu uses it for connect/auth/start; future scenes can use it for submit/save messages.

**States:** the five from the table above. Color and message mutate; the GameObject reference stays stable.

**Placement:** one per scene. Bottom-center for modal scenes.

**Implementation:** single `Phaser.GameObjects.Text` with `setText` + `setColor` on progression. See `MainMenu.statusText`.

### 3. Button (text-as-button)

**Purpose:** primary action. Visual metaphor: bracketed label `[ Start Run ]` with a flat filled rectangle backing.

**States:**

| State      | Visual                                                          |
| ---------- | --------------------------------------------------------------- |
| **Idle**   | `color: #f5f0e6` (Bone), `backgroundColor: #2b3d5c` (Navy)      |
| **Hover**  | `color: #ffdd33` (Tracer yellow)                                |
| **Pressed**| No explicit press state; `pointerdown` fires the action         |
| **Destructive hover** | `color: #e64a55` (Mask red) — used for "Leave Queue", future confirm dialogs |
| **Disabled** | `color: #5a564c` (Text dim); skip `setInteractive`            |

**Interactions:**
- `setInteractive({ useHandCursor: true })`.
- `pointerdown` fires the action (not `pointerup`).
- `pointerover` / `pointerout` toggle color.
- Keyboard Enter (future) — currently click-only.

**Placement:** single-button scenes centered horizontally; multi-button rows share a `cy`.

**Example:**

```ts
this.startButton = this.add.text(cx, cy + 80, "[ Start Run ]", {
    fontFamily: "monospace",
    fontSize: "28px",
    color: "#f5f0e6",
    backgroundColor: "#2b3d5c",
    padding: { left: 16, right: 16, top: 8, bottom: 8 },
})
    .setOrigin(0.5)
    .setInteractive({ useHandCursor: true });
this.startButton.on("pointerover", () => this.startButton?.setColor("#ffdd33"));
this.startButton.on("pointerout",  () => this.startButton?.setColor("#f5f0e6"));
this.startButton.on("pointerdown", () => this.onStart());
```

### 4. HTML text input (DOM overlay)

**Purpose:** capture free-form text. Phaser has no native text input; we render an HTML `<input>` over the canvas.

**Styling (brand-matched):**

```css
font: 18px monospace;
color: #f5f0e6;           /* Bone white */
background: #1a2338;      /* Deep navy */
border: 1px solid #5c8fbf;/* Mid blue */
padding: 6px 10px;
text-align: center;
outline: none;
width: 280px;
box-sizing: border-box;
```

`:focus` (future CSS rule): border becomes `#ffdd33` (Tracer yellow). Currently not styled; add in the re-tint sweep.

**Interactions:** auto-focus on mount, `Enter` submits, re-positioned on `resize`. See `MainMenu.mountTextInput()`.

### 5. Progress bar

**Purpose:** linear progress. Used by `Preloader` (asset loading), `GameHUD` (HP), `Player`/`Enemy` (HP above head).

#### 5a. Preloader bar

- 468 × 32 px frame, 1 px Bone white stroke, transparent fill.
- 4–464 px Bone white fill, aligned left.
- 18 px monospace `0%` Text secondary centered below.
- Future: swap the fill color to **Mid blue → Sky blue** (vertical gradient) to echo the title's letter fill. Requires generating a gradient texture at boot; not yet implemented.

#### 5b. HUD HP bar

- 200 × 16 px background: `#0d1222` (Midnight) α 0.6, 1 px Bone stroke, pinned at `(12, h-20)`.
- 196 × 12 px fill, color:
  - `#5aa94a` (green) when `hp/max > 60 %`
  - `#f5c03a` (amber) at 30–60 %
  - `#c4303b` (Blood red) below 30 %
- `setScrollFactor(0)`, depth 1000 (bg) / 1001 (fill).

#### 5c. Entity HP bar

- 32 × 4 px (player) or 28 × 3 px (enemy), offset `-14` / `-18` above the sprite.
- Background `#330000` (darker sibling of Blood red; keep as-is — reads as "wound under the bar").
- Fill thresholds identical to HUD bar.
- **Hidden at full HP** (enemy). Player bar always visible.

Files: `Preloader.ts`, `GameHUD.ts`, `Player.ts`, `Enemy.ts`.

### 6. HUD timer

**Purpose:** run elapsed time.

**Format:** `MM:SS.mmm`, zero-padded. `02:14.732`.

**Update rate:** every render frame (~60 Hz), polled from `state.elapsedMs`.

**Placement:** top-left, 12 px gutter, `setScrollFactor(0)`, depth 1000.

**Styling:** Body emphasis (20 px) Bone white, 3 px Midnight stroke.

**Implementation:** `GameHUD.timeText` + `formatTime(ms)`. `formatTime` is currently duplicated in `WinScreen.ts`; consolidate into `client/src/game/ui/formatters.ts` when a third caller appears.

### 7. HUD room indicator

**Purpose:** `Room N/ROOM_COUNT`.

**Placement:** top-right, 12 px gutter, `.setOrigin(1, 0)`.

**Styling:** same as timer.

### 8. HUD weapon / ammo line

**Purpose:** current weapon name + ammo count.

**Format:** `<WeaponName><2 spaces><ammo|—>`.

**Placement:** bottom-right, 12 px gutter.

**Data source:** `state.player?.weapon_id` → `ALL_WEAPON_IDS[clamp(id)]` → `WEAPON_STATS[id].displayName`, `state.player?.ammo`.

**Future polish:** render the weapon's icon from `weapon_*.png` to the left of the text when the weapon atlas is loaded. Atlas wiring tracked in [Asset integration status](#asset-integration-status).

### 9. Modal panel

**Purpose:** foreground a decision (shop, pause, future confirm dialogs).

**Structure:**

1. **Dim layer** — full-screen rectangle, Panel dim (`#0d1222` α 0.55), depth 1000.
2. **Panel** — centered rectangle, size per content (480 × 360 for the shop), fill Panel fill (`#1a2338` α 0.95), 2 px Panel stroke (Mid blue), depth 1001.
3. **Title** — 28 px Tracer yellow `#ffdd33`, centered at panel top.
4. **Hint line** — 14 px Text muted, centered at panel bottom, lists shortcut keys.
5. **Content area** — per modal.

**Interaction model:** `scene.pause()` underneath, `scene.launch()` on top. Modal owns its own keyboard (does NOT share `Controls`).

**Close actions:** Esc, modal's toggle key (e.g. `B` for shop), or Enter after a selection. `scene.resume("Game"); scene.stop();`.

**Implementation:** `ShopMenu.ts` is the canonical reference. `PauseMenu.ts` follows the same pattern.

### 10. Selectable list (vertical)

**Purpose:** modal content — shop catalog rows, future settings.

**States:**
- **Default row:** `  label` (2-space prefix), Bone white.
- **Selected row:** `> label`, Tracer yellow `#ffdd33`.
- **Empty state:** `"(empty)"`, Text dim `#5a564c`.

**Interactions:** Up/Down with modular wrap (`(sel + delta + len) % len`). Enter activates. Esc cancels.

**Layout:** 20 px monospace, 28 px row height, centered horizontally, starts at `cy - 70`.

**Implementation:** `ShopMenu.renderRows()`.

### 11. Two-column summary (splits + leaderboard)

**Purpose:** end-of-run summary.

**Structure:** left "Splits" column, right "Leaderboard" column. Each has a 20 px Tracer yellow header, a 16 px Bone white column-header row, and 16 px body rows at 22 px pitch.

**Alignment:** monospace + `padStart` / `padEnd` on fixed-width slices. Name column truncated to 20 chars + `padEnd(20)` for stable alignment.

**Implementation:** `WinScreen.renderSplits()`, `WinScreen.renderLeaderboard()`.

### 12. Room-id debug badge

**Purpose:** label the fallback-rendered room with its tilemap id. Invisible in Tiled-rendered rooms.

**Styling:** 12 px monospace Bone white, alpha 0.35, anchored top-right `(ROOM_WIDTH - 24, 22)`, depth -40.

**Implementation:** `roomLayout.paintRoomBackground()`.

### 13. Splash screen (authored PNG)

**Purpose:** main-menu hero. Game identity.

**Source:** `art/title.png` (1920×1080). Export as-is to `client/public/assets/title.png`, register with key `"title"` in `Preloader.preload()`, add to the MainMenu scene as `scene.add.image(cx, cy, "title").setDisplaySize(this.scale.width, this.scale.height)` behind all other MainMenu GameObjects.

**Consequences for MainMenu layout:** once the splash shows, the in-scene text "DEFCON / Dungeon Speedrun" string should be removed (the splash's baked logo supersedes it). Player-ID label + HTML input + Start button + status line all move to the bottom-left corner so the splash art is visible.

**Status:** authored, **not wired.**

### 14. Entity sprites as UI

Players read entity state visually before they read text. These visual states are part of the UI language.

| Entity   | Idle                  | Hit                                | Dying                                                        | Future state       |
| -------- | --------------------- | ---------------------------------- | ------------------------------------------------------------ | ------------------ |
| Player   | fedora cat, 8 dirs    | HP bar color shift                 | n/a (run over)                                                | muzzle-flash frame |
| Enemy    | white masked cat      | HP bar appears below full          | 180 ms scale 1×→1.4× + alpha fade, flash white              | aim-assist glow    |
| Bullet   | `#ffdd33` rect/sprite | —                                  | despawn                                                      | per-weapon sprite  |
| Pickup   | 900 ms bob yoyo       | —                                  | 220 ms alpha fade + 12 px rise; spawns a 900 ms `+N Label` toast that rises 28 px and fades (`PickupToast.ts`) when consumed by the local player | interact prompt ring |
| Door     | red/green tint + icon | —                                  | n/a (state change only)                                      | "Press E" prompt   |

Full detail in [05-entities-rendering.md](./05-entities-rendering.md).

## Composition: scenes as component compositions

Each scene is a specific arrangement of the components above.

### MainMenu — **target layout** with splash

When `title.png` is wired up:

```
┌────────────────────────────────────────────────────────┐
│                                                        │
│                                                        │
│                  ┌──── art/title.png full-bleed ────┐  │
│                  │  (cityscape + cats + pistol +    │  │
│                  │   baked "VIBECAT 404" logo)      │  │
│                  └─────────────────────────────────┘  │
│                                                        │
│                                                        │
│  Player ID                                             │
│  ┌────────────────────────────┐                        │
│  │ [HTML <input>]             │                        │
│  └────────────────────────────┘                        │
│                                                        │
│  [ Start Run ]                                         │
│                                                        │
│  Connected. Enter a player ID and press Start.         │
└────────────────────────────────────────────────────────┘
```

Splash is the full canvas; input panel sits in the bottom-left with ~40 px gutter. Status line at the very bottom-left. The splash has its own logo, so no "DEFCON / Dungeon Speedrun" string is rendered in scene.

### MainMenu — **current layout** (no splash)

Centered column of title text + input + button + status. Plain `#0d1222` Midnight canvas. This is a placeholder until the splash ships.

### Game + GameHUD (in-run)

```
┌────────────────────────────────────────────────────────┐
│ 02:14.732                              Room 3/11       │  ← timer | room indicator
│                                                        │
│              (tilemap — grass/dirt/stone)              │
│                                                        │
│                       ☻ (player)                       │
│                   ≡☠ ≡☠  (enemies)                     │
│                       → → (bullets, tracer yellow)     │
│                                                        │
│                                                        │
│ HP 78/100                                              │
│ ████████████░░░░░░░░░░░                Rifle  24       │  ← HP bar | weapon / ammo
└────────────────────────────────────────────────────────┘
```

### Game + ShopMenu (modal)

```
┌────────────────────────────────────────────────────────┐
│  (dim — Midnight α 0.55 — gameplay showing through)    │
│         ┌──────────────────────────────────┐           │
│         │             Shop                 │ ← Tracer yellow header
│         │                                  │
│         │       Ammo       $5              │
│         │     > Speed      $10  (x1)       │ ← Tracer yellow selection
│         │       Fire Rate  $15              │
│         │       Damage     $20              │
│         │                                  │
│         │  [Up/Down] select [Enter] buy    │
│         │  [Esc/B] close                   │ ← Text muted hint
│         └──────────────────────────────────┘
└────────────────────────────────────────────────────────┘
```

### WinScreen

Canvas is Midnight. Title "Run Complete" in 48 px Tracer yellow. Centered subtitle + rank. Two columns: Splits (left) and Leaderboard (right). `[ Play Again ]` button centered at the bottom.

## Cross-scene consistency rules

1. **Two registers only.** Baked pixel PNG for display marquees (splash, future chapter cards); monospace for everything interactive. Never mix fonts within a single component.
2. **Stroke on any text over world geometry.** HUD + over-entity labels: 3 px Midnight stroke. Over-modal text (text on a Panel fill rectangle): no stroke.
3. **Tracer yellow = "this is what you act on next."** Selected item, hover state, section headers in modals, the bracketed button label in its hover state.
4. **Blood red = harm / failure.** HP critical, error status, destructive-action hover. Don't use for general emphasis.
5. **Midnight is the canvas.** Anything that needs to read as "background / void / gutter" is Midnight or a near-midnight alpha.
6. **Panels use Mid blue strokes.** Gives modals a distinct brand edge vs. HUD, which has no stroke.
7. **Depth discipline.** Overlays ≥ 1000; world < 1000. No exceptions.
8. **`setInteractive` only on interactive components.** Don't attach to static text.
9. **Every clickable component has hover feedback** (minimum: color change).
10. **All numeric displays format via a helper** (`formatTime` is canonical; add `formatCoin`, `formatRank` in the same module when they appear).
11. **No CSS-animated elements inside the canvas.** Use Phaser tweens. DOM overlays (text input) are the only exception and get no animation.
12. **Pixel-art sprites render without smoothing.** Phaser's default filter is linear — for crisp pixels, atlases should be loaded with `{ frameConfig: { ..., filterMode: NEAREST } }` or the scene should set `scene.cameras.main.setRoundPixels(true)` and the texture's filter to `Phaser.Textures.FilterMode.NEAREST`. Not currently configured; open follow-up.

## Accessibility considerations

Current state is kiosk-at-DEF-CON; baseline decencies:

- **Color-blind friendly HP:** the bar color alone isn't enough — the `HP 78/100` numeric sits next to it. Keep both.
- **High-contrast text:** Bone white / Tracer yellow on Midnight all clear contrast ≥ 10:1.
- **Keyboard navigation:** `MainMenu` accepts Enter. `ShopMenu` is keyboard-only. `WinScreen`'s button is click-only — future improvement.
- **Motion:** pickup bob (900 ms yoyo), enemy death tween (180 ms), no long-running flashes.
- **Font size floor:** 12 px debug badge; everything actionable ≥ 16 px.
- **Audio cues:** none currently. When audio ships, pair every interactive feedback (hover / select / damage) with a short chiptune sfx.

## Asset integration status

Audit of what's authored vs. wired. Priority ordered.

| Asset                              | Authored? | Exported PNG in `client/public/assets/`? | Loaded in `Preloader.ts`? | Rendered by a component? | Priority |
| ---------------------------------- | --------- | --------------------------------------- | ------------------------ | ------------------------ | -------- |
| `title.png` (splash)               | ✅        | ❌                                       | ❌                        | ❌                        | **P0**   |
| `player.png` atlas                 | ✅        | ✅                                       | ✅                        | Single frame (no anim)   | P1       |
| `enemy.png` + `.json`              | ✅        | ❌                                       | ❌                        | Color rect fallback      | **P0**   |
| `bullets.png` + `.json`            | ✅        | ❌                                       | ❌                        | Yellow rect fallback     | P1       |
| `coin.png`                         | ✅        | ✅                                       | ✅                        | ✅ (`Pickup.ts` type 3)   | Done     |
| `heart.png`                        | ✅        | ❌                                       | ❌                        | Glyph fallback           | P1       |
| `weapon_pistol/_rifle/_shotgun.png` + `.json` | ✅ | ✅ (png + json)                     | ❌                        | ❌ (no weapon sprite yet) | P2       |
| `player_hand.png`                  | ✅        | ❌                                       | ❌                        | ❌                        | P2       |
| `tilemap.png`                      | ✅        | ✅                                       | ✅                        | ✅ (per-room tilemaps)    | Done     |
| `poster.jpg` (NUS Greyhats promo)  | ✅        | n/a (out-of-game)                        | n/a                       | n/a                      | Not shipped |
| `bg.png` (Phaser template gradient)| n/a       | ✅                                       | ✅                        | (unused by any scene)    | **Remove** once splash lands |
| `logo.png` (Phaser logo)           | n/a       | ✅                                       | ✅                        | (unused by any scene)    | **Remove** |
| `character.png`                    | n/a       | ✅                                       | ❌                        | n/a                      | Legacy — delete when safe |
| `sprite.png` / `sprite_top.png`    | ✅ (early) | partial                                  | ❌                        | n/a                      | Legacy |
| `level-1.tmx/json`                 | ✅ (early) | ✅                                       | ❌                        | n/a                      | Legacy |

### Wire-up roadmap

**P0 (ship with the palette re-tint):**

1. Export `art/title.png` → `client/public/assets/title.png`. Register in `Preloader.preload()` as `this.load.image("title", "assets/title.png")`.
2. Update `MainMenu.create()` to render `scene.add.image(cx, cy, "title").setDisplaySize(w, h).setDepth(-10)` behind existing MainMenu GameObjects; move the input/button/status to the bottom-left per the target layout above. Remove the in-scene "DEFCON / Dungeon Speedrun" text.
3. Export `art/enemy.png` + `enemy.json` → `client/public/assets/`. Register with `this.load.atlas("enemy", "assets/enemy.png", "assets/enemy.json")`. `Enemy.ts` already reads the atlas key and tints per type — no entity-code change needed.
4. Re-tint all inline hexes in `MainMenu.ts`, `GameHUD.ts`, `ShopMenu.ts`, `WinScreen.ts`, `Preloader.ts`, `Game.ts`, `roomLayout.ts` to the palette above. Mechanical replace.

**P1 (subsequent PR):**

5. Export `art/bullets.png` + `bullets.json` → assets. Register atlas `"bullets"`. Update `Bullet.ts` to `scene.add.sprite(..., "bullets", frameForWeapon(ownerWeaponId))`.
6. Export `art/heart.png` → assets. Register image `"heart"`. (`coin.png` already exported, loaded, and rendered by `Pickup.ts`.)
7. Implement `Player` walk animation using the existing `player.json` frame durations + `scene.anims.create(...)` in `Preloader.create()`.

**P2 (later):**

8. Load `weapon_*.png/json` atlases. Add a weapon sprite child to `Player` container positioned at the paw anchor; swap frame on `applySnapshot` when `weapon_id` changes.
9. Consider adding `scene.textures.setFilter(Phaser.Textures.FilterMode.NEAREST)` for crisp pixels (currently defaults to linear).

**Cleanup (do alongside P0):**

10. Delete `client/public/assets/bg.png`, `logo.png`, `character.png`, `sprite.png`, `level-1.json`, `level-1.tmx` — Phaser template leftovers. Remove the `bg` / `logo` load lines from `Preloader.preload()`.

## Extending the component library

When adding a new component:

1. Check this catalog first.
2. Use the design tokens. Don't invent a new color or font size without adding it here in the same PR.
3. Follow the depth rules (overlays ≥ 1000, world < 1000).
4. Add a row to the component catalog.
5. Document stateful variants (hover, pressed, disabled, empty).
6. Keyboard activatable wherever it's mouse-activatable.

## Implementation notes

- No central "UIKit" module today; components are inlined. Proposed future: `client/src/game/ui/` containing:
  - `tokens.ts` — palette, sizes, spacing as exported constants (replaces inline hexes).
  - `Button.ts` — text-as-button factory.
  - `StatusLine.ts` — status-line state machine.
  - `Panel.ts` — modal-panel factory.
  - `List.ts` — selectable-list factory.
  - `Bar.ts` — progress/HP bar.
  - `formatters.ts` — `formatTime`, future `formatRank`, `formatCoin`.
- DOM overlays (MainMenu's HTML input) must always `.remove()` in scene `shutdown`.
- Favor Phaser tweens over CSS for any in-canvas animation.
- For crisp pixel art, set texture filter to NEAREST at Preloader time; linear filtering on pixel sprites looks soft and off-brand.

## Implementation status

- Palette migration: **not started.** Scenes use the prior `#ffcc33` + `#0d0d10` pair; mechanical retint pending per [Asset integration status](#asset-integration-status) P0 step 4.
- Splash wiring: **not started.**
- `Enemy` / `bullets` / `heart` wiring: **not started.** `coin` wired (Preloader + `Pickup.ts`).
- Walk anim: **not started** (atlas loaded, no `anims.create`).
- Weapon rendering: **not started.**
- Centralized UIKit module: **not started.**
- Audio: none.

## Intentional bugs in this area

None. Visual design is not exploit surface. The cosmetic visibility of **gameplay** bugs (kill-log garbage, heal-past-max, drift) is desired — don't paper over server-side effects with client-side clamping or smoothing.

## Cross-references

- [04-scenes.md](./04-scenes.md) — lifecycle of the scenes these components live in.
- [05-entities-rendering.md](./05-entities-rendering.md) — entity sprites + fallback strategy.
- [06-world-rooms.md](./06-world-rooms.md) — tilemap + tint palette.
- [07-input-controls.md](./07-input-controls.md) — how inputs reach these components.
- [08-assets-pipeline.md](./08-assets-pipeline.md) — how the assets above are served (and how they'd be added to `Preloader`).
- [09-spectator-mode.md](./09-spectator-mode.md) — spectator viewer reuses the component library minus input-producing pieces.
