import Phaser from "phaser";
import {
  ENEMY_SPEED,
  ENEMY_HEALTH,
  ENEMY_HEAVY_HEALTH,
  ENEMY_GRUNT_DAMAGE,
  ENEMY_HEAVY_DAMAGE,
} from "../constants";

export type RobotType = "robot-grunt" | "robot-heavy";

export default class Enemy extends Phaser.Physics.Arcade.Sprite {
  syncId: number = 0;
  health: number;
  damage: number;
  private moveSpeed: number;
  private target: Phaser.GameObjects.Sprite;

  constructor(
    scene: Phaser.Scene,
    x: number,
    y: number,
    type: RobotType,
    target: Phaser.GameObjects.Sprite,
  ) {
    super(scene, x, y, type);

    this.target = target;

    scene.add.existing(this);
    scene.physics.add.existing(this);

    const body = this.body as Phaser.Physics.Arcade.Body;

    if (type === "robot-heavy") {
      this.health = ENEMY_HEAVY_HEALTH;
      this.damage = ENEMY_HEAVY_DAMAGE;
      this.moveSpeed = ENEMY_SPEED * 0.7;
      body.setCircle(16, 4, 4);
    } else {
      this.health = ENEMY_HEALTH;
      this.damage = ENEMY_GRUNT_DAMAGE;
      this.moveSpeed = ENEMY_SPEED;
      body.setCircle(12, 4, 4);
    }

    this.setDepth(8);
  }

  update() {
    if (!this.target || !this.target.active) {
      this.setVelocity(0);
      return;
    }

    const angle = Phaser.Math.Angle.Between(
      this.x,
      this.y,
      this.target.x,
      this.target.y,
    );
    this.setRotation(angle);
    this.setVelocity(
      Math.cos(angle) * this.moveSpeed,
      Math.sin(angle) * this.moveSpeed,
    );
  }

  takeDamage(amount: number): boolean {
    this.health -= amount;

    this.setTintFill(0xffffff);
    this.scene.time.delayedCall(80, () => {
      if (this.active) this.clearTint();
    });

    if (this.health <= 0) {
      this.destroy();
      return true;
    }
    return false;
  }
}
