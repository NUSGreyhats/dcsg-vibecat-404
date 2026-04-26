import Phaser from "phaser";
import { resetReplaySync } from "../net/runContext";
import { formatTime } from "../ui/formatters";
import {
  ACCENT_PRIMARY,
  CANVAS_BG,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_CAPTION,
  FONT_SIZE_SECTION,
  MID_BLUE,
  TEXT_MUTED,
  TEXT_PRIMARY,
  hex,
} from "../ui/tokens";

type ReplayMeta = {
  display_name: string;
  total_ms: number;
  splits: { room_index: number; time_ms: number; kills: number }[];
};

/**
 * Terminal screen after a replay session ends — engine emitted
 * RUN_COMPLETE, the WS dropped, the meta fetch 404'd, etc. Two outbound
 * paths: Restart (re-enters ReplayConnecting with the same replay_id)
 * and Back to admin panel.
 */
export class ReplayEnd extends Phaser.Scene {
  constructor() {
    super({ key: "ReplayEnd" });
  }

  init(data: {
    meta?: ReplayMeta;
    outcome?: string;
    totalMs?: number;
    rank?: number;
    reason?: string;
  }): void {
    this.data.set("meta", data.meta);
    this.data.set("outcome", data.outcome);
    this.data.set("totalMs", data.totalMs);
    this.data.set("rank", data.rank);
    this.data.set("reason", data.reason ?? "Replay ended");
  }

  create(): void {
    resetReplaySync();

    this.cameras.main.setBackgroundColor(CANVAS_BG);
    const cx = this.scale.width / 2;
    let cy = this.scale.height / 2 - 100;

    const meta = this.data.get("meta") as ReplayMeta | undefined;
    const reason = this.data.get("reason") as string;
    const totalMs = this.data.get("totalMs") as number | undefined;
    const outcome = this.data.get("outcome") as string | undefined;

    this.add
      .text(cx, cy, meta?.display_name ? `Replay of ${meta.display_name}` : "Replay", {
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
    cy += 36;

    if (typeof totalMs === "number") {
      const label = outcome === "failed" ? "FAILED" : formatTime(totalMs);
      this.add
        .text(cx, cy, `Reproduced: ${label}`, {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_BODY,
          color: hex(ACCENT_PRIMARY),
        })
        .setOrigin(0.5);
      cy += 24;
    }

    if (meta) {
      this.add
        .text(cx, cy, `Recorded: ${formatTime(meta.total_ms)}`, {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_BODY,
          color: hex(TEXT_MUTED),
        })
        .setOrigin(0.5);
      cy += 28;

      if (meta.splits && meta.splits.length > 0) {
        this.add
          .text(cx, cy, "SPLITS", {
            fontFamily: FONT_FAMILY,
            fontSize: FONT_SIZE_CAPTION,
            color: hex(TEXT_MUTED),
          })
          .setOrigin(0.5);
        cy += 18;
        for (const s of meta.splits) {
          this.add
            .text(cx, cy, `Room ${s.room_index}: ${formatTime(s.time_ms)}  (${s.kills} kills)`, {
              fontFamily: FONT_FAMILY,
              fontSize: FONT_SIZE_CAPTION,
              color: hex(TEXT_PRIMARY),
            })
            .setOrigin(0.5);
          cy += 16;
        }
      }
    }

    cy += 24;
    const restart = this.add
      .text(cx - 80, cy, "↻ Restart", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(ACCENT_PRIMARY),
      })
      .setOrigin(0.5)
      .setInteractive({ useHandCursor: true });
    restart.on("pointerdown", () => {
      this.scene.start("ReplayConnecting", { meta });
    });

    const back = this.add
      .text(cx + 80, cy, "← Back to admin", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(MID_BLUE),
      })
      .setOrigin(0.5)
      .setInteractive({ useHandCursor: true });
    back.on("pointerover", () => back.setColor(hex(ACCENT_PRIMARY)));
    back.on("pointerout", () => back.setColor(hex(MID_BLUE)));
    back.on("pointerdown", () => {
      window.location.href = "/admin/";
    });
  }
}

export default ReplayEnd;
