import Phaser from "phaser";
import {
  ACCENT_PRIMARY,
  DEPTH_HUD_BG,
  DEPTH_HUD_FILL,
  DEPTH_MODAL_TEXT,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_BODY_EM,
  FONT_SIZE_CAPTION,
  FONT_SIZE_HEADING,
  MID_BLUE,
  MIDNIGHT,
  PANEL_FILL,
  TEXT_MUTED,
  TEXT_PRIMARY,
  hex,
} from "../ui/tokens";

type Page = "main" | "controls";

const MAIN_ITEMS = [
  { label: "Resume", key: "resume" },
  { label: "Controls", key: "controls" },
] as const;

// Single source of truth for the controls page. Mirrors the bindings
// in scenes/utils/Controls.ts — keep in sync if a key is added there.
const CONTROL_BINDINGS: ReadonlyArray<readonly [string, string]> = [
  ["Move", "W A S D / Arrows"],
  ["Aim", "Mouse"],
  ["Shoot", "Left mouse button"],
  ["Switch weapon", "1 / 2 / 3"],
  ["Interact / enter door", "E"],
  ["Inventory", "I"],
  ["Pause", "Esc"],
];

const PANEL_WIDTH = 480;
const PANEL_HEIGHT = 360;

/**
 * Modal pause overlay. Pauses the Game scene while open; closes via Esc
 * (or the "Resume" entry) and resumes Game. Hosts a small page stack —
 * the "Controls" entry pushes a controls page that Esc pops.
 */
export class PauseMenu extends Phaser.Scene {
  private page: Page = "main";
  private selected = 0;
  private title?: Phaser.GameObjects.Text;
  private hint?: Phaser.GameObjects.Text;
  private body: Phaser.GameObjects.Text[] = [];
  private keys?: {
    up: Phaser.Input.Keyboard.Key;
    down: Phaser.Input.Keyboard.Key;
    enter: Phaser.Input.Keyboard.Key;
    esc: Phaser.Input.Keyboard.Key;
  };

  constructor() {
    super({ key: "PauseMenu" });
  }

  create(): void {
    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;

    // Dim layer over the paused game.
    this.add
      .rectangle(cx, cy, this.scale.width, this.scale.height, MIDNIGHT, 0.55)
      .setDepth(DEPTH_HUD_BG);

    // Modal panel.
    this.add
      .rectangle(cx, cy, PANEL_WIDTH, PANEL_HEIGHT, PANEL_FILL, 0.95)
      .setStrokeStyle(2, MID_BLUE)
      .setDepth(DEPTH_HUD_FILL);

    this.renderPage();

    const kb = this.input.keyboard!;
    this.keys = {
      up: kb.addKey(Phaser.Input.Keyboard.KeyCodes.UP),
      down: kb.addKey(Phaser.Input.Keyboard.KeyCodes.DOWN),
      enter: kb.addKey(Phaser.Input.Keyboard.KeyCodes.ENTER),
      esc: kb.addKey(Phaser.Input.Keyboard.KeyCodes.ESC),
    };
    this.keys.up.on("down", () => this.move(-1));
    this.keys.down.on("down", () => this.move(1));
    this.keys.enter.on("down", () => this.activate());
    this.keys.esc.on("down", () => this.back());

    this.events.on("shutdown", this.cleanup, this);
  }

  private renderPage(): void {
    this.title?.destroy();
    this.title = undefined;
    this.hint?.destroy();
    this.hint = undefined;
    for (const t of this.body) t.destroy();
    this.body = [];

    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;

    if (this.page === "main") {
      this.title = this.add
        .text(cx, cy - 150, "Paused", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_HEADING,
          color: hex(ACCENT_PRIMARY),
        })
        .setOrigin(0.5)
        .setDepth(DEPTH_MODAL_TEXT);

      this.hint = this.add
        .text(
          cx,
          cy + 150,
          "[Up/Down] select   [Enter] confirm   [Esc] resume",
          {
            fontFamily: FONT_FAMILY,
            fontSize: FONT_SIZE_CAPTION,
            color: hex(TEXT_MUTED),
          },
        )
        .setOrigin(0.5)
        .setDepth(DEPTH_MODAL_TEXT);

      const startY = cy - 50;
      for (let i = 0; i < MAIN_ITEMS.length; i++) {
        const isSel = i === this.selected;
        const label = `${isSel ? "> " : "  "}${MAIN_ITEMS[i].label}`;
        const t = this.add
          .text(cx, startY + i * 36, label, {
            fontFamily: FONT_FAMILY,
            fontSize: FONT_SIZE_BODY_EM,
            color: hex(isSel ? ACCENT_PRIMARY : TEXT_PRIMARY),
          })
          .setOrigin(0.5)
          .setDepth(DEPTH_MODAL_TEXT);
        this.body.push(t);
      }
    } else {
      this.title = this.add
        .text(cx, cy - 150, "Controls", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_HEADING,
          color: hex(ACCENT_PRIMARY),
        })
        .setOrigin(0.5)
        .setDepth(DEPTH_MODAL_TEXT);

      this.hint = this.add
        .text(cx, cy + 150, "[Esc] back", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_CAPTION,
          color: hex(TEXT_MUTED),
        })
        .setOrigin(0.5)
        .setDepth(DEPTH_MODAL_TEXT);

      const labelX = cx - 160;
      const valueX = cx + 160;
      const startY = cy - 100;
      const rowGap = 26;
      for (let i = 0; i < CONTROL_BINDINGS.length; i++) {
        const [name, key] = CONTROL_BINDINGS[i];
        const t1 = this.add
          .text(labelX, startY + i * rowGap, name, {
            fontFamily: FONT_FAMILY,
            fontSize: FONT_SIZE_BODY,
            color: hex(TEXT_PRIMARY),
          })
          .setOrigin(0, 0)
          .setDepth(DEPTH_MODAL_TEXT);
        const t2 = this.add
          .text(valueX, startY + i * rowGap, key, {
            fontFamily: FONT_FAMILY,
            fontSize: FONT_SIZE_BODY,
            color: hex(ACCENT_PRIMARY),
          })
          .setOrigin(1, 0)
          .setDepth(DEPTH_MODAL_TEXT);
        this.body.push(t1, t2);
      }
    }
  }

  private move(delta: number): void {
    if (this.page !== "main") return;
    this.selected =
      (this.selected + delta + MAIN_ITEMS.length) % MAIN_ITEMS.length;
    this.renderPage();
  }

  private activate(): void {
    if (this.page !== "main") return;
    const item = MAIN_ITEMS[this.selected];
    if (item.key === "resume") {
      this.close();
    } else if (item.key === "controls") {
      this.page = "controls";
      this.renderPage();
    }
  }

  private back(): void {
    if (this.page === "controls") {
      this.page = "main";
      this.renderPage();
      return;
    }
    this.close();
  }

  private close(): void {
    this.scene.resume("Game");
    this.scene.stop();
  }

  private cleanup(): void {
    this.title?.destroy();
    this.title = undefined;
    this.hint?.destroy();
    this.hint = undefined;
    for (const t of this.body) t.destroy();
    this.body = [];

    if (this.keys) {
      this.keys.up.removeAllListeners();
      this.keys.down.removeAllListeners();
      this.keys.enter.removeAllListeners();
      this.keys.esc.removeAllListeners();
      this.keys = undefined;
    }
  }
}

export default PauseMenu;
