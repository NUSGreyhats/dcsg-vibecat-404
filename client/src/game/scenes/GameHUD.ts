import Phaser from "phaser";
import { getSync } from "../net/runContext";
import { PLAYER_MAX_HEALTH, ROOM_COUNT } from "../constants";
import { WEAPON_STATS, weaponFromServerId, type WeaponId } from "../weapons";
import { formatTime } from "../ui/formatters";
import {
  ACCENT_PRIMARY,
  BONE_WHITE,
  DEPTH_HUD_BG,
  DEPTH_HUD_FILL,
  FONT_FAMILY,
  FONT_SIZE_CAPTION,
  FONT_SIZE_SMALL,
  GUTTER_MEDIUM,
  HP_AMBER,
  HP_GREEN,
  HP_RED,
  HUD_TEXT_STYLE,
  MIDNIGHT,
  TEXT_MUTED,
  TEXT_PRIMARY,
  TEXT_STROKE,
  hex,
} from "../ui/tokens";

const HP_BAR_WIDTH = 200;
const HP_BAR_HEIGHT = 16;

// Splits panel dimensions. Sized for ROOM_COUNT rows of small text.
const SPLITS_ROW_HEIGHT = 18;
const SPLITS_RIGHT_GUTTER = GUTTER_MEDIUM;
const SPLITS_TOP_OFFSET = 36;

// Weapon sprites are authored at 32×16, so the icon area mirrors that
// 2:1 aspect ratio.
const WEAPON_ICON_HEIGHT = 22;
const WEAPON_ICON_WIDTH = 44;
const WEAPON_ICON_GAP = 8;

// Weapon atlases ship `idle` / `shoot` tags; the first frame is the
// idle pose we want for the static HUD icon. See Preloader anims setup.
const WEAPON_ICON_FRAME = "0";

/**
 * Heads-up display overlay. Launched alongside the Game scene. Reads state
 * directly from the sync on every tick — no input handling here.
 */
export class GameHUD extends Phaser.Scene {
  private timeText?: Phaser.GameObjects.Text;
  private roomText?: Phaser.GameObjects.Text;
  private hpBarBg?: Phaser.GameObjects.Rectangle;
  private hpBarFill?: Phaser.GameObjects.Rectangle;
  private weaponText?: Phaser.GameObjects.Text;
  private weaponIcon?: Phaser.GameObjects.Sprite;
  private splitsHeader?: Phaser.GameObjects.Text;
  private splitsRows: Phaser.GameObjects.Text[] = [];
  private currentWeaponKey?: string;
  // Once the run finishes the timer latches at the server's authoritative
  // total so it doesn't keep ticking past the final split.
  private latchedTotalMs?: number;

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

    this.buildSplitsPanel(w);

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

    // Weapon icon to the left of the weapon text. Texture is set in the
    // first update() once the player snapshot resolves the equipped slot.
    this.weaponIcon = this.add
      .sprite(0, h - 24, "__fallback_pixel")
      .setOrigin(1, 0.5)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_BG)
      .setVisible(false);

    const sync = getSync();
    sync.state.on("runComplete", this.onRunComplete, this);

    this.events.on("shutdown", this.cleanup, this);
  }

  update(): void {
    const state = getSync().state;

    if (this.timeText) {
      const t =
        this.latchedTotalMs !== undefined
          ? this.latchedTotalMs
          : state.elapsedMs ?? 0;
      this.timeText.setText(formatTime(t));
      this.timeText.setColor(
        this.latchedTotalMs !== undefined
          ? hex(ACCENT_PRIMARY)
          : hex(TEXT_PRIMARY),
      );
    }

    if (this.roomText) {
      const cur = state.currentRoom ?? 1;
      this.roomText.setText(`Room ${cur}/${ROOM_COUNT}`);
    }

    this.refreshSplits(state.splits);

    const hp = Math.max(0, state.player?.hp ?? 0);
    const maxHp = PLAYER_MAX_HEALTH;
    if (this.hpBarFill) {
      const pct = Phaser.Math.Clamp(hp / maxHp, 0, 1);
      this.hpBarFill.width = (HP_BAR_WIDTH - 4) * pct;
      const colour = pct > 0.6 ? HP_GREEN : pct > 0.3 ? HP_AMBER : HP_RED;
      this.hpBarFill.fillColor = colour;
    }

    this.refreshWeapon(state.player?.weapon_id ?? 0, state.player?.ammo ?? 0);
  }

  private buildSplitsPanel(width: number): void {
    const x = width - SPLITS_RIGHT_GUTTER;
    this.splitsHeader = this.add
      .text(x, SPLITS_TOP_OFFSET, "// splits", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_CAPTION,
        color: hex(TEXT_MUTED),
      })
      .setOrigin(1, 0)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_BG)
      .setVisible(false);

    // Pre-allocate one text object per possible room so we never thrash
    // GameObjects on each transition; visibility is toggled in update().
    const startY = SPLITS_TOP_OFFSET + 20;
    for (let i = 0; i < ROOM_COUNT; i++) {
      const row = this.add
        .text(x, startY + i * SPLITS_ROW_HEIGHT, "", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_SMALL,
          color: hex(TEXT_PRIMARY),
          stroke: hex(TEXT_STROKE),
          strokeThickness: 3,
        })
        .setOrigin(1, 0)
        .setScrollFactor(0)
        .setDepth(DEPTH_HUD_BG)
        .setVisible(false);
      this.splitsRows.push(row);
    }
  }

  private refreshSplits(splits: ReadonlyArray<{ room_index: number; time_ms: number }>): void {
    if (!this.splitsHeader) return;
    const visible = splits.length > 0;
    this.splitsHeader.setVisible(visible);
    for (let i = 0; i < this.splitsRows.length; i++) {
      const row = this.splitsRows[i];
      const split = splits[i];
      if (split) {
        row.setText(`Rm ${split.room_index}  ${formatTime(split.time_ms)}`);
        row.setVisible(true);
      } else {
        row.setVisible(false);
      }
    }
  }

  private refreshWeapon(weaponId: number, ammo: number): void {
    if (!this.weaponText || !this.weaponIcon) return;

    const id: WeaponId = weaponFromServerId(weaponId);
    const stats = WEAPON_STATS[id];
    const name = stats?.displayName ?? "—";
    this.weaponText.setText(`${name}  ${ammo}`);

    const spriteKey = stats?.spriteKey;
    if (!spriteKey || !this.textures.exists(spriteKey)) {
      this.weaponIcon.setVisible(false);
      return;
    }
    if (this.currentWeaponKey !== spriteKey) {
      this.weaponIcon.setTexture(spriteKey, WEAPON_ICON_FRAME);
      this.weaponIcon.setDisplaySize(WEAPON_ICON_WIDTH, WEAPON_ICON_HEIGHT);
      this.currentWeaponKey = spriteKey;
    }
    this.weaponIcon.setVisible(true);

    // Right-edge of the icon sits to the left of the weapon text. Polled
    // each frame because the text width changes with the weapon name.
    const textLeft = this.weaponText.x - this.weaponText.displayWidth;
    this.weaponIcon.setX(textLeft - WEAPON_ICON_GAP);
  }

  private onRunComplete(totalMs: number): void {
    this.latchedTotalMs = totalMs;
  }

  private cleanup(): void {
    const sync = getSync();
    sync.state.off("runComplete", this.onRunComplete, this);

    this.timeText?.destroy();
    this.roomText?.destroy();
    this.hpBarBg?.destroy();
    this.hpBarFill?.destroy();
    this.weaponText?.destroy();
    this.weaponIcon?.destroy();
    this.splitsHeader?.destroy();
    for (const r of this.splitsRows) r.destroy();
    this.splitsRows = [];
    this.timeText = undefined;
    this.roomText = undefined;
    this.hpBarBg = undefined;
    this.hpBarFill = undefined;
    this.weaponText = undefined;
    this.weaponIcon = undefined;
    this.splitsHeader = undefined;
    this.currentWeaponKey = undefined;
    this.latchedTotalMs = undefined;
  }
}

export default GameHUD;
