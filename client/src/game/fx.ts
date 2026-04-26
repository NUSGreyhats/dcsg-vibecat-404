import Phaser from "phaser";
import type { WeaponId } from "./weapons";
import { BLOOD_RED, DEPTH_FX, TRACER_YELLOW } from "./ui/tokens";

/**
 * Game-feel effects: hit-flash, hitstop, screen shake, muzzle flash,
 * and impact particles. Pure-visual, scene-local — none of these touch
 * the server. Tunings are conservative; bump intensities here to taste.
 */

export type HitstopHost = Phaser.Scene & { hitstopUntil?: number };

/** Per-weapon punch tunings used for muzzle flash + camera shake. Only
 *  the shotgun gets a (minimal) shake — pistol/rifle shoot too often
 *  for shake to feel like anything but motion sickness. */
export const WEAPON_FX: Record<
  WeaponId,
  {
    muzzleLength: number;
    muzzleThickness: number;
    shakeMs: number;
    shakeIntensity: number;
  }
> = {
  pistol: {
    muzzleLength: 12,
    muzzleThickness: 6,
    shakeMs: 0,
    shakeIntensity: 0,
  },
  rifle: {
    muzzleLength: 16,
    muzzleThickness: 7,
    shakeMs: 0,
    shakeIntensity: 0,
  },
  shotgun: {
    muzzleLength: 22,
    muzzleThickness: 12,
    shakeMs: 70,
    shakeIntensity: 0.0025,
  },
};

/**
 * Schedule (or extend) a global hitstop. Entities and projectiles that
 * consult {@link isHitstopped} skip their lerp tick during this window,
 * which gives a brief "freeze frame" on impact.
 */
export function triggerHitstop(scene: HitstopHost, ms: number): void {
  const target = scene.time.now + ms;
  if ((scene.hitstopUntil ?? 0) < target) {
    scene.hitstopUntil = target;
  }
}

export function isHitstopped(scene: HitstopHost): boolean {
  return (scene.hitstopUntil ?? 0) > scene.time.now;
}

export function cameraShake(
  scene: Phaser.Scene,
  durationMs: number,
  intensity: number,
): void {
  if (durationMs <= 0 || intensity <= 0) return;
  scene.cameras?.main?.shake(durationMs, intensity, false);
}

/**
 * Briefly tint a sprite solid white, then restore the previous tint.
 * Works for both Sprite (uses tintFill) and Rectangle (fillColor).
 */
export function flashSpriteWhite(
  sprite: Phaser.GameObjects.Sprite | Phaser.GameObjects.Rectangle,
  durationMs: number,
  restoreTint: number,
): void {
  if (sprite instanceof Phaser.GameObjects.Sprite) {
    sprite.setTintFill(0xffffff);
  } else {
    sprite.setFillStyle(0xffffff);
  }
  sprite.scene.time.delayedCall(durationMs, () => {
    if (!sprite.active) return;
    if (sprite instanceof Phaser.GameObjects.Sprite) {
      sprite.clearTint();
      sprite.setTint(restoreTint);
    } else {
      sprite.setFillStyle(restoreTint);
    }
  });
}

/**
 * Burst of small rectangles flying outward from (x, y). Tweens itself
 * to alpha 0 and self-destructs. Cheap enough to spawn on every hit.
 */
export function spawnImpactBurst(
  scene: Phaser.Scene,
  x: number,
  y: number,
  opts?: {
    color?: number;
    count?: number;
    speed?: number;
    lifeMs?: number;
    sizeMin?: number;
    sizeMax?: number;
  },
): void {
  const color = opts?.color ?? TRACER_YELLOW;
  const count = opts?.count ?? 6;
  const speed = opts?.speed ?? 80;
  const life = opts?.lifeMs ?? 220;
  const sizeMin = opts?.sizeMin ?? 2;
  const sizeMax = opts?.sizeMax ?? 3;

  for (let i = 0; i < count; i++) {
    const angle = Phaser.Math.FloatBetween(0, Math.PI * 2);
    const v = Phaser.Math.FloatBetween(speed * 0.4, speed);
    const size = Phaser.Math.Between(sizeMin, sizeMax);
    const p = scene.add.rectangle(x, y, size, size, color);
    p.setDepth(DEPTH_FX);
    scene.tweens.add({
      targets: p,
      x: x + Math.cos(angle) * v * (life / 1000),
      y: y + Math.sin(angle) * v * (life / 1000),
      alpha: 0,
      duration: life,
      ease: "Cubic.easeOut",
      onComplete: () => p.destroy(),
    });
  }
}

/**
 * Brief additive flash at the gun barrel, oriented along the aim angle.
 * Stretches longer + thinner as it fades to read as a shot of light.
 */
export function spawnMuzzleFlash(
  scene: Phaser.Scene,
  x: number,
  y: number,
  angle: number,
  opts?: { length?: number; thickness?: number; color?: number; lifeMs?: number },
): void {
  const length = opts?.length ?? 14;
  const thickness = opts?.thickness ?? 7;
  const color = opts?.color ?? TRACER_YELLOW;
  const life = opts?.lifeMs ?? 70;

  const flash = scene.add.rectangle(x, y, length, thickness, color);
  flash.setOrigin(0, 0.5);
  flash.setRotation(angle);
  flash.setBlendMode(Phaser.BlendModes.ADD);
  flash.setDepth(DEPTH_FX);
  scene.tweens.add({
    targets: flash,
    alpha: 0,
    scaleX: 1.4,
    scaleY: 0.4,
    duration: life,
    ease: "Cubic.easeOut",
    onComplete: () => flash.destroy(),
  });
}

/** Convenience wrapper so callers don't have to import the colors. */
export const FX_COLOR = {
  spark: TRACER_YELLOW,
  blood: BLOOD_RED,
} as const;
