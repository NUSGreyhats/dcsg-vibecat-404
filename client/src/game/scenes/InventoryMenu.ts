import Phaser from "phaser";
import { getSync } from "../net/runContext";
import {
  ACCENT_PRIMARY,
  DEPTH_HUD_BG,
  DEPTH_HUD_FILL,
  DEPTH_MODAL_TEXT,
  FONT_FAMILY,
  FONT_SIZE_BODY_EM,
  FONT_SIZE_CAPTION,
  FONT_SIZE_HEADING,
  MID_BLUE,
  MIDNIGHT,
  PANEL_FILL,
  TEXT_DIM,
  TEXT_MUTED,
  TEXT_PRIMARY,
  hex,
} from "../ui/tokens";

/**
 * Modal inventory overlay. Pauses the Game scene while active.
 *
 * - Arrow keys: move selection.
 * - Enter: use the selected item (sendUseItem), then close.
 * - Esc / I: cancel, close.
 */
export class InventoryMenu extends Phaser.Scene {
  private panel?: Phaser.GameObjects.Rectangle;
  private title?: Phaser.GameObjects.Text;
  private hint?: Phaser.GameObjects.Text;
  private emptyText?: Phaser.GameObjects.Text;
  private itemTexts: Phaser.GameObjects.Text[] = [];
  private items: string[] = [];
  private selected = 0;
  private keys?: {
    up: Phaser.Input.Keyboard.Key;
    down: Phaser.Input.Keyboard.Key;
    enter: Phaser.Input.Keyboard.Key;
    esc: Phaser.Input.Keyboard.Key;
    i: Phaser.Input.Keyboard.Key;
  };

  constructor() {
    super({ key: "InventoryMenu" });
  }

  create(): void {
    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;

    this.add
      .rectangle(cx, cy, this.scale.width, this.scale.height, MIDNIGHT, 0.55)
      .setDepth(DEPTH_HUD_BG);

    this.panel = this.add
      .rectangle(cx, cy, 480, 360, PANEL_FILL, 0.95)
      .setStrokeStyle(2, MID_BLUE)
      .setDepth(DEPTH_HUD_FILL);

    this.title = this.add
      .text(cx, cy - 150, "Inventory", {
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
        "[Up/Down] select   [Enter] use   [Esc/I] close",
        {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_CAPTION,
          color: hex(TEXT_MUTED),
        },
      )
      .setOrigin(0.5)
      .setDepth(DEPTH_MODAL_TEXT);

    this.items = getSync().state.player?.inventory?.slice() ?? [];

    if (this.items.length === 0) {
      this.emptyText = this.add
        .text(cx, cy, "(empty)", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_BODY_EM,
          color: hex(TEXT_DIM),
        })
        .setOrigin(0.5)
        .setDepth(DEPTH_MODAL_TEXT);
    } else {
      this.renderItems(cx, cy);
    }

    const kb = this.input.keyboard!;
    this.keys = {
      up: kb.addKey(Phaser.Input.Keyboard.KeyCodes.UP, false),
      down: kb.addKey(Phaser.Input.Keyboard.KeyCodes.DOWN, false),
      enter: kb.addKey(Phaser.Input.Keyboard.KeyCodes.ENTER, false),
      esc: kb.addKey(Phaser.Input.Keyboard.KeyCodes.ESC, false),
      i: kb.addKey(Phaser.Input.Keyboard.KeyCodes.I, false),
    };

    this.keys.up.on("down", () => this.move(-1));
    this.keys.down.on("down", () => this.move(1));
    this.keys.enter.on("down", () => this.useSelected());
    this.keys.esc.on("down", () => this.close());
    this.keys.i.on("down", () => this.close());

    this.events.on("shutdown", this.cleanup, this);
  }

  private renderItems(cx: number, cy: number): void {
    for (const t of this.itemTexts) t.destroy();
    this.itemTexts = [];

    const startY = cy - 70;
    for (let i = 0; i < this.items.length; i++) {
      const isSel = i === this.selected;
      const prefix = isSel ? "> " : "  ";
      const txt = this.add
        .text(cx, startY + i * 28, `${prefix}${this.items[i]}`, {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_BODY_EM,
          color: hex(isSel ? ACCENT_PRIMARY : TEXT_PRIMARY),
        })
        .setOrigin(0.5)
        .setDepth(DEPTH_MODAL_TEXT);
      this.itemTexts.push(txt);
    }
  }

  private move(delta: number): void {
    if (this.items.length === 0) return;
    this.selected =
      (this.selected + delta + this.items.length) % this.items.length;
    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;
    this.renderItems(cx, cy);
  }

  private useSelected(): void {
    if (this.items.length === 0) {
      this.close();
      return;
    }
    const name = this.items[this.selected];
    getSync().sendUseItem(name);
    this.close();
  }

  private close(): void {
    this.scene.resume("Game");
    this.scene.stop();
  }

  private cleanup(): void {
    for (const t of this.itemTexts) t.destroy();
    this.itemTexts = [];
    this.panel?.destroy();
    this.title?.destroy();
    this.hint?.destroy();
    this.emptyText?.destroy();
    this.panel = undefined;
    this.title = undefined;
    this.hint = undefined;
    this.emptyText = undefined;

    if (this.keys) {
      this.keys.up.removeAllListeners();
      this.keys.down.removeAllListeners();
      this.keys.enter.removeAllListeners();
      this.keys.esc.removeAllListeners();
      this.keys.i.removeAllListeners();
      this.keys = undefined;
    }
  }
}

export default InventoryMenu;
