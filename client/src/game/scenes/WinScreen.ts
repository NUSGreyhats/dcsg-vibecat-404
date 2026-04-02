import { Scene } from "phaser";

export class WinScreen extends Scene {
  constructor() {
    super("WinScreen");
  }

  create() {
    this.add.rectangle(512, 384, 1024, 768, 0x000000, 0.85);

    this.add
      .text(512, 200, "YOU WIN!", {
        fontFamily: "Arial Black",
        fontSize: "72px",
        color: "#66ff99",
        stroke: "#000000",
        strokeThickness: 8,
      })
      .setOrigin(0.5);

    this.add
      .text(512, 300, "All rooms cleared!", {
        fontFamily: "Arial",
        fontSize: "28px",
        color: "#ffffff",
      })
      .setOrigin(0.5);

    this.add
      .text(512, 360, "Your name has been added to the leaderboard.", {
        fontFamily: "Arial",
        fontSize: "20px",
        color: "#aaaaaa",
      })
      .setOrigin(0.5);

    const menuBtn = this.add
      .text(512, 460, "[ Main Menu ]", {
        fontFamily: "Arial Black",
        fontSize: "28px",
        color: "#ffcc00",
        stroke: "#000000",
        strokeThickness: 4,
      })
      .setOrigin(0.5)
      .setInteractive({ useHandCursor: true });

    menuBtn.on("pointerover", () => menuBtn.setColor("#ffffff"));
    menuBtn.on("pointerout", () => menuBtn.setColor("#ffcc00"));
    menuBtn.on("pointerdown", () => {
      this.scene.stop("GameHUD");
      this.scene.stop("Game");
      this.scene.start("MainMenu");
    });
  }
}
