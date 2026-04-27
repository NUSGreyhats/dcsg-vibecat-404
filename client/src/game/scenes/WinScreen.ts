import Phaser from "phaser";
import { getSync, resetSync } from "../net/runContext";
import { RunOutcome } from "../../net/packet";
import type { LeaderboardEntry, SplitData } from "../../net/packet";
import { getConfig } from "../../net/config";
import { formatTime } from "../ui/formatters";
import {
  ACCENT_PRIMARY,
  BUTTON_HOVER_COLOR,
  BUTTON_IDLE_COLOR,
  BUTTON_TEXT_STYLE,
  ERROR,
  FONT_FAMILY,
  FONT_SIZE_BODY_EM,
  FONT_SIZE_SECTION,
  FONT_SIZE_SMALL,
  FONT_SIZE_SUBTITLE,
  FONT_SIZE_TITLE,
  TEXT_PRIMARY,
  TEXT_SECONDARY,
  hex,
} from "../ui/tokens";

const HEADER_STYLE: Phaser.Types.GameObjects.Text.TextStyle = {
  fontFamily: FONT_FAMILY,
  fontSize: FONT_SIZE_BODY_EM,
  color: hex(ACCENT_PRIMARY),
};

const ROW_STYLE: Phaser.Types.GameObjects.Text.TextStyle = {
  fontFamily: FONT_FAMILY,
  fontSize: FONT_SIZE_SMALL,
  color: hex(TEXT_PRIMARY),
};

/**
 * End-of-run summary + live leaderboard. "Play Again" tears down the WS and
 * returns to MainMenu.
 */
export class WinScreen extends Phaser.Scene {
  private leaderboardTexts: Phaser.GameObjects.Text[] = [];
  private leaderboardHeader?: Phaser.GameObjects.Text;
  private playAgainButton?: Phaser.GameObjects.Text;
  private outcome: RunOutcome = RunOutcome.COMPLETED;

  constructor() {
    super({ key: "WinScreen" });
  }

  init(data?: { outcome?: RunOutcome }): void {
    // Prefer scene-launch data; fall back to state mirror so a late direct
    // start() (e.g. hot-reload) still picks up the outcome.
    this.outcome =
      data?.outcome ?? getSync().state.runOutcome ?? RunOutcome.COMPLETED;
  }

  create(): void {
    const cx = this.scale.width / 2;

    const failed = this.outcome === RunOutcome.FAILED;
    const title = failed ? "Run Failed" : "Run Complete";
    const titleColor = failed ? hex(ERROR) : hex(ACCENT_PRIMARY);

    this.add
      .text(cx, 60, title, {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_TITLE,
        color: titleColor,
      })
      .setOrigin(0.5);

    const state = getSync().state;

    const totalMs = state.totalMs ?? 0;
    const rank = state.rank ?? 0;
    const splits = state.splits ?? [];

    this.add
      .text(cx, 130, `Total time:  ${formatTime(totalMs)}`, {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SUBTITLE,
        color: hex(TEXT_PRIMARY),
      })
      .setOrigin(0.5);

    // Rank row only makes sense for completed runs; failed runs are not
    // leaderboard-eligible (see foundations/08-leaderboard-replay.md).
    if (!failed) {
      this.add
        .text(cx, 170, `Rank:  #${rank}`, {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_SECTION,
          color: hex(TEXT_SECONDARY),
        })
        .setOrigin(0.5);
    } else {
      this.add
        .text(cx, 170, "Run not submitted to leaderboard", {
          fontFamily: FONT_FAMILY,
          fontSize: FONT_SIZE_SECTION,
          color: hex(TEXT_SECONDARY),
        })
        .setOrigin(0.5);
    }

    // Splits table (left column)
    this.add.text(120, 220, "Splits", HEADER_STYLE).setOrigin(0, 0);
    this.add
      .text(120, 250, "Room   Time        Kills", ROW_STYLE)
      .setOrigin(0, 0);

    this.renderSplits(splits);

    // Right column: leaderboard table when enabled, booth CTA when not.
    // The CTA path replaces the entire leaderboard pane (header + column
    // titles + entries) with a centered call-to-action so deploys that
    // record official runs in person have somewhere to redirect players.
    const config = getConfig();
    if (config.leaderboard_enabled) {
      this.leaderboardHeader = this.add
        .text(this.scale.width - 120, 220, "Leaderboard", HEADER_STYLE)
        .setOrigin(1, 0);
      this.add
        .text(
          this.scale.width - 120,
          250,
          "Rank  Name                 Time",
          ROW_STYLE,
        )
        .setOrigin(1, 0);

      this.renderLeaderboard(state.leaderboard ?? []);

      // Live leaderboard updates.
      getSync().state.on("leaderboard", this.onLeaderboard, this);
      this.leaderboardListenerAttached = true;
    } else {
      const message =
        config.leaderboard_disabled_message ??
        "Please visit our booth to record an official attempt";
      this.renderBoothCTA(message);
    }

    this.playAgainButton = this.add
      .text(cx, this.scale.height - 70, "[ Play Again ]", BUTTON_TEXT_STYLE)
      .setOrigin(0.5)
      .setInteractive({ useHandCursor: true });

    this.playAgainButton.on("pointerover", () =>
      this.playAgainButton?.setColor(BUTTON_HOVER_COLOR),
    );
    this.playAgainButton.on("pointerout", () =>
      this.playAgainButton?.setColor(BUTTON_IDLE_COLOR),
    );
    this.playAgainButton.on("pointerdown", () => this.onPlayAgain());

    this.events.on("shutdown", this.cleanup, this);
  }

  private renderBoothCTA(message: string): void {
    // Roughly the same horizontal footprint as the leaderboard table
    // (~360 px) so the right column doesn't feel empty. Right-aligned
    // origin matches the leaderboard layout this is replacing.
    const width = 360;
    const x = this.scale.width - 120;
    this.add
      .text(x, 220, "Submit your run", HEADER_STYLE)
      .setOrigin(1, 0);
    this.add
      .text(x, 280, message, {
        ...ROW_STYLE,
        wordWrap: { width, useAdvancedWrap: true },
        align: "right",
      })
      .setOrigin(1, 0);
  }

  private renderSplits(splits: SplitData[]): void {
    const startY = 280;
    for (let i = 0; i < splits.length; i++) {
      const s = splits[i];
      const line =
        `${String(s.room_index).padEnd(6)}` +
        `${formatTime(s.time_ms).padEnd(12)}` +
        `${s.kills}`;
      this.add.text(120, startY + i * 22, line, ROW_STYLE).setOrigin(0, 0);
    }
  }

  private renderLeaderboard(entries: LeaderboardEntry[]): void {
    for (const t of this.leaderboardTexts) t.destroy();
    this.leaderboardTexts = [];

    const startY = 280;
    const shown = entries.slice(0, 10);
    for (let i = 0; i < shown.length; i++) {
      const e = shown[i];
      const name = (e.display_name ?? "").slice(0, 20).padEnd(20);
      const line =
        `${String(e.rank).padEnd(5)}` +
        `${name} ` +
        `${formatTime(e.total_ms)}`;
      const txt = this.add
        .text(this.scale.width - 120, startY + i * 22, line, ROW_STYLE)
        .setOrigin(1, 0);
      this.leaderboardTexts.push(txt);
    }
  }

  private onLeaderboard(entries: LeaderboardEntry[]): void {
    this.renderLeaderboard(entries);
  }

  private onPlayAgain(): void {
    // Detach listeners BEFORE resetSync, otherwise cleanup() later would
    // hit a freshly-created sync and try to remove a listener that isn't there.
    if (this.leaderboardListenerAttached) {
      getSync().state.off("leaderboard", this.onLeaderboard, this);
      this.leaderboardListenerAttached = false;
    }
    this.resetHandlersAttached = false;
    resetSync();
    this.scene.start("MainMenu");
  }

  private resetHandlersAttached = true;
  private leaderboardListenerAttached = false;

  private cleanup(): void {
    if (this.resetHandlersAttached && this.leaderboardListenerAttached) {
      getSync().state.off("leaderboard", this.onLeaderboard, this);
      this.leaderboardListenerAttached = false;
    }
    for (const t of this.leaderboardTexts) t.destroy();
    this.leaderboardTexts = [];
    this.leaderboardHeader?.destroy();
    this.leaderboardHeader = undefined;
    this.playAgainButton?.destroy();
    this.playAgainButton = undefined;
  }
}

export default WinScreen;
