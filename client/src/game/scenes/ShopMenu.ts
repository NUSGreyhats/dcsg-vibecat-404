import Phaser, { Scene } from "phaser";
import { KEYCODE_BACKTICK, SHOP_DEALS } from "../shop";
import type { Game } from "./Game";

export class ShopMenu extends Scene {
  private coinText!: Phaser.GameObjects.Text;
  private rowTexts: Phaser.GameObjects.Text[] = [];

  constructor() {
    super("ShopMenu");
  }

  create() {
    this.add.rectangle(512, 384, 1024, 768, 0x000000, 0.78);

    this.add
      .rectangle(512, 384, 540, 420, 0x1a1510, 0.97)
      .setStrokeStyle(2, 0xc9a227);

    this.add
      .text(512, 160, "ARMORY", {
        fontFamily: "Arial Black",
        fontSize: "38px",
        color: "#f5e6a8",
        stroke: "#000000",
        strokeThickness: 4,
      })
      .setOrigin(0.5);

    this.add
      .text(512, 212, "` (backtick) or ESC to close", {
        fontFamily: "Arial",
        fontSize: "15px",
        color: "#998866",
      })
      .setOrigin(0.5);

    const game = this.scene.get("Game") as Game;
    const player = game.player;

    this.coinText = this.add
      .text(512, 248, "", {
        fontFamily: "Arial Black",
        fontSize: "20px",
        color: "#ffcc33",
        stroke: "#000000",
        strokeThickness: 3,
      })
      .setOrigin(0.5);

    this.rowTexts = [];
    let y = 300;
    for (const deal of SHOP_DEALS) {
      const row = this.add
        .text(512, y, "", {
          fontFamily: "Arial",
          fontSize: "20px",
          color: "#f0f0f0",
          stroke: "#000000",
          strokeThickness: 2,
        })
        .setOrigin(0.5);
      this.rowTexts.push(row);
      row.setData("deal", deal);
      y += 52;
    }

    const closeBtn = this.add
      .text(512, 500, "Close", {
        fontFamily: "Arial",
        fontSize: "22px",
        color: "#c9a227",
        stroke: "#000000",
        strokeThickness: 2,
      })
      .setOrigin(0.5)
      .setInteractive({ useHandCursor: true });

    closeBtn.on("pointerover", () => closeBtn.setColor("#ffffff"));
    closeBtn.on("pointerout", () => closeBtn.setColor("#c9a227"));
    closeBtn.on("pointerdown", () => this.closeShop());

    const kb = this.input.keyboard!;
    const keyBacktick = kb.addKey(KEYCODE_BACKTICK);
    const keyEsc = kb.addKey(Phaser.Input.Keyboard.KeyCodes.ESC);

    const onClose = () => this.closeShop();
    keyBacktick.on("down", onClose);
    keyEsc.on("down", onClose);

    this.events.once(Phaser.Scenes.Events.SHUTDOWN, () => {
      keyBacktick.removeAllListeners();
      keyEsc.removeAllListeners();
      const g = this.scene.manager.getScene("Game") as Game | undefined;
      if (g) {
        g.shopOpen = false;
        g.input.keyboard!.enabled = true;
      }
    });

    this.syncShopUi(game);
  }

  private syncShopUi(game: Game) {
    const player = game.player;
    this.coinText.setText(`Your coins: ${player.coins}`);

    for (const row of this.rowTexts) {
      row.removeAllListeners();
      row.disableInteractive();
      const deal = row.getData("deal") as (typeof SHOP_DEALS)[number];
      const owned = player.ownsWeapon(deal.weaponId);
      const canBuy = !owned && player.coins >= deal.price;

      if (owned) {
        row.setText(`${deal.shopLabel}  —  ${deal.price} coins  [Owned]`);
        row.setColor("#779977");
      } else if (canBuy) {
        row.setText(`${deal.shopLabel}  —  ${deal.price} coins  [Click to buy]`);
        row.setColor("#ffffff");
        row
          .setInteractive({ useHandCursor: true })
          .on("pointerover", () => row.setColor("#ffdd66"))
          .on("pointerout", () => row.setColor("#ffffff"))
          .on("pointerdown", () => {
            if (game.tryPurchaseShopWeapon(deal.weaponId, deal.price)) {
              this.syncShopUi(game);
            }
          });
      } else {
        row.setText(`${deal.shopLabel}  —  ${deal.price} coins  [Not enough coins]`);
        row.setColor("#887766");
      }
    }
  }

  private closeShop() {
    this.scene.resume("Game");
    this.scene.stop();
  }
}
