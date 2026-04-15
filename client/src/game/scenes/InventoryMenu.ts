import { Scene } from "phaser";
import { ALL_WEAPON_IDS, getWeaponStats } from "../weapons";
import type { Game } from "./Game";

const CX = 640;
const CY = 384;

export class InventoryMenu extends Scene {
  constructor() {
    super("InventoryMenu");
  }

  create() {
    this.add.rectangle(CX, CY, 1280, 768, 0x000000, 0.78);

    this.add
      .rectangle(CX, CY, 520, 440, 0x161b26, 0.96)
      .setStrokeStyle(2, 0x5a8fc4);

    this.add
      .text(CX, CY - 220, "WEAPONS", {
        fontFamily: "Arial Black",
        fontSize: "36px",
        color: "#e8f0ff",
        stroke: "#000000",
        strokeThickness: 4,
      })
      .setOrigin(0.5);

    this.add
      .text(CX, CY - 170, "Click a weapon to equip    ·    I, Tab, or ESC to close", {
        fontFamily: "Arial",
        fontSize: "15px",
        color: "#8899aa",
      })
      .setOrigin(0.5);

    const game = this.scene.get("Game") as Game;
    const player = game.player;

    let y = CY - 120;
    for (const id of ALL_WEAPON_IDS) {
      const def = getWeaponStats(id);
      const owned = player.ownsWeapon(id);
      const equipped = player.equippedWeaponId === id;

      let line = def.displayName;
      if (equipped) line += "   [Equipped]";
      if (!owned) line += "   (Locked)";

      const baseColor = !owned ? "#555566" : equipped ? "#7dffb3" : "#f0f4ff";

      const row = this.add
        .text(CX, y, line, {
          fontFamily: "Arial",
          fontSize: "22px",
          color: baseColor,
          stroke: "#000000",
          strokeThickness: 3,
        })
        .setOrigin(0.5);

      if (owned) {
        row
          .setInteractive({ useHandCursor: true })
          .on("pointerover", () => {
            if (!equipped) row.setColor("#ffdd66");
          })
          .on("pointerout", () => row.setColor(baseColor))
          .on("pointerdown", () => {
            game.playerEquipWeapon(id);
            this.closeInventory();
          });
      }

      y += 48;
    }

    const resume = this.add
      .text(CX, CY + 130, "Resume", {
        fontFamily: "Arial",
        fontSize: "22px",
        color: "#88bbff",
        stroke: "#000000",
        strokeThickness: 2,
      })
      .setOrigin(0.5)
      .setInteractive({ useHandCursor: true });

    resume.on("pointerover", () => resume.setColor("#ffffff"));
    resume.on("pointerout", () => resume.setColor("#88bbff"));
    resume.on("pointerdown", () => this.closeInventory());

    const kb = this.input.keyboard!;
    const K = Phaser.Input.Keyboard.KeyCodes;
    const keyI = kb.addKey(K.I);
    const keyTab = kb.addKey(K.TAB);
    const keyEsc = kb.addKey(K.ESC);

    const onCloseKey = () => this.closeInventory();
    keyI.on("down", onCloseKey);
    keyTab.on("down", onCloseKey);
    keyEsc.on("down", onCloseKey);

    this.events.once(Phaser.Scenes.Events.SHUTDOWN, () => {
      keyI.removeAllListeners();
      keyTab.removeAllListeners();
      keyEsc.removeAllListeners();
      const g = this.scene.manager.getScene("Game") as Game | undefined;
      if (g) {
        g.inventoryOpen = false;
        g.input.keyboard!.enabled = true;
      }
    });
  }

  private closeInventory() {
    this.scene.resume("Game");
    this.scene.stop();
  }
}
