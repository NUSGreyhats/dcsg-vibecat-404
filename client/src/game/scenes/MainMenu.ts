import Phaser from "phaser";
import { getSync } from "../net/runContext";
import {
  clearCachedPlayerId,
  getCachedPlayerId,
  setCachedPlayerId,
} from "../prefs";
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
    // Transparent card — only the MID_BLUE stroke defines each slot;
    // the parent DEEP_NAVY panel shows through. Logos are HTML <img>
    // overlays for high-quality browser downscaling that Phaser's NPOT
    // textures don't provide.
    this.add
      .rectangle(x, y, w, h, 0, 0)
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
        const cachedId = getCachedPlayerId();
        if (cachedId) {
          // Skip the form — re-auth with the cached id and let onAuthOk
          // kick off START_RUN. If the server rejects (account gone /
          // server restarted), onError will clear the cache and re-show
          // the form so the user can re-enter credentials.
          this.hideLoginForm();
          this.statusText?.setText("Resuming session...");
          this.statusText?.setColor(hex(TEXT_MUTED));
          this.submitting = true;
          getSync().sendAuth(cachedId);
        } else {
          this.statusText?.setText("Connected. Log in to play.");
          this.statusText?.setColor(hex(SUCCESS));
        }
      })
      .catch((err) => {
        console.error("[MainMenu] connect failed", err);
        this.statusText?.setText(
          "Could not connect to the game server. Refresh to retry.",
        );
        this.statusText?.setColor(hex(ERROR));
      });
  }

  private hideLoginForm(): void {
    if (this.usernameInput) this.usernameInput.style.display = "none";
    if (this.passwordInput) this.passwordInput.style.display = "none";
    if (this.startButton) this.startButton.style.display = "none";
  }

  private showLoginForm(): void {
    if (this.usernameInput) this.usernameInput.style.display = "";
    if (this.passwordInput) this.passwordInput.style.display = "";
    if (this.startButton) this.startButton.style.display = "";
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
        this.statusText?.setText(
          err instanceof Error ? err.message : "Queue error.",
        );
        this.statusText?.setColor(hex(ERROR));
        this.submitting = false;
        return;
      }

      // Speedrun fairness: when the player went through the queue,
      // make them explicitly start the run. Auto-starting after a
      // multi-minute wait would mean the timer began the instant
      // the queue popped them, which is unfair if they walked away
      // from the keyboard. Skipped when the queue is off (dev), since
      // the player just clicked Log In moments ago.
      if (viaQueue) {
        const ready = await this.showReadyPrompt();
        if (!ready) {
          this.statusText?.setText("Forfeit — didn't start in time. Log in again to re-queue.");
          this.statusText?.setColor(hex(ERROR));
          this.submitting = false;
          return;
        }
      }

      this.statusText?.setText("Authenticating...");
      this.statusText?.setColor(hex(TEXT_MUTED));
      // Cache for auto-relogin on subsequent runs (Restart, Play Again,
      // page refresh). Cleared by onError if the server rejects the id.
      setCachedPlayerId(body.player_id);
      getSync().sendAuth(body.player_id);
    } catch (err) {
      console.error("[MainMenu] login failed", err);
      this.statusText?.setText("Network error. Try again.");
      this.statusText?.setColor(hex(ERROR));
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
    this.statusText?.setText(message);
    this.statusText?.setColor(hex(WARNING));
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

  /**
   * showReadyPrompt mounts a fullscreen overlay with a START button
   * and a 50 s countdown after a queue admission, so the speedrun
   * timer never starts without an explicit player action. Resolves
   * with `true` on click / Enter / Space, `false` on countdown
   * expiry. The 50 s window leaves ~10 s of margin against the
   * lobby's 60 s StartGrace AFK eviction.
   */
  private async showReadyPrompt(): Promise<boolean> {
    return new Promise<boolean>((resolve) => {
      const overlay = document.createElement("div");
      Object.assign(overlay.style, {
        position: "fixed",
        inset: "0",
        background: "rgba(13, 18, 34, 0.92)",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        zIndex: "9999",
        fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
        color: "#f5f0e6",
      });

      const title = document.createElement("div");
      title.textContent = "YOU'RE UP";
      Object.assign(title.style, {
        fontSize: "64px",
        letterSpacing: "8px",
        color: "#ffdd33",
        marginBottom: "16px",
      });

      const sub = document.createElement("div");
      sub.textContent = "Click START or press SPACE / ENTER when ready.";
      Object.assign(sub.style, {
        fontSize: "16px",
        color: "#8a8679",
        marginBottom: "32px",
      });

      const btn = document.createElement("button");
      btn.textContent = "START";
      Object.assign(btn.style, {
        fontSize: "24px",
        padding: "16px 48px",
        background: "transparent",
        color: "#ffdd33",
        border: "2px solid #ffdd33",
        cursor: "pointer",
        fontFamily: "inherit",
        letterSpacing: "8px",
        fontWeight: "bold",
      });

      const countdown = document.createElement("div");
      Object.assign(countdown.style, {
        marginTop: "32px",
        color: "#8a8679",
        fontSize: "13px",
      });

      overlay.append(title, sub, btn, countdown);
      document.body.appendChild(overlay);

      // Client gives 50 s; lobby's StartGrace is 60 s. The 10 s
      // margin covers polling lag (the player may not learn they're
      // admitted until up to 2 s after the lobby tick) plus the
      // few-hundred-ms WS auth + start_run round trip that gets
      // them into /internal/live-games and stops the AFK timer.
      const graceMs = 50_000;
      const startedAt = Date.now();
      let interval: number | undefined;
      const cleanup = (val: boolean) => {
        if (interval !== undefined) window.clearInterval(interval);
        window.removeEventListener("keydown", onKey);
        overlay.remove();
        resolve(val);
      };
      const onKey = (ev: KeyboardEvent) => {
        // Space reports as ev.key " " on modern browsers but "Spacebar"
        // on legacy Edge/IE; ev.code is consistently "Space" everywhere.
        // Match all three so the START prompt is keyboard-operable
        // wherever the page loads.
        if (
          ev.key === "Enter" ||
          ev.code === "Space" ||
          ev.key === " " ||
          ev.key === "Spacebar"
        ) {
          ev.preventDefault();
          cleanup(true);
        }
      };
      window.addEventListener("keydown", onKey);
      btn.addEventListener("click", () => cleanup(true));
      interval = window.setInterval(() => {
        const remaining = Math.max(0, graceMs - (Date.now() - startedAt));
        countdown.textContent = `Auto-forfeit in ${Math.ceil(remaining / 1000)}s`;
        if (remaining <= 0) cleanup(false);
      }, 250);
      btn.focus();
    });
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
    // If a cached auth attempt failed, drop the cache and re-expose the
    // login form so the user can re-enter credentials. Network drops
    // ("disconnected") aren't auth issues, so leave the cache alone.
    if (err.code !== "disconnected") {
      clearCachedPlayerId();
      this.showLoginForm();
    }
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

    // Phaser reuses the same scene instance across stop/start, so any
    // field set during a previous login (submitting/connected) would
    // otherwise carry over to the next mount and silently swallow the
    // first onStart click. Reset to the fresh-instance defaults.
    this.submitting = false;
    this.connected = false;
  }
}

export default MainMenu;
