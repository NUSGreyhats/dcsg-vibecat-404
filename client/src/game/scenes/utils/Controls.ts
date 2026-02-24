export default class Controls {
  keys: any;

  constructor(scene: Phaser.Scene) {
    this.keys = scene.input.keyboard!.addKeys({
      up: Phaser.Input.Keyboard.KeyCodes.W,
      down: Phaser.Input.Keyboard.KeyCodes.S,
      left: Phaser.Input.Keyboard.KeyCodes.A,
      right: Phaser.Input.Keyboard.KeyCodes.D,

      up2: Phaser.Input.Keyboard.KeyCodes.UP,
      down2: Phaser.Input.Keyboard.KeyCodes.DOWN,
      left2: Phaser.Input.Keyboard.KeyCodes.LEFT,
      right2: Phaser.Input.Keyboard.KeyCodes.RIGHT,

      sprint: Phaser.Input.Keyboard.KeyCodes.SHIFT,
    });
  }

  get moveLeft(): boolean {
    return this.keys.left.isDown || this.keys.left2.isDown;
  }

  get moveRight(): boolean {
    return this.keys.right.isDown || this.keys.right2.isDown;
  }

  get moveUp(): boolean {
    return this.keys.up.isDown || this.keys.up2.isDown;
  }

  get moveDown(): boolean {
    return this.keys.down.isDown || this.keys.down2.isDown;
  }

  get sprint(): boolean {
    return this.keys.sprint.isDown;
  }
}
