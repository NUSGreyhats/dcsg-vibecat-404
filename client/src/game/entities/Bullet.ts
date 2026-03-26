import Phaser from "phaser";
import { BULLET_SPEED, BULLET_LIFESPAN } from "../constants";

export default class Bullet extends Phaser.Physics.Arcade.Sprite {
  private lifespan: number;

  constructor(scene: Phaser.Scene, x: number, y: number, angle: number) {
    super(scene, x, y, "bullet");

    scene.add.existing(this);
    scene.physics.add.existing(this);

    this.setDepth(9);

    const body = this.body as Phaser.Physics.Arcade.Body;
    body.setCircle(4);

    this.setVelocity(
      Math.cos(angle) * BULLET_SPEED,
      Math.sin(angle) * BULLET_SPEED,
    );

    this.lifespan = BULLET_LIFESPAN;
  }

  update(_time: number, delta: number) {
    this.lifespan -= delta;
    if (this.lifespan <= 0) {
      this.destroy();
    }
  }
}
