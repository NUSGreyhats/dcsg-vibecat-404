import Phaser from "phaser";
import { getSync } from "../net/runContext";
import { PLAYER_MAX_HEALTH, ROOM_COUNT } from "../constants";
import { WEAPON_STATS, ALL_WEAPON_IDS } from "../weapons";

const HP_BAR_WIDTH = 200;
const HP_BAR_HEIGHT = 16;

function formatTime(ms: number): string {
  const totalMs = Math.max(0, Math.floor(ms));
  const minutes = Math.floor(totalMs / 60000);
  const seconds = Math.floor((totalMs % 60000) / 1000);
  const millis = totalMs % 1000;
  const mm = String(minutes).padStart(2, "0");
  const ss = String(seconds).padStart(2, "0");
  const mmm = String(millis).padStart(3, "0");
  return `${mm}:${ss}.${mmm}`;
}

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

    const textStyle: Phaser.Types.GameObjects.Text.TextStyle = {
      fontFamily: "monospace",
      fontSize: "20px",
      color: "#ffffff",
      stroke: "#000000",
      strokeThickness: 3,
    };

    this.timeText = this.add
      .text(12, 10, "00:00.000", textStyle)
      .setScrollFactor(0)
      .setDepth(1000);

    this.roomText = this.add
      .text(w - 12, 10, `Room 1/${ROOM_COUNT}`, textStyle)
      .setOrigin(1, 0)
      .setScrollFactor(0)
      .setDepth(1000);

    this.hpText = this.add
      .text(12, h - 44, "HP 100/100", textStyle)
      .setScrollFactor(0)
      .setDepth(1000);

    this.hpBarBg = this.add
      .rectangle(12, h - 20, HP_BAR_WIDTH, HP_BAR_HEIGHT, 0x000000, 0.6)
      .setOrigin(0, 0.5)
      .setStrokeStyle(1, 0xffffff)
      .setScrollFactor(0)
      .setDepth(1000);

    this.hpBarFill = this.add
      .rectangle(12, h - 20, HP_BAR_WIDTH, HP_BAR_HEIGHT - 4, 0x44cc44)
      .setOrigin(0, 0.5)
      .setScrollFactor(0)
      .setDepth(1001);

    this.weaponText = this.add
      .text(w - 12, h - 24, "Pistol  —", textStyle)
      .setOrigin(1, 0.5)
      .setScrollFactor(0)
      .setDepth(1000);

    this.events.on("shutdown", this.cleanup, this);
  }

  update(): void {
    const state = getSync().state;

    // Time
    if (this.timeText) {
      this.timeText.setText(formatTime(state.elapsedMs ?? 0));
    }

    // Room
    if (this.roomText) {
      const cur = state.currentRoom ?? 1;
      this.roomText.setText(`Room ${cur}/${ROOM_COUNT}`);
    }

    // HP
    const hp = Math.max(0, state.player?.hp ?? 0);
    const maxHp = PLAYER_MAX_HEALTH;
    if (this.hpText) this.hpText.setText(`HP ${hp}/${maxHp}`);
    if (this.hpBarFill) {
      const pct = Phaser.Math.Clamp(hp / maxHp, 0, 1);
      this.hpBarFill.width = (HP_BAR_WIDTH - 4) * pct;
      // shift colour as health drops
      const colour = pct > 0.6 ? 0x44cc44 : pct > 0.3 ? 0xddaa22 : 0xcc3333;
      this.hpBarFill.fillColor = colour;
    }

    // Weapon + ammo
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
