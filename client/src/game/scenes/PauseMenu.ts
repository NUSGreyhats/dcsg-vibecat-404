import { Scene } from "phaser";

export class PauseMenu extends Scene {
  private isGameOver: boolean = false;

  constructor() {
    super("PauseMenu");
  }

  init(data: { gameOver?: boolean }) {
    this.isGameOver = data.gameOver ?? false;
  }

  create() {
    this.add.rectangle(512, 384, 1024, 768, 0x000000, 0.7);

    if (this.isGameOver) {
      this.createGameOverScreen();
    } else {
      this.createPauseScreen();
    }
  }

  private createPauseScreen() {
    this.add
      .text(512, 260, "PAUSED", {
        fontFamily: "Arial Black",
        fontSize: "64px",
        color: "#ffffff",
        stroke: "#000000",
        strokeThickness: 6,
      })
      .setOrigin(0.5);

    this.add
      .text(512, 340, "Press ESC to Resume", {
        fontFamily: "Arial",
        fontSize: "24px",
        color: "#cccccc",
      })
      .setOrigin(0.5);

    const menuBtn = this.add
      .text(512, 420, "Main Menu", {
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
      .text(512, 240, "GAME OVER", {
        fontFamily: "Arial Black",
        fontSize: "64px",
        color: "#ff3333",
        stroke: "#000000",
        strokeThickness: 6,
      })
      .setOrigin(0.5);

    const restartBtn = this.add
      .text(512, 340, "Click to Restart", {
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
