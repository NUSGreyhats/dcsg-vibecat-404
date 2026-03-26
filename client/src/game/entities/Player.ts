import Phaser from "phaser";
import Controls from "../scenes/utils/Controls";
import {
  SPEED,
  SPRINT_MULTIPLIER,
  PLAYER_MAX_HEALTH,
  PLAYER_INVINCIBLE_MS,
} from "../constants";

/** Texture aims along +Y (gun down); aim math uses +X as 0 rad. */
const CHARACTER_AIM_OFFSET = -Math.PI / 2;

export default class Player extends Phaser.Physics.Arcade.Sprite {
  controls: Controls;
  health: number;
  maxHealth: number;
  /** Player run progress (Phaser reserves `state` on GameObject). */
  progress: { currentRoom: number };
  private invincibleUntil: number = 0;

  constructor(scene: Phaser.Scene, x: number, y: number, controls: Controls) {
    super(scene, x, y, "character");

    this.controls = controls;
    this.progress = { currentRoom: 1 };
    this.maxHealth = PLAYER_MAX_HEALTH;
    this.health = this.maxHealth;

    scene.add.existing(this);
    scene.physics.add.existing(this);

    this.setDepth(10);
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

    // normalize diagonal speed
    this.body!.velocity.normalize().scale(speed);

    const pointer = this.scene.input.activePointer;
    pointer.updateWorldPoint(this.scene.cameras.main);
    const angle = Phaser.Math.Angle.Between(
      this.x,
      this.y,
      pointer.worldX,
      pointer.worldY,
    );
    this.setRotation(angle + CHARACTER_AIM_OFFSET);
  }
}
