import Phaser from "phaser";

import type { ProjSnapshot } from "../../net/packet";

const LERP_FACTOR = 0.4;
const WIDTH = 6;
const HEIGHT = 3;
const COLOR = 0xfff23d;

/**
 * Projectile sprite. Server-authoritative — we just lerp toward the
 * last reported target position and rotate by the reported angle.
 */
export class Bullet extends Phaser.GameObjects.Rectangle {
  readonly projId: number;
  readonly ownerId: number;
  targetX: number;
  targetY: number;

  constructor(scene: Phaser.Scene, snap: ProjSnapshot) {
    super(scene, snap.x, snap.y, WIDTH, HEIGHT, COLOR);
    this.projId = snap.proj_id;
    this.ownerId = snap.owner;
    this.targetX = snap.x;
    this.targetY = snap.y;
    this.setRotation(snap.angle);
    this.setOrigin(0.5, 0.5);
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
