import { Scene } from "phaser";

const CX = 640;
const CY = 384;

export class PauseMenu extends Scene {
  private isGameOver: boolean = false;

  constructor() {
    super("PauseMenu");
  }

  init(data: { gameOver?: boolean }) {
    this.isGameOver = data.gameOver ?? false;
  }

  create() {
    this.add.rectangle(CX, CY, 1280, 768, 0x000000, 0.7);

    if (this.isGameOver) {
      this.createGameOverScreen();
    } else {
      this.createPauseScreen();
    }
  }

  private createPauseScreen() {
    this.add
      .text(CX, CY - 80, "PAUSED", {
        fontFamily: "Arial Black",
        fontSize: "64px",
        color: "#ffffff",
        stroke: "#000000",
        strokeThickness: 6,
      })
      .setOrigin(0.5);

    this.add
      .text(CX, CY, "Press ESC to Resume", {
        fontFamily: "Arial",
        fontSize: "24px",
        color: "#cccccc",
      })
      .setOrigin(0.5);

    const menuBtn = this.add
      .text(CX, CY + 80, "Main Menu", {
        fontFamily: "Arial",
        fontSize: "28px",
        color: "#ff8888",
        stroke: "#000000",
        strokeThickness: 3,
      })
      .setOrigin(0.5)
      .setInteractive({ useHandCursor: true });

    menuBtn.on("pointerover", () => menuBtn.setColor("#ffffff"));
    menuBtn.on("pointerout", () => menuBtn.setColor("#ff8888"));
    menuBtn.on("pointerdown", () => this.returnToMainMenu());

    this.input.keyboard!.on("keydown-ESC", () => {
      this.scene.resume("Game");
      this.scene.stop();
    });
  }

  private createGameOverScreen() {
    this.add
      .text(CX, CY - 100, "GAME OVER", {
        fontFamily: "Arial Black",
        fontSize: "64px",
        color: "#ff3333",
        stroke: "#000000",
        strokeThickness: 6,
      })
      .setOrigin(0.5);

    const restartBtn = this.add
      .text(CX, CY, "Click to Restart", {
        fontFamily: "Arial",
        fontSize: "28px",
        color: "#ffffff",
      })
      .setOrigin(0.5);

    this.tweens.add({
      targets: restartBtn,
      alpha: 0.4,
      duration: 700,
      yoyo: true,
      repeat: -1,
    });

    this.time.delayedCall(500, () => {
      this.input.once("pointerdown", () => this.returnToMainMenu());
    });
  }

  private returnToMainMenu() {
    this.scene.stop("GameHUD");
    this.scene.stop("Game");
    this.scene.start("MainMenu");
  }
}
