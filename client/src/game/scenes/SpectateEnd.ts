import Phaser from "phaser";
import { resetSpectatorSync } from "../net/runContext";
import { formatTime } from "../ui/formatters";
import {
  ACCENT_PRIMARY,
  CANVAS_BG,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_SECTION,
  MID_BLUE,
  TEXT_MUTED,
  TEXT_PRIMARY,
  hex,
} from "../ui/tokens";

/**
 * Terminal screen after a spectator session ends — run completed,
 * connection dropped, server full, or any other reason returned by the
 * sync. Offers a single nav back to the selector.
 */
export class SpectateEnd extends Phaser.Scene {
  constructor() {
    super({ key: "SpectateEnd" });
  }

  init(data: {
    displayName?: string;
    outcome?: string;
    totalMs?: number;
    rank?: number;
    reason?: string;
  }): void {
    this.data.set("displayName", data.displayName ?? "");
    this.data.set("outcome", data.outcome);
    this.data.set("totalMs", data.totalMs);
    this.data.set("rank", data.rank);
    this.data.set("reason", data.reason ?? "Watch ended");
  }

  create(): void {
    resetSpectatorSync();

    this.cameras.main.setBackgroundColor(CANVAS_BG);
    const cx = this.scale.width / 2;
    let cy = this.scale.height / 2 - 60;

    const displayName = this.data.get("displayName") as string;
    const reason = this.data.get("reason") as string;
    const totalMs = this.data.get("totalMs") as number | undefined;
    const rank = this.data.get("rank") as number | undefined;
    const outcome = this.data.get("outcome") as string | undefined;

    this.add
      .text(cx, cy, displayName ? `Watching ${displayName}` : "Spectator", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SECTION,
        color: hex(TEXT_MUTED),
      })
      .setOrigin(0.5);
    cy += 36;

    this.add
      .text(cx, cy, reason, {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SECTION,
        color: hex(TEXT_PRIMARY),
      })
      .setOrigin(0.5);
    cy += 32;

    if (typeof totalMs === "number") {
      const label = outcome === "failed" ? "FAILED" : formatTime(totalMs);
      this.add
        .text(cx, cy, label, {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_SECTION,
          color: hex(ACCENT_PRIMARY),
        })
        .setOrigin(0.5);
      cy += 28;
      if (typeof rank === "number" && rank > 0 && outcome !== "failed") {
        this.add
          .text(cx, cy, `Rank #${rank}`, {
            fontFamily: FONT_FAMILY,
            fontSize: FONT_SIZE_BODY,
            color: hex(TEXT_MUTED),
          })
          .setOrigin(0.5);
        cy += 24;
      }
    }

    cy += 24;
    const link = this.add
      .text(cx, cy, "← Back to selector", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(MID_BLUE),
      })
      .setOrigin(0.5)
      .setInteractive({ useHandCursor: true });
    link.on("pointerover", () => link.setColor(hex(ACCENT_PRIMARY)));
    link.on("pointerout", () => link.setColor(hex(MID_BLUE)));
    link.on("pointerdown", () => {
      window.location.href = "/spectate";
    });
  }
}

export default SpectateEnd;
