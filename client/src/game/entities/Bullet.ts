import Phaser from "phaser";

import type { ProjSnapshot } from "../../net/packet";
import { isHitstopped } from "../fx";
import { TRACER_YELLOW } from "../ui/tokens";

const LERP_FACTOR = 0.4;
const FALLBACK_WIDTH = 8;
const FALLBACK_HEIGHT = 4;
// Tracer is a fading polyline through the last N rendered positions —
// 6 samples at ~60 fps reads as a ~100 ms streak.
const TRAIL_LEN = 6;
const TRAIL_WIDTH = 2;

/**
 * Projectile sprite. Server-authoritative — we lerp toward the last
 * reported target position and rotate by the reported angle.
 *
 * When the bullets atlas is loaded, the frame is picked from the
 * owner's current weapon (pistol / rifle / shotgun). Otherwise we
 * render a yellow `Rectangle` (Tracer yellow, same as in the title art).
 */
export class Bullet extends Phaser.GameObjects.Container {
  readonly projId: number;
  readonly ownerId: number;
  targetX: number;
  targetY: number;
  private readonly visual: Phaser.GameObjects.Sprite | Phaser.GameObjects.Rectangle;
  // Tracer trail — drawn in world space, not parented to this container,
  // so the line doesn't rotate/translate with the bullet sprite.
  private readonly trail: Phaser.GameObjects.Graphics;
  private readonly trailPts: { x: number; y: number }[] = [];

  constructor(scene: Phaser.Scene, snap: ProjSnapshot) {
    super(scene, snap.x, snap.y);
    this.projId = snap.proj_id;
    this.ownerId = snap.owner;
    this.targetX = snap.x;
    this.targetY = snap.y;

    this.trail = scene.add.graphics();
    this.trail.setDepth(1);
    this.trail.setBlendMode(Phaser.BlendModes.ADD);

    if (scene.textures.exists("bullets")) {
      const frame = frameForOwner(snap.owner);
      const sprite = scene.add.sprite(0, 0, "bullets", frame);
      sprite.setOrigin(0.5, 0.5);
      this.visual = sprite;
    } else {
      const rect = scene.add.rectangle(
        0,
        0,
        FALLBACK_WIDTH,
        FALLBACK_HEIGHT,
        TRACER_YELLOW,
      );
      rect.setOrigin(0.5, 0.5);
      this.visual = rect;
    }
    this.add(this.visual);

    this.setRotation(snap.angle);
    scene.add.existing(this);
  }

  applySnapshot(snap: ProjSnapshot): void {
    this.targetX = snap.x;
    this.targetY = snap.y;
    this.setRotation(snap.angle);
  }

  tick(_dt: number): void {
    if (isHitstopped(this.scene)) return;
    this.x = Phaser.Math.Linear(this.x, this.targetX, LERP_FACTOR);
    this.y = Phaser.Math.Linear(this.y, this.targetY, LERP_FACTOR);
    this.pushTrail();
  }

  private pushTrail(): void {
    this.trailPts.push({ x: this.x, y: this.y });
    if (this.trailPts.length > TRAIL_LEN) this.trailPts.shift();
    this.redrawTrail();
  }

  private redrawTrail(): void {
    this.trail.clear();
    if (this.trailPts.length < 2) return;
    // Draw segment-by-segment so each step can fade independently. Older
    // samples (lower index) are more transparent than newer ones.
    for (let i = 1; i < this.trailPts.length; i++) {
      const t = i / (this.trailPts.length - 1); // 0..1, newer = higher
      this.trail.lineStyle(TRAIL_WIDTH, TRACER_YELLOW, t * 0.55);
      const a = this.trailPts[i - 1];
      const b = this.trailPts[i];
      this.trail.lineBetween(a.x, a.y, b.x, b.y);
    }
  }

  destroy(fromScene?: boolean): void {
    this.trail.destroy();
    super.destroy(fromScene);
  }
}

/**
 * Pick a bullet frame from the owner's current weapon. The current
 * protocol's `ProjSnapshot.owner` is the entity id (0 = player), so we
 * don't know the exact weapon here — default to pistol. Future work:
 * thread `weapon_id` through `ProjSnapshot` so bullets visually match
 * the firing weapon.
 */
function frameForOwner(_ownerId: number): string {
  return "bullets_pistol_0.aseprite";
}
