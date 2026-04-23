import Phaser from "phaser";
import { getSync } from "../net/runContext";
import { PLAYER_MAX_HEALTH, ROOM_COUNT } from "../constants";
import { WEAPON_STATS, ALL_WEAPON_IDS } from "../weapons";
import { formatTime } from "../ui/formatters";
import {
  BONE_WHITE,
  DEPTH_HUD_BG,
  DEPTH_HUD_FILL,
  GUTTER_MEDIUM,
  HP_AMBER,
  HP_GREEN,
  HP_RED,
  HUD_TEXT_STYLE,
  MIDNIGHT,
} from "../ui/tokens";

const HP_BAR_WIDTH = 200;
const HP_BAR_HEIGHT = 16;

/**
 * Heads-up display overlay. Launched alongside the Game scene. Reads state
 * directly from the sync on every tick — no input handling here.
 */
export class GameHUD extends Phaser.Scene {
  private timeText?: Phaser.GameObjects.Text;
  private roomText?: Phaser.GameObjects.Text;
  private hpText?: Phaser.GameObjects.Text;
  private hpBarBg?: Phaser.GameObjects.Rectangle;
  private hpBarFill?: Phaser.GameObjects.Rectangle;
  private weaponText?: Phaser.GameObjects.Text;

  constructor() {
    super({ key: "GameHUD" });
  }

  create(): void {
    const w = this.scale.width;
    const h = this.scale.height;

    this.timeText = this.add
      .text(GUTTER_MEDIUM, 10, "00:00.000", HUD_TEXT_STYLE)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_BG);

    this.roomText = this.add
      .text(w - GUTTER_MEDIUM, 10, `Room 1/${ROOM_COUNT}`, HUD_TEXT_STYLE)
      .setOrigin(1, 0)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_BG);

    this.hpText = this.add
      .text(GUTTER_MEDIUM, h - 44, "HP 100/100", HUD_TEXT_STYLE)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_BG);

    this.hpBarBg = this.add
      .rectangle(GUTTER_MEDIUM, h - 20, HP_BAR_WIDTH, HP_BAR_HEIGHT, MIDNIGHT, 0.6)
      .setOrigin(0, 0.5)
      .setStrokeStyle(1, BONE_WHITE)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_BG);

    this.hpBarFill = this.add
      .rectangle(GUTTER_MEDIUM, h - 20, HP_BAR_WIDTH, HP_BAR_HEIGHT - 4, HP_GREEN)
      .setOrigin(0, 0.5)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_FILL);

    this.weaponText = this.add
      .text(w - GUTTER_MEDIUM, h - 24, "Pistol  —", HUD_TEXT_STYLE)
      .setOrigin(1, 0.5)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_BG);

    this.events.on("shutdown", this.cleanup, this);
  }

  update(): void {
    const state = getSync().state;

    if (this.timeText) {
      this.timeText.setText(formatTime(state.elapsedMs ?? 0));
    }

    if (this.roomText) {
      const cur = state.currentRoom ?? 1;
      this.roomText.setText(`Room ${cur}/${ROOM_COUNT}`);
    }

    const hp = Math.max(0, state.player?.hp ?? 0);
    const maxHp = PLAYER_MAX_HEALTH;
    if (this.hpText) this.hpText.setText(`HP ${hp}/${maxHp}`);
    if (this.hpBarFill) {
      const pct = Phaser.Math.Clamp(hp / maxHp, 0, 1);
      this.hpBarFill.width = (HP_BAR_WIDTH - 4) * pct;
      const colour = pct > 0.6 ? HP_GREEN : pct > 0.3 ? HP_AMBER : HP_RED;
      this.hpBarFill.fillColor = colour;
    }

    if (this.weaponText) {
      const weaponId = state.player?.weapon_id ?? 0;
      const idx = Phaser.Math.Clamp(weaponId, 0, ALL_WEAPON_IDS.length - 1);
      const id = ALL_WEAPON_IDS[idx];
      const name = id ? WEAPON_STATS[id].displayName : "—";
      const ammo = state.player?.ammo ?? 0;
      this.weaponText.setText(`${name}  ${ammo}`);
    }
  }

  private cleanup(): void {
    this.timeText?.destroy();
    this.roomText?.destroy();
    this.hpText?.destroy();
    this.hpBarBg?.destroy();
    this.hpBarFill?.destroy();
    this.weaponText?.destroy();
    this.timeText = undefined;
    this.roomText = undefined;
    this.hpText = undefined;
    this.hpBarBg = undefined;
    this.hpBarFill = undefined;
    this.weaponText = undefined;
  }
}

export default GameHUD;
