import Phaser from "phaser";
import { ROOM_COUNT } from "../constants";
import { registerCatAnims } from "../ui/anims";
import { ALL_WEAPON_IDS, WEAPON_STATS } from "../weapons";
import {
  CANVAS_BG,
  DEEP_NAVY,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_SECTION,
  MID_BLUE,
  SKY_BLUE,
  TEXT_PRIMARY,
  TEXT_SECONDARY,
  hex,
} from "../ui/tokens";

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
    // NEAREST filter + roundPixels are set globally via
    // `pixelArt: true` in main.ts's Phaser config, so atlases load
    // crisp by default. Nothing to do here for filtering.

    // Register animations for any atlas that ships Aseprite tags.
    if (this.textures.exists("player")) {
      registerCatAnims(this, "player", "player", { withAttack: false });
    }
    if (this.textures.exists("enemy")) {
      registerCatAnims(this, "enemy", "enemy", { withAttack: true });
    }

    this.registerWeaponAnims();

    this.scene.start("MainMenu");
  }

  /**
   * Each weapon atlas ships Aseprite tags `idle` + `shoot`. Register
   * `${spriteKey}_idle` / `${spriteKey}_shoot` anims so the Player can
   * play them on its weapon sprite. `idle` loops; `shoot` plays once.
   */
  private registerWeaponAnims(): void {
    for (const id of ALL_WEAPON_IDS) {
      const spriteKey = WEAPON_STATS[id].spriteKey;
      if (!this.textures.exists(spriteKey)) continue;

      const texture = this.textures.get(spriteKey);
      const meta = (texture.customData ?? {}) as {
        frameTags?: { name: string; from: number; to: number }[];
      };
      const frameTags = meta.frameTags;
      if (!frameTags) continue;

      const cooldownMs = WEAPON_STATS[id].cooldownMs;
      for (const tag of frameTags) {
        const animKey = `${spriteKey}_${tag.name}`;
        if (this.anims.exists(animKey)) continue;

        const frames: Phaser.Types.Animations.AnimationFrame[] = [];
        for (let f = tag.from; f <= tag.to; f++) {
          frames.push({ key: spriteKey, frame: String(f) });
        }
        const frameCount = Math.max(1, tag.to - tag.from + 1);
        // shoot anim fills the cooldown window so the weapon is visually
        // "cocked back" for the same duration the server enforces.
        const frameRate =
          tag.name === "idle" ? 6 : frameCount / (cooldownMs / 1000);
        this.anims.create({
          key: animKey,
          frames,
          frameRate,
          repeat: tag.name === "idle" ? -1 : 0,
        });
      }
    }
  }

  preload(): void {
    this.cameras.main.setBackgroundColor(CANVAS_BG);
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
      console.warn(
        `[Preloader] failed to load ${file.key} from ${file.url ?? "?"}`,
      );
    });

    this.load.on("complete", () => {
      this.progressBar?.destroy();
      this.progressFrame?.destroy();
      this.percentText?.destroy();
    });

    // -- Cast atlases (player, enemy) --
    this.load.atlas("player", "assets/player.png", "assets/player.json");
    this.load.atlas("enemy", "assets/enemy.png", "assets/enemy.json");

    // -- Weapons (one atlas per weapon; idle + shoot tags) --
    for (const id of ALL_WEAPON_IDS) {
      const spriteKey = WEAPON_STATS[id].spriteKey;
      this.load.atlas(
        spriteKey,
        `assets/${spriteKey}.png`,
        `assets/${spriteKey}.json`,
      );
    }

    // -- Projectiles --
    this.load.atlas("bullets", "assets/bullets.png", "assets/bullets.json");

    // -- Pickup icons --
    this.load.image("coin", "assets/coin.png");
    this.load.image("heart", "assets/heart.png");

    // -- Splash --
    this.load.image("title", "assets/title.png");

    // -- Sponsor logos --
    this.load.image("logo_greyhats", "assets/greyhats_logo.png");
    this.load.image("logo_ottersec", "assets/ottersec_logo.png");
    this.load.image("logo_htx", "assets/htx_logo.png");

    // -- Tileset (shared by all room tilemaps) --
    this.load.image("tilemap", "assets/tilemap.png");

    // -- Per-room tilemaps --
    for (let i = 1; i <= ROOM_COUNT; i++) {
      this.load.tilemapTiledJSON(`room-${i}`, `assets/rooms/room-${i}.json`);
    }
  }

  private buildProgressUI(): void {
    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;

    this.add
      .text(cx, cy - 60, "Loading...", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SECTION,
        color: hex(TEXT_PRIMARY),
      })
      .setOrigin(0.5);

    this.progressFrame = this.add
      .rectangle(cx, cy, 468, 32)
      .setStrokeStyle(1, SKY_BLUE)
      .setFillStyle(DEEP_NAVY, 0.6);

    this.progressBar = this.add.rectangle(cx - 230, cy, 4, 28, MID_BLUE);
    this.progressBar.setOrigin(0, 0.5);

    this.percentText = this.add
      .text(cx, cy + 40, "0%", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(TEXT_SECONDARY),
      })
      .setOrigin(0.5);
  }
}

export default Preloader;
