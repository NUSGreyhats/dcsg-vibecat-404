import { Scene, GameObjects } from "phaser";

let savedPlayerName = "";

export function getPlayerName(): string {
  return savedPlayerName;
}

export class MainMenu extends Scene {
  background: GameObjects.Image;
  titleImage: GameObjects.Image;
  private nameInput: HTMLInputElement | null = null;

  constructor() {
    super("MainMenu");
  }

  create() {
    this.titleImage = this.add.image(640, 384, "title");
    this.titleImage.setDisplaySize(1280, 768);

    this.add
      .text(640+1280/4+100, 530, "Enter your name:", {
        fontFamily: "Arial Black",
        fontSize: "32px",
        color: "#66ff99",
        stroke: "#000000",
        strokeThickness: 4,
      })
      .setOrigin(0.5);

    this.createNameInput();

    const playBtn = this.add
      .text(640+1280/4+100, 610, "[ PLAY ]", {
        fontFamily: "Arial Black",
        fontSize: "32px",
        color: "#66ff99",
        stroke: "#000000",
        strokeThickness: 4,
      })
      .setOrigin(0.5)
      .setInteractive({ useHandCursor: true });

    playBtn.on("pointerover", () => playBtn.setColor("#ffffff"));
    playBtn.on("pointerout", () => playBtn.setColor("#66ff99"));
    playBtn.on("pointerdown", () => this.startGame());

    this.events.once("shutdown", () => this.removeNameInput());
  }

  private createNameInput() {
    const canvas = this.game.canvas;
    const rect = canvas.getBoundingClientRect();

    const input = document.createElement("input");
    input.type = "text";
    input.maxLength = 24;
    input.placeholder = "Player";
    input.value = savedPlayerName;
    input.style.position = "absolute";
    input.style.width = "220px";
    input.style.padding = "8px 12px";
    input.style.fontSize = "18px";
    input.style.fontFamily = "Arial, sans-serif";
    input.style.textAlign = "center";
    input.style.background = "#111";
    input.style.color = "#fff";
    input.style.border = "2px solid #555";
    input.style.borderRadius = "6px";
    input.style.outline = "none";
    input.style.zIndex = "10";

    const scaleX = rect.width / 1280;
    const scaleY = rect.height / 768;
    const left = rect.left + rect.width / 2 - 122 * scaleX + (1280/4+100)*scaleX;
    const top = rect.top + 550 * scaleY;
    input.style.left = `${left}px`;
    input.style.top = `${top}px`;

    input.addEventListener("keydown", (e) => {
      if (e.key === "Enter") this.startGame();
    });

    document.body.appendChild(input);
    this.nameInput = input;
    input.focus();
  }

  private removeNameInput() {
    if (this.nameInput) {
      this.nameInput.remove();
      this.nameInput = null;
    }
  }

  private startGame() {
    const raw = this.nameInput?.value.trim() ?? "";
    savedPlayerName = raw || "Player";
    this.removeNameInput();
    this.scene.start("Game");
  }
}
