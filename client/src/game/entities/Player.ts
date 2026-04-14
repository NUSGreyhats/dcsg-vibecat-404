import Phaser from "phaser";
import Controls from "../scenes/utils/Controls";
import {
  SPEED,
  SPRINT_MULTIPLIER,
  PLAYER_MAX_HEALTH,
  PLAYER_INVINCIBLE_MS,
  WALK_ANIM_FRAMERATE,
} from "../constants";
import { type WeaponId } from "../weapons";

type Direction =
  | "down"
  | "downright"
  | "right"
  | "upright"
  | "up"
  | "upleft"
  | "left"
  | "downleft";

const DIR_FROM_INDEX: Direction[] = [
  "right",
  "downright",
  "down",
  "downleft",
  "left",
  "upleft",
  "up",
  "upright",
];

function angleToDirection(angle: number): Direction {
  const TWO_PI = Math.PI * 2;
  const normalized = ((angle % TWO_PI) + TWO_PI) % TWO_PI;
  const index = Math.round(normalized / (Math.PI / 4)) % 8;
  return DIR_FROM_INDEX[index];
}

export default class Player extends Phaser.Physics.Arcade.Sprite {
  controls: Controls;
  health: number;
  maxHealth: number;
  coins: number;
  ownedWeaponIds: WeaponId[];
  equippedWeaponId: WeaponId;
  progress: { currentRoom: number };
  walkAnimFrameRate: number;
  private invincibleUntil: number = 0;
  private currentDirection: Direction = "down";

  constructor(scene: Phaser.Scene, x: number, y: number, controls: Controls) {
    super(scene, x, y, "player");

    this.controls = controls;
    this.progress = { currentRoom: 1 };
    this.coins = 0;
    this.ownedWeaponIds = ["sar21"];
    this.equippedWeaponId = "sar21";
    this.maxHealth = PLAYER_MAX_HEALTH;
    this.health = this.maxHealth;
    this.walkAnimFrameRate = WALK_ANIM_FRAMERATE;

    scene.add.existing(this);
    scene.physics.add.existing(this);

    const body = this.body as Phaser.Physics.Arcade.Body;
    body.setCircle(12, this.width / 2 - 12, this.height / 2 - 12);

    this.setDepth(10);

    this.play("idle_down");
  }

  takeDamage(amount: number): boolean {
    if (this.scene.time.now < this.invincibleUntil) return false;

    this.health = Math.max(0, this.health - amount);
    this.invincibleUntil = this.scene.time.now + PLAYER_INVINCIBLE_MS;

    this.setTintFill(0xff0000);
    this.scene.time.delayedCall(150, () => {
      if (this.active) this.clearTint();
    });

    const blinkCount = Math.floor(PLAYER_INVINCIBLE_MS / 200) - 1;
    this.scene.tweens.add({
      targets: this,
      alpha: 0.3,
      duration: 100,
      yoyo: true,
      repeat: blinkCount,
      onComplete: () => {
        if (this.active) this.setAlpha(1);
      },
    });

    return true;
  }

  ownsWeapon(id: WeaponId): boolean {
    return this.ownedWeaponIds.includes(id);
  }

  equipWeapon(id: WeaponId): boolean {
    if (!this.ownsWeapon(id)) return false;
    this.equippedWeaponId = id;
    return true;
  }

  acquireWeapon(id: WeaponId): boolean {
    if (this.ownsWeapon(id)) return false;
    this.ownedWeaponIds.push(id);
    return true;
  }

  update() {
    const speed = SPEED * (this.controls.sprint ? SPRINT_MULTIPLIER : 1);

    this.setVelocity(0);

    if (this.controls.moveLeft) {
      this.setVelocityX(-1);
    }

    if (this.controls.moveRight) {
      this.setVelocityX(1);
    }

    if (this.controls.moveUp) {
      this.setVelocityY(-1);
    }

    if (this.controls.moveDown) {
      this.setVelocityY(1);
    }

    this.body!.velocity.normalize().scale(speed);

    const pointer = this.scene.input.activePointer;
    pointer.updateWorldPoint(this.scene.cameras.main);
    const angle = Phaser.Math.Angle.Between(
      this.x,
      this.y,
      pointer.worldX,
      pointer.worldY,
    );

    this.currentDirection = angleToDirection(angle);

    const isMoving = this.body!.velocity.length() > 0;
    const animKey = isMoving
      ? `walk_${this.currentDirection}`
      : `idle_${this.currentDirection}`;

    if (this.anims.currentAnim?.key !== animKey) {
      const frameRate = isMoving
        ? this.walkAnimFrameRate *
          (this.controls.sprint ? SPRINT_MULTIPLIER : 1)
        : undefined;
      this.play({ key: animKey, frameRate, repeat: -1 });
    } else if (isMoving) {
      const targetRate =
        this.walkAnimFrameRate *
        (this.controls.sprint ? SPRINT_MULTIPLIER : 1);
      if (this.anims.msPerFrame !== Math.floor(1000 / targetRate)) {
        this.anims.msPerFrame = Math.floor(1000 / targetRate);
      }
    }
  }
}
