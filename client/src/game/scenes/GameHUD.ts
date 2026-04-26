import Phaser from "phaser";
import { getActiveState } from "../net/runContext";
import { PLAYER_MAX_HEALTH, ROOM_COUNT } from "../constants";
import { getStoredPB, isTimingsVisible } from "../prefs";
import type { SplitData } from "../../net/packet";
import { WEAPON_STATS, weaponFromServerId, type WeaponId } from "../weapons";
import { formatTime } from "../ui/formatters";
import {
  ACCENT_PRIMARY,
  BONE_WHITE,
  COIN_GOLD,
  DEPTH_HUD_BG,
  DEPTH_HUD_FILL,
  ERROR,
  FONT_FAMILY,
  FONT_SIZE_BODY_EM,
  FONT_SIZE_CAPTION,
  FONT_SIZE_MICRO,
  FONT_SIZE_SMALL,
  GUTTER_MEDIUM,
  HP_AMBER,
  HP_GREEN,
  HP_RED,
  HUD_TEXT_STYLE,
  MIDNIGHT,
  STROKE_THICKNESS_HUD,
  SUCCESS,
  TEXT_SECONDARY,
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
// Ghost-delta column sits to the LEFT of the split-time text. Width is
// fixed so rows don't reflow as the time number changes width.
const SPLITS_DELTA_GAP = 8;

// Weapon sprites are authored at 32×16, so the icon area mirrors that
// 2:1 aspect ratio.
const WEAPON_ICON_HEIGHT = 22;
const WEAPON_ICON_WIDTH = 44;
const WEAPON_ICON_GAP = 8;

// Weapon atlases ship `idle` / `shoot` tags; the first frame is the
// idle pose we want for the static HUD icon. See Preloader anims setup.
const WEAPON_ICON_FRAME = "0";

// Background panel padding around each cluster of HUD elements.
const PANEL_PAD_X = 8;
const PANEL_PAD_Y = 4;
const PANEL_ALPHA = 0.45;
const TOAST_Y = 74;

/**
 * Heads-up display overlay. Launched alongside the Game scene. Reads state
 * directly from the sync on every tick — no input handling here.
 */
export class GameHUD extends Phaser.Scene {
  private timeText?: Phaser.GameObjects.Text;
  private timePanel?: Phaser.GameObjects.Rectangle;
  private roomText?: Phaser.GameObjects.Text;
  private coinText?: Phaser.GameObjects.Text;
  private hpBarBg?: Phaser.GameObjects.Rectangle;
  private hpBarFill?: Phaser.GameObjects.Rectangle;
  private weaponText?: Phaser.GameObjects.Text;
  private weaponIcon?: Phaser.GameObjects.Sprite;
  private weaponPanel?: Phaser.GameObjects.Rectangle;
  private splitsHeader?: Phaser.GameObjects.Text;
  private splitsPanel?: Phaser.GameObjects.Rectangle;
  private splitsRows: Phaser.GameObjects.Text[] = [];
  private splitsDeltas: Phaser.GameObjects.Text[] = [];
  private currentWeaponKey?: string;
  private toastBg?: Phaser.GameObjects.Rectangle;
  private toastText?: Phaser.GameObjects.Text;
  // Once the run finishes the timer latches at the server's authoritative
  // total so it doesn't keep ticking past the final split.
  private latchedTotalMs?: number;
  // Snapshot of the user's PB splits captured at scene-start. Compared
  // per-row in refreshSplits to render the dim ghost delta.
  private pbSplits: SplitData[] = [];
  // Controls only the speedrun-related HUD (timer + splits column). HP,
  // weapon, and room counter ignore this — toggling Hide Timings doesn't
  // strip out information you'd want to keep visible mid-run.
  private timingsVisible = true;

  constructor() {
    super({ key: "GameHUD" });
  }

  create(): void {
    const w = this.scale.width;
    const h = this.scale.height;

    this.pbSplits = getStoredPB()?.splits ?? [];
    this.timingsVisible = isTimingsVisible();

    // -- Top-left: timer -- (panel built around the text after we know its size)
    this.timeText = this.add
      .text(GUTTER_MEDIUM, 10, "00:00.000", HUD_TEXT_STYLE)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_FILL);
    this.timePanel = this.makePanelBehind(this.timeText);

    // -- Top-right: current room -- (no backdrop; the splits panel
    // immediately below already provides one for this column).
    this.roomText = this.add
      .text(w - GUTTER_MEDIUM, 10, `Room 1/${ROOM_COUNT}`, HUD_TEXT_STYLE)
      .setOrigin(1, 0)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_FILL);

    // Coin counter sits in the bottom-left, just above the HP bar.
    // Top-right is reserved for the splits panel, so the coin readout
    // lives next to the other "stat" widget. Shop opens with "B" — see
    // ShopMenu.
    this.coinText = this.add
      .text(GUTTER_MEDIUM, h - 32, "$ 0", {
        ...HUD_TEXT_STYLE,
        color: hex(COIN_GOLD),
      })
      .setOrigin(0, 1)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_FILL);

    this.buildSplitsPanel(w);

    // -- Bottom-left: HP --
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

    // -- Bottom-right: weapon name + ammo + icon --
    this.weaponText = this.add
      .text(w - GUTTER_MEDIUM, h - 24, "Pistol  —", HUD_TEXT_STYLE)
      .setOrigin(1, 0.5)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_FILL);

    this.weaponIcon = this.add
      .sprite(0, h - 24, "__fallback_pixel")
      .setOrigin(1, 0.5)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_FILL)
      .setVisible(false);

    // Single panel covering icon + text region. Sized in refreshWeapon.
    this.weaponPanel = this.add
      .rectangle(w - GUTTER_MEDIUM, h - 24, 1, 1, MIDNIGHT, PANEL_ALPHA)
      .setOrigin(1, 0.5)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_BG);

    const state = getActiveState();
    state.on("runComplete", this.onRunComplete, this);

    this.events.on("shutdown", this.cleanup, this);
    // The PauseMenu emits this on the cross-scene game registry so the HUD
    // can react to a toggle without needing a back-pointer.
    this.game.events.on(
      "timingsVisibilityChanged",
      this.onTimingsVisibilityChanged,
      this,
    );
    this.game.events.on("doorUnlockedToast", this.onDoorUnlockedToast, this);

    this.applyTimingsVisibility();
  }

  update(): void {
    const state = getActiveState();

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
      this.timeText.setVisible(this.timingsVisible);
      this.fitPanelTo(this.timePanel, this.timeText);
      this.timePanel?.setVisible(this.timingsVisible);
    }

    if (this.roomText) {
      const cur = state.currentRoom ?? 1;
      this.roomText.setText(`Room ${cur}/${ROOM_COUNT}`);
    }

    if (this.coinText) {
      this.coinText.setText(`$ ${state.player?.coins ?? 0}`);
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
      .setDepth(DEPTH_HUD_FILL)
      .setVisible(false);

    // Background panel covers header + all rows. Sized in refreshSplits
    // once we know how many rows are populated.
    this.splitsPanel = this.add
      .rectangle(x, SPLITS_TOP_OFFSET, 1, 1, MIDNIGHT, PANEL_ALPHA)
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
        .setDepth(DEPTH_HUD_FILL)
        .setVisible(false);
      this.splitsRows.push(row);

      // Delta text sits to the LEFT of the row, anchored by the row's
      // left edge each frame.
      const delta = this.add
        .text(x, startY + i * SPLITS_ROW_HEIGHT + 2, "", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_MICRO,
          color: hex(TEXT_MUTED),
          stroke: hex(TEXT_STROKE),
          strokeThickness: 2,
        })
        .setOrigin(1, 0)
        .setScrollFactor(0)
        .setDepth(DEPTH_HUD_FILL)
        .setVisible(false);
      this.splitsDeltas.push(delta);
    }
  }

  private refreshSplits(splits: ReadonlyArray<SplitData>): void {
    if (!this.splitsHeader || !this.splitsPanel || !this.roomText) return;
    // Splits header / rows / deltas are speedrun timings, gated by the
    // toggle. The panel itself is always visible because it backs the
    // room counter (which is not gated).
    const showRows = splits.length > 0 && this.timingsVisible;
    this.splitsHeader.setVisible(showRows);
    this.splitsPanel.setVisible(true);

    let maxRowWidth = this.roomText.displayWidth;
    if (this.timingsVisible) {
      maxRowWidth = Math.max(maxRowWidth, this.splitsHeader.displayWidth);
    }
    let lastRowBottom = this.roomText.y + this.roomText.displayHeight;
    if (showRows) {
      lastRowBottom = Math.max(
        lastRowBottom,
        this.splitsHeader.y + this.splitsHeader.displayHeight,
      );
    }

    for (let i = 0; i < this.splitsRows.length; i++) {
      const row = this.splitsRows[i];
      const delta = this.splitsDeltas[i];
      const split = splits[i];
      if (split && this.timingsVisible) {
        row.setText(`Rm ${split.room_index}  ${formatTime(split.time_ms)}`);
        row.setVisible(true);

        const pb = this.pbSplits[i];
        if (pb && pb.room_index === split.room_index) {
          const diff = split.time_ms - pb.time_ms;
          delta.setText(formatDelta(diff));
          delta.setColor(diff <= 0 ? hex(SUCCESS) : hex(ERROR));
          delta.setAlpha(0.75);
          // Position delta just left of the split row's left edge.
          delta.setX(row.x - row.displayWidth - SPLITS_DELTA_GAP);
          delta.setVisible(true);
          maxRowWidth = Math.max(
            maxRowWidth,
            row.displayWidth + SPLITS_DELTA_GAP + delta.displayWidth,
          );
        } else {
          delta.setVisible(false);
          maxRowWidth = Math.max(maxRowWidth, row.displayWidth);
        }
        lastRowBottom = row.y + row.displayHeight;
      } else {
        row.setVisible(false);
        delta.setVisible(false);
      }
    }

    const top = this.roomText.y - PANEL_PAD_Y;
    const w = maxRowWidth + PANEL_PAD_X * 2;
    const h = lastRowBottom - top + PANEL_PAD_Y;
    this.splitsPanel.setSize(w, h);
    this.splitsPanel.setPosition(this.roomText.x + PANEL_PAD_X, top);
  }

  private refreshWeapon(weaponId: number, ammo: number): void {
    if (!this.weaponText || !this.weaponIcon || !this.weaponPanel) return;

    const id: WeaponId = weaponFromServerId(weaponId);
    const stats = WEAPON_STATS[id];
    const name = stats?.displayName ?? "—";
    this.weaponText.setText(`${name}  ${ammo}`);

    const spriteKey = stats?.spriteKey;
    let iconShown = false;
    if (spriteKey && this.textures.exists(spriteKey)) {
      if (this.currentWeaponKey !== spriteKey) {
        this.weaponIcon.setTexture(spriteKey, WEAPON_ICON_FRAME);
        this.weaponIcon.setDisplaySize(WEAPON_ICON_WIDTH, WEAPON_ICON_HEIGHT);
        this.currentWeaponKey = spriteKey;
      }
      this.weaponIcon.setVisible(true);
      iconShown = true;
      // Right-edge of the icon sits to the left of the weapon text.
      const textLeft = this.weaponText.x - this.weaponText.displayWidth;
      this.weaponIcon.setX(textLeft - WEAPON_ICON_GAP);
    } else {
      this.weaponIcon.setVisible(false);
    }

    // Panel wraps icon (if shown) + text. Anchored to the text's right edge.
    const totalWidth = iconShown
      ? WEAPON_ICON_WIDTH + WEAPON_ICON_GAP + this.weaponText.displayWidth
      : this.weaponText.displayWidth;
    const totalHeight = Math.max(
      WEAPON_ICON_HEIGHT,
      this.weaponText.displayHeight,
    );
    this.weaponPanel.setSize(totalWidth + PANEL_PAD_X * 2, totalHeight + PANEL_PAD_Y * 2);
    this.weaponPanel.setPosition(
      this.weaponText.x - PANEL_PAD_X * (1 - 2 * this.weaponPanel.originX),
      this.weaponText.y - PANEL_PAD_Y * (1 - 2 * this.weaponPanel.originY),
    );
    this.weaponPanel.setVisible(true);
  }

  private makePanelBehind(text: Phaser.GameObjects.Text): Phaser.GameObjects.Rectangle {
    // Origin matches the text so the panel grows in the same direction
    // when the text width changes (top-right HUD anchors right-edge etc).
    const panel = this.add
      .rectangle(text.x, text.y, 1, 1, MIDNIGHT, PANEL_ALPHA)
      .setOrigin(text.originX, text.originY)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_BG);
    this.fitPanelTo(panel, text);
    return panel;
  }

  private fitPanelTo(
    panel: Phaser.GameObjects.Rectangle | undefined,
    text: Phaser.GameObjects.Text,
  ): void {
    if (!panel) return;
    panel.setSize(
      text.displayWidth + PANEL_PAD_X * 2,
      text.displayHeight + PANEL_PAD_Y * 2,
    );
    // Recenter origin offset: shift panel by pad in the direction opposite
    // to the origin so the text ends up centered within the padded panel.
    panel.setPosition(
      text.x - PANEL_PAD_X * (1 - 2 * text.originX),
      text.y - PANEL_PAD_Y * (1 - 2 * text.originY),
    );
  }

  private onTimingsVisibilityChanged(visible: boolean): void {
    this.timingsVisible = visible;
    this.applyTimingsVisibility();
  }

  private applyTimingsVisibility(): void {
    // Only the run-timer + splits column hide. HP / weapon / room
    // counter / splits backdrop stay visible — refreshSplits will
    // re-show the per-row text and resize the panel on the next tick.
    const v = this.timingsVisible;
    this.timeText?.setVisible(v);
    this.timePanel?.setVisible(v);
    this.splitsHeader?.setVisible(false);
    for (const r of this.splitsRows) r.setVisible(false);
    for (const d of this.splitsDeltas) d.setVisible(false);
  }

  private onRunComplete(_outcome: string, totalMs: number): void {
    this.latchedTotalMs = totalMs;
  }

  private onDoorUnlockedToast(): void {
    this.showToast("LEVEL CLEAR - DOOR UNLOCKED");
  }

  private showToast(message: string): void {
    const cx = this.scale.width / 2;
    if (this.toastText) this.tweens.killTweensOf(this.toastText);
    if (this.toastBg) this.tweens.killTweensOf(this.toastBg);
    this.toastText?.destroy();
    this.toastBg?.destroy();

    this.toastText = this.add
      .text(cx, TOAST_Y, message, {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY_EM,
        color: hex(BONE_WHITE),
        stroke: hex(TEXT_STROKE),
        strokeThickness: STROKE_THICKNESS_HUD,
      })
      .setOrigin(0.5)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_FILL);

    this.toastBg = this.add
      .rectangle(
        cx,
        TOAST_Y,
        this.toastText.displayWidth + PANEL_PAD_X * 4,
        this.toastText.displayHeight + PANEL_PAD_Y * 4,
        MIDNIGHT,
        0.74,
      )
      .setOrigin(0.5)
      .setStrokeStyle(1, TEXT_SECONDARY, 0.95)
      .setScrollFactor(0)
      .setDepth(DEPTH_HUD_BG);

    this.toastText.setAlpha(0);
    this.toastBg.setAlpha(0);
    this.tweens.add({
      targets: [this.toastText, this.toastBg],
      alpha: 1,
      y: TOAST_Y - 8,
      duration: 160,
      ease: "Cubic.easeOut",
      hold: 1500,
      yoyo: true,
      onComplete: () => {
        this.toastText?.destroy();
        this.toastBg?.destroy();
        this.toastText = undefined;
        this.toastBg = undefined;
      },
    });
  }

  private cleanup(): void {
    const state = getActiveState();
    state.off("runComplete", this.onRunComplete, this);
    this.game.events.off(
      "timingsVisibilityChanged",
      this.onTimingsVisibilityChanged,
      this,
    );
    this.game.events.off("doorUnlockedToast", this.onDoorUnlockedToast, this);

    this.timeText?.destroy();
    this.timePanel?.destroy();
    this.roomText?.destroy();
    this.coinText?.destroy();
    this.hpBarBg?.destroy();
    this.hpBarFill?.destroy();
    this.weaponText?.destroy();
    this.weaponIcon?.destroy();
    this.weaponPanel?.destroy();
    if (this.toastText) this.tweens.killTweensOf(this.toastText);
    if (this.toastBg) this.tweens.killTweensOf(this.toastBg);
    this.toastText?.destroy();
    this.toastBg?.destroy();
    this.splitsHeader?.destroy();
    this.splitsPanel?.destroy();
    for (const r of this.splitsRows) r.destroy();
    for (const d of this.splitsDeltas) d.destroy();
    this.splitsRows = [];
    this.splitsDeltas = [];
    this.timeText = undefined;
    this.timePanel = undefined;
    this.roomText = undefined;
    this.coinText = undefined;
    this.hpBarBg = undefined;
    this.hpBarFill = undefined;
    this.weaponText = undefined;
    this.weaponIcon = undefined;
    this.weaponPanel = undefined;
    this.splitsHeader = undefined;
    this.splitsPanel = undefined;
    this.currentWeaponKey = undefined;
    this.latchedTotalMs = undefined;
    this.toastText = undefined;
    this.toastBg = undefined;
  }
}

function formatDelta(diffMs: number): string {
  const sign = diffMs <= 0 ? "−" : "+";
  const abs = Math.abs(diffMs);
  const mins = Math.floor(abs / 60000);
  const secs = (abs - mins * 60000) / 1000;
  if (mins > 0) {
    return `${sign}${mins}:${secs.toFixed(2).padStart(5, "0")}`;
  }
  return `${sign}${secs.toFixed(2)}`;
}

export default GameHUD;
