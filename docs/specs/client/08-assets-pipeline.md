# 08 — Assets Pipeline

## Purpose

Where art, tilemaps, and structured game data live on disk; how they reach the Phaser cache; the naming conventions that hold tightly across server + client; Vite's role in bundling it all; and the non-fatal loader strategy that keeps the game playable when an individual asset fails. Asset organization is a frequent source of bugs-in-practice (wrong key, missing atlas frame, stale Tiled export) — this doc is the source of truth for the naming + load rules.

## Disk layout

```
client/
├── public/
│   └── assets/
│       ├── player.png                ← atlas image
│       ├── player.json               ← atlas frames (Aseprite export)
│       ├── bg.png                    ← title-screen background
│       ├── logo.png                  ← title logo
│       ├── title.png                 ← alt title art
│       ├── tilemap.png               ← 32px tileset, shared across all rooms
│       ├── topdown-tilesheet-extruded.png ← extruded variant for tile seams
│       ├── sprite.png                ← single-frame sprite (legacy?)
│       ├── character.png             ← standalone character frame
│       ├── level-1.json              ← legacy Tiled map (first room prototype)
│       ├── level-1.tmx               ← Tiled source for level-1
│       ├── weapon_pistol.png         ← weapon atlas image
│       ├── weapon_pistol.json        ← weapon atlas frames
│       ├── weapon_rifle.png / .json
│       ├── weapon_shotgun.png / .json
│       └── rooms/
│           ├── room-1.json           ← Tiled JSON exports, one per room
│           ├── room-2.json
│           ├── …
│           └── room-10.json
└── src/                              ← no binary assets here; TypeScript only
```

Everything under `client/public/` is served verbatim by Vite (and by the lobby in prod). URLs are relative to the HTML's origin, so an asset lives at `/assets/<filename>` at runtime.

### Raw-source art lives outside `client/`

The original Aseprite files (`.aseprite`) live in the repo's top-level `art/` directory, alongside Tiled source files (`.tmx`). These are **never** shipped — only the exported `.png` + `.json` pair lands under `client/public/assets/`. If a sprite ever needs a new frame, edit the Aseprite file in `art/`, re-export, and commit both the source and the updated export.

Tiled `.tmx` sources sometimes show up inside `client/public/assets/` (e.g. `level-1.tmx`) for local authoring convenience. Vite will cheerfully serve them, but they're harmless — Phaser only loads the `.json` exports.

## Asset types and Phaser cache keys

| Asset                      | Phaser API                          | Key                                  | Where loaded                    |
| -------------------------- | ----------------------------------- | ------------------------------------ | ------------------------------- |
| Player atlas               | `this.load.atlas`                   | `"player"`                           | `Preloader.preload()`           |
| Background / title         | `this.load.image`                   | `"bg"`                               | `Preloader.preload()`           |
| Logo                       | `this.load.image`                   | `"logo"`                             | `Preloader.preload()`           |
| Tileset image              | `this.load.image`                   | `"tilemap"`                          | `Preloader.preload()`           |
| Room tilemap (per room)    | `this.load.tilemapTiledJSON`        | `"room-1"` … `"room-10"`             | `Preloader.preload()` (loop)    |
| Fallback pixel             | `this.load.image` (data URI)        | `"__fallback_pixel"`                 | `Boot.preload()`                |
| Player fallback texture    | `Graphics → generateTexture`        | `"__player_fallback_tex"`            | `Player` constructor (lazy)     |
| Enemy atlas                | *(not yet loaded)*                  | `"enemy"`                            | Future — see Implementation status |
| Heart icon                 | *(not yet loaded)*                  | `"heart"`                            | Future                          |
| Coin icon                  | *(not yet loaded)*                  | `"coin"`                             | Future                          |
| Bullet atlas               | *(not yet loaded)*                  | `"bullet"`, `"bullet_lmg"`, `"bullet_matador"` | Future (referenced in `weapons.ts`) |
| Weapon atlases             | *(not yet loaded)*                  | `"weapon_pistol"` / `_rifle` / `_shotgun` | Future                     |

Entities check `scene.textures.exists(key)` before using a texture (see [05-entities-rendering.md](./05-entities-rendering.md#fallback-texture-strategy)), so the "Future" rows are not blocking — rendering falls back to colored rectangles + glyph labels.

Key naming conventions:

- **Lowercase, snake-case.** (`"weapon_pistol"`, `"room-1"`.)
- **Room keys mirror the server's `tilemap_id`.** The server sends `tilemap_id: "room-3"` in `RoomData`; `Preloader` registers `"room-3"` from `assets/rooms/room-3.json`. These strings must match 1:1 or the fallback tint path kicks in (harmless but unintended).
- **Internal-use keys prefixed `__`.** `__fallback_pixel`, `__player_fallback_tex`. Distinguishes from real art keys.

## `Preloader` — the asset loader

```ts
preload(): void {
    this.buildProgressUI();   // progress bar + "0%" text

    this.load.on("progress", (value) => { /* update bar + %% */ });
    this.load.on("loaderror", (file) => {
        // Non-fatal — we keep going with rectangle fallbacks.
        console.warn(`[Preloader] failed to load ${file.key} from ${file.url ?? "?"}`);
    });
    this.load.on("complete", () => { /* destroy progress UI */ });

    this.load.atlas("player", "assets/player.png", "assets/player.json");
    this.load.image("bg", "assets/bg.png");
    this.load.image("logo", "assets/logo.png");
    this.load.image("tilemap", "assets/tilemap.png");

    for (let i = 1; i <= ROOM_COUNT; i++) {
        this.load.tilemapTiledJSON(`room-${i}`, `assets/rooms/room-${i}.json`);
    }
}
```

Four guarantees:

1. **Non-fatal load errors.** A 404 on an individual asset logs a warning and continues. The game falls back to rectangles + glyphs; the alternative — halting the whole boot — would be a much worse experience.
2. **One progress handler.** Progress value is the fraction (0.0 → 1.0) of files loaded. Bar width: `4 + 460 × value`.
3. **Deterministic transition.** `create()` calls `scene.start("MainMenu")` unconditionally — if assets are fully loaded by the time `preload()` returns (usually the case), Phaser proceeds straight into `create()`; otherwise it waits for `complete` before calling `create()`.
4. **Single source for the room loop.** `ROOM_COUNT` from `constants.ts` drives how many rooms to load. Bumping `ROOM_COUNT` to 12 automatically tries to load `room-12.json`; missing file triggers the warn and the fallback tint path.

## Boot — the fallback pixel

```ts
preload(): void {
    const PX = "data:image/png;base64,iVBORw0KGgoAAAA..."; // 1x1 transparent
    this.load.image("__fallback_pixel", PX);
}

create(): void {
    this.scene.start("Preloader");
}
```

Minimal bootstrap scene whose only job is to guarantee at least one valid texture before `Preloader` runs. The 1×1 transparent PNG is inlined as a data URI so it can't 404. Any future class that needs "a definitely-loaded texture" as a placeholder can use `"__fallback_pixel"`.

## Atlas format — Aseprite / Texture Packer style

Atlases follow Aseprite's export format:

```jsonc
{ "frames": {
    "0": { "frame": { "x": 64, "y": 121, "w": 32, "h": 29 }, "duration": 200, ... },
    "1": { "frame": { "x":  0, "y":   0, "w": 32, "h": 31 }, "duration": 200, ... },
    "2": { "frame": { "x": 64, "y": 121, "w": 32, "h": 29 }, "duration": 200, ... },
    ...
}}
```

Phaser's `load.atlas(key, imageUrl, jsonUrl)` consumes this. Frame names are **numeric strings** (`"0"`, `"1"`, `"2"`), not semantic labels like `"idle"` / `"walk"`. Code that selects a frame picks by index:

```ts
const frame = Number.isFinite(snap.type) ? snap.type : 0;
scene.add.sprite(0, 0, "enemy", frame);
```

When an atlas is used for animation (e.g. `"walk"` = frames 0..3 at `WALK_ANIM_FRAMERATE = 8` fps), the animation must be registered post-load via `scene.anims.create({ key: "player-walk", frames: [...], frameRate: 8 })`. This is not yet wired up — see Implementation status.

## Tilemap format — Tiled JSON

Per-room maps are Tiled editor (`mapeditor.org`) JSON exports at 40×24 tiles × 32 px = 1280×768 px. Phaser's `load.tilemapTiledJSON` parses them. Key fields:

| Tiled JSON field    | Meaning                                                                                |
| ------------------- | -------------------------------------------------------------------------------------- |
| `height`, `width`   | Tile count (24, 40).                                                                   |
| `tileheight`, `tilewidth` | Tile size (32, 32).                                                              |
| `layers[]`          | Array of layers; each has `name`, `data` (flat tile-id array), and `type` (`"tilelayer"`). |
| `tilesets[]`        | Array with `{ name, firstgid, tilecount, image }` — `name` is how `addTilesetImage` finds the Phaser texture. |

See `client/public/assets/rooms/room-1.json` for a canonical example. The `data` array is row-major, one entry per tile, `1` indexing into the tileset (`0` = empty).

**Server uses the same files.** Commit `55e2a99` wires server-side collision from the embedded Tiled JSON — the `/server/` process reads the same files the client renders. This means:

- Do not hand-edit `room-N.json` outside Tiled; subtle format drift breaks both client rendering and server collision.
- Do not change `tilewidth` / `tileheight` without coordinating all 11 rooms. Mixed tile sizes would require per-room scaling on both sides.
- When adding a room, export from Tiled **with "Embed tilesets" off** if the `tilemap.png` tileset is already loaded (the default); or with "Embed" on if we ever introduce a per-room tileset.

## Vite configuration

Two configs, same `base: "./"`:

### `client/vite/config.dev.mjs`

```js
defineConfig({
    base: "./",
    build: {
        rollupOptions: { output: { manualChunks: { phaser: ["phaser"] } } },
    },
    server: { port: 8080 },
});
```

- Dev server on port 8080. Matches the `WS_URL` default so a single-port dev setup works without env overrides.
- `manualChunks.phaser = ["phaser"]` splits Phaser into its own chunk. Avoids re-downloading Phaser on every deployment that only changed gameplay code.

### `client/vite/config.prod.mjs`

Adds:

- `minify: "terser"` with `passes: 2` and `mangle: true`.
- `format.comments: false` (strip all comments from output JS).
- `logLevel: "warning"`.
- A `phasermsg` plugin that prints a heart-filled banner to stdout at build start/end — harmless Phaser template residue.

Build command: `yarn build` (alias for `vite build --config vite/config.prod.mjs`). Output: `client/dist/` with static `index.html` + chunked JS + `assets/` copied verbatim.

### Published source (stripped) contrasts

The server is stripped via `go/ast` before being served at `/source/` on the lobby (see [foundations/09-ci-cd-deploy.md#source-code-publishing-served-by-lobby](../foundations/09-ci-cd-deploy.md#source-code-publishing-served-by-lobby)). The **client is not stripped** — participants who want to read client code can inspect the loaded JavaScript in their browser's devtools. This is intentional: (a) minified bundled TypeScript is already partially obfuscated by Terser; (b) client code carries no intentional bugs, so there's nothing to hide here; (c) the server is where exploits live and where the publishing rules apply.

## Standalone `/leaderboard`

`client/public/leaderboard.html` is a separate static HTML page, not part of the Phaser bundle. It polls a leaderboard endpoint and renders rankings without loading Phaser at all — a zero-Phaser "spectator lite" for non-players. Served at `/leaderboard` by whichever process hosts `public/` (Vite in dev, lobby in prod).

See [foundations/04-lobby.md#live-spectating](../foundations/04-lobby.md#live-spectating) for the broader spectator/leaderboard routing.

## Asset budget and performance

No per-asset budget is enforced, but practical guidelines:

- **Atlases ≤ 1 MB PNG.** Anything larger should be split.
- **Tilemaps ≤ 50 KB JSON.** Room maps ship at ~3–5 KB compressed; headroom is abundant.
- **Total preload ≤ 2 MB.** Keeps cold-load under ~2 s on typical LAN.
- **Audio: none currently.** If audio ships, load via `this.load.audio(key, url)` in `Preloader`.

## Adding a new asset

1. Author in `art/` (source: `.aseprite` / `.tmx`).
2. Export PNG + JSON (for atlases) or JSON (for tilemaps) into `client/public/assets/`.
3. Register in `Preloader.preload()` with a stable key.
4. If it's an atlas used across multiple entity classes, update [05-entities-rendering.md](./05-entities-rendering.md)'s texture table.
5. Commit both the source (`art/`) and the export (`client/public/assets/`) — keeps the repo self-contained and re-exportable.

## Adding a new room

1. Author room-N in Tiled at 40×24 tiles × 32 px. Use the existing `tilemap.png` tileset.
2. Export as JSON to `client/public/assets/rooms/room-N.json`.
3. Bump `ROOM_COUNT` in `constants.ts` if needed.
4. Update `ROOM_TINTS` in `roomLayout.ts` if you want a specific fallback color; otherwise the mod-wrap takes over.
5. Update server-side room catalog (see [features/rooms-doors.md](../features/rooms-doors.md)).

## Implementation notes

- `public/assets/` paths must be served relative to root. Vite's `base: "./"` handles this; do not change to an absolute `base` without auditing every `load.image` call.
- The loader's `"loaderror"` handler logs a warning rather than failing. Keep it that way — the fallback strategy depends on never halting boot.
- Phaser caches by key. Calling `load.image("player", ...)` twice with different URLs silently reuses the first. If a test needs to swap, clear the cache explicitly.
- Legacy assets (`sprite.png`, `character.png`, `level-1.json/.tmx`) exist from earlier prototyping. They're not referenced by current code; safe to remove in a future cleanup once it's clear no WIP branch still needs them.
- When shipping the prod build behind a reverse proxy (lobby → `/`), keep `base: "./"`: relative asset URLs (`./assets/foo.png`) are resolved against the page URL, which the proxy already rewrites correctly.

## Implementation status

**Loaded in Preloader:**
- `player` atlas (with frames 0..7, player walking animation data embedded in the JSON — duration-per-frame ms).
- `bg`, `logo`, `tilemap` images.
- `room-1` through `room-10` Tiled tilemaps.

**Not yet loaded but referenced in code:**
- `enemy` atlas — `Enemy.ts` checks for `"enemy"` and falls back to a colored `Rectangle` when missing. Every run currently takes the fallback path.
- `heart`, `coin` — `Pickup.ts` references these for prettier icons. Currently falls back to glyph text.
- `bullet`, `bullet_lmg`, `bullet_matador` — referenced in `weapons.ts`'s `bulletTextureKey` but `Bullet.ts` doesn't currently use them (renders a plain yellow `Rectangle`).
- `weapon_pistol`, `weapon_rifle`, `weapon_shotgun` — image + json exist in `public/assets/` but not loaded in `Preloader`.

**Room 11 (boss):** tilemap file not yet committed. `Preloader` currently loops only to `ROOM_COUNT = 11`, so it attempts a `room-11` load; the `loaderror` handler logs and the fallback tint path takes over at runtime. When the boss room is authored, drop it in and the loader automatically picks it up.

**Walk animation:** `player.json` has per-frame `duration` fields, but no `scene.anims.create(...)` is registered. `Player.ts` shows a single frame. Future work wires up `walk-up`/`walk-left`/`walk-right`/`walk-down` using the frame groups in the atlas.

**Audio:** none. If added later, a separate `Preloader` phase for SFX / music would help keep the progress bar readable.

**Standalone `/leaderboard.html`:** works against the current monolithic server; will be updated when the server splits into game-server + lobby (see [foundations/01-architecture.md](../foundations/01-architecture.md)).

## Intentional bugs in this area

None. Assets are content, not logic. Any asset-related issue is a correctness / UX problem and must be fixed.

## Cross-references

- [01-architecture.md](./01-architecture.md) — Vite build + deployment.
- [04-scenes.md](./04-scenes.md) — `Boot` + `Preloader` scene lifecycle.
- [05-entities-rendering.md](./05-entities-rendering.md) — fallback texture strategy for missing assets.
- [06-world-rooms.md](./06-world-rooms.md) — tilemap consumption in `paintRoomBackground`.
- [features/combat-weapons.md](../features/combat-weapons.md) — `WEAPON_STATS` which lists bullet texture keys.
- [foundations/09-ci-cd-deploy.md](../foundations/09-ci-cd-deploy.md) — source-code publishing rules (client not stripped).
