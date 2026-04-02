import { Scene, GameObjects } from "phaser";

let savedPlayerName = "";

export function getPlayerName(): string {
  return savedPlayerName;
}

export class MainMenu extends Scene {
  background: GameObjects.Image;
  logo: GameObjects.Image;
  title: GameObjects.Text;
  private nameInput: HTMLInputElement | null = null;

  constructor() {
    super("MainMenu");
  }

  create() {
    this.background = this.add.image(512, 384, "background");
    this.logo = this.add.image(512, 260, "logo");

    this.title = this.add
      .text(512, 420, "DEFCON", {
        fontFamily: "Arial Black",
        fontSize: "42px",
        color: "#ffffff",
        stroke: "#000000",
        strokeThickness: 8,
        align: "center",
      })
      .setOrigin(0.5);

    this.add
      .text(512, 480, "Enter your name:", {
        fontFamily: "Arial",
        fontSize: "20px",
        color: "#cccccc",
      })
      .setOrigin(0.5);

    this.createNameInput();

    const playBtn = this.add
      .text(512, 580, "[ PLAY ]", {
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

    const left = rect.left + rect.width / 2 - 122;
    const top = rect.top + 510;
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
