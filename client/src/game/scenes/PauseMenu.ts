import Phaser from "phaser";
import { resetSync } from "../net/runContext";
import {
  clearCachedPlayerId,
  isTimingsVisible,
  setTimingsVisible,
} from "../prefs";
import {
  ACCENT_PRIMARY,
  DEPTH_HUD_BG,
  DEPTH_HUD_FILL,
  DEPTH_MODAL_TEXT,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_BODY_EM,
  FONT_SIZE_CAPTION,
  FONT_SIZE_HEADING,
  MID_BLUE,
  MIDNIGHT,
  PANEL_FILL,
  TEXT_MUTED,
  TEXT_PRIMARY,
  hex,
} from "../ui/tokens";

type Page = "main" | "controls";

type MainItem = {
  key: "resume" | "toggleTimings" | "controls" | "restart" | "logout";
  label(): string;
};

type ControlSection = {
  title: string;
  bindings: ReadonlyArray<readonly [string, string]>;
};

// Mirrors gameplay controls plus scene-local menu bindings from
// Controls.ts, InventoryMenu.ts, ShopMenu.ts, and this scene.
const CONTROL_SECTIONS: ReadonlyArray<ControlSection> = [
  {
    title: "Gameplay",
    bindings: [
      ["Move", "W A S D / Arrows"],
      ["Aim", "Mouse"],
      ["Shoot", "Left mouse"],
      ["Switch weapon", "1 / 2 / 3"],
      ["Pick up item", "Walk near pickup"],
      ["Enter door", "Walk near unlocked door"],
      ["Open inventory", "I"],
      ["Open shop", "B"],
      ["Open menu", "Esc"],
    ],
  },
  {
    title: "Inventory",
    bindings: [
      ["Select item", "Up / Down"],
      ["Use item", "Enter"],
      ["Close", "Esc / I"],
    ],
  },
  {
    title: "Shop",
    bindings: [
      ["Select offer", "Up / Down"],
      ["Buy offer", "Enter"],
      ["Close", "Esc / B"],
    ],
  },
  {
    title: "Pause menu",
    bindings: [
      ["Select item", "Up / Down"],
      ["Confirm", "Enter"],
      ["Back / close", "Esc"],
    ],
  },
];

const PANEL_WIDTH = 720;
const PANEL_HEIGHT = 560;
// Phaser camera post-FX blur tunings. quality=0 is low-pass (cheap and
// the only one that visibly works on most platforms); the (x, y) values
// are the blur radius in px.
const BLUR_QUALITY = 0;
const BLUR_X = 3;
const BLUR_Y = 3;
const BLUR_STRENGTH = 1;
const BLUR_STEPS = 4;
const BLURRED_SCENES = ["Game", "GameHUD"] as const;

/**
 * Modal in-game menu. Pauses the local Game scene rendering while open;
 * the SERVER continues ticking, so this is not a true pause — the player
 * can still take damage. Closes via Esc (or the "Close" entry). Hosts a
 * small page stack — the "Controls" entry pushes a controls page that
 * Esc pops. While open the Game + GameHUD cameras get a Phaser post-FX
 * blur so the menu (drawn on a separate scene/camera) stays sharp on top.
 */
export class PauseMenu extends Phaser.Scene {
  private page: Page = "main";
  private selected = 0;
  private title?: Phaser.GameObjects.Text;
  private hint?: Phaser.GameObjects.Text;
  private body: Phaser.GameObjects.Text[] = [];
  private items: MainItem[] = [];
  private keys?: {
    up: Phaser.Input.Keyboard.Key;
    down: Phaser.Input.Keyboard.Key;
    enter: Phaser.Input.Keyboard.Key;
    esc: Phaser.Input.Keyboard.Key;
  };
  private appliedBlurs: {
    cam: Phaser.Cameras.Scene2D.Camera;
    fx: Phaser.FX.Blur;
  }[] = [];

  constructor() {
    super({ key: "PauseMenu" });
  }

  create(): void {
    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;

    this.applyCanvasBlur();

    this.items = [
      { key: "resume", label: () => "Close" },
      {
        key: "toggleTimings",
        label: () =>
          isTimingsVisible() ? "Hide Timings" : "Show Timings",
      },
      { key: "controls", label: () => "Controls" },
      { key: "restart", label: () => "Restart Run" },
      { key: "logout", label: () => "Log Out" },
    ];

    // Dim layer over the paused game.
    this.add
      .rectangle(cx, cy, this.scale.width, this.scale.height, MIDNIGHT, 0.55)
      .setDepth(DEPTH_HUD_BG);

    // Modal panel.
    this.add
      .rectangle(cx, cy, PANEL_WIDTH, PANEL_HEIGHT, PANEL_FILL, 0.95)
      .setStrokeStyle(2, MID_BLUE)
      .setDepth(DEPTH_HUD_FILL);

    this.renderPage();

    const kb = this.input.keyboard!;
    this.keys = {
      up: kb.addKey(Phaser.Input.Keyboard.KeyCodes.UP),
      down: kb.addKey(Phaser.Input.Keyboard.KeyCodes.DOWN),
      enter: kb.addKey(Phaser.Input.Keyboard.KeyCodes.ENTER),
      esc: kb.addKey(Phaser.Input.Keyboard.KeyCodes.ESC),
    };
    this.keys.up.on("down", () => this.move(-1));
    this.keys.down.on("down", () => this.move(1));
    this.keys.enter.on("down", () => this.activate());
    this.keys.esc.on("down", () => this.back());

    this.events.on("shutdown", this.cleanup, this);
  }

  private renderPage(): void {
    this.title?.destroy();
    this.title = undefined;
    this.hint?.destroy();
    this.hint = undefined;
    for (const t of this.body) t.destroy();
    this.body = [];

    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;

    if (this.page === "main") {
      this.title = this.add
        .text(cx, cy - 220, "Menu", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_HEADING,
          color: hex(ACCENT_PRIMARY),
        })
        .setOrigin(0.5)
        .setDepth(DEPTH_MODAL_TEXT);

      this.hint = this.add
        .text(
          cx,
          cy + 220,
          "[Up/Down] select   [Enter] confirm   [Esc] close",
          {
            fontFamily: FONT_FAMILY,
            fontSize: FONT_SIZE_CAPTION,
            color: hex(TEXT_MUTED),
          },
        )
        .setOrigin(0.5)
        .setDepth(DEPTH_MODAL_TEXT);

      const startY = cy - 110;
      for (let i = 0; i < this.items.length; i++) {
        const isSel = i === this.selected;
        const label = `${isSel ? "> " : "  "}${this.items[i].label()}`;
        const t = this.add
          .text(cx, startY + i * 44, label, {
            fontFamily: FONT_FAMILY,
            fontSize: FONT_SIZE_BODY_EM,
            color: hex(isSel ? ACCENT_PRIMARY : TEXT_PRIMARY),
          })
          .setOrigin(0.5)
          .setDepth(DEPTH_MODAL_TEXT);
        t.setInteractive({ useHandCursor: true });
        t.on("pointerover", () => {
          if (this.page !== "main") return;
          this.selected = i;
          this.renderPage();
        });
        t.on("pointerdown", () => {
          if (this.page !== "main") return;
          this.selected = i;
          this.activate();
        });
        this.body.push(t);
      }
    } else {
      this.title = this.add
        .text(cx, cy - 230, "Controls", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_HEADING,
          color: hex(ACCENT_PRIMARY),
        })
        .setOrigin(0.5)
        .setDepth(DEPTH_MODAL_TEXT);

      this.hint = this.add
        .text(cx, cy + 230, "[Esc] back", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_CAPTION,
          color: hex(TEXT_MUTED),
        })
        .setOrigin(0.5)
        .setDepth(DEPTH_MODAL_TEXT)
        .setInteractive({ useHandCursor: true });
      this.hint.on("pointerdown", () => this.back());

      this.renderControlSection(CONTROL_SECTIONS[0], cx - 320, cy - 175, 300);
      this.renderControlSection(CONTROL_SECTIONS[1], cx + 20, cy - 175, 300);
      this.renderControlSection(CONTROL_SECTIONS[2], cx + 20, cy - 40, 300);
      this.renderControlSection(CONTROL_SECTIONS[3], cx + 20, cy + 95, 300);
    }
  }

  private renderControlSection(
    section: ControlSection,
    x: number,
    y: number,
    width: number,
  ): void {
    const heading = this.add
      .text(x, y, section.title, {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_CAPTION,
        color: hex(TEXT_MUTED),
      })
      .setOrigin(0, 0)
      .setDepth(DEPTH_MODAL_TEXT);
    this.body.push(heading);

    const labelX = x;
    const valueX = x + width;
    const startY = y + 26;
    const rowGap = 24;
    for (let i = 0; i < section.bindings.length; i++) {
      const [name, key] = section.bindings[i];
      const rowY = startY + i * rowGap;
      const label = this.add
        .text(labelX, rowY, name, {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_BODY,
          color: hex(TEXT_PRIMARY),
        })
        .setOrigin(0, 0)
        .setDepth(DEPTH_MODAL_TEXT);
      const value = this.add
        .text(valueX, rowY, key, {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_BODY,
          color: hex(ACCENT_PRIMARY),
        })
        .setOrigin(1, 0)
        .setDepth(DEPTH_MODAL_TEXT);
      this.body.push(label, value);
    }
  }

  private move(delta: number): void {
    if (this.page !== "main") return;
    this.selected =
      (this.selected + delta + this.items.length) % this.items.length;
    this.renderPage();
  }

  private activate(): void {
    if (this.page !== "main") return;
    const item = this.items[this.selected];
    switch (item.key) {
      case "resume":
        this.close();
        return;
      case "toggleTimings": {
        const next = !isTimingsVisible();
        setTimingsVisible(next);
        // Tell the HUD scene live — it listens on the global game events bus.
        this.game.events.emit("timingsVisibilityChanged", next);
        // Re-render so the menu label flips immediately.
        this.renderPage();
        return;
      }
      case "controls":
        this.page = "controls";
        this.renderPage();
        return;
      case "restart":
        this.restart();
        return;
      case "logout":
        this.logout();
        return;
    }
  }

  private back(): void {
    if (this.page === "controls") {
      this.page = "main";
      this.renderPage();
      return;
    }
    this.close();
  }

  private close(): void {
    this.removeCanvasBlur();
    this.scene.resume("Game");
    this.scene.stop();
  }

  private restart(): void {
    // Mirror the WinScreen "Play Again" path: tear down the WS so the
    // server-side run state is dropped, then return to MainMenu where the
    // user re-auths and starts a fresh run. The blur is removed so the
    // login form behind isn't smeared. Other overlays are stopped
    // defensively so nothing bleeds through to the main menu.
    //
    // We don't explicitly stop self — scene.start() handles that. Calling
    // this.scene.stop() before this.scene.start() can leave the scene
    // plugin in an awkward state where the start call no-ops.
    this.removeCanvasBlur();
    this.scene.stop("Game");
    this.scene.stop("GameHUD");
    this.scene.stop("InventoryMenu");
    this.scene.stop("WinScreen");
    resetSync();
    this.scene.start("MainMenu");
  }

  private logout(): void {
    // Same as restart, but also wipes the cached player_id so MainMenu
    // shows the login form instead of auto-resuming the previous account.
    clearCachedPlayerId();
    this.restart();
  }

  private applyCanvasBlur(): void {
    for (const key of BLURRED_SCENES) {
      const target = this.scene.get(key);
      const cam = target?.cameras?.main;
      if (!cam || !cam.postFX) continue;
      const fx = cam.postFX.addBlur(
        BLUR_QUALITY,
        BLUR_X,
        BLUR_Y,
        BLUR_STRENGTH,
        0xffffff,
        BLUR_STEPS,
      );
      this.appliedBlurs.push({ cam, fx });
    }
  }

  private removeCanvasBlur(): void {
    for (const { cam, fx } of this.appliedBlurs) {
      // The Game/GameHUD scene may have been stopped first (e.g. during
      // an endRun teardown), in which case the camera is gone. Ignore
      // any access errors — the FX is going away with the camera anyway.
      try {
        cam.postFX?.remove(fx);
      } catch {
        // ignore
      }
    }
    this.appliedBlurs = [];
  }

  private cleanup(): void {
    this.title?.destroy();
    this.title = undefined;
    this.hint?.destroy();
    this.hint = undefined;
    for (const t of this.body) t.destroy();
    this.body = [];

    if (this.keys) {
      this.keys.up.removeAllListeners();
      this.keys.down.removeAllListeners();
      this.keys.enter.removeAllListeners();
      this.keys.esc.removeAllListeners();
      this.keys = undefined;
    }
    // Defensive: a forced shutdown (e.g. scene.stop from elsewhere) should
    // never leave the canvas blurred behind the next scene.
    this.removeCanvasBlur();
  }
}

export default PauseMenu;
