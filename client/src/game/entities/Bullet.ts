import Phaser from "phaser";

import type { ProjSnapshot } from "../../net/packet";
import { TRACER_YELLOW } from "../ui/tokens";

const LERP_FACTOR = 0.4;
const FALLBACK_WIDTH = 8;
const FALLBACK_HEIGHT = 4;

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

  constructor(scene: Phaser.Scene, snap: ProjSnapshot) {
    super(scene, snap.x, snap.y);
    this.projId = snap.proj_id;
    this.ownerId = snap.owner;
    this.targetX = snap.x;
    this.targetY = snap.y;

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
    this.x = Phaser.Math.Linear(this.x, this.targetX, LERP_FACTOR);
    this.y = Phaser.Math.Linear(this.y, this.targetY, LERP_FACTOR);
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
