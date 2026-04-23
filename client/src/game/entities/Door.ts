import Phaser from "phaser";

import type { DoorDef } from "../../net/packet";

const DOOR_WIDTH = 40;
const DOOR_HEIGHT = 52;

const LOCKED_COLOR = 0x6b1818;
const LOCKED_STROKE = 0x2a0808;
const UNLOCKED_COLOR = 0x2d8b2d;
const UNLOCKED_STROKE = 0x0d3a0d;

/**
 * Interactable door. Locks/unlocks are driven by server
 * DOOR_UNLOCKED messages — call {@link setUnlocked}.
 */
export class Door extends Phaser.GameObjects.Container {
  readonly doorId: number;
  readonly targetRoom: number;
  unlocked: boolean;
  private readonly rect: Phaser.GameObjects.Rectangle;
  private readonly icon: Phaser.GameObjects.Text;

  constructor(scene: Phaser.Scene, def: DoorDef) {
    super(scene, def.x, def.y);
    this.doorId = def.door_id;
    this.targetRoom = def.target_room;
    this.unlocked = !def.locked;

    this.rect = scene.add.rectangle(0, 0, DOOR_WIDTH, DOOR_HEIGHT, LOCKED_COLOR);
    this.rect.setStrokeStyle(2, LOCKED_STROKE);
    this.add(this.rect);

    this.icon = scene.add.text(0, 0, this.unlocked ? "▲" : "⚿", {
      fontFamily: "monospace",
      fontSize: "18px",
      color: "#ffffff",
      stroke: "#000000",
      strokeThickness: 3,
    });
    this.icon.setOrigin(0.5, 0.5);
    this.add(this.icon);

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
    if (this.unlocked) {
      this.rect.setFillStyle(UNLOCKED_COLOR);
      this.rect.setStrokeStyle(2, UNLOCKED_STROKE);
      this.icon.setText("▲");
    } else {
      this.rect.setFillStyle(LOCKED_COLOR);
      this.rect.setStrokeStyle(2, LOCKED_STROKE);
      this.icon.setText("⚿");
    }
  }
}
