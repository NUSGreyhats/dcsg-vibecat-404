import Phaser from "phaser";
import { getSync, resetSync } from "../net/runContext";
import type { LeaderboardEntry, SplitData } from "../../net/packet";
import { formatTime } from "../ui/formatters";
import {
  ACCENT_PRIMARY,
  BUTTON_HOVER_COLOR,
  BUTTON_IDLE_COLOR,
  BUTTON_TEXT_STYLE,
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

  constructor() {
    super({ key: "WinScreen" });
  }

  create(): void {
    const cx = this.scale.width / 2;

    this.add
      .text(cx, 60, "Run Complete", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_TITLE,
        color: hex(ACCENT_PRIMARY),
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

    this.add
      .text(cx, 170, `Rank:  #${rank}`, {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SECTION,
        color: hex(TEXT_SECONDARY),
      })
      .setOrigin(0.5);

    // Splits table (left column)
    this.add.text(120, 220, "Splits", HEADER_STYLE).setOrigin(0, 0);
    this.add
      .text(120, 250, "Room   Time        Kills", ROW_STYLE)
      .setOrigin(0, 0);

    this.renderSplits(splits);

    // Leaderboard (right column)
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

    // Live leaderboard updates.
    getSync().state.on("leaderboard", this.onLeaderboard, this);

    this.events.on("shutdown", this.cleanup, this);
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
    getSync().state.off("leaderboard", this.onLeaderboard, this);
    this.resetHandlersAttached = false;
    resetSync();
    this.scene.start("MainMenu");
  }

  private resetHandlersAttached = true;

  private cleanup(): void {
    if (this.resetHandlersAttached) {
      getSync().state.off("leaderboard", this.onLeaderboard, this);
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
