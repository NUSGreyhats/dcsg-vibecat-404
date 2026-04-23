import Phaser from "phaser";

import type { EntitySnapshot } from "../../net/packet";

const LERP_FACTOR = 0.3;
const HP_BAR_WIDTH = 28;
const HP_BAR_HEIGHT = 3;
const HP_BAR_OFFSET_Y = -18;

const ENEMY_TYPE = {
  GRUNT: 1,
  RUNNER: 2,
  TANK: 3,
  KAMIKAZE: 4,
  BOSS: 5,
} as const;

function colorForType(type: number): number {
  switch (type) {
    case ENEMY_TYPE.GRUNT:
      return 0x6b8e23;
    case ENEMY_TYPE.RUNNER:
      return 0xff8c00;
    case ENEMY_TYPE.TANK:
      return 0x4169e1;
    case ENEMY_TYPE.KAMIKAZE:
      return 0xdc143c;
    case ENEMY_TYPE.BOSS:
      return 0x8b0000;
    default:
      return 0x808080;
  }
}

function sizeForType(type: number): number {
  switch (type) {
    case ENEMY_TYPE.TANK:
    case ENEMY_TYPE.BOSS:
      return 36;
    case ENEMY_TYPE.KAMIKAZE:
      return 14;
    default:
      return 24;
  }
}

/**
 * Server-driven AI actor.
 *
 * Constructed from the initial {@link EntitySnapshot}; subsequent
 * snapshots update the target position + HP via {@link applySnapshot}.
 * {@link markDead} plays a brief flash/tween then destroys.
 */
export class Enemy extends Phaser.GameObjects.Container {
  readonly entityId: number;
  readonly enemyType: number;
  sprite: Phaser.GameObjects.Sprite | Phaser.GameObjects.Rectangle;
  private readonly hpBar: Phaser.GameObjects.Graphics;
  private readonly maxHp: number;
  private currentHp: number;
  private targetX: number;
  private targetY: number;
  private dying = false;

  constructor(scene: Phaser.Scene, snap: EntitySnapshot) {
    super(scene, snap.x, snap.y);
    this.entityId = snap.entity_id;
    this.enemyType = snap.type;
    this.maxHp = Math.max(1, snap.hp);
    this.currentHp = snap.hp;
    this.targetX = snap.x;
    this.targetY = snap.y;

    const size = sizeForType(snap.type);
    const tint = colorForType(snap.type);

    if (scene.textures.exists("enemy")) {
      const frame = Number.isFinite(snap.type) ? snap.type : 0;
      const sprite = scene.add.sprite(0, 0, "enemy", frame);
      sprite.setDisplaySize(size, size);
      sprite.setTint(tint);
      this.sprite = sprite;
    } else {
      this.sprite = scene.add.rectangle(0, 0, size, size, tint);
    }
    this.add(this.sprite);

    this.hpBar = scene.add.graphics();
    this.add(this.hpBar);
    this.drawHpBar();

    scene.add.existing(this);
  }

  applySnapshot(snap: EntitySnapshot): void {
    if (this.dying) return;
    this.targetX = snap.x;
    this.targetY = snap.y;
    if (snap.hp !== this.currentHp) {
      this.currentHp = snap.hp;
      this.drawHpBar();
    }
  }

  tick(_dt: number): void {
    if (this.dying) return;
    this.x = Phaser.Math.Linear(this.x, this.targetX, LERP_FACTOR);
    this.y = Phaser.Math.Linear(this.y, this.targetY, LERP_FACTOR);
  }

  markDead(): void {
    if (this.dying) return;
    this.dying = true;

    const target = this.sprite;
    if (target instanceof Phaser.GameObjects.Sprite) {
      target.setTintFill(0xffffff);
    } else {
      target.setFillStyle(0xffffff);
    }

    this.scene.tweens.add({
      targets: this,
      alpha: 0,
      scaleX: 1.4,
      scaleY: 1.4,
      duration: 180,
      ease: "Cubic.easeOut",
      onComplete: () => {
        this.destroy();
      },
    });
  }

  private drawHpBar(): void {
    const ratio = Phaser.Math.Clamp(this.currentHp / this.maxHp, 0, 1);
    const filled = Math.floor(HP_BAR_WIDTH * ratio);

    this.hpBar.clear();
    if (ratio >= 1 || this.currentHp <= 0) {
      // hide bar at full or dead
      return;
    }
    this.hpBar.fillStyle(0x200000, 1);
    this.hpBar.fillRect(
      -HP_BAR_WIDTH / 2,
      HP_BAR_OFFSET_Y,
      HP_BAR_WIDTH,
      HP_BAR_HEIGHT,
    );
    const color = ratio > 0.5 ? 0x3ddc3d : ratio > 0.25 ? 0xe0c020 : 0xdc3d3d;
    this.hpBar.fillStyle(color, 1);
    this.hpBar.fillRect(-HP_BAR_WIDTH / 2, HP_BAR_OFFSET_Y, filled, HP_BAR_HEIGHT);
  }
}
