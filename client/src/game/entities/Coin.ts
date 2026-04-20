import Phaser from "phaser";

export default class Coin extends Phaser.Physics.Arcade.Sprite {
  in_wall: boolean;
  value: number;
  constructor(scene: Phaser.Scene, x: number, y: number, in_wall: boolean) {
    super(scene, x, y, "coin");
    this.value = in_wall ? 15 : 1;

    scene.add.existing(this);
    scene.physics.add.existing(this);

    this.setDepth(4);

    const body = this.body as Phaser.Physics.Arcade.Body;
    body.setCircle(9);

    scene.tweens.add({
      targets: this,
      y: y - 4,
      duration: 700,
      yoyo: true,
      repeat: -1,
      ease: "Sine.easeInOut",
    });
  }
}
