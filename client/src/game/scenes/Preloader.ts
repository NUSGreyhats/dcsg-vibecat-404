import { Scene } from "phaser";

export class Preloader extends Scene {
  constructor() {
    super("Preloader");
  }

  init() {
    this.add.image(512, 384, "background");

    this.add.rectangle(512, 384, 468, 32).setStrokeStyle(1, 0xffffff);

    const bar = this.add.rectangle(512 - 230, 384, 4, 28, 0xffffff);

    this.load.on("progress", (progress: number) => {
      bar.width = 4 + 460 * progress;
    });
  }

  preload() {
    this.load.setPath("assets");

    this.load.image("logo", "logo.png");
    this.load.image("topdown-tiles", "topdown-tilesheet-extruded.png");
    this.load.image("character", "character.png");
    this.load.tilemapTiledJSON("map", "level-1.json");
  }

  create() {
    this.generateRobotTextures();
    this.generateBulletTexture();
    this.scene.start("MainMenu");
  }

  private generateRobotTextures() {
    const g1 = this.add.graphics();
    g1.fillStyle(0x000000, 0.2);
    g1.fillCircle(17, 17, 13);
    g1.fillStyle(0x6a6a6a);
    g1.fillCircle(16, 16, 12);
    g1.fillStyle(0x8a8a8a);
    g1.fillCircle(14, 14, 8);
    g1.fillStyle(0x333333);
    g1.fillCircle(22, 16, 5);
    g1.fillStyle(0x00ffcc);
    g1.fillCircle(22, 16, 3);
    g1.fillStyle(0xffffff, 0.7);
    g1.fillCircle(21, 15, 1);
    g1.generateTexture("robot-grunt", 32, 32);
    g1.destroy();

    const g2 = this.add.graphics();
    g2.fillStyle(0x000000, 0.2);
    g2.fillCircle(21, 21, 17);
    g2.fillStyle(0x8b1a1a);
    g2.fillCircle(20, 20, 16);
    g2.fillStyle(0xaa3333);
    g2.fillCircle(18, 18, 10);
    g2.fillStyle(0x441111);
    g2.fillCircle(28, 14, 5);
    g2.fillCircle(28, 26, 5);
    g2.fillStyle(0xffcc00);
    g2.fillCircle(28, 14, 3);
    g2.fillCircle(28, 26, 3);
    g2.fillStyle(0xffffff, 0.6);
    g2.fillCircle(27, 13, 1);
    g2.fillCircle(27, 25, 1);
    g2.generateTexture("robot-heavy", 40, 40);
    g2.destroy();
  }

  private generateBulletTexture() {
    const g = this.add.graphics();
    g.fillStyle(0xffaa00, 0.4);
    g.fillCircle(4, 4, 4);
    g.fillStyle(0xffdd00);
    g.fillCircle(4, 4, 3);
    g.fillStyle(0xffffff);
    g.fillCircle(4, 4, 1);
    g.generateTexture("bullet", 8, 8);
    g.destroy();
  }
}
