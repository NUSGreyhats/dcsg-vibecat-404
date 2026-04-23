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
 * compact bottom-right login panel: username + password → Log In. The
 * server resolves the credentials to a player_id via /api/login, the
 * scene then opens the WS, sends C2S_AUTH + C2S_START_RUN, and hands
 * off to the Game scene. Players with no account click Register → to
 * open the /register page in a new tab.
 */
export class MainMenu extends Phaser.Scene {
  private usernameInput?: HTMLInputElement;
  private passwordInput?: HTMLInputElement;
  private statusText?: Phaser.GameObjects.Text;
  private startButton?: Phaser.GameObjects.Text;
  private connected = false;
  private submitting = false;
  private resizeHandlers: Array<() => void> = [];

  private static readonly PANEL_WIDTH = 460;
  private static readonly PANEL_HEIGHT = 340;
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

    if (this.textures.exists("title")) {
      this.add
        .image(w / 2, h / 2, "title")
        .setDisplaySize(w, h)
        .setDepth(DEPTH_SPLASH);
    }

    const panelX = w - MainMenu.PANEL_WIDTH - MainMenu.PANEL_MARGIN;
    const panelY = h - MainMenu.PANEL_HEIGHT - MainMenu.PANEL_MARGIN + 30;
    const contentX = panelX + MainMenu.PANEL_PADDING;

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

    this.add.text(contentX, panelY - 10, "Username", {
      fontFamily: FONT_FAMILY,
      fontSize: FONT_SIZE_BODY,
      color: hex(TEXT_PRIMARY),
    });
    this.usernameInput = this.mountTextInput(contentX, panelY + 20, {
      type: "text",
      placeholder: "username",
      maxLength: 32,
      autocomplete: "username",
    });

    this.add.text(contentX, panelY + 65, "Password", {
      fontFamily: FONT_FAMILY,
      fontSize: FONT_SIZE_BODY,
      color: hex(TEXT_PRIMARY),
    });
    this.passwordInput = this.mountTextInput(contentX, panelY + 95, {
      type: "password",
      placeholder: "password",
      maxLength: 128,
      autocomplete: "current-password",
    });

    this.startButton = this.add
      .text(contentX, panelY + 145, "[ Log In ]", BUTTON_TEXT_STYLE)
      .setOrigin(0, 0)
      .setInteractive({ useHandCursor: true });
    this.startButton.on("pointerover", () =>
      this.startButton?.setColor(BUTTON_HOVER_COLOR),
    );
    this.startButton.on("pointerout", () =>
      this.startButton?.setColor(BUTTON_IDLE_COLOR),
    );
    this.startButton.on("pointerdown", () => this.onStart());

    this.statusText = this.add.text(contentX, panelY + 210, "Connecting...", {
      fontFamily: FONT_FAMILY,
      fontSize: FONT_SIZE_SMALL,
      color: hex(TEXT_MUTED),
      wordWrap: { width: MainMenu.INPUT_WIDTH },
    });

    const registerLink = this.add
      .text(contentX, panelY + 260, "No account? [ Register → ]", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SMALL,
        color: hex(ACCENT_PRIMARY),
      })
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

    setTimeout(() => this.usernameInput?.focus(), 50);
  }

  private connect(): void {
    const sync = getSync();
    sync
      .connect(WS_URL)
      .then(() => {
        this.connected = true;
        this.statusText?.setText("Connected. Log in to play.");
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

  /**
   * Mount a DOM <input> overlaid on the Phaser canvas at the given scene
   * coords, re-positioning on resize so the input tracks the panel.
   */
  private mountTextInput(
    sceneX: number,
    sceneY: number,
    opts: {
      type: "text" | "password";
      placeholder: string;
      maxLength: number;
      autocomplete: string;
    },
  ): HTMLInputElement {
    const canvas = this.game.canvas;
    const parent = canvas.parentElement ?? document.body;
    const input = document.createElement("input");
    input.type = opts.type;
    input.maxLength = opts.maxLength;
    input.placeholder = opts.placeholder;
    input.autocomplete = opts.autocomplete;
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
      const leftInCanvas = sceneX * scaleX;
      const topInCanvas = sceneY * scaleX;
      input.style.left = `${rect.left - parentRect.left + leftInCanvas}px`;
      input.style.top = `${rect.top - parentRect.top + topInCanvas}px`;
      const visualWidth = MainMenu.INPUT_WIDTH * scaleX;
      input.style.width = `${visualWidth}px`;
    };
    place();
    window.addEventListener("resize", place);
    this.scale.on("resize", place);
    this.resizeHandlers.push(place);

    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") this.onStart();
    });

    return input;
  }

  private async onStart(): Promise<void> {
    if (this.submitting) return;
    if (!this.connected) {
      this.statusText?.setText("Still connecting — hang on.");
      this.statusText?.setColor(hex(WARNING));
      return;
    }
    const username = this.usernameInput?.value.trim() ?? "";
    const password = this.passwordInput?.value ?? "";
    if (!username || !password) {
      this.statusText?.setText("Enter your username and password.");
      this.statusText?.setColor(hex(ERROR));
      return;
    }

    this.submitting = true;
    this.statusText?.setText("Logging in...");
    this.statusText?.setColor(hex(TEXT_MUTED));

    try {
      const res = await fetch("/api/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username, password }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as {
          error?: string;
        };
        this.statusText?.setText(
          body.error ?? `Login failed (HTTP ${res.status}).`,
        );
        this.statusText?.setColor(hex(ERROR));
        this.submitting = false;
        return;
      }
      const body = (await res.json()) as {
        player_id: string;
        display_name: string;
      };
      this.statusText?.setText("Authenticating...");
      getSync().sendAuth(body.player_id);
    } catch (err) {
      console.error("[MainMenu] login failed", err);
      this.statusText?.setText("Network error. Try again.");
      this.statusText?.setColor(hex(ERROR));
      this.submitting = false;
    }
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
    this.submitting = false;
  }

  private cleanup(): void {
    const sync = getSync();
    sync.state.off("authOk", this.onAuthOk, this);
    sync.state.off("runStarted", this.onRunStarted, this);
    sync.state.off("error", this.onError, this);

    for (const fn of this.resizeHandlers) {
      window.removeEventListener("resize", fn);
      this.scale.off("resize", fn);
    }
    this.resizeHandlers = [];

    this.usernameInput?.remove();
    this.usernameInput = undefined;
    this.passwordInput?.remove();
    this.passwordInput = undefined;
  }
}

export default MainMenu;
