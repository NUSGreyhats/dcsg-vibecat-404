import Phaser from "phaser";
import { getSync } from "../net/runContext";
import { WS_URL } from "../constants";
import {
  ACCENT_PRIMARY,
  BUTTON_HOVER_COLOR,
  BUTTON_IDLE_COLOR,
  BUTTON_TEXT_STYLE,
  DEEP_NAVY,
  DEPTH_SPLASH,
  ERROR,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_SMALL,
  MID_BLUE,
  SUCCESS,
  TEXT_MUTED,
  TEXT_PRIMARY,
  WARNING,
  hex,
} from "../ui/tokens";

/**
 * Title screen. Shows the art/title.png splash full-bleed behind a
 * compact bottom-right login panel: player_id text input → Start Run
 * button → status line. Connects on create; on Start fires
 * C2S_AUTH → C2S_START_RUN and hands off to the Game scene.
 */
export class MainMenu extends Phaser.Scene {
  private overlayInput?: HTMLInputElement;
  private statusText?: Phaser.GameObjects.Text;
  private startButton?: Phaser.GameObjects.Text;
  private connected = false;
  private resizeHandler?: () => void;

  private static readonly PANEL_WIDTH = 460;
  private static readonly PANEL_HEIGHT = 280;
  private static readonly PANEL_MARGIN = 40;
  private static readonly PANEL_PADDING = 20;
  private static readonly INPUT_WIDTH =
    MainMenu.PANEL_WIDTH - MainMenu.PANEL_PADDING * 2;

  constructor() {
    super({ key: "MainMenu" });
  }

  create(): void {
    const w = this.scale.width;
    const h = this.scale.height;

    // -- Splash --
    if (this.textures.exists("title")) {
      this.add
        .image(w / 2, h / 2, "title")
        .setDisplaySize(w, h)
        .setDepth(DEPTH_SPLASH);
    }

    // -- Login panel (bottom-right) --
    const panelX = w - MainMenu.PANEL_WIDTH - MainMenu.PANEL_MARGIN;
    const panelY = h - MainMenu.PANEL_HEIGHT - MainMenu.PANEL_MARGIN + 30;

    this.add
      .rectangle(
        panelX,
        panelY - 30,
        MainMenu.PANEL_WIDTH,
        MainMenu.PANEL_HEIGHT,
        DEEP_NAVY,
        0.85,
      )
      .setStrokeStyle(2, MID_BLUE)
      .setOrigin(0, 0);

    this.add.text(panelX + MainMenu.PANEL_PADDING, panelY - 10, "Player ID", {
      fontFamily: FONT_FAMILY,
      fontSize: FONT_SIZE_BODY,
      color: hex(TEXT_PRIMARY),
    });

    this.mountTextInput(panelX + MainMenu.PANEL_PADDING, panelY + 20);

    this.startButton = this.add
      .text(
        panelX + MainMenu.PANEL_PADDING,
        panelY + 70,
        "[ Start Run ]",
        BUTTON_TEXT_STYLE,
      )
      .setOrigin(0, 0)
      .setInteractive({ useHandCursor: true });

    this.startButton.on("pointerover", () =>
      this.startButton?.setColor(BUTTON_HOVER_COLOR),
    );
    this.startButton.on("pointerout", () =>
      this.startButton?.setColor(BUTTON_IDLE_COLOR),
    );
    this.startButton.on("pointerdown", () => this.onStart());

    this.statusText = this.add.text(
      panelX + MainMenu.PANEL_PADDING,
      panelY + 135,
      "Connecting...",
      {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SMALL,
        color: hex(TEXT_MUTED),
        wordWrap: { width: MainMenu.INPUT_WIDTH },
      },
    );

    const registerLink = this.add
      .text(
        panelX + MainMenu.PANEL_PADDING,
        panelY + 195,
        "No player ID? [ Register → ]",
        {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_SMALL,
          color: hex(ACCENT_PRIMARY),
        },
      )
      .setOrigin(0, 0)
      .setInteractive({ useHandCursor: true });
    registerLink.on("pointerover", () =>
      registerLink.setColor(hex(TEXT_PRIMARY)),
    );
    registerLink.on("pointerout", () =>
      registerLink.setColor(hex(ACCENT_PRIMARY)),
    );
    registerLink.on("pointerdown", () => {
      window.open("/register", "_blank", "noopener,noreferrer");
    });

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
        this.statusText?.setColor(hex(SUCCESS));
      })
      .catch((err) => {
        console.error("[MainMenu] connect failed", err);
        this.statusText?.setText(
          "Could not connect to the game server. Refresh to retry.",
        );
        this.statusText?.setColor(hex(ERROR));
      });
  }

  private mountTextInput(sceneX: number, sceneY: number): void {
    const canvas = this.game.canvas;
    const parent = canvas.parentElement ?? document.body;
    const input = document.createElement("input");
    input.type = "text";
    input.maxLength = 64;
    input.placeholder = "player_id";
    input.autocomplete = "off";
    input.spellcheck = false;
    Object.assign(input.style, {
      position: "absolute",
      zIndex: "20",
      fontFamily: "monospace",
      fontSize: "18px",
      padding: "6px 10px",
      border: `1px solid ${hex(MID_BLUE)}`,
      background: hex(DEEP_NAVY),
      color: hex(TEXT_PRIMARY),
      width: `${MainMenu.INPUT_WIDTH}px`,
      outline: "none",
      boxSizing: "border-box",
    } as Partial<CSSStyleDeclaration>);

    input.addEventListener("focus", () => {
      input.style.borderColor = hex(ACCENT_PRIMARY);
    });
    input.addEventListener("blur", () => {
      input.style.borderColor = hex(MID_BLUE);
    });

    if (!parent.style.position) parent.style.position = "relative";
    parent.appendChild(input);

    const place = () => {
      const rect = canvas.getBoundingClientRect();
      const parentRect = parent.getBoundingClientRect();
      const scaleX = rect.width / this.scale.width;
      const scaleY = rect.height / this.scale.height;
      const leftInCanvas = sceneX * scaleX;
      const topInCanvas = sceneY * scaleY;
      input.style.left = `${rect.left - parentRect.left + leftInCanvas}px`;
      input.style.top = `${rect.top - parentRect.top + topInCanvas}px`;
      // Scale the input with the canvas so the panel lines up.
      const visualWidth = MainMenu.INPUT_WIDTH * scaleX;
      input.style.width = `${visualWidth}px`;
    };
    place();
    this.resizeHandler = place;
    window.addEventListener("resize", this.resizeHandler);
    this.scale.on("resize", this.resizeHandler);

    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") this.onStart();
    });

    this.overlayInput = input;
    setTimeout(() => input.focus(), 50);
  }

  private onStart(): void {
    if (!this.connected) {
      this.statusText?.setText("Still connecting — hang on.");
      this.statusText?.setColor(hex(WARNING));
      return;
    }
    const value = this.overlayInput?.value.trim() ?? "";
    if (!value) {
      this.statusText?.setText("Enter a player ID first.");
      this.statusText?.setColor(hex(ERROR));
      return;
    }
    this.statusText?.setText("Authenticating...");
    this.statusText?.setColor(hex(TEXT_MUTED));
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
    this.statusText?.setColor(hex(ERROR));
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
