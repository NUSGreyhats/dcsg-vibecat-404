import Phaser from "phaser";

import { PLAYER_MAX_HEALTH } from "../constants";
import { directionFromVector, type Direction8 } from "../ui/anims";
import { WEAPON_STATS, weaponFromServerId, type WeaponId } from "../weapons";
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
// Below this lerp-delta the player is considered stationary and plays
// the idle anim instead of walk.
const MOVE_EPSILON = 0.4;
// Distance the weapon muzzle sits out from the player's center along
// the aim vector.
const WEAPON_OFFSET = 14;

/**
 * Locally-controlled player avatar.
 *
 * Rendering only — does not own any networking state. Call
 * {@link applySnapshot} whenever a server PlayerSnapshot arrives and
 * {@link tick} every frame so the rendered sprite lerps toward the
 * server-authoritative position. Facing follows the mouse cursor; the
 * weapon sprite orbits the player along the same aim angle.
 */
export class Player extends Phaser.GameObjects.Container {
  readonly sprite: Phaser.GameObjects.Sprite;
  private readonly hpBar: Phaser.GameObjects.Graphics;
  private readonly weaponSprite: Phaser.GameObjects.Sprite;
  private lastHp = PLAYER_MAX_HEALTH;
  private targetX: number;
  private targetY: number;
  private facing: Direction8 = "down";
  private moving = false;
  private aimAngle = 0;
  private weaponId: WeaponId = "pistol";
  // When true, aimAngle is updated from the server's PlayerSnapshot.aim_angle
  // instead of from the local cursor. Spectator mode sets this — they have
  // no shooter cursor to read from. Default false preserves the player's
  // original behaviour exactly.
  private useSnapshotAim = false;

  constructor(
    scene: Phaser.Scene,
    x: number,
    y: number,
    opts: { useSnapshotAim?: boolean } = {},
  ) {
    super(scene, x, y);
    this.useSnapshotAim = opts.useSnapshotAim ?? false;
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

    // Weapon sprite is a sibling, not a child — its world-space position
    // is driven by the player's aim angle, not the container's transform.
    const weaponKey = WEAPON_STATS[this.weaponId].spriteKey;
    const initialKey = scene.textures.exists(weaponKey)
      ? weaponKey
      : "__fallback_pixel";
    this.weaponSprite = scene.add.sprite(x, y, initialKey);
    this.weaponSprite.setOrigin(0.5, 0.5);
    this.weaponSprite.setDepth(DEPTH_PLAYER + 1);
    this.playWeaponAnim("idle");

    scene.add.existing(this);
  }

  /**
   * Apply a server snapshot. The rendered position is not set
   * immediately; instead we store the target so {@link tick} can
   * smoothly interpolate toward it.
   */
  applySnapshot(snap: {
    x: number;
    y: number;
    hp: number;
    weapon_id?: number;
    aim_angle?: number;
  }): void {
    this.targetX = snap.x;
    this.targetY = snap.y;
    if (snap.hp !== this.lastHp) {
      this.lastHp = snap.hp;
      this.drawHpBar(snap.hp);
    }
    if (typeof snap.weapon_id === "number") {
      const next = weaponFromServerId(snap.weapon_id);
      if (next !== this.weaponId) {
        this.weaponId = next;
        const spriteKey = WEAPON_STATS[next].spriteKey;
        if (this.scene.textures.exists(spriteKey)) {
          this.weaponSprite.setTexture(spriteKey);
        }
        this.playWeaponAnim("idle");
      }
    }
    if (this.useSnapshotAim && typeof snap.aim_angle === "number") {
      this.aimAngle = snap.aim_angle;
    }
  }

  /**
   * Play the shoot animation, then fall back to idle. Called by the
   * Game scene whenever the player fires.
   */
  triggerShootAnim(): void {
    const spriteKey = WEAPON_STATS[this.weaponId].spriteKey;
    const shootKey = `${spriteKey}_shoot`;
    const idleKey = `${spriteKey}_idle`;
    if (!this.scene?.anims?.exists(shootKey)) return;
    this.weaponSprite.play(shootKey);
    this.weaponSprite.once(
      Phaser.Animations.Events.ANIMATION_COMPLETE,
      () => {
        if (this.weaponSprite.active && this.scene?.anims?.exists(idleKey)) {
          this.weaponSprite.play(idleKey);
        }
      },
    );
  }

  /**
   * Advance smoothing by one frame. Updates facing + anim from the
   * aim direction (mouse) so the sprite reads where the player is
   * looking, while moving state follows the lerp delta so idle vs
   * walk reflects actual motion.
   */
  tick(_dt: number): void {
    if (!this.useSnapshotAim) {
      // Aim toward the pointer in world-space coords. Re-projecting per
      // frame guards against camera scroll changing the relationship
      // between screen pixels and world coords. Spectators skip this
      // entirely — their aimAngle is set in applySnapshot from the
      // server's authoritative aim_angle field.
      const pointer = this.scene.input.activePointer;
      const cam = this.scene.cameras.main;
      if (cam) pointer.updateWorldPoint(cam);
      const worldX = Number.isFinite(pointer.worldX) ? pointer.worldX : pointer.x;
      const worldY = Number.isFinite(pointer.worldY) ? pointer.worldY : pointer.y;
      this.aimAngle = Math.atan2(worldY - this.y, worldX - this.x);
    }

    // Smooth toward the server-authoritative target position.
    const dx = this.targetX - this.x;
    const dy = this.targetY - this.y;
    this.x = Phaser.Math.Linear(this.x, this.targetX, LERP_FACTOR);
    this.y = Phaser.Math.Linear(this.y, this.targetY, LERP_FACTOR);

    // Facing follows aim direction; moving state follows lerp delta.
    const aimFacing =
      directionFromVector(
        Math.cos(this.aimAngle),
        Math.sin(this.aimAngle),
        0.01,
      ) ?? this.facing;
    const moving =
      Math.abs(dx) >= MOVE_EPSILON || Math.abs(dy) >= MOVE_EPSILON;

    const facingChanged = aimFacing !== this.facing;
    this.facing = aimFacing;
    if (moving !== this.moving || facingChanged) {
      this.moving = moving;
      this.playAnim(moving ? "walk" : "idle");
    }

    this.updateWeaponSprite();
  }

  private playAnim(kind: "idle" | "walk"): void {
    const key = `player-${kind}_${this.facing}`;
    if (!this.sprite.anims || !this.scene?.anims?.exists(key)) return;
    if (this.sprite.anims.currentAnim?.key === key) return;
    this.sprite.play(key);
  }

  private playWeaponAnim(kind: "idle" | "shoot"): void {
    const spriteKey = WEAPON_STATS[this.weaponId].spriteKey;
    const key = `${spriteKey}_${kind}`;
    if (!this.scene?.anims?.exists(key)) return;
    if (this.weaponSprite.anims?.currentAnim?.key === key) return;
    this.weaponSprite.play(key);
  }

  private updateWeaponSprite(): void {
    this.weaponSprite.setPosition(
      this.x + Math.cos(this.aimAngle) * WEAPON_OFFSET,
      this.y + Math.sin(this.aimAngle) * WEAPON_OFFSET,
    );
    this.weaponSprite.setRotation(this.aimAngle);

    // Aiming left = upper hemisphere of unit circle in screen coords;
    // flip the sprite vertically so the barrel reads right-side-up.
    const aimingLeft =
      this.aimAngle < -Math.PI / 2 || this.aimAngle > Math.PI / 2;
    this.weaponSprite.setFlipY(aimingLeft);

    // Aiming up = draw weapon behind the player so it tucks under the
    // shoulder; aiming down = weapon in front.
    const aimingUp = this.aimAngle < 0;
    this.weaponSprite.setDepth(
      aimingUp ? DEPTH_PLAYER - 1 : DEPTH_PLAYER + 1,
    );
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

  destroy(fromScene?: boolean): void {
    this.weaponSprite?.destroy();
    super.destroy(fromScene);
  }
}
