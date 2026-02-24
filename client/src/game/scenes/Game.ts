import { Scene } from "phaser";
import Controls from "./utils/Controls";
import Player from "../entities/Player";

export class Game extends Scene {
  camera: Phaser.Cameras.Scene2D.Camera;
  background: Phaser.GameObjects.Image;
  msg_text: Phaser.GameObjects.Text;
  controls: Controls;
  player: Player;

  constructor() {
    super("Game");
  }

  create() {
    this.controls = new Controls(this);
    this.player = new Player(this, 50, 50, this.controls);

    const map = this.make.tilemap({ key: "map" });

    // tilesetName corresponds to the name of the tileset in Tiles
    const tileset = map.addTilesetImage("top_down_shooter", "topdown-tiles")!;

    map.createLayer("Base", tileset, 0, 0);
    const decor_layer = map.createLayer("Decor", tileset, 0, 0)!;

    decor_layer.setCollisionByProperty({ collides: true });
    this.physics.add.collider(this.player, decor_layer!);

    this.camera = this.cameras.main;
    this.camera.setBackgroundColor(0x00ff00);

    // Constrain the camera so that it isn't allowed to move outside the width/height of tilemap
    this.cameras.main.setBounds(0, 0, map.widthInPixels, map.heightInPixels);
    this.cameras.main.startFollow(this.player);

    this.input.keyboard!.once("keydown-M", (_: KeyboardEvent) => {
      this.physics.world.createDebugGraphic();

      const graphics = this.add.graphics().setAlpha(0.75).setDepth(20);
      decor_layer.renderDebug(graphics, {
        tileColor: null, // Color of non-colliding tiles
        collidingTileColor: new Phaser.Display.Color(243, 134, 48, 255), // Color of colliding tiles
        faceColor: new Phaser.Display.Color(40, 39, 37, 255), // Color of colliding face edges
      });
    });
  }

  update() {
    this.player.update();
  }
}
