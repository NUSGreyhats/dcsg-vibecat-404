import Phaser from "phaser";

import type { DoorDef } from "../../net/packet";

const DOOR_WIDTH = 40;
const DOOR_HEIGHT = 52;

const WOOD_DARK = 0x3a2417;
const WOOD_MID = 0x7a4a2a;
const WOOD_LIGHT = 0xb5753d;
const WOOD_SHADOW = 0x1d120b;
const WOOD_GRAIN = 0xd08a4a;
const IRON_DARK = 0x16191f;
const IRON_LIGHT = 0x4a515e;
const OPENING = 0x07090f;

/**
 * Interactable door. Locks/unlocks are driven by server
 * DOOR_UNLOCKED messages — call {@link setUnlocked}.
 */
export class Door extends Phaser.GameObjects.Container {
  readonly doorId: number;
  readonly targetRoom: number;
  unlocked: boolean;
  private readonly body: Phaser.GameObjects.Graphics;

  constructor(scene: Phaser.Scene, def: DoorDef) {
    super(scene, def.x, def.y);
    this.doorId = def.door_id;
    this.targetRoom = def.target_room;
    this.unlocked = !def.locked;

    this.body = scene.add.graphics();
    this.add(this.body);

    this.repaint();

    scene.add.existing(this);
  }

  setUnlocked(unlocked: boolean): void {
    if (this.unlocked === unlocked) return;
    this.unlocked = unlocked;
    this.repaint();
  }

  /**
   * Hit-test for prompt UI. Treats the door as an axis-aligned box
   * centered on this container's position.
   */
  contains(px: number, py: number): boolean {
    const left = this.x - DOOR_WIDTH / 2;
    const right = this.x + DOOR_WIDTH / 2;
    const top = this.y - DOOR_HEIGHT / 2;
    const bottom = this.y + DOOR_HEIGHT / 2;
    return px >= left && px <= right && py >= top && py <= bottom;
  }

  private repaint(): void {
    this.drawGate(this.unlocked);
  }

  private drawGate(open: boolean): void {
    const left = -DOOR_WIDTH / 2;
    const top = -DOOR_HEIGHT / 2;

    this.body.clear();

    this.drawFrame(left, top);
    if (open) {
      this.drawOpenLeaves(left, top);
    } else {
      this.drawClosedLeaves(left, top);
    }
  }

  private drawFrame(left: number, top: number): void {
    this.body.fillStyle(OPENING, 0.75);
    this.body.fillRect(left + 2, top + 3, DOOR_WIDTH - 4, DOOR_HEIGHT - 3);

    this.body.fillStyle(WOOD_SHADOW, 0.8);
    this.body.fillRect(left - 4, top + 4, 6, DOOR_HEIGHT - 1);
    this.body.fillRect(left + DOOR_WIDTH - 2, top + 4, 6, DOOR_HEIGHT - 1);
    this.body.fillRect(left - 4, top - 2, DOOR_WIDTH + 8, 8);

    this.body.fillStyle(WOOD_DARK, 1);
    this.body.fillRect(left - 5, top + 1, 7, DOOR_HEIGHT + 1);
    this.body.fillRect(left + DOOR_WIDTH - 2, top + 1, 7, DOOR_HEIGHT + 1);
    this.body.fillRect(left - 5, top - 4, DOOR_WIDTH + 10, 8);

    this.body.lineStyle(2, WOOD_SHADOW, 0.95);
    this.body.strokeRect(left - 5, top + 1, 7, DOOR_HEIGHT + 1);
    this.body.strokeRect(left + DOOR_WIDTH - 2, top + 1, 7, DOOR_HEIGHT + 1);
    this.body.strokeRect(left - 5, top - 4, DOOR_WIDTH + 10, 8);

    this.body.fillStyle(WOOD_MID, 1);
    this.body.fillRect(left - 3, top + 4, 3, DOOR_HEIGHT - 3);
    this.body.fillRect(left + DOOR_WIDTH, top + 4, 3, DOOR_HEIGHT - 3);
    this.body.fillRect(left - 3, top - 2, DOOR_WIDTH + 6, 3);

    this.body.lineStyle(1, WOOD_LIGHT, 0.45);
    this.body.lineBetween(left - 2, top + 4, left - 2, top + DOOR_HEIGHT - 2);
    this.body.lineBetween(
      left + DOOR_WIDTH + 2,
      top + 4,
      left + DOOR_WIDTH + 2,
      top + DOOR_HEIGHT - 2,
    );
  }

  private drawClosedLeaves(left: number, top: number): void {
    this.drawWoodPanel(left + 3, top + 6, 17, DOOR_HEIGHT - 8, false);
    this.drawWoodPanel(left + 20, top + 6, 17, DOOR_HEIGHT - 8, false);

    this.body.lineStyle(2, WOOD_SHADOW, 0.95);
    this.body.lineBetween(0, top + 8, 0, top + DOOR_HEIGHT - 3);

    this.body.fillStyle(IRON_DARK, 0.95);
    this.body.fillRect(left + 4, -6, DOOR_WIDTH - 8, 7);
    this.body.fillRect(left + 4, top + DOOR_HEIGHT - 16, DOOR_WIDTH - 8, 7);
    this.body.fillStyle(IRON_LIGHT, 0.75);
    this.body.fillRect(left + 5, -5, DOOR_WIDTH - 10, 1);
    this.body.fillRect(left + 5, top + DOOR_HEIGHT - 15, DOOR_WIDTH - 10, 1);

    this.body.lineStyle(2, IRON_DARK, 0.95);
    this.body.lineBetween(left + 6, top + 9, left + 18, top + 26);
    this.body.lineBetween(left + 34, top + 9, left + 22, top + 26);

    this.drawStud(left + 8, -3);
    this.drawStud(left + DOOR_WIDTH - 8, -3);
    this.drawStud(left + 8, top + DOOR_HEIGHT - 12);
    this.drawStud(left + DOOR_WIDTH - 8, top + DOOR_HEIGHT - 12);
  }

  private drawOpenLeaves(left: number, top: number): void {
    this.drawWoodPanel(left - 9, top + 9, 14, DOOR_HEIGHT - 14, true);
    this.drawWoodPanel(left + DOOR_WIDTH - 5, top + 9, 14, DOOR_HEIGHT - 14, true);

    this.body.fillStyle(OPENING, 0.95);
    this.body.fillRect(left + 9, top + 8, DOOR_WIDTH - 18, DOOR_HEIGHT - 10);
    this.body.lineStyle(1, WOOD_LIGHT, 0.35);
    this.body.lineBetween(left + 9, top + 8, left + 9, top + DOOR_HEIGHT - 4);
    this.body.lineBetween(
      left + DOOR_WIDTH - 9,
      top + 8,
      left + DOOR_WIDTH - 9,
      top + DOOR_HEIGHT - 4,
    );

    this.body.fillStyle(IRON_DARK, 0.95);
    this.body.fillRect(left - 7, -4, 13, 7);
    this.body.fillRect(left + DOOR_WIDTH - 6, -4, 13, 7);
    this.body.fillRect(left - 7, top + DOOR_HEIGHT - 15, 13, 6);
    this.body.fillRect(left + DOOR_WIDTH - 6, top + DOOR_HEIGHT - 15, 13, 6);

    this.body.lineStyle(2, WOOD_LIGHT, 0.8);
    this.body.lineBetween(left + 11, top + 9, left + 4, top + 21);
    this.body.lineBetween(left + DOOR_WIDTH - 11, top + 9, left + DOOR_WIDTH - 4, top + 21);

    this.body.lineStyle(2, WOOD_LIGHT, 0.5);
    this.body.lineBetween(left + 11, top + DOOR_HEIGHT - 9, left + 4, top + DOOR_HEIGHT - 21);
    this.body.lineBetween(
      left + DOOR_WIDTH - 11,
      top + DOOR_HEIGHT - 9,
      left + DOOR_WIDTH - 4,
      top + DOOR_HEIGHT - 21,
    );

    this.drawStud(left - 3, -2);
    this.drawStud(left + DOOR_WIDTH + 3, -2);
    this.drawStud(left - 3, top + DOOR_HEIGHT - 12);
    this.drawStud(left + DOOR_WIDTH + 3, top + DOOR_HEIGHT - 12);
  }

  private drawWoodPanel(
    x: number,
    y: number,
    width: number,
    height: number,
    narrow: boolean,
  ): void {
    const plankCount = narrow ? 2 : 3;
    const plankWidth = width / plankCount;

    this.body.fillStyle(WOOD_MID, 1);
    this.body.fillRect(x, y, width, height);
    this.body.lineStyle(2, WOOD_SHADOW, 0.9);
    this.body.strokeRect(x, y, width, height);

    for (let i = 1; i < plankCount; i++) {
      const seamX = x + Math.floor(plankWidth * i);
      this.body.lineStyle(1, WOOD_SHADOW, 0.75);
      this.body.lineBetween(seamX, y + 2, seamX, y + height - 2);
    }

    this.body.lineStyle(1, WOOD_LIGHT, 0.45);
    this.body.lineBetween(x + 3, y + 3, x + 3, y + height - 4);
    this.body.lineBetween(x + width - 4, y + 4, x + width - 4, y + height - 3);

    this.body.lineStyle(1, WOOD_GRAIN, 0.5);
    this.body.lineBetween(x + 5, y + 10, x + width - 5, y + 13);
    this.body.lineBetween(x + 4, y + height - 16, x + width - 6, y + height - 19);
  }

  private drawStud(x: number, y: number): void {
    this.body.fillStyle(IRON_LIGHT, 1);
    this.body.fillCircle(x, y, 2);
    this.body.lineStyle(1, IRON_DARK, 0.9);
    this.body.strokeCircle(x, y, 2);
  }
}
