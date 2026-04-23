import Phaser from "phaser";

import type { PickupSpawn } from "../../net/packet";
import { BONE_WHITE, COIN_GOLD, TEXT_STROKE, hex } from "../ui/tokens";

const PICKUP_TYPE = {
  HEALTH: 1,
  AMMO: 2,
  SPEED: 3,
  DAMAGE: 4,
  LARGE_HEART: 5,
} as const;

type PickupStyle = {
  color: number;
  size: number;
  glyph: string;
  iconKey?: string;
};

function styleFor(type: number): PickupStyle {
  switch (type) {
    case PICKUP_TYPE.HEALTH:
      return { color: 0xdc2d2d, size: 16, glyph: "♥", iconKey: "heart" };
    case PICKUP_TYPE.AMMO:
      return { color: COIN_GOLD, size: 16, glyph: "■", iconKey: "coin" };
    case PICKUP_TYPE.SPEED:
      return { color: 0x00e5ff, size: 16, glyph: "⚡" };
    case PICKUP_TYPE.DAMAGE:
      return { color: 0xff8c00, size: 16, glyph: "⚔" };
    case PICKUP_TYPE.LARGE_HEART:
      return { color: 0xff69b4, size: 22, glyph: "♥", iconKey: "heart" };
    default:
      return { color: 0xcccccc, size: 12, glyph: "?" };
  }
}

/**
 * Pickup / floor item. Visual only — the scene owns consumption logic.
 */
export class Pickup extends Phaser.GameObjects.Container {
  readonly entityId: number;
  readonly pickupType: number;
  consumed = false;

  constructor(scene: Phaser.Scene, spawn: PickupSpawn) {
    super(scene, spawn.x, spawn.y);
    this.entityId = spawn.entity_id;
    this.pickupType = spawn.type;

    const style = styleFor(spawn.type);

    // Body: icon sprite if we have one, else a colored rect.
    if (style.iconKey && scene.textures.exists(style.iconKey)) {
      const icon = scene.add.image(0, 0, style.iconKey);
      icon.setDisplaySize(style.size, style.size);
      this.add(icon);
    } else {
      const rect = scene.add.rectangle(0, 0, style.size, style.size, style.color);
      rect.setStrokeStyle(1, 0x000000, 0.5);
      this.add(rect);
    }

    // Glyph label above.
    const label = scene.add.text(0, -style.size / 2 - 8, style.glyph, {
      fontFamily: "monospace",
      fontSize: "12px",
      color: hex(BONE_WHITE),
      stroke: hex(TEXT_STROKE),
      strokeThickness: 2,
    });
    label.setOrigin(0.5, 0.5);
    this.add(label);

    // Gentle bob so pickups read as interactable.
    scene.tweens.add({
      targets: this,
      y: spawn.y - 3,
      duration: 900,
      yoyo: true,
      repeat: -1,
      ease: "Sine.easeInOut",
    });

    scene.add.existing(this);
  }

  setConsumed(): void {
    if (this.consumed) return;
    this.consumed = true;
    this.scene.tweens.killTweensOf(this);
    this.scene.tweens.add({
      targets: this,
      alpha: 0,
      y: this.y - 12,
      duration: 220,
      ease: "Cubic.easeOut",
      onComplete: () => {
        this.destroy();
      },
    });
  }
}
