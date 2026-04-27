import Phaser from "phaser";
import {
  ACCENT_PRIMARY,
  BUTTON_IDLE_COLOR,
  BONE_WHITE,
  DEEP_NAVY,
  DEPTH_HUD_BG,
  DEPTH_HUD_FILL,
  DEPTH_MODAL_TEXT,
  ERROR,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_CAPTION,
  FONT_SIZE_HEADING,
  MID_BLUE,
  MIDNIGHT,
  SUCCESS,
  TEXT_MUTED,
  TEXT_PRIMARY,
  hex,
} from "../ui/tokens";

export class RegisterMenu extends Phaser.Scene {
  private usernameInput?: HTMLInputElement;
  private contactInput?: HTMLInputElement;
  private passwordInput?: HTMLInputElement;
  private statusText?: Phaser.GameObjects.Text;
  private submitButton?: Phaser.GameObjects.Text;
  private backButton?: Phaser.GameObjects.Text;
  private resizeHandlers: Array<() => void> = [];
  private submitted = false;
  private appliedBlur?: { cam: Phaser.Cameras.Scene2D.Camera; fx: Phaser.FX.Blur };

  private static readonly PANEL_WIDTH = 400;
  private static readonly PANEL_HEIGHT = 440;
  private static readonly PANEL_PADDING = 20;
  private static readonly INPUT_WIDTH = RegisterMenu.PANEL_WIDTH - RegisterMenu.PANEL_PADDING * 2;

  constructor() {
    super({ key: "RegisterMenu" });
  }

  create(): void {
    const w = this.scale.width;
    const h = this.scale.height;
    const cx = w / 2;
    const cy = h / 2;

    this.applyCanvasBlur();

    this.add
      .rectangle(cx, cy, w, h, MIDNIGHT, 0.78)
      .setDepth(DEPTH_HUD_BG);

    this.add
      .rectangle(cx, cy + 10, RegisterMenu.PANEL_WIDTH, RegisterMenu.PANEL_HEIGHT, DEEP_NAVY, 0.92)
      .setStrokeStyle(2, MID_BLUE)
      .setDepth(DEPTH_HUD_FILL);

    this.add
      .text(cx, cy - 166, "Register", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_HEADING,
        color: hex(ACCENT_PRIMARY),
      })
      .setOrigin(0.5)
      .setDepth(DEPTH_MODAL_TEXT);

    this.add
      .text(cx, cy - 136, "Create an account.", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_CAPTION,
        color: hex(BONE_WHITE),
      })
      .setOrigin(0.5)
      .setDepth(DEPTH_MODAL_TEXT);

    const contentX = cx - RegisterMenu.PANEL_WIDTH / 2 + RegisterMenu.PANEL_PADDING;
    const usernameLabelY = cy - 104;
    const usernameRowY = usernameLabelY + 24;
    const rowGap = 52;
    const contactLabelY = usernameRowY + rowGap;
    const contactRowY = contactLabelY + 24;
    const passwordLabelY = contactRowY + rowGap;
    const passwordRowY = passwordLabelY + 24;

    this.add
      .text(contentX, usernameLabelY, "Username", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(BONE_WHITE),
      })
      .setDepth(DEPTH_MODAL_TEXT);
    this.usernameInput = this.mountTextInput(contentX, usernameRowY, RegisterMenu.INPUT_WIDTH, {
      placeholder: "username",
      maxLength: 32,
      autocomplete: "username",
    });

    this.add
      .text(contentX, contactLabelY, "Contact", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(BONE_WHITE),
      })
      .setDepth(DEPTH_MODAL_TEXT);
    this.contactInput = this.mountTextInput(contentX, contactRowY, RegisterMenu.INPUT_WIDTH, {
      placeholder: "email / phone / discord",
      maxLength: 256,
      autocomplete: "off",
    });

    this.add
      .text(contentX, passwordLabelY, "Password", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(BONE_WHITE),
      })
      .setDepth(DEPTH_MODAL_TEXT);
    this.passwordInput = this.mountTextInput(contentX, passwordRowY, RegisterMenu.INPUT_WIDTH, {
      placeholder: "password (min 6 chars)",
      maxLength: 128,
      autocomplete: "new-password",
      type: "password",
    });

    this.statusText = this.add.text(contentX, passwordRowY + 58, " ", {
      fontFamily: FONT_FAMILY,
      fontSize: FONT_SIZE_CAPTION,
      color: hex(TEXT_MUTED),
      wordWrap: { width: RegisterMenu.INPUT_WIDTH },
      fixedWidth: RegisterMenu.INPUT_WIDTH,
      align: "left",
      lineSpacing: 4,
    })
      .setOrigin(0, 0)
      .setDepth(DEPTH_MODAL_TEXT);

    const footerY = passwordRowY + 106;
    this.submitButton = this.add
      .text(contentX + RegisterMenu.INPUT_WIDTH - 10, footerY, "[ Register ]", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: BUTTON_IDLE_COLOR,
      })
      .setOrigin(1, 0)
      .setDepth(DEPTH_MODAL_TEXT)
      .setInteractive({ useHandCursor: true });
    this.backButton = this.add
      .text(contentX + 10, footerY, "[ Back to Login ]", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: BUTTON_IDLE_COLOR,
      })
      .setOrigin(0, 0)
      .setDepth(DEPTH_MODAL_TEXT)
      .setInteractive({ useHandCursor: true });
    this.bindButton(this.submitButton, () => this.onSubmit());
    this.bindButton(this.backButton, () => this.close());

    this.events.on("shutdown", this.cleanup, this);
    setTimeout(() => this.usernameInput?.focus(), 50);
  }

  private async onSubmit(): Promise<void> {
    if (this.submitted) return;

    const display_name = this.usernameInput?.value.trim() ?? "";
    const contact = this.contactInput?.value.trim() ?? "";
    const password = this.passwordInput?.value ?? "";

    if (!display_name || !contact || password.length < 6) {
      this.setStatus("Please fill out every field. Password must be 6+ characters.", "error");
      return;
    }

    this.submitted = true;
    this.submitButton?.disableInteractive();
    this.setStatus("Creating account...", "");

    try {
      const res = await fetch("/api/register", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ display_name, contact, password }),
      });
      const data = (await res.json().catch(() => ({}))) as { error?: string };
      if (!res.ok) {
        this.setStatus(data.error || `Registration failed (HTTP ${res.status}).`, "error");
        this.submitted = false;
        this.submitButton?.setInteractive({ useHandCursor: true });
        return;
      }

      this.setStatus("Account created. Back to Login.", "success");
      this.submitted = false;
      this.submitButton?.setInteractive({ useHandCursor: true });
      if (this.passwordInput) this.passwordInput.value = "";
    } catch {
      this.setStatus("Could not reach the server. Try again.", "error");
      this.submitted = false;
      this.submitButton?.setInteractive({ useHandCursor: true });
    }
  }

  private close(): void {
    this.input.enabled = false;
    this.scene.resume("MainMenu");
    this.scene.stop();
  }

  private applyCanvasBlur(): void {
    const target = this.scene.get("MainMenu");
    const cam = target?.cameras?.main;
    if (!cam || !cam.postFX) return;
    const fx = cam.postFX.addBlur(0, 3, 3, 1, 0xffffff, 4);
    this.appliedBlur = { cam, fx };
  }

  private mountTextInput(
    sceneX: number,
    sceneY: number,
    widthPx: number,
    opts: {
      placeholder: string;
      maxLength: number;
      autocomplete: string;
      type?: "text" | "password";
    },
  ): HTMLInputElement {
    const canvas = this.game.canvas;
    const parent = canvas.parentElement ?? document.body;
    const input = document.createElement("input");
    input.type = opts.type ?? "text";
    input.maxLength = opts.maxLength;
    input.placeholder = opts.placeholder;
    input.autocomplete = opts.autocomplete as AutoFill;
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
      input.style.left = `${rect.left - parentRect.left + sceneX * scaleX}px`;
      input.style.top = `${rect.top - parentRect.top + sceneY * scaleX}px`;
      input.style.width = `${widthPx * scaleX}px`;
    };
    place();
    window.addEventListener("resize", place);
    this.scale.on("resize", place);
    this.resizeHandlers.push(place);

    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") this.onSubmit();
    });

    return input;
  }

  private bindButton(btn: Phaser.GameObjects.Text, onClick: () => void): void {
    const idle = hex(ACCENT_PRIMARY);
    const hover = BUTTON_IDLE_COLOR;
    btn.setColor(idle);
    btn.on("pointerover", () => btn.setColor(hover));
    btn.on("pointerout", () => btn.setColor(idle));
    btn.on("pointerdown", onClick);
  }

  private setStatus(text: string, kind: "error" | "success" | ""): void {
    if (!this.statusText) return;
    this.statusText.setText(text);
    this.statusText.setColor(
      kind === "error"
        ? hex(ERROR)
        : kind === "success"
          ? hex(SUCCESS)
          : hex(TEXT_MUTED),
    );
  }

  private cleanup(): void {
    if (this.appliedBlur) {
      try {
        this.appliedBlur.cam.postFX?.remove(this.appliedBlur.fx);
      } catch {
        // ignore
      }
      this.appliedBlur = undefined;
    }

    for (const fn of this.resizeHandlers) {
      window.removeEventListener("resize", fn);
      this.scale.off("resize", fn);
    }
    this.resizeHandlers = [];

    this.usernameInput?.remove();
    this.usernameInput = undefined;
    this.contactInput?.remove();
    this.contactInput = undefined;
    this.passwordInput?.remove();
    this.passwordInput = undefined;
    this.submitButton?.destroy();
    this.submitButton = undefined;
    this.backButton?.destroy();
    this.backButton = undefined;
  }
}

export default RegisterMenu;
