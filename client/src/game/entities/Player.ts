import Phaser from "phaser";
import Controls from "../scenes/utils/Controls";
import { SPEED, SPRINT_MULTIPLIER } from "../constants";

export default class Player extends Phaser.Physics.Arcade.Sprite {
  controls: Controls;

  constructor(scene: Phaser.Scene, x: number, y: number, controls: Controls) {
    super(scene, x, y, "character");

    this.controls = controls;

    scene.add.existing(this);
    scene.physics.add.existing(this);

    this.setDepth(10);
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
    this.setRotation(angle);
  }
}
