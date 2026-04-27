import Phaser from "phaser";
import { getReplaySync } from "../net/runContext";
import {
  ACCENT_PRIMARY,
  DEEP_NAVY,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_CAPTION,
  MID_BLUE,
  TEXT_MUTED,
  TEXT_PRIMARY,
  hex,
} from "../ui/tokens";

const SPEEDS = [0.25, 0.5, 1, 2, 4, 8] as const;
const BAR_HEIGHT = 36;
const BAR_BG_ALPHA = 0.7;

type SpeedButton = {
  speed: number;
  rect: Phaser.GameObjects.Rectangle;
  text: Phaser.GameObjects.Text;
};

/**
 * Persistent overlay that drives the replay engine via
 * C2S_REPLAY_CONTROL. Owns play/pause, speed selection, and restart.
 *
 * The server is authoritative — buttons fire-and-forget; we update our
 * local highlight optimistically so the moderator gets immediate visual
 * feedback. The next S2C_STATE will reflect the engine actually
 * applying the change.
 *
 * Keyboard shortcuts are scene-local (Phaser Input.Keyboard), not the
 * player's Controls facade, so the player bundle stays unaffected.
 *
 * See docs/specs/client/11-replay-viewer.md.
 */
export class ReplayControls extends Phaser.Scene {
  private playing = true;
  private currentSpeed = 1;
  private playButton?: { rect: Phaser.GameObjects.Rectangle; text: Phaser.GameObjects.Text };
  private speedButtons: SpeedButton[] = [];

  constructor() {
    super({ key: "ReplayControls" });
  }

  create(): void {
    const w = this.scale.width;
    const h = this.scale.height;

    const bar = this.add
      .rectangle(0, h - BAR_HEIGHT, w, BAR_HEIGHT, DEEP_NAVY, BAR_BG_ALPHA)
      .setOrigin(0, 0)
      .setScrollFactor(0)
      .setDepth(15000);
    bar.setInteractive(); // swallow clicks under buttons

    let x = 12;
    const cy = h - BAR_HEIGHT / 2;

    this.playButton = this.makeButton(x, cy, "⏸ Pause", () => this.togglePlay());
    x += 96;

    this.add
      .text(x, cy, "Speed", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_CAPTION,
        color: hex(TEXT_MUTED),
      })
      .setOrigin(0, 0.5)
      .setScrollFactor(0)
      .setDepth(15001);
    x += 50;

    for (const speed of SPEEDS) {
      const label = `${speed}×`;
      const btn = this.makeButton(x, cy, label, () => this.setSpeed(speed));
      this.speedButtons.push({ speed, rect: btn.rect, text: btn.text });
      x += 56;
    }

    x += 16;
    this.makeButton(x, cy, "↻ Restart", () => this.restart());

    this.add
      .text(w - 12, cy, "Space · play  [/] · speed  R · restart  Esc · exit", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_CAPTION,
        color: hex(TEXT_MUTED),
      })
      .setOrigin(1, 0.5)
      .setScrollFactor(0)
      .setDepth(15001);

    this.refreshHighlights();
    this.bindKeys();
  }

  private makeButton(
    x: number,
    cy: number,
    label: string,
    handler: () => void,
  ): { rect: Phaser.GameObjects.Rectangle; text: Phaser.GameObjects.Text } {
    const w = label.length * 9 + 16;
    const rect = this.add
      .rectangle(x, cy, w, 26, DEEP_NAVY, 0.9)
      .setStrokeStyle(1, MID_BLUE)
      .setOrigin(0, 0.5)
      .setScrollFactor(0)
      .setDepth(15001)
      .setInteractive({ useHandCursor: true });
    const text = this.add
      .text(x + w / 2, cy, label, {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(TEXT_PRIMARY),
      })
      .setOrigin(0.5)
      .setScrollFactor(0)
      .setDepth(15002);
    rect.on("pointerover", () => rect.setStrokeStyle(2, ACCENT_PRIMARY));
    rect.on("pointerout", () => this.refreshHighlights());
    rect.on("pointerdown", handler);
    return { rect, text };
  }

  private bindKeys(): void {
    if (!this.input.keyboard) return;
    const kb = this.input.keyboard;
    kb.on("keydown-SPACE", () => this.togglePlay());
    kb.on("keydown-R", () => this.restart());
    kb.on("keydown-ESC", () => {
      window.location.href = "/admin/";
    });
    kb.on("keydown-OPEN_BRACKET", () => this.cycleSpeed(-1));
    kb.on("keydown-CLOSED_BRACKET", () => this.cycleSpeed(1));
  }

  private togglePlay(): void {
    this.playing = !this.playing;
    getReplaySync().sendControl(this.playing ? "play" : "pause");
    if (this.playButton) {
      this.playButton.text.setText(this.playing ? "⏸ Pause" : "▶ Play");
    }
  }

  private setSpeed(speed: number): void {
    this.currentSpeed = speed;
    getReplaySync().sendControl("set_speed", speed);
    this.refreshHighlights();
  }

  private cycleSpeed(direction: -1 | 1): void {
    const idx = SPEEDS.indexOf(this.currentSpeed as (typeof SPEEDS)[number]);
    if (idx < 0) return;
    const next = SPEEDS[Math.max(0, Math.min(SPEEDS.length - 1, idx + direction))];
    this.setSpeed(next);
  }

  private restart(): void {
    // Restart re-enters ReplayConnecting which mints a fresh ticket and
    // reconnects. This is more reliable than sending C2S_REPLAY_CONTROL
    // {action:"restart"} when the original ticket may have aged out.
    const meta = (this.scene.get("ReplayGame") as Phaser.Scene)?.scene
      ?.settings.data as { meta?: unknown };
    this.scene.stop("ReplayGame");
    this.scene.stop("GameHUD");
    this.scene.start("ReplayConnecting", { meta: meta?.meta });
    this.scene.stop();
  }

  private refreshHighlights(): void {
    for (const b of this.speedButtons) {
      const active = b.speed === this.currentSpeed;
      b.rect.setStrokeStyle(active ? 2 : 1, active ? ACCENT_PRIMARY : MID_BLUE);
      b.text.setColor(active ? hex(ACCENT_PRIMARY) : hex(TEXT_PRIMARY));
    }
  }
}

export default ReplayControls;
