import Phaser from "phaser";
import { BULLET_SPEED, BULLET_LIFESPAN } from "../constants";

export type BulletOptions = {
  speed?: number;
  damage?: number;
  textureKey?: string;
  bodyRadius?: number;
};

export default class Bullet extends Phaser.Physics.Arcade.Sprite {
  readonly damage: number;
  private lifespan: number;

  constructor(
    scene: Phaser.Scene,
    x: number,
    y: number,
    angle: number,
    opts?: BulletOptions,
  ) {
    const textureKey = opts?.textureKey ?? "bullet";
    super(scene, x, y, textureKey);

    scene.add.existing(this);
    scene.physics.add.existing(this);

    this.setDepth(9);

    const speed = opts?.speed ?? BULLET_SPEED;
    this.damage = opts?.damage ?? 1;

    const body = this.body as Phaser.Physics.Arcade.Body;
    body.setCircle(opts?.bodyRadius ?? 4);

    this.setVelocity(
      Math.cos(angle) * speed,
      Math.sin(angle) * speed,
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
