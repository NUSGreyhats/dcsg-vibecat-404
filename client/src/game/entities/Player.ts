import Phaser from "phaser";

import { PLAYER_MAX_HEALTH } from "../constants";

const LERP_FACTOR = 0.3;
const HP_BAR_WIDTH = 32;
const HP_BAR_HEIGHT = 4;
const HP_BAR_OFFSET_Y = -14;
const DEFAULT_SIZE = 20;

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

  constructor(scene: Phaser.Scene, x: number, y: number) {
    super(scene, x, y);
    this.targetX = x;
    this.targetY = y;

    if (scene.textures.exists("player")) {
      this.sprite = scene.add.sprite(0, 0, "player");
      this.sprite.setDisplaySize(DEFAULT_SIZE, DEFAULT_SIZE);
      this.add(this.sprite);
    } else {
      // No "player" texture available — fall back to a small colored
      // rectangle drawn into a generated texture so `sprite` still
      // behaves like a real Sprite to outside callers.
      const fallbackKey = "__player_fallback_tex";
      if (!scene.textures.exists(fallbackKey)) {
        const g = scene.make.graphics({ x: 0, y: 0 }, false);
        g.fillStyle(0x4ec9b0, 1);
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
   * Advance smoothing by one frame. `dt` is accepted so the scene can
   * pass delta ms uniformly; plain-lerp doesn't need it.
   */
  tick(_dt: number): void {
    this.x = Phaser.Math.Linear(this.x, this.targetX, LERP_FACTOR);
    this.y = Phaser.Math.Linear(this.y, this.targetY, LERP_FACTOR);
  }

  private drawHpBar(hp: number): void {
    const clamped = Phaser.Math.Clamp(hp, 0, PLAYER_MAX_HEALTH);
    const ratio = clamped / PLAYER_MAX_HEALTH;
    const filled = Math.floor(HP_BAR_WIDTH * ratio);

    this.hpBar.clear();
    // background
    this.hpBar.fillStyle(0x330000, 1);
    this.hpBar.fillRect(
      -HP_BAR_WIDTH / 2,
      HP_BAR_OFFSET_Y,
      HP_BAR_WIDTH,
      HP_BAR_HEIGHT,
    );
    // foreground — green high, yellow mid, red low
    const color = ratio > 0.5 ? 0x3ddc3d : ratio > 0.25 ? 0xe0c020 : 0xdc3d3d;
    this.hpBar.fillStyle(color, 1);
    this.hpBar.fillRect(-HP_BAR_WIDTH / 2, HP_BAR_OFFSET_Y, filled, HP_BAR_HEIGHT);
  }
}
