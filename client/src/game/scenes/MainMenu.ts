import Phaser from "phaser";
import { getSync } from "../net/runContext";
import { WS_URL } from "../constants";
import {
  ACCENT_PRIMARY,
  BUTTON_HOVER_COLOR,
  BUTTON_IDLE_COLOR,
  DEEP_NAVY,
  DEPTH_SPLASH,
  ERROR,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_CAPTION,
  FONT_SIZE_SMALL,
  MID_BLUE,
  NAVY,
  SUCCESS,
  TEXT_MUTED,
  TEXT_PRIMARY,
  WARNING,
  hex,
} from "../ui/tokens";

/**
 * Sponsor row. `imageSrc` is the asset URL for the HTML <img> overlay
 * the scene mounts over the card; falls back to the `placeholder` glyph
 * when absent.
 */
interface SponsorSpec {
  readonly label: string;
  readonly placeholder: string;
  readonly url: string;
  readonly imageSrc?: string;
}

const SPONSORS: ReadonlyArray<SponsorSpec> = [
  {
    label: "HTX",
    placeholder: "×",
    url: "https://www.htx.gov.sg/",
    imageSrc: "assets/htx_logo.png",
  },
  {
    label: "OtterSec",
    placeholder: "◉",
    url: "https://osec.io/",
    imageSrc: "assets/ottersec_logo.png",
  },
  {
    label: "Niklaus",
    placeholder: "N",
    url: "",
    imageSrc: "assets/niklas_logo.png",
  },
];

/**
 * Title screen. Shows the art/title.png splash full-bleed behind a
 * compact bottom-right login panel: inline username + password row →
 * Log In. The server resolves the credentials to a player_id via
 * /api/login, the scene then opens the WS, sends C2S_AUTH +
 * C2S_START_RUN, and hands off to the Game scene. Players with no
 * account click Register → to open the /register page in a new tab.
 * A small sponsors panel sits directly below the login panel.
 */
export class MainMenu extends Phaser.Scene {
  private usernameInput?: HTMLInputElement;
  private passwordInput?: HTMLInputElement;
  private statusText?: Phaser.GameObjects.Text;
  private startButton?: HTMLButtonElement;
  private connected = false;
  private submitting = false;
  private resizeHandlers: Array<() => void> = [];
  private sponsorLogoEls: HTMLImageElement[] = [];

  private static readonly PANEL_WIDTH = 460;
  private static readonly PANEL_HEIGHT = 200;
  private static readonly PANEL_MARGIN = 40;
  private static readonly PANEL_PADDING = 20;
  private static readonly INPUT_WIDTH =
    MainMenu.PANEL_WIDTH - MainMenu.PANEL_PADDING * 2;
  // Inline label column: "Username:" / "Password:" sit to the left of
  // each input, same row.
  private static readonly LABEL_COLUMN_WIDTH = 110;
  private static readonly LABEL_GAP = 8;
  private static readonly ROW_INPUT_WIDTH =
    MainMenu.INPUT_WIDTH - MainMenu.LABEL_COLUMN_WIDTH - MainMenu.LABEL_GAP;
  private static readonly SPONSOR_PANEL_HEIGHT = 140;
  private static readonly SPONSOR_PANEL_GAP = 12;
  private static readonly SPONSOR_CARD_HEIGHT = 92;
  private static readonly SPONSOR_CARD_GAP = 10;

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
    // Stack = login + gap + sponsors, pinned to the bottom with PANEL_MARGIN.
    const stackBottom = h - MainMenu.PANEL_MARGIN;
    const sponsorTop =
      stackBottom - MainMenu.SPONSOR_PANEL_HEIGHT;
    const loginBottom = sponsorTop - MainMenu.SPONSOR_PANEL_GAP;
    const loginTop = loginBottom - MainMenu.PANEL_HEIGHT;
    // panelY is the content reference (30 px below the panel top); keep
    // the existing layout math centered on it.
    const panelY = loginTop + 30;
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

    const inputX = contentX + MainMenu.LABEL_COLUMN_WIDTH + MainMenu.LABEL_GAP;
    // Vertically-center the 18 px label against the 32 px input.
    const labelOffsetY = 7;
    const usernameRowY = panelY - 10;
    const passwordRowY = usernameRowY + 42;

    this.add.text(contentX, usernameRowY + labelOffsetY, "Username:", {
      fontFamily: FONT_FAMILY,
      fontSize: FONT_SIZE_BODY,
      color: hex(TEXT_PRIMARY),
    });
    this.usernameInput = this.mountTextInput(
      inputX,
      usernameRowY,
      MainMenu.ROW_INPUT_WIDTH,
      {
        type: "text",
        placeholder: "username",
        maxLength: 32,
        autocomplete: "username",
      },
    );

    this.add.text(contentX, passwordRowY + labelOffsetY, "Password:", {
      fontFamily: FONT_FAMILY,
      fontSize: FONT_SIZE_BODY,
      color: hex(TEXT_PRIMARY),
    });
    this.passwordInput = this.mountTextInput(
      inputX,
      passwordRowY,
      MainMenu.ROW_INPUT_WIDTH,
      {
        type: "password",
        placeholder: "password",
        maxLength: 128,
        autocomplete: "current-password",
      },
    );

    this.startButton = this.mountButton(contentX, panelY + 78, "[ Log In ]");

    this.statusText = this.add.text(contentX, panelY + 120, "Connecting...", {
      fontFamily: FONT_FAMILY,
      fontSize: FONT_SIZE_SMALL,
      color: hex(TEXT_MUTED),
      wordWrap: { width: MainMenu.INPUT_WIDTH },
    });

    const registerLink = this.add
      .text(contentX, panelY + 146, "No account? [ Register → ]", {
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

    this.drawSponsorsPanel(panelX, sponsorTop);

    this.connect();

    const sync = getSync();
    sync.state.on("authOk", this.onAuthOk, this);
    sync.state.on("runStarted", this.onRunStarted, this);
    sync.state.on("error", this.onError, this);

    this.events.on("shutdown", this.cleanup, this);

    setTimeout(() => this.usernameInput?.focus(), 50);
  }

  /**
   * Sponsors panel below the login panel. Same width + stroke as the login
   * panel, a `// sponsors` header in muted blue, then N evenly-spaced cards
   * that currently show a placeholder glyph. When a sponsor's `imageKey` is
   * populated, the scene will draw the image in place of the glyph.
   */
  private drawSponsorsPanel(panelX: number, panelTop: number): void {
    this.add
      .rectangle(
        panelX,
        panelTop,
        MainMenu.PANEL_WIDTH,
        MainMenu.SPONSOR_PANEL_HEIGHT,
        DEEP_NAVY,
        0.85,
      )
      .setStrokeStyle(2, MID_BLUE)
      .setOrigin(0, 0);

    this.add.text(
      panelX + MainMenu.PANEL_PADDING,
      panelTop + 10,
      "// sponsors",
      {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_CAPTION,
        color: hex(MID_BLUE),
      },
    );

    const count = SPONSORS.length;
    const innerWidth = MainMenu.INPUT_WIDTH;
    const cardWidth =
      (innerWidth - MainMenu.SPONSOR_CARD_GAP * (count - 1)) / count;
    const cardsY = panelTop + 32;
    const cardsX = panelX + MainMenu.PANEL_PADDING;
    for (let i = 0; i < count; i++) {
      const cx = cardsX + i * (cardWidth + MainMenu.SPONSOR_CARD_GAP);
      this.drawSponsorCard(
        cx,
        cardsY,
        cardWidth,
        MainMenu.SPONSOR_CARD_HEIGHT,
        SPONSORS[i],
      );
    }
  }

  private drawSponsorCard(
    x: number,
    y: number,
    w: number,
    h: number,
    sponsor: SponsorSpec,
  ): void {
    // NAVY card fill (one shade above the parent panel) so cards stay
    // distinct against DEEP_NAVY without breaking the dark theme.
    // Logos are HTML <img> overlays — browsers do high-quality
    // downscaling that Phaser's NPOT textures don't.
    this.add
      .rectangle(x, y, w, h, NAVY, 1)
      .setStrokeStyle(1, MID_BLUE)
      .setOrigin(0, 0);

    if (sponsor.imageSrc) {
      this.mountSponsorImage(x, y, w, h, sponsor);
    } else {
      this.add
        .text(x + w / 2, y + h / 2, sponsor.placeholder, {
          fontFamily: FONT_FAMILY,
          fontSize: "24px",
          color: hex(TEXT_PRIMARY),
        })
        .setOrigin(0.5);
    }
  }

  /**
   * Overlay an HTML <img> on the canvas over the given scene-space rect.
   * Handles hover + click locally so we don't need a Phaser hit-area on
   * the underlying rectangle. Re-positioned on resize so it tracks the
   * panel the same way login inputs do.
   */
  private mountSponsorImage(
    sceneX: number,
    sceneY: number,
    sceneW: number,
    sceneH: number,
    sponsor: SponsorSpec,
  ): void {
    const canvas = this.game.canvas;
    const parent = canvas.parentElement ?? document.body;
    const img = document.createElement("img");
    img.src = sponsor.imageSrc!;
    img.alt = sponsor.label;
    img.draggable = false;
    Object.assign(img.style, {
      position: "absolute",
      zIndex: "19",
      objectFit: "contain",
      padding: "4px",
      boxSizing: "border-box",
      cursor: sponsor.url ? "pointer" : "default",
      userSelect: "none",
      pointerEvents: "auto",
    } as Partial<CSSStyleDeclaration>);

    if (sponsor.url) {
      img.addEventListener("click", () => {
        window.open(sponsor.url, "_blank", "noopener,noreferrer");
      });
    }

    if (!parent.style.position) parent.style.position = "relative";
    parent.appendChild(img);
    this.sponsorLogoEls.push(img);

    const place = () => {
      const rect = canvas.getBoundingClientRect();
      const parentRect = parent.getBoundingClientRect();
      const scaleX = rect.width / this.scale.width;
      img.style.left = `${rect.left - parentRect.left + sceneX * scaleX}px`;
      img.style.top = `${rect.top - parentRect.top + sceneY * scaleX}px`;
      img.style.width = `${sceneW * scaleX}px`;
      img.style.height = `${sceneH * scaleX}px`;
    };
    place();
    window.addEventListener("resize", place);
    this.scale.on("resize", place);
    this.resizeHandlers.push(place);
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
    widthPx: number,
    opts: {
      type: "text" | "password";
      placeholder: string;
      maxLength: number;
      autocomplete: AutoFill;
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
      width: `${widthPx}px`,
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
      const visualWidth = widthPx * scaleX;
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

  /**
   * Mount a DOM <button> overlaid on the Phaser canvas at the given scene
   * coords. Uses a real `<button>` so it joins the natural Tab order after
   * the password input and activates on Enter without extra wiring.
   */
  private mountButton(
    sceneX: number,
    sceneY: number,
    label: string,
  ): HTMLButtonElement {
    const canvas = this.game.canvas;
    const parent = canvas.parentElement ?? document.body;
    const btn = document.createElement("button");
    btn.type = "button";
    btn.textContent = label;
    Object.assign(btn.style, {
      position: "absolute",
      zIndex: "20",
      fontFamily: "monospace",
      fontSize: "16px",
      padding: "0",
      border: "none",
      background: "transparent",
      color: BUTTON_IDLE_COLOR,
      cursor: "pointer",
      outline: "none",
    } as Partial<CSSStyleDeclaration>);

    const setHover = (on: boolean) => {
      btn.style.color = on ? BUTTON_HOVER_COLOR : BUTTON_IDLE_COLOR;
    };
    btn.addEventListener("focus", () => setHover(true));
    btn.addEventListener("blur", () => setHover(false));
    btn.addEventListener("mouseenter", () => setHover(true));
    btn.addEventListener("mouseleave", () => {
      // Keep the hover colour while focused so keyboard users still see
      // the affordance after the mouse leaves.
      if (document.activeElement !== btn) setHover(false);
    });
    btn.addEventListener("click", () => this.onStart());

    if (!parent.style.position) parent.style.position = "relative";
    parent.appendChild(btn);

    const place = () => {
      const rect = canvas.getBoundingClientRect();
      const parentRect = parent.getBoundingClientRect();
      const scaleX = rect.width / this.scale.width;
      btn.style.left = `${rect.left - parentRect.left + sceneX * scaleX}px`;
      btn.style.top = `${rect.top - parentRect.top + sceneY * scaleX}px`;
      btn.style.fontSize = `${16 * scaleX}px`;
    };
    place();
    window.addEventListener("resize", place);
    this.scale.on("resize", place);
    this.resizeHandlers.push(place);

    return btn;
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
    this.startButton?.remove();
    this.startButton = undefined;

    for (const el of this.sponsorLogoEls) el.remove();
    this.sponsorLogoEls = [];
  }
}

export default MainMenu;
