import Phaser from "phaser";
import { getSync } from "../net/runContext";
import { WS_URL } from "../constants";

/**
 * Title screen. Connects to the game server, takes a player_id via an HTML
 * <input> overlay (Phaser has no native text input), then triggers auth +
 * start-run and hands off to the Game scene.
 */
export class MainMenu extends Phaser.Scene {
  private overlayInput?: HTMLInputElement;
  private statusText?: Phaser.GameObjects.Text;
  private startButton?: Phaser.GameObjects.Text;
  private connected = false;
  private resizeHandler?: () => void;

  constructor() {
    super({ key: "MainMenu" });
  }

  create(): void {
    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;

    if (this.textures.exists("bg")) {
      this.add
        .image(cx, cy, "bg")
        .setDisplaySize(this.scale.width, this.scale.height);
    }

    this.add
      .text(cx, cy - 200, "DEFCON", {
        fontFamily: "monospace",
        fontSize: "72px",
        color: "#ffcc33",
      })
      .setOrigin(0.5);

    this.add
      .text(cx, cy - 130, "Dungeon Speedrun", {
        fontFamily: "monospace",
        fontSize: "20px",
        color: "#cccccc",
      })
      .setOrigin(0.5);

    this.add
      .text(cx, cy - 40, "Player ID", {
        fontFamily: "monospace",
        fontSize: "18px",
        color: "#ffffff",
      })
      .setOrigin(0.5);

    this.mountTextInput(cx, cy);

    this.startButton = this.add
      .text(cx, cy + 80, "[ Start Run ]", {
        fontFamily: "monospace",
        fontSize: "28px",
        color: "#ffffff",
        backgroundColor: "#333333",
        padding: { left: 16, right: 16, top: 8, bottom: 8 },
      })
      .setOrigin(0.5)
      .setInteractive({ useHandCursor: true });

    this.startButton.on("pointerover", () => {
      this.startButton?.setColor("#ffcc33");
    });
    this.startButton.on("pointerout", () => {
      this.startButton?.setColor("#ffffff");
    });
    this.startButton.on("pointerdown", () => this.onStart());

    this.statusText = this.add
      .text(cx, cy + 150, "Connecting...", {
        fontFamily: "monospace",
        fontSize: "16px",
        color: "#aaaaaa",
      })
      .setOrigin(0.5);

    this.connect();

    const sync = getSync();
    sync.state.on("authOk", this.onAuthOk, this);
    sync.state.on("runStarted", this.onRunStarted, this);
    sync.state.on("error", this.onError, this);

    this.events.on("shutdown", this.cleanup, this);
  }

  private connect(): void {
    const sync = getSync();
    sync
      .connect(WS_URL)
      .then(() => {
        this.connected = true;
        this.statusText?.setText(
          "Connected. Enter a player ID and press Start.",
        );
        this.statusText?.setColor("#88ff88");
      })
      .catch((err) => {
        console.error("[MainMenu] connect failed", err);
        this.statusText?.setText(
          "Could not connect to the game server. Refresh to retry.",
        );
        this.statusText?.setColor("#ff6666");
      });
  }

  private mountTextInput(cx: number, cy: number): void {
    const canvas = this.game.canvas;
    const parent = canvas.parentElement ?? document.body;
    const input = document.createElement("input");
    input.type = "text";
    input.maxLength = 64;
    input.placeholder = "player_id";
    input.autocomplete = "off";
    input.spellcheck = false;
    input.style.position = "absolute";
    input.style.zIndex = "20";
    input.style.fontFamily = "monospace";
    input.style.fontSize = "18px";
    input.style.padding = "6px 10px";
    input.style.border = "1px solid #888";
    input.style.background = "#111";
    input.style.color = "#fff";
    input.style.textAlign = "center";
    input.style.width = "280px";
    input.style.outline = "none";
    input.style.boxSizing = "border-box";

    if (!parent.style.position) parent.style.position = "relative";
    parent.appendChild(input);

    const place = () => {
      const rect = canvas.getBoundingClientRect();
      const parentRect = parent.getBoundingClientRect();
      const scaleX = rect.width / this.scale.width;
      const scaleY = rect.height / this.scale.height;
      const leftInCanvas = cx * scaleX - 140;
      const topInCanvas = cy * scaleY - 16;
      input.style.left = `${rect.left - parentRect.left + leftInCanvas}px`;
      input.style.top = `${rect.top - parentRect.top + topInCanvas}px`;
    };
    place();
    this.resizeHandler = place;
    window.addEventListener("resize", this.resizeHandler);
    this.scale.on("resize", this.resizeHandler);

    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") this.onStart();
    });

    this.overlayInput = input;
    // Focus after a tick so it doesn't fight with Phaser bootstrap.
    setTimeout(() => input.focus(), 50);
  }

  private onStart(): void {
    if (!this.connected) {
      this.statusText?.setText("Still connecting — hang on.");
      this.statusText?.setColor("#ffaa33");
      return;
    }
    const value = this.overlayInput?.value.trim() ?? "";
    if (!value) {
      this.statusText?.setText("Enter a player ID first.");
      this.statusText?.setColor("#ff6666");
      return;
    }
    this.statusText?.setText("Authenticating...");
    this.statusText?.setColor("#aaaaaa");
    getSync().sendAuth(value);
  }

  private onAuthOk(_displayName: string): void {
    this.statusText?.setText("Starting run...");
    getSync().sendStartRun();
  }

  private onRunStarted(): void {
    this.scene.start("Game");
    this.scene.launch("GameHUD");
  }

  private onError(err: { code: string; message: string }): void {
    this.statusText?.setText(`Error: ${err.message}`);
    this.statusText?.setColor("#ff6666");
  }

  private cleanup(): void {
    const sync = getSync();
    sync.state.off("authOk", this.onAuthOk, this);
    sync.state.off("runStarted", this.onRunStarted, this);
    sync.state.off("error", this.onError, this);

    if (this.resizeHandler) {
      window.removeEventListener("resize", this.resizeHandler);
      this.scale.off("resize", this.resizeHandler);
      this.resizeHandler = undefined;
    }

    if (this.overlayInput) {
      this.overlayInput.remove();
      this.overlayInput = undefined;
    }
  }
}

export default MainMenu;
