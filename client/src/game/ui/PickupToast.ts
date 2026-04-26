import Phaser from "phaser";

import {
  DEPTH_FX,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  STROKE_THICKNESS_HUD,
  TEXT_STROKE,
  hex,
} from "./tokens";

const RISE_PX = 28;
const DURATION_MS = 900;

/**
 * World-space pickup feedback. Spawns a `+N Label` text at the pickup's
 * last position, rises and fades, self-destroys on tween complete.
 */
export function spawnPickupToast(
  scene: Phaser.Scene,
  x: number,
  y: number,
  text: string,
  color: number,
): void {
  const label = scene.add.text(x, y - 10, text, {
    fontFamily: FONT_FAMILY,
    fontSize: FONT_SIZE_BODY,
    color: hex(color),
    stroke: hex(TEXT_STROKE),
    strokeThickness: STROKE_THICKNESS_HUD,
  });
  label.setOrigin(0.5, 1);
  label.setDepth(DEPTH_FX);
  scene.tweens.add({
    targets: label,
    y: label.y - RISE_PX,
    alpha: 0,
    duration: DURATION_MS,
    ease: "Cubic.easeOut",
    onComplete: () => label.destroy(),
  });
}
