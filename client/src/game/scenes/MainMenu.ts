import Phaser from "phaser";
import { getSync, resetSync } from "../net/runContext";
import { CONTROL_BINDINGS } from "./utils/Controls";
import {
  clearCachedPlayerId,
  getCachedPlayerId,
  setCachedPlayerId,
} from "../prefs";
import { WS_URL } from "../constants";
import {
  ACCENT_PRIMARY,
  BUTTON_HOVER_COLOR,
  DEEP_NAVY,
  DEPTH_SPLASH,
  ERROR,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_CAPTION,
  FONT_SIZE_SMALL,
  MID_BLUE,
  SUCCESS,
  TEXT_DIM,
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
    label: "Niklaus",
    placeholder: "N",
    url: "",
    imageSrc: "assets/niklas_logo.png",
  },
  {
    label: "OtterSec",
    placeholder: "◉",
    url: "https://osec.io/",
    imageSrc: "assets/ottersec_logo.png",
  },
];

/**
 * Title screen. Shows the art/title.png splash full-bleed behind a
 * compact bottom-right login panel: inline username + password row →
 * Log In. The server resolves the credentials to a player_id via
 * /api/login, the scene then opens the WS, sends C2S_AUTH +
 * C2S_START_RUN, and hands off to the Game scene. Players with no
 * account click Register to open the in-game registration modal.
 * A small sponsors panel sits directly below the login panel.
 */
export class MainMenu extends Phaser.Scene {
  private usernameInput?: HTMLInputElement;
  private passwordInput?: HTMLInputElement;
  private statusDot?: Phaser.GameObjects.Arc;
  private statusText?: Phaser.GameObjects.Text;
  private startButton?: HTMLButtonElement;
  private submitting = false;
  private resizeHandlers: Array<() => void> = [];
  private sponsorLogoEls: HTMLImageElement[] = [];
  private sponsorHoverRects: Phaser.GameObjects.Rectangle[] = [];
  private controlsTexts: Phaser.GameObjects.Text[] = [];
  private loginTexts: Phaser.GameObjects.Text[] = [];
  private readyTexts: Phaser.GameObjects.GameObject[] = [];
  private readyEnterKey?: Phaser.Input.Keyboard.Key;
  private readyPlayerName = "";
  private readyActive = false;

  private static readonly PANEL_WIDTH = 460;
  private static readonly PANEL_MIN_WIDTH = 340;
  private static readonly PANEL_HEIGHT = 200;
  private static readonly PANEL_MARGIN = 40;
  private static readonly PANEL_PADDING = 20;
  // Inline label column: "Username:" / "Password:" sit to the left of
  // each input, same row.
  private static readonly LABEL_COLUMN_WIDTH = 110;
  private static readonly LABEL_GAP = 8;
  private static readonly SPONSOR_PANEL_HEIGHT = 140;
  private static readonly SPONSOR_PANEL_GAP = 12;
  private static readonly SPONSOR_CARD_HEIGHT = 92;
  private static readonly SPONSOR_CARD_GAP = 10;
  private getPanelLayout() {
    const margin = Math.min(MainMenu.PANEL_MARGIN, Math.max(16, this.scale.width * 0.05));
    const panelWidth = Phaser.Math.Clamp(
      this.scale.width - margin * 2,
      MainMenu.PANEL_MIN_WIDTH,
      MainMenu.PANEL_WIDTH,
    );
    const inputWidth = panelWidth - MainMenu.PANEL_PADDING * 2;
    const labelColumnWidth = panelWidth < 400 ? 92 : MainMenu.LABEL_COLUMN_WIDTH;
    const rowInputWidth = inputWidth - labelColumnWidth - MainMenu.LABEL_GAP;
    const panelX = this.scale.width - panelWidth - margin;
    const stackBottom = this.scale.height - margin;
    const sponsorTop = stackBottom - MainMenu.SPONSOR_PANEL_HEIGHT;
    const loginBottom = sponsorTop - MainMenu.SPONSOR_PANEL_GAP;
    const loginTop = loginBottom - MainMenu.PANEL_HEIGHT;
    const panelY = loginTop + 30;
    const contentX = panelX + MainMenu.PANEL_PADDING;

    return {
      panelWidth,
      inputWidth,
      labelColumnWidth,
      rowInputWidth,
      panelX,
      sponsorTop,
      panelY,
      contentX,
    };
  }
  private setModalActive(active: boolean): void {
    if (this.usernameInput) this.usernameInput.disabled = active;
    if (this.passwordInput) this.passwordInput.disabled = active;
    if (this.startButton) this.startButton.disabled = active;
    for (const img of this.sponsorLogoEls) {
      img.style.pointerEvents = active ? "none" : "auto";
      img.style.filter = active ? "blur(4px) brightness(0.7)" : "";
    }
    this.input.enabled = !active;
  }

  constructor() {
    super({ key: "MainMenu" });
  }

  create(): void {
    const w = this.scale.width;
    const h = this.scale.height;

    // Restart / return-to-menu can leave gameplay keys marked as held on the
    // shared keyboard manager. Clear that state before the menu becomes
    // interactive again so held movement / restart keys don't leak across.
    this.input.keyboard?.resetKeys();

    if (this.textures.exists("title")) {
      this.add
        .image(w / 2, h / 2, "title")
        .setDisplaySize(w, h)
        .setDepth(DEPTH_SPLASH);
    }

    const layout = this.getPanelLayout();
    const { panelX, panelY, panelWidth, inputWidth, labelColumnWidth, rowInputWidth, sponsorTop, contentX } = layout;

    this.add
      .rectangle(
        panelX,
        panelY - 30,
        panelWidth,
        MainMenu.PANEL_HEIGHT,
        DEEP_NAVY,
        0.85,
      )
      .setStrokeStyle(2, MID_BLUE)
      .setOrigin(0, 0);

    const inputX = contentX + labelColumnWidth + MainMenu.LABEL_GAP;
    // Vertically-center the 18 px label against the 32 px input.
    const labelOffsetY = 7;
    const usernameRowY = panelY - 10;
    const passwordRowY = usernameRowY + 42;

    this.loginTexts.push(
      this.add.text(contentX, usernameRowY + labelOffsetY, "Username:", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(TEXT_PRIMARY),
      }),
    );
    this.usernameInput = this.mountTextInput(
      inputX,
      usernameRowY,
      rowInputWidth,
      {
        type: "text",
        placeholder: "username",
        maxLength: 32,
        autocomplete: "username",
      },
    );

    this.loginTexts.push(
      this.add.text(contentX, passwordRowY + labelOffsetY, "Password:", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(TEXT_PRIMARY),
      }),
    );
    this.passwordInput = this.mountTextInput(
      inputX,
      passwordRowY,
      rowInputWidth,
      {
        type: "password",
        placeholder: "password",
        maxLength: 128,
        autocomplete: "current-password",
      },
    );

    this.startButton = this.mountButton(
      contentX + inputWidth,
      panelY + 78,
      "[ Log In ]",
      true,
    );

    this.statusDot = this.add.circle(w - 132, h - 18, 4, TEXT_MUTED);
    this.statusText = this.add
      .text(w - 120, h - 18, "Idle", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SMALL,
        color: hex(TEXT_MUTED),
        wordWrap: { width: inputWidth - 34 },
      })
      .setOrigin(0, 0.5);

    this.loginTexts.push(
      ...this.makePanelActionRow(contentX, panelY + 140, inputWidth, [
        {
          label: "[ Register → ]",
          onClick: () => {
            this.hideLoginForm();
            this.setModalActive(true);
            this.scene.launch("RegisterMenu");
            this.scene.pause();
          },
        },
        {
          label: "[ Source ↓ ]",
          onClick: () => window.open("/source.tar.gz", "_blank", "noopener,noreferrer"),
        },
        {
          label: "[ Leaderboard ↗ ]",
          onClick: () => window.open("/leaderboard", "_blank", "noopener,noreferrer"),
        },
      ]),
    );

    this.drawControlsPanel(h);

    this.drawSponsorsPanel(panelX, sponsorTop, panelWidth, inputWidth);

    const sync = getSync();
    sync.state.on("authOk", this.onAuthOk, this);
    sync.state.on("runStarted", this.onRunStarted, this);
    sync.state.on("error", this.onError, this);

    const kb = this.input.keyboard;
    if (kb) {
      this.readyEnterKey = kb.addKey(Phaser.Input.Keyboard.KeyCodes.ENTER, false);
      this.readyEnterKey.on("down", this.onReadyEnter, this);
    }

    this.events.on("shutdown", this.cleanup, this);
    this.events.on("resume", this.onResume, this);

    const cachedId = getCachedPlayerId();
    if (cachedId) {
      void this.resumeCachedSession(cachedId);
    } else {
      setTimeout(() => this.usernameInput?.focus(), 50);
    }
  }

  /**
   * Sponsors panel below the login panel. Same width + stroke as the login
   * panel, a `// sponsors` header in muted blue, then N evenly-spaced cards
   * that currently show a placeholder glyph. When a sponsor's `imageKey` is
   * populated, the scene will draw the image in place of the glyph.
   */
  private drawSponsorsPanel(
    panelX: number,
    panelTop: number,
    panelWidth: number,
    inputWidth: number,
  ): void {
    this.add
      .rectangle(
        panelX,
        panelTop,
        panelWidth,
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
    const innerWidth = inputWidth;
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
    // Transparent card — only the MID_BLUE stroke defines each slot;
    // the parent DEEP_NAVY panel shows through. Logos are HTML <img>
    // overlays for high-quality browser downscaling that Phaser's NPOT
    // textures don't provide.
    const frame = this.add
      .rectangle(x, y, w, h, 0, 0)
      .setStrokeStyle(1, MID_BLUE)
      .setOrigin(0, 0);
    const glow = this.add
      .rectangle(x, y, w, h, 0, 0)
      .setStrokeStyle(2, ACCENT_PRIMARY, 0)
      .setOrigin(0, 0);
    this.sponsorHoverRects.push(frame, glow);

    let hoverTween: Phaser.Tweens.Tween | undefined;
    const setHover = (active: boolean): void => {
      if (!sponsor.url) return;
      frame.setStrokeStyle(1, active ? ACCENT_PRIMARY : MID_BLUE);
      if (hoverTween) hoverTween.stop();
      hoverTween = this.tweens.add({
        targets: glow,
        alpha: active ? 0.85 : 0,
        duration: active ? 120 : 160,
        ease: active ? "Sine.easeOut" : "Sine.easeIn",
      });
    };

    if (sponsor.url) {
      frame.setInteractive({ useHandCursor: true });
      frame.on("pointerover", () => setHover(true));
      frame.on("pointerout", () => setHover(false));
      frame.on("pointerdown", () => {
        window.open(sponsor.url, "_blank", "noopener,noreferrer");
      });
    }

    if (sponsor.imageSrc) {
      this.mountSponsorImage(x, y, w, h, sponsor, setHover);
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
    onHoverChange?: (active: boolean) => void,
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
      transition: sponsor.url ? "transform 140ms ease, filter 140ms ease" : "none",
    } as Partial<CSSStyleDeclaration>);

    if (!parent.style.position) parent.style.position = "relative";
    parent.appendChild(img);
    this.sponsorLogoEls.push(img);

    if (sponsor.url) {
      img.addEventListener("click", () => {
        window.open(sponsor.url, "_blank", "noopener,noreferrer");
      });
      img.addEventListener("mouseenter", () => {
        onHoverChange?.(true);
        img.style.transform = "translateY(-1px) scale(1.03)";
        img.style.filter = "brightness(1.08)";
      });
      img.addEventListener("mouseleave", () => {
        onHoverChange?.(false);
        img.style.transform = "";
        img.style.filter = "";
      });
    }

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

  private async connectForAuth(): Promise<boolean> {
    const sync = getSync();
    if (sync.isConnected()) return true;

    this.setStatus("Connecting", WARNING);
    try {
      await sync.connect(WS_URL);
      return true;
    } catch (err) {
      console.error("[MainMenu] connect failed", err);
      this.setStatus("Connection failed. Refresh to retry.", ERROR);
      return false;
    }
  }

  private async resumeCachedSession(playerId: string): Promise<void> {
    this.hideLoginForm();
    this.setStatus("Resuming", WARNING);
    this.submitting = true;

    if (!(await this.connectForAuth())) {
      this.submitting = false;
      this.showLoginForm();
      return;
    }

    getSync().sendAuth(playerId);
  }

  private hideLoginForm(): void {
    if (this.usernameInput) this.usernameInput.style.display = "none";
    if (this.passwordInput) this.passwordInput.style.display = "none";
    if (this.startButton) this.startButton.style.display = "none";
    for (const text of this.loginTexts) text.setVisible(false);
    // Drop focus off the now-hidden form controls so Space goes to the
    // queue handler instead of re-firing the login button.
    if (
      document.activeElement === this.usernameInput ||
      document.activeElement === this.passwordInput ||
      document.activeElement === this.startButton
    ) {
      (document.activeElement as HTMLElement).blur();
    }
  }

  private showLoginForm(): void {
    if (this.usernameInput) this.usernameInput.style.display = "";
    if (this.passwordInput) this.passwordInput.style.display = "";
    if (this.startButton) this.startButton.style.display = "";
    for (const text of this.loginTexts) text.setVisible(true);
    this.clearReadyView();
    this.readyActive = false;
    this.setStatus("Idle", TEXT_MUTED);
  }

  private showReadyView(displayName: string): void {
    this.hideLoginForm();
    this.clearReadyView();
    this.readyActive = true;
    this.readyPlayerName = displayName;

    const { panelY, contentX, inputWidth } = this.getPanelLayout();
    const buttonY = panelY + 43;
    const footerY = panelY + 140;

    const cat = this.add.text(contentX, panelY - 8, "Cat", {
      fontFamily: FONT_FAMILY,
      fontSize: FONT_SIZE_BODY,
      color: hex(TEXT_PRIMARY),
    });
    const name = this.add.text(contentX + 42, panelY - 10, displayName, {
      fontFamily: FONT_FAMILY,
      fontSize: FONT_SIZE_BODY,
      color: hex(ACCENT_PRIMARY),
    });
    const comma = this.add.text(name.x + name.displayWidth, panelY - 8, ",", {
      fontFamily: FONT_FAMILY,
      fontSize: FONT_SIZE_BODY,
      color: hex(TEXT_PRIMARY),
    });
    const prompt = this.add.text(
      comma.x + comma.displayWidth + 8,
      panelY - 8,
      "proceed.",
      {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(TEXT_PRIMARY),
      },
    );
    const startWidth = Math.floor(inputWidth * 0.46) + 10;
    const startX = contentX + (inputWidth - startWidth) / 2;
    const start = this.makeReadyPrimaryButton(startX, buttonY, startWidth, 46, "START", () =>
      this.startAuthenticatedRun(),
    );
    const hint = this.add
      .text(contentX + inputWidth / 2, buttonY + 51, "Press [ Enter ] or click Start to begin.", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SMALL,
        color: hex(TEXT_MUTED),
        wordWrap: { width: inputWidth },
      })
      .setOrigin(0.5, 0);
    const actions = this.makePanelActionRow(contentX, footerY, inputWidth, [
      { label: "[ ← Log Out ]", onClick: () => this.logoutReadySession() },
      {
        label: "[ Source ↓ ]",
        onClick: () => window.open("/source.tar.gz", "_blank", "noopener,noreferrer"),
      },
      {
        label: "[ Leaderboard ↗ ]",
        onClick: () => window.open("/leaderboard", "_blank", "noopener,noreferrer"),
      },
    ]);

    this.readyTexts.push(
      cat,
      name,
      comma,
      prompt,
      hint,
      ...start,
      ...actions,
    );
    this.setStatus("Authenticated", SUCCESS);
  }

  private setStatus(message: string, color: number): void {
    const visible = message.length > 0;
    this.statusDot?.setVisible(visible);
    this.statusText?.setVisible(visible);
    if (!visible) return;
    const displayColor = this.darkenStatusColor(color);
    this.statusDot?.setFillStyle(displayColor);
    this.statusText?.setText(message);
    this.statusText?.setColor(hex(displayColor));
  }

  private darkenStatusColor(color: number): number {
    if (color === SUCCESS) return 0x1f7f35;
    if (color === WARNING) return 0xb88716;
    if (color === ERROR) return 0x9f202a;
    return TEXT_DIM;
  }

  private clearReadyView(): void {
    for (const obj of this.readyTexts) obj.destroy();
    this.readyTexts = [];
  }

  private makeReadyPrimaryButton(
    x: number,
    y: number,
    width: number,
    height: number,
    label: string,
    onClick: () => void,
  ): Phaser.GameObjects.GameObject[] {
    const bg = this.add
      .rectangle(x, y, width, height, ACCENT_PRIMARY, 0)
      .setOrigin(0, 0)
      .setStrokeStyle(2, TEXT_PRIMARY)
      .setInteractive({ useHandCursor: true });
    const text = this.add
      .text(x + width / 2, y + height / 2, label, {
        fontFamily: FONT_FAMILY,
        fontSize: "20px",
        color: hex(ACCENT_PRIMARY),
      })
      .setOrigin(0.5)
      .setInteractive({ useHandCursor: true });
    text.setLetterSpacing(4);

    const setHover = (active: boolean): void => {
      bg.setFillStyle(ACCENT_PRIMARY, active ? 1 : 0);
      bg.setStrokeStyle(2, TEXT_PRIMARY);
      text.setColor(active ? hex(DEEP_NAVY) : hex(ACCENT_PRIMARY));
    };

    bg.on("pointerover", () => setHover(true));
    bg.on("pointerout", () => setHover(false));
    text.on("pointerover", () => setHover(true));
    text.on("pointerout", () => setHover(false));
    bg.on("pointerdown", onClick);
    text.on("pointerdown", onClick);

    return [bg, text];
  }

  private makePanelActionRow(
    x: number,
    y: number,
    width: number,
    actions: ReadonlyArray<{ label: string; onClick: () => void }>,
  ): Phaser.GameObjects.Text[] {
    const columns = [width / 6, width / 2 - 5, (width * 5) / 6];
    return actions.map((action, i) =>
      this.makeReadyAction(x + columns[i], y, action.label, action.onClick, "center"),
    );
  }

  private makeReadyAction(
    x: number,
    y: number,
    label: string,
    onClick: () => void,
    align: "left" | "center" | "right" = "left",
  ): Phaser.GameObjects.Text {
    const originX = align === "right" ? 1 : align === "center" ? 0.5 : 0;
    const text = this.add
      .text(x, y, label, {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SMALL,
        color: hex(TEXT_PRIMARY),
      })
      .setOrigin(originX, 0)
      .setInteractive({ useHandCursor: true });
    text.on("pointerover", () => text.setColor(hex(ACCENT_PRIMARY)));
    text.on("pointerout", () => text.setColor(hex(TEXT_PRIMARY)));
    text.on("pointerdown", onClick);
    return text;
  }

  private onReadyEnter(): void {
    if (!this.readyActive || this.submitting) return;
    const active = document.activeElement;
    if (active === this.usernameInput || active === this.passwordInput) return;
    this.startAuthenticatedRun();
  }

  private startAuthenticatedRun(): void {
    if (this.submitting) return;
    this.submitting = true;
    this.readyActive = false;
    this.clearReadyView();
    this.setStatus("Starting", WARNING);
    getSync().sendStartRun();
  }

  private logoutReadySession(): void {
    clearCachedPlayerId();
    resetSync();
    this.scene.restart();
  }

  private drawControlsPanel(sceneHeight: number): void {
    const panelX = MainMenu.PANEL_MARGIN;
    const panelHeight = 196;
    const panelTop = sceneHeight - panelHeight - MainMenu.PANEL_MARGIN;
    const panelWidth = 248;
    const paddingX = 16;
    const actionX = panelX + paddingX;
    const keyX = panelX + panelWidth - paddingX;
    const rowY = panelTop + 38;

    this.add
      .rectangle(panelX, panelTop, panelWidth, panelHeight, DEEP_NAVY, 0.68)
      .setStrokeStyle(2, MID_BLUE)
      .setOrigin(0, 0);

    this.controlsTexts.push(
      this.add.text(actionX, panelTop + 10, "// controls", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_CAPTION,
        color: hex(MID_BLUE),
      }),
      this.add
        .text(actionX, rowY, "Action", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_CAPTION,
          color: hex(TEXT_MUTED),
        })
        .setAlpha(0.9),
      this.add
        .text(keyX, rowY, "Key", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_CAPTION,
          color: hex(TEXT_MUTED),
        })
        .setOrigin(1, 0)
        .setAlpha(0.9),
    );

    for (let i = 0; i < CONTROL_BINDINGS.length; i++) {
      const [action, key] = CONTROL_BINDINGS[i];
      const y = rowY + 22 + i * 19;
      this.controlsTexts.push(
        this.add.text(actionX, y, action, {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_CAPTION,
          color: hex(TEXT_PRIMARY),
        }),
        this.add
          .text(keyX, y, key, {
            fontFamily: FONT_FAMILY,
            fontSize: FONT_SIZE_CAPTION,
            color: hex(TEXT_PRIMARY),
          })
          .setOrigin(1, 0),
      );
    }
  }

  private onResume(): void {
    this.setModalActive(false);
    this.showLoginForm();
    this.input.keyboard?.resetKeys();
    setTimeout(() => this.usernameInput?.focus(), 50);
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
    rightAligned = false,
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
      color: hex(ACCENT_PRIMARY),
      cursor: "pointer",
      outline: "none",
      whiteSpace: "nowrap",
    } as Partial<CSSStyleDeclaration>);

    const setHover = (on: boolean) => {
      btn.style.color = on ? BUTTON_HOVER_COLOR : hex(ACCENT_PRIMARY);
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
      btn.style.transform = rightAligned ? "translateX(-100%)" : "";
    };
    place();
    window.addEventListener("resize", place);
    this.scale.on("resize", place);
    this.resizeHandlers.push(place);

    return btn;
  }

  private async onStart(): Promise<void> {
    if (this.submitting) return;
    const username = this.usernameInput?.value.trim() ?? "";
    const password = this.passwordInput?.value ?? "";
    if (!username || !password) {
      this.setStatus("Missing username or password", ERROR);
      return;
    }

    this.submitting = true;
    this.setStatus("Logging in", WARNING);

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
        this.setStatus(body.error ?? `Login failed (HTTP ${res.status})`, ERROR);
        this.submitting = false;
        return;
      }
      const body = (await res.json()) as {
        player_id: string;
        display_name: string;
      };

      // Queue gate (Phase 5). When the lobby has QUEUE_ENABLED=true the
      // game server enforces AUTH_MODE=token, so we must be admitted
      // before the WS auth will pass. waitForQueueAdmission resolves
      // with `true` when the player went through the queue (waited
      // and was admitted, or admitted on first try) and `false` when
      // the queue is disabled in this deployment.
      let viaQueue: boolean;
      try {
        viaQueue = await this.waitForQueueAdmission(body.player_id, password);
      } catch (err) {
        console.error("[MainMenu] queue: failed", err);
        this.setStatus(err instanceof Error ? err.message : "Queue error", ERROR);
        this.submitting = false;
        return;
      }

      if (!(await this.connectForAuth())) {
        this.submitting = false;
        return;
      }

      this.setStatus("Authenticating", WARNING);
      // Cache the last authenticated id so explicit logout can clear any
      // stale local session state. WS auth is still performed only after
      // login + queue admission; pre-auth sockets can go idle behind the
      // queue and leave the client stuck with an unsent AUTH packet.
      setCachedPlayerId(body.player_id);
      getSync().sendAuth(body.player_id);
    } catch (err) {
      console.error("[MainMenu] login failed", err);
      this.setStatus("Network error. Try again", ERROR);
      this.submitting = false;
    }
  }

  /**
   * Polls /api/queue/status until the player has been admitted, or
   * resolves immediately if the queue is disabled (503).
   *
   * Resolves to `true` when the player went through the queue (waited
   * and was admitted, or admitted on first try); resolves to `false`
   * when the queue is disabled in this deployment so the caller can
   * skip the ready-prompt step.
   *
   * On position > 0 the method updates statusText to show line
   * position + estimated wait. Aborts only on a hard error (network
   * down, bad credentials); otherwise it keeps polling — the spec's
   * 5-min turn TTL means even a long wait is bounded.
   */
  private async waitForQueueAdmission(
    playerId: string,
    password: string,
  ): Promise<boolean> {
    let res: Response;
    try {
      res = await fetch("/api/queue/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ player_id: playerId, password }),
      });
    } catch {
      throw new Error("Network error contacting queue.");
    }
    if (res.status === 503) {
      // Queue feature off — proceed straight to WS auth.
      return false;
    }
    if (res.status === 409) {
      // Already in queue from a previous tab/session. Treat as queued
      // and fall into the poll loop.
    } else if (!res.ok) {
      // Lobby queue handlers respond with text/plain via http.Error,
      // not JSON, so prefer the raw response text. Fall back to a
      // generic message only if the body is empty.
      const text = (await res.text().catch(() => "")).trim();
      throw new Error(text || `Queue join failed (HTTP ${res.status}).`);
    } else {
      const body = (await res.json()) as {
        position: number;
        est_wait_ms: number;
        admitted: boolean;
      };
      if (body.admitted) return true;
      this.renderQueueStatus(body.position, body.est_wait_ms);
    }

    // Poll every 2 s until we're admitted. Identical cadence to the
    // lobby advancer tick so we shouldn't usually wait more than one
    // poll past the actual admit.
    const pollMs = 2000;
    while (true) {
      await new Promise((r) => setTimeout(r, pollMs));
      let statusRes: Response;
      try {
        statusRes = await fetch(
          "/api/queue/status/" + encodeURIComponent(playerId),
        );
      } catch {
        // Transient network glitch — keep trying. The user can refresh
        // to bail out.
        continue;
      }
      if (statusRes.status === 404) {
        // We were popped + AFK-evicted, or admin-kicked. Surface as a
        // recoverable error so the user can re-login.
        throw new Error("Queue slot expired. Log in again.");
      }
      if (!statusRes.ok) {
        // 503 mid-poll: queue disabled while we were waiting (admin
        // intervention?). Just proceed to auth — game server will
        // accept us if AUTH_MODE relaxed. Treat as "not via queue"
        // so the ready-prompt is skipped.
        if (statusRes.status === 503) return false;
        continue;
      }
      const status = (await statusRes.json()) as {
        position: number;
        est_wait_ms: number;
        admitted: boolean;
      };
      if (status.admitted) return true;
      this.renderQueueStatus(status.position, status.est_wait_ms);
    }
  }

  private renderQueueStatus(position: number, estWaitMs: number): void {
    // Math.ceil over Math.round so a 61 s wait reads "~2 min" rather
    // than "<1 min" — overstating by ≤59 s is friendlier than a
    // surprise "you said <1 min, why am I still waiting?".
    const minutes = Math.max(0, Math.ceil(estWaitMs / 60_000));
    const wait = minutes <= 1 ? "<1 min" : `~${minutes} min`;
    // position<=0 normally arrives via the admitted=true short-circuit
    // upstream, but a Join→Position race on the lobby could leak
    // {position:0, admitted:false} into here. Render a neutral
    // "Checking admission…" string instead of "0th in line" until the
    // next status poll (1 s later) clarifies.
    const message =
      position > 0
        ? `In queue: ${this.formatOrdinal(position)} in line · ${wait} wait`
        : `Queue status: Checking admission… · ${wait} wait`;
    this.setStatus(message, WARNING);
  }

  /**
   * formatOrdinal turns a positive integer into its English ordinal
   * (1 → "1st", 22 → "22nd", 113 → "113th"). The 11/12/13 exceptions
   * are special-cased; everything else looks at the last digit.
   */
  private formatOrdinal(n: number): string {
    const mod100 = n % 100;
    if (mod100 >= 11 && mod100 <= 13) return `${n}th`;
    switch (n % 10) {
      case 1:
        return `${n}st`;
      case 2:
        return `${n}nd`;
      case 3:
        return `${n}rd`;
      default:
        return `${n}th`;
    }
  }

  private onAuthOk(_displayName: string): void {
    this.submitting = false;
    this.showReadyView(_displayName);
  }

  private onRunStarted(): void {
    this.scene.start("Game");
    this.scene.launch("GameHUD");
  }

  private onError(err: { code: string; message: string }): void {
    // If a cached auth attempt failed, drop the cache and re-expose the
    // login form so the user can re-enter credentials. Network drops
    // ("disconnected") aren't auth issues, so leave the cache alone.
    if (err.code !== "disconnected") {
      clearCachedPlayerId();
      this.showLoginForm();
    }
    this.readyActive = false;
    this.clearReadyView();
    this.setStatus(`Error: ${err.message}`, ERROR);
    this.submitting = false;
  }

  private cleanup(): void {
    const sync = getSync();
    sync.state.off("authOk", this.onAuthOk, this);
    sync.state.off("runStarted", this.onRunStarted, this);
    sync.state.off("error", this.onError, this);
    this.events.off("resume", this.onResume, this);
    this.readyEnterKey?.off("down", this.onReadyEnter, this);
    this.readyEnterKey = undefined;
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

    for (const text of this.controlsTexts) text.destroy();
    this.controlsTexts = [];

    this.clearReadyView();
    this.loginTexts = [];

    for (const rect of this.sponsorHoverRects) rect.destroy();
    this.sponsorHoverRects = [];

    // Phaser reuses the same scene instance across stop/start, so any
    // field set during a previous login would otherwise carry over to
    // the next mount and silently swallow the first onStart click.
    // Reset to the fresh-instance defaults.
    this.submitting = false;
    this.readyPlayerName = "";
    this.readyActive = false;
  }
}

export default MainMenu;
