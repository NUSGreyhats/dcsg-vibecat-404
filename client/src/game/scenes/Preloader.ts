import { Scene } from "phaser";
import { ROOM_COUNT } from "../constants";
import { ALL_WEAPON_IDS, getWeaponStats } from "../weapons";

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
    this.load.aseprite("player", "player.png", "player.json");
    for (const id of ALL_WEAPON_IDS) {
      const sk = getWeaponStats(id).spriteKey;
      this.load.aseprite(sk, `${sk}.png`, `${sk}.json`);
    }
    this.load.image("topdown-tiles", "topdown-tilesheet-extruded.png");

    for (let i = 1; i <= ROOM_COUNT; i++) {
      this.load.tilemapTiledJSON(`room-${i}`, `rooms/room-${i}.json`);
    }
  }

  create() {
    this.createPlayerAnimations();
    this.createWeaponAnimations();
    this.generateRobotTextures();
    this.generateBulletTexture();
    this.generateLmgBulletTexture();
    this.generateDmrBulletTexture();
    this.generateMatadorBulletTexture();
    this.generateCoinTexture();
    this.scene.start("MainMenu");
  }

  private createPlayerAnimations() {
    this.anims.createFromAseprite("player");
  }

  private createWeaponAnimations() {
    for (const id of ALL_WEAPON_IDS) {
      const w = getWeaponStats(id);
      const sk = w.spriteKey;
      const texture = this.textures.get(sk);
      const customData = texture.customData as Record<string, unknown>;
      const tags = customData?.frameTags as
        | { name: string; from: number; to: number }[]
        | undefined;
      if (!tags) continue;

      const frameNames = texture.getFrameNames();

      for (const tag of tags) {
        const frames = [];
        for (let i = tag.from; i <= tag.to; i++) {
          frames.push({ key: sk, frame: frameNames[i] });
        }

        const frameCount = tag.to - tag.from + 1;
        this.anims.create({
          key: `${sk}_${tag.name}`,
          frames,
          frameRate: frameCount / (w.cooldownMs / 1000),
          repeat: tag.name === "idle" ? -1 : 0,
        });
      }
    }
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

  private generateLmgBulletTexture() {
    const g = this.add.graphics();
    g.fillStyle(0xff6600, 0.45);
    g.fillCircle(5, 5, 5);
    g.fillStyle(0xffaa44);
    g.fillCircle(5, 5, 3);
    g.fillStyle(0xffffcc);
    g.fillCircle(5, 5, 1);
    g.generateTexture("bullet_lmg", 10, 10);
    g.destroy();
  }

  private generateDmrBulletTexture() {
    const g = this.add.graphics();
    g.fillStyle(0x4488cc, 0.5);
    g.fillCircle(6, 6, 6);
    g.fillStyle(0xccddff);
    g.fillCircle(6, 6, 4);
    g.fillStyle(0xffffff);
    g.fillCircle(5, 5, 1);
    g.generateTexture("bullet_dmr", 12, 12);
    g.destroy();
  }

  private generateMatadorBulletTexture() {
    const g = this.add.graphics();
    g.fillStyle(0x331100, 0.6);
    g.fillCircle(7, 7, 7);
    g.fillStyle(0xff4400);
    g.fillCircle(7, 7, 5);
    g.fillStyle(0xffcc66);
    g.fillCircle(6, 6, 2);
    g.generateTexture("bullet_matador", 14, 14);
    g.destroy();
  }

  private generateCoinTexture() {
    const g = this.add.graphics();
    g.fillStyle(0x8b6914);
    g.fillCircle(10, 10, 9);
    g.fillStyle(0xffcc33);
    g.fillCircle(10, 10, 7);
    g.fillStyle(0xffee88);
    g.fillCircle(8, 8, 3);
    g.generateTexture("coin", 20, 20);
    g.destroy();
  }
}
