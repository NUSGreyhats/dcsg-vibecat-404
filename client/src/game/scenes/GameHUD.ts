import { Scene } from "phaser";
import { PLAYER_MAX_HEALTH } from "../constants";

const BAR_X = 56;
const BAR_Y = 20;
const BAR_WIDTH = 160;
const BAR_HEIGHT = 18;

export class GameHUD extends Scene {
  private healthBarFill: Phaser.GameObjects.Graphics;
  private healthText: Phaser.GameObjects.Text;
  private killText: Phaser.GameObjects.Text;
  private roomText: Phaser.GameObjects.Text;

  constructor() {
    super("GameHUD");
  }

  create() {
    const panel = this.add.graphics();
    panel.fillStyle(0x000000, 0.55);
    panel.fillRoundedRect(10, 8, 260, 40, 8);
    panel.lineStyle(1, 0x555555, 0.6);
    panel.strokeRoundedRect(10, 8, 260, 40, 8);

    this.add.text(18, 15, "HP", {
      fontFamily: "Arial Black",
      fontSize: "18px",
      color: "#ffffff",
      stroke: "#000000",
      strokeThickness: 2,
    });

    const barBg = this.add.graphics();
    barBg.fillStyle(0x222222);
    barBg.fillRoundedRect(BAR_X, BAR_Y, BAR_WIDTH, BAR_HEIGHT, 4);
    barBg.lineStyle(1, 0x444444);
    barBg.strokeRoundedRect(BAR_X, BAR_Y, BAR_WIDTH, BAR_HEIGHT, 4);

    this.healthBarFill = this.add.graphics();

    this.healthText = this.add.text(222, 16, "", {
      fontFamily: "Arial",
      fontSize: "16px",
      color: "#ffffff",
      stroke: "#000000",
      strokeThickness: 2,
    });

    const killPanel = this.add.graphics();
    killPanel.fillStyle(0x000000, 0.55);
    killPanel.fillRoundedRect(1024 - 140, 8, 130, 40, 8);
    killPanel.lineStyle(1, 0x555555, 0.6);
    killPanel.strokeRoundedRect(1024 - 140, 8, 130, 40, 8);

    this.killText = this.add.text(1024 - 130, 16, "Kills: 0", {
      fontFamily: "Arial Black",
      fontSize: "16px",
      color: "#ffcc00",
      stroke: "#000000",
      strokeThickness: 2,
    });

    this.roomText = this.add.text(512, 22, "Room 1", {
      fontFamily: "Arial Black",
      fontSize: "20px",
      color: "#aaddff",
      stroke: "#000000",
      strokeThickness: 4,
    }).setOrigin(0.5, 0.5);

    const gameScene = this.scene.get("Game");
    gameScene.events.on(
      "player-health-changed",
      this.updateHealthBar,
      this,
    );
    gameScene.events.on("kill-count-changed", this.updateKillCount, this);
    gameScene.events.on("room-changed", this.updateRoomLabel, this);
    gameScene.events.once("shutdown", () => {
      gameScene.events.off(
        "player-health-changed",
        this.updateHealthBar,
        this,
      );
      gameScene.events.off(
        "kill-count-changed",
        this.updateKillCount,
        this,
      );
      gameScene.events.off("room-changed", this.updateRoomLabel, this);
    });

    this.updateHealthBar(PLAYER_MAX_HEALTH, PLAYER_MAX_HEALTH);
  }

  private updateRoomLabel(room: number) {
    this.roomText.setText(`Room ${room}`);
  }

  private updateHealthBar(health: number, maxHealth: number) {
    this.healthBarFill.clear();

    const ratio = Math.max(0, health / maxHealth);
    const fillWidth = BAR_WIDTH * ratio;

    if (fillWidth > 0) {
      this.healthBarFill.fillStyle(this.getHealthColor(ratio));
      this.healthBarFill.fillRoundedRect(
        BAR_X,
        BAR_Y,
        fillWidth,
        BAR_HEIGHT,
        4,
      );
    }

    this.healthText.setText(`${health}/${maxHealth}`);
  }

  private updateKillCount(kills: number) {
    this.killText.setText(`Kills: ${kills}`);
  }

  private getHealthColor(ratio: number): number {
    if (ratio > 0.6) return 0x00cc44;
    if (ratio > 0.3) return 0xccaa00;
    return 0xcc2222;
  }
}
