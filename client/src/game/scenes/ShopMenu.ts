import Phaser from "phaser";
import { getSync } from "../net/runContext";
import {
  ACCENT_PRIMARY,
  COIN_GOLD,
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

type ShopOffer = {
  id: number;
  name: string;
  cost: number;
  blurb: string;
};

// Mirrors server/internal/game/shop.go:ShopCatalog. IDs and costs must
// match the server. Display names/blurbs are client-only flavor.
const SHOP_CATALOG: ShopOffer[] = [
  { id: 1, name: "Ammo",      cost: 5,  blurb: "Refill all weapons" },
  { id: 2, name: "Speed",     cost: 10, blurb: "+10% move speed" },
  { id: 3, name: "Fire Rate", cost: 15, blurb: "+10% fire rate" },
  { id: 4, name: "Damage",    cost: 20, blurb: "+10% damage" },
];

// Panel needs to fit the longest formatted row at FONT_SIZE_BODY_EM (20px
// monospace). Longest label is ~36 chars (~430px wide); 560 wide leaves
// comfortable padding.
const PANEL_WIDTH = 560;
const PANEL_HEIGHT = 380;
// Offset from panel center to the left edge of each row's text — keeps the
// rows lined up inside the panel with ~24px of inner padding.
const ROW_X_OFFSET = PANEL_WIDTH / 2 - 24;

/**
 * Modal shop overlay. Pauses the Game scene while active.
 *
 * - Up/Down: move selection.
 * - Enter: purchase the highlighted offer (if affordable).
 * - Esc / B: close.
 */
export class ShopMenu extends Phaser.Scene {
  private panel?: Phaser.GameObjects.Rectangle;
  private title?: Phaser.GameObjects.Text;
  private hint?: Phaser.GameObjects.Text;
  private coinText?: Phaser.GameObjects.Text;
  private rowTexts: Phaser.GameObjects.Text[] = [];
  private selected = 0;
  private keys?: {
    up: Phaser.Input.Keyboard.Key;
    down: Phaser.Input.Keyboard.Key;
    enter: Phaser.Input.Keyboard.Key;
    esc: Phaser.Input.Keyboard.Key;
    b: Phaser.Input.Keyboard.Key;
  };
  private returnTo = "Game";

  constructor() {
    super({ key: "ShopMenu" });
  }

  init(data?: { returnTo?: string }): void {
    this.returnTo = data?.returnTo ?? "Game";
  }

  create(): void {
    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;

    this.add
      .rectangle(cx, cy, this.scale.width, this.scale.height, MIDNIGHT, 0.55)
      .setDepth(DEPTH_HUD_BG);

    this.panel = this.add
      .rectangle(cx, cy, PANEL_WIDTH, PANEL_HEIGHT, PANEL_FILL, 0.95)
      .setStrokeStyle(2, MID_BLUE)
      .setDepth(DEPTH_HUD_FILL);

    this.title = this.add
      .text(cx, cy - 150, "Shop", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_HEADING,
        color: hex(ACCENT_PRIMARY),
      })
      .setOrigin(0.5)
      .setDepth(DEPTH_MODAL_TEXT);

    this.coinText = this.add
      .text(cx, cy - 110, "$ 0", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY_EM,
        color: hex(COIN_GOLD),
      })
      .setOrigin(0.5)
      .setDepth(DEPTH_MODAL_TEXT);

    this.hint = this.add
      .text(
        cx,
        cy + 150,
        "[Up/Down] select   [Enter] buy   [Esc/B] close",
        {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_CAPTION,
          color: hex(TEXT_MUTED),
        },
      )
      .setOrigin(0.5)
      .setDepth(DEPTH_MODAL_TEXT);

    this.buildRows();

    const kb = this.input.keyboard!;
    this.keys = {
      up: kb.addKey(Phaser.Input.Keyboard.KeyCodes.UP, false),
      down: kb.addKey(Phaser.Input.Keyboard.KeyCodes.DOWN, false),
      enter: kb.addKey(Phaser.Input.Keyboard.KeyCodes.ENTER, false),
      esc: kb.addKey(Phaser.Input.Keyboard.KeyCodes.ESC, false),
      b: kb.addKey(Phaser.Input.Keyboard.KeyCodes.B, false),
    };

    this.keys.up.on("down", () => this.move(-1));
    this.keys.down.on("down", () => this.move(1));
    this.keys.enter.on("down", () => this.buySelected());
    this.keys.esc.on("down", () => this.close());
    this.keys.b.on("down", () => this.close());

    this.events.on("shutdown", this.cleanup, this);
  }

  update(): void {
    const player = getSync().state.player;
    const coins = player?.coins ?? 0;
    if (this.coinText) {
      this.coinText.setText(`$ ${coins}`);
    }
    this.refreshRows(coins, player);
  }

  private buildRows(): void {
    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;
    for (const t of this.rowTexts) t.destroy();
    this.rowTexts = [];

    const startY = cy - 60;
    for (let i = 0; i < SHOP_CATALOG.length; i++) {
      const txt = this.add
        .text(cx - ROW_X_OFFSET, startY + i * 32, "", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_BODY_EM,
          color: hex(TEXT_PRIMARY),
        })
        .setOrigin(0, 0.5)
        .setDepth(DEPTH_MODAL_TEXT);
      this.rowTexts.push(txt);
    }

    const player = getSync().state.player;
    this.refreshRows(player?.coins ?? 0, player);
  }

  private refreshRows(
    coins: number,
    player: ReturnType<typeof getSync>["state"]["player"],
  ): void {
    for (let i = 0; i < this.rowTexts.length; i++) {
      const offer = SHOP_CATALOG[i];
      this.rowTexts[i].setText(this.labelFor(i, offer, player));
      this.rowTexts[i].setColor(this.colorFor(i, offer.cost, coins));
    }
  }

  private labelFor(
    index: number,
    offer: ShopOffer,
    player: ReturnType<typeof getSync>["state"]["player"],
  ): string {
    const prefix = index === this.selected ? "> " : "  ";
    // Ammo (id=1) is a one-shot refill — no stack count to display.
    const tail = offer.id === 1 ? "" : `  x${stacksFor(offer.id, player)}`;
    const cost = `$${offer.cost}`.padEnd(4);
    return `${prefix}${offer.name.padEnd(10)} ${cost}  ${offer.blurb}${tail}`;
  }

  private colorFor(index: number, cost: number, coins: number): string {
    if (index === this.selected) return hex(ACCENT_PRIMARY);
    if (cost > coins) return hex(TEXT_DIM);
    return hex(TEXT_PRIMARY);
  }

  private move(delta: number): void {
    this.selected =
      (this.selected + delta + SHOP_CATALOG.length) % SHOP_CATALOG.length;
    const player = getSync().state.player;
    this.refreshRows(player?.coins ?? 0, player);
  }

  private buySelected(): void {
    const offer = SHOP_CATALOG[this.selected];
    const coins = getSync().state.player?.coins ?? 0;
    if (offer.cost > coins) return; // server would reject anyway
    getSync().sendShopPurchase(offer.id);
  }

  private close(): void {
    this.scene.resume(this.returnTo);
    this.scene.stop();
  }

  private cleanup(): void {
    for (const t of this.rowTexts) t.destroy();
    this.rowTexts = [];
    this.panel?.destroy();
    this.title?.destroy();
    this.hint?.destroy();
    this.coinText?.destroy();
    this.panel = undefined;
    this.title = undefined;
    this.hint = undefined;
    this.coinText = undefined;

    if (this.keys) {
      this.keys.up.removeAllListeners();
      this.keys.down.removeAllListeners();
      this.keys.enter.removeAllListeners();
      this.keys.esc.removeAllListeners();
      this.keys.b.removeAllListeners();
      this.keys = undefined;
    }
  }
}

function stacksFor(
  itemId: number,
  player:
    | {
        speed_stacks?: number;
        fire_rate_stacks?: number;
        damage_stacks?: number;
      }
    | undefined,
): number {
  if (!player) return 0;
  switch (itemId) {
    case 2:
      return player.speed_stacks ?? 0;
    case 3:
      return player.fire_rate_stacks ?? 0;
    case 4:
      return player.damage_stacks ?? 0;
    default:
      return 0;
  }
}

export default ShopMenu;
