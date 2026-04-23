import Phaser from "phaser";

import { PLAYER_MAX_HEALTH } from "../constants";
import { directionFromVector, type Direction8 } from "../ui/anims";
import {
  BONE_WHITE,
  BLOOD_RED,
  DEPTH_PLAYER,
  HP_AMBER,
  HP_GREEN,
  HP_RED,
} from "../ui/tokens";

const LERP_FACTOR = 0.3;
const HP_BAR_WIDTH = 32;
const HP_BAR_HEIGHT = 4;
const HP_BAR_OFFSET_Y = -22;
const DEFAULT_SIZE = 32;
const HP_BAR_BG = 0x220a0a;
const IDLE_EPSILON = 0.4;

/**
 * Locally-controlled player avatar.
 *
 * Rendering only — does not own any networking state. Call
 * {@link applySnapshot} whenever a server PlayerSnapshot arrives and
 * {@link tick} every frame so the rendered sprite lerps toward the
 * server-authoritative position.
 */
export class Player extends Phaser.GameObjects.Container {
  readonly sprite: Phaser.GameObjects.Sprite;
  private readonly hpBar: Phaser.GameObjects.Graphics;
  private lastHp = PLAYER_MAX_HEALTH;
  private targetX: number;
  private targetY: number;
  private facing: Direction8 = "down";
  private moving = false;

  constructor(scene: Phaser.Scene, x: number, y: number) {
    super(scene, x, y);
    this.targetX = x;
    this.targetY = y;

    if (scene.textures.exists("player")) {
      this.sprite = scene.add.sprite(0, 0, "player");
      this.sprite.setDisplaySize(DEFAULT_SIZE, DEFAULT_SIZE);
      this.add(this.sprite);
      this.playAnim("idle");
    } else {
      const fallbackKey = "__player_fallback_tex";
      if (!scene.textures.exists(fallbackKey)) {
        const g = scene.make.graphics({ x: 0, y: 0 }, false);
        g.fillStyle(BLOOD_RED, 1);
        g.fillRect(0, 0, DEFAULT_SIZE, DEFAULT_SIZE);
        g.generateTexture(fallbackKey, DEFAULT_SIZE, DEFAULT_SIZE);
        g.destroy();
      }
      this.sprite = scene.add.sprite(0, 0, fallbackKey);
      this.add(this.sprite);
    }

    this.hpBar = scene.add.graphics();
    this.add(this.hpBar);
    this.drawHpBar(this.lastHp);

    // Keep the player above tilemaps (depth 0) and entities (depth ≈ 0–5).
    // Without this, a room tilemap loaded on C2S_ROOM_LOAD renders on top
    // of the player and visually hides them.
    this.setDepth(DEPTH_PLAYER);

    scene.add.existing(this);
  }

  /**
   * Apply a server snapshot. The rendered position is not set
   * immediately; instead we store the target so {@link tick} can
   * smoothly interpolate toward it.
   */
  applySnapshot(snap: { x: number; y: number; hp: number }): void {
    this.targetX = snap.x;
    this.targetY = snap.y;
    if (snap.hp !== this.lastHp) {
      this.lastHp = snap.hp;
      this.drawHpBar(snap.hp);
    }
  }

  /**
   * Advance smoothing by one frame. Updates facing + anim from the
   * lerp delta so the sprite reads correct direction.
   */
  tick(_dt: number): void {
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
    } else if (moving && this.scene?.anims) {
      // Re-issue the walk anim if direction changed mid-motion.
      const expected = `player-walk_${this.facing}`;
      if (this.sprite.anims?.currentAnim?.key !== expected) {
        this.playAnim("walk");
      }
    }
  }

  private playAnim(kind: "idle" | "walk"): void {
    const key = `player-${kind}_${this.facing}`;
    if (!this.sprite.anims || !this.scene?.anims?.exists(key)) return;
    if (this.sprite.anims.currentAnim?.key === key) return;
    this.sprite.play(key);
  }

  private drawHpBar(hp: number): void {
    const clamped = Phaser.Math.Clamp(hp, 0, PLAYER_MAX_HEALTH);
    const ratio = clamped / PLAYER_MAX_HEALTH;
    const filled = Math.floor(HP_BAR_WIDTH * ratio);

    this.hpBar.clear();
    this.hpBar.fillStyle(HP_BAR_BG, 1);
    this.hpBar.fillRect(
      -HP_BAR_WIDTH / 2,
      HP_BAR_OFFSET_Y,
      HP_BAR_WIDTH,
      HP_BAR_HEIGHT,
    );
    const color = ratio > 0.6 ? HP_GREEN : ratio > 0.3 ? HP_AMBER : HP_RED;
    this.hpBar.fillStyle(color, 1);
    this.hpBar.fillRect(-HP_BAR_WIDTH / 2, HP_BAR_OFFSET_Y, filled, HP_BAR_HEIGHT);
    // Thin bone-white outline for legibility against busy tiles.
    this.hpBar.lineStyle(1, BONE_WHITE, 0.7);
    this.hpBar.strokeRect(
      -HP_BAR_WIDTH / 2,
      HP_BAR_OFFSET_Y,
      HP_BAR_WIDTH,
      HP_BAR_HEIGHT,
    );
  }
}
