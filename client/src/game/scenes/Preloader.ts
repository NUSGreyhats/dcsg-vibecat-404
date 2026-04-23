import Phaser from "phaser";
import { ROOM_COUNT } from "../constants";

/**
 * Loads all runtime assets. Shows a progress bar + percentage text. If any
 * single file 404s we log and continue — entities draw rectangle fallbacks
 * so the game still runs without art.
 */
export class Preloader extends Phaser.Scene {
  private progressBar?: Phaser.GameObjects.Rectangle;
  private progressFrame?: Phaser.GameObjects.Rectangle;
  private percentText?: Phaser.GameObjects.Text;

  constructor() {
    super({ key: "Preloader" });
  }

  create(): void {
    // Trigger scene start from create once assets are loaded below.
    // Nothing to do here — actual asset load kicks off in preload().
    this.scene.start("MainMenu");
  }

  preload(): void {
    this.buildProgressUI();

    this.load.on("progress", (value: number) => {
      if (this.progressBar) {
        const maxWidth = 460;
        this.progressBar.width = 4 + maxWidth * value;
      }
      if (this.percentText) {
        this.percentText.setText(`${Math.round(value * 100)}%`);
      }
    });

    this.load.on("loaderror", (file: Phaser.Loader.File) => {
      // Non-fatal — we keep going with rectangle fallbacks.
      console.warn(
        `[Preloader] failed to load ${file.key} from ${file.url ?? "?"}`,
      );
    });

    this.load.on("complete", () => {
      this.progressBar?.destroy();
      this.progressFrame?.destroy();
      this.percentText?.destroy();
    });

    // Core atlases / images.
    this.load.atlas("player", "assets/player.png", "assets/player.json");
    this.load.image("bg", "assets/bg.png");
    this.load.image("logo", "assets/logo.png");
    this.load.image("tilemap", "assets/tilemap.png");

    // Per-room tilemaps.
    for (let i = 1; i <= ROOM_COUNT; i++) {
      this.load.tilemapTiledJSON(`room-${i}`, `assets/rooms/room-${i}.json`);
    }
  }

  private buildProgressUI(): void {
    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;

    this.add
      .text(cx, cy - 60, "Loading...", {
        fontFamily: "monospace",
        fontSize: "24px",
        color: "#ffffff",
      })
      .setOrigin(0.5);

    this.progressFrame = this.add
      .rectangle(cx, cy, 468, 32)
      .setStrokeStyle(1, 0xffffff)
      .setFillStyle(0x000000, 0);

    this.progressBar = this.add.rectangle(
      cx - 230,
      cy,
      4,
      28,
      0xffffff,
    );
    this.progressBar.setOrigin(0, 0.5);

    this.percentText = this.add
      .text(cx, cy + 40, "0%", {
        fontFamily: "monospace",
        fontSize: "18px",
        color: "#cccccc",
      })
      .setOrigin(0.5);
  }
}

export default Preloader;
