import Phaser from "phaser";

import type { EntitySnapshot } from "../../net/packet";
import { flashSpriteWhite, isHitstopped } from "../fx";
import { directionFromVector, type Direction8 } from "../ui/anims";
import { BONE_WHITE, HP_AMBER, HP_GREEN, HP_RED } from "../ui/tokens";

const LERP_FACTOR = 0.3;
const HP_BAR_WIDTH = 28;
const HP_BAR_HEIGHT = 3;
const HP_BAR_OFFSET_Y = -24;
const IDLE_EPSILON = 0.4;
const KAMIKAZE_BLAST_COLORS = [0xfff1a8, 0xff9a3c, 0xe33b2f] as const;

const ENEMY_TYPE = {
  GRUNT: 1,
  RUNNER: 2,
  TANK: 3,
  KAMIKAZE: 4,
  BOSS: 5,
} as const;

/**
 * Archetype tint — the enemy atlas ships a single white-cat silhouette
 * with red markings. Per-archetype visual variation comes from a
 * multiplicative tint. Tints push white into red/blue/yellow hues
 * without losing the red blood markings.
 */
function tintForType(type: number): number {
  switch (type) {
    case ENEMY_TYPE.GRUNT:
      return 0xffffff; // stock white
    case ENEMY_TYPE.RUNNER:
      return 0xffc080; // warm amber — runner
    case ENEMY_TYPE.TANK:
      return 0x9eb8ff; // steely blue — tank
    case ENEMY_TYPE.KAMIKAZE:
      return 0xff8080; // pink-red — volatile
    case ENEMY_TYPE.BOSS:
      return 0xffd0d0; // pale blood — boss
    default:
      return 0xcccccc;
  }
}

function sizeForType(type: number): number {
  switch (type) {
    case ENEMY_TYPE.TANK:
    case ENEMY_TYPE.BOSS:
      return 44;
    case ENEMY_TYPE.KAMIKAZE:
      return 20;
    default:
      return 32;
  }
}

/**
 * Server-driven AI actor. Rendered as a masked-kitsune-cat from
 * `enemy.json`; tinted and scaled per archetype. Direction and
 * walk/idle state derive from the lerp delta each tick.
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
  private facing: Direction8 = "down";
  private moving = false;

  constructor(scene: Phaser.Scene, snap: EntitySnapshot) {
    super(scene, snap.x, snap.y);
    this.entityId = snap.entity_id;
    this.enemyType = snap.type;
    this.maxHp = Math.max(1, snap.hp);
    this.currentHp = snap.hp;
    this.targetX = snap.x;
    this.targetY = snap.y;

    const size = sizeForType(snap.type);
    const tint = tintForType(snap.type);

    if (scene.textures.exists("enemy")) {
      const sprite = scene.add.sprite(0, 0, "enemy");
      sprite.setDisplaySize(size, size);
      sprite.setTint(tint);
      this.sprite = sprite;
      this.playAnim("idle");
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
      const tookDamage = snap.hp < this.currentHp;
      this.currentHp = snap.hp;
      this.drawHpBar();
      if (tookDamage) {
        flashSpriteWhite(this.sprite, 70, tintForType(this.enemyType));
        // Lets the Game scene spawn impact fx + hitstop without
        // needing a back-pointer here.
        this.scene.events.emit("enemyHit", this.x, this.y, this.entityId);
      }
    }
  }

  tick(_dt: number): void {
    if (this.dying) return;
    if (isHitstopped(this.scene)) return;

    const dx = this.targetX - this.x;
    const dy = this.targetY - this.y;
    this.x = Phaser.Math.Linear(this.x, this.targetX, LERP_FACTOR);
    this.y = Phaser.Math.Linear(this.y, this.targetY, LERP_FACTOR);

    const facing = directionFromVector(dx, dy, IDLE_EPSILON);
    const moving = facing !== null;
    if (facing !== null && facing !== this.facing) {
      this.facing = facing;
    }
    if (moving !== this.moving) {
      this.moving = moving;
      this.playAnim(moving ? "walk" : "idle");
    } else if (moving && this.sprite instanceof Phaser.GameObjects.Sprite) {
      const expected = `enemy-walk_${this.facing}`;
      if (this.sprite.anims?.currentAnim?.key !== expected) {
        this.playAnim("walk");
      }
    }
  }

  markDead(): void {
    if (this.dying) return;
    this.dying = true;

    if (this.enemyType === ENEMY_TYPE.KAMIKAZE) {
      this.spawnKamikazeExplosion();
    }

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

  private spawnKamikazeExplosion(): void {
    const scene = this.scene;
    const x = this.x;
    const y = this.y;

    const flash = scene.add.circle(x, y, 7, 0xffffff, 0.95);
    flash.setBlendMode(Phaser.BlendModes.ADD);
    scene.tweens.add({
      targets: flash,
      alpha: 0,
      scaleX: 2.3,
      scaleY: 2.3,
      duration: 120,
      ease: "Cubic.easeOut",
      onComplete: () => flash.destroy(),
    });

    for (let i = 0; i < KAMIKAZE_BLAST_COLORS.length; i++) {
      const ring = scene.add.circle(x, y, 8 + i * 3, 0xffffff, 0);
      ring.setStrokeStyle(3 - i * 0.5, KAMIKAZE_BLAST_COLORS[i], 0.9);
      ring.setBlendMode(Phaser.BlendModes.ADD);
      scene.tweens.add({
        targets: ring,
        alpha: 0,
        scaleX: 2.1 + i * 0.35,
        scaleY: 2.1 + i * 0.35,
        duration: 180 + i * 45,
        ease: "Cubic.easeOut",
        onComplete: () => ring.destroy(),
      });
    }

    for (let i = 0; i < 8; i++) {
      const angle = (Math.PI * 2 * i) / 8;
      const ember = scene.add.circle(x, y, 2, 0xffb347, 0.95);
      ember.setBlendMode(Phaser.BlendModes.ADD);
      scene.tweens.add({
        targets: ember,
        x: x + Math.cos(angle) * 22,
        y: y + Math.sin(angle) * 22,
        alpha: 0,
        duration: 220,
        ease: "Quad.easeOut",
        onComplete: () => ember.destroy(),
      });
    }
  }

  private playAnim(kind: "idle" | "walk"): void {
    if (!(this.sprite instanceof Phaser.GameObjects.Sprite)) return;
    const key = `enemy-${kind}_${this.facing}`;
    if (!this.scene?.anims?.exists(key)) return;
    if (this.sprite.anims.currentAnim?.key === key) return;
    this.sprite.play(key);
  }

  private drawHpBar(): void {
    const ratio = Phaser.Math.Clamp(this.currentHp / this.maxHp, 0, 1);
    const filled = Math.floor(HP_BAR_WIDTH * ratio);

    this.hpBar.clear();
    if (ratio >= 1 || this.currentHp <= 0) {
      return;
    }
    this.hpBar.fillStyle(0x200000, 1);
    this.hpBar.fillRect(
      -HP_BAR_WIDTH / 2,
      HP_BAR_OFFSET_Y,
      HP_BAR_WIDTH,
      HP_BAR_HEIGHT,
    );
    const color = ratio > 0.5 ? HP_GREEN : ratio > 0.25 ? HP_AMBER : HP_RED;
    this.hpBar.fillStyle(color, 1);
    this.hpBar.fillRect(-HP_BAR_WIDTH / 2, HP_BAR_OFFSET_Y, filled, HP_BAR_HEIGHT);
    this.hpBar.lineStyle(1, BONE_WHITE, 0.5);
    this.hpBar.strokeRect(
      -HP_BAR_WIDTH / 2,
      HP_BAR_OFFSET_Y,
      HP_BAR_WIDTH,
      HP_BAR_HEIGHT,
    );
  }
}
