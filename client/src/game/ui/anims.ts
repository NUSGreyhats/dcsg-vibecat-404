import Phaser from "phaser";

/**
 * 8-direction sprite key. Matches the Aseprite tag naming used in
 * `player.json` and `enemy.json`.
 */
export type Direction8 =
  | "down"
  | "downright"
  | "right"
  | "upright"
  | "up"
  | "upleft"
  | "left"
  | "downleft";

export const DIRECTIONS: Direction8[] = [
  "down",
  "downright",
  "right",
  "upright",
  "up",
  "upleft",
  "left",
  "downleft",
];

/**
 * Map a 2D movement vector to one of the 8 directions. Returns
 * `null` if the vector is below the idle threshold — caller should
 * keep the previous facing in that case.
 */
export function directionFromVector(
  dx: number,
  dy: number,
  epsilon = 0.01,
): Direction8 | null {
  if (Math.abs(dx) < epsilon && Math.abs(dy) < epsilon) return null;
  const angle = Math.atan2(dy, dx); // -PI..PI, 0 = +x (right)
  // Bucket into 8 slices of 45°, starting at -22.5° so right is centered.
  const slice = Math.floor((angle + Math.PI + Math.PI / 8) / (Math.PI / 4)) % 8;
  // angle=-π → slice 0 → left; progression ccw mod 8.
  const sliceToDir: Direction8[] = [
    "left",
    "upleft",
    "up",
    "upright",
    "right",
    "downright",
    "down",
    "downleft",
  ];
  return sliceToDir[slice];
}

/**
 * Register `idle_<dir>` / `walk_<dir>` (and optionally `attack_<dir>`)
 * animations on the scene's global anim manager, prefixed by `keyPrefix`
 * so multiple atlases can share animations keyed distinctly (`player-…`,
 * `enemy-…`).
 *
 * The source atlas must have Aseprite tags named `idle_<dir>`,
 * `walk_<dir>`, optionally `attack_<dir>`. Call once at Preloader
 * `create()` time; Phaser deduplicates if called twice.
 */
export function registerCatAnims(
  scene: Phaser.Scene,
  atlasKey: string,
  keyPrefix: string,
  opts: { withAttack?: boolean } = {},
): void {
  const { withAttack = false } = opts;
  const mgr = scene.anims;

  const tagsRaw = (scene.cache.json.get(atlasKey) ??
    scene.textures.get(atlasKey).customData ??
    {}) as { meta?: { frameTags?: AsepriteTag[] } };

  const frameTags = tagsRaw?.meta?.frameTags;
  if (!frameTags || frameTags.length === 0) {
    // The atlas JSON may not live in the json cache when loaded via
    // load.atlas; fall back to a positional-frame scheme using the
    // known tag indices. Since both our atlases follow the
    // {idle,walk[,attack]} × 8-dir layout, we can synthesize anims
    // from offsets.
    registerByLayout(mgr, atlasKey, keyPrefix, withAttack);
    return;
  }

  for (const tag of frameTags) {
    const key = `${keyPrefix}-${tag.name}`;
    if (mgr.exists(key)) continue;
    const frameNumbers: Phaser.Types.Animations.AnimationFrame[] = [];
    for (let f = tag.from; f <= tag.to; f++) {
      frameNumbers.push({ key: atlasKey, frame: String(f) });
    }
    const repeat = tag.name.startsWith("idle_") ? 0 : -1;
    const frameRate = tag.name.startsWith("attack_") ? 12 : 8;
    mgr.create({ key, frames: frameNumbers, frameRate, repeat });
  }
}

type AsepriteTag = {
  name: string;
  from: number;
  to: number;
  direction: string;
  color: string;
};

/**
 * Fallback anim registration when the atlas JSON didn't surface
 * `frameTags`. Uses the well-known layout of the player + enemy
 * atlases: 8 directions × (1 idle + 4 walk [+ 4 attack]).
 */
function registerByLayout(
  mgr: Phaser.Animations.AnimationManager,
  atlasKey: string,
  keyPrefix: string,
  withAttack: boolean,
): void {
  const perDir = withAttack ? 9 : 5;
  for (let d = 0; d < DIRECTIONS.length; d++) {
    const dir = DIRECTIONS[d];
    const base = d * perDir;
    const idleKey = `${keyPrefix}-idle_${dir}`;
    const walkKey = `${keyPrefix}-walk_${dir}`;
    if (!mgr.exists(idleKey)) {
      mgr.create({
        key: idleKey,
        frames: [{ key: atlasKey, frame: String(base) }],
        frameRate: 1,
        repeat: 0,
      });
    }
    if (!mgr.exists(walkKey)) {
      const walkFrames: Phaser.Types.Animations.AnimationFrame[] = [];
      for (let f = base + 1; f <= base + 4; f++) {
        walkFrames.push({ key: atlasKey, frame: String(f) });
      }
      mgr.create({
        key: walkKey,
        frames: walkFrames,
        frameRate: 8,
        repeat: -1,
      });
    }
    if (withAttack) {
      const attackKey = `${keyPrefix}-attack_${dir}`;
      if (!mgr.exists(attackKey)) {
        const attackFrames: Phaser.Types.Animations.AnimationFrame[] = [];
        for (let f = base + 5; f <= base + 8; f++) {
          attackFrames.push({ key: atlasKey, frame: String(f) });
        }
        mgr.create({
          key: attackKey,
          frames: attackFrames,
          frameRate: 12,
          repeat: -1,
        });
      }
    }
  }
}
