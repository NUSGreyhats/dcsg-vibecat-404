import Phaser from "phaser";

export type MovementInput = { dx: number; dy: number };

export const CONTROL_BINDINGS: ReadonlyArray<readonly [string, string]> = [
  ["Move", "W A S D / Arrows"],
  ["Aim", "Mouse"],
  ["Shoot", "Left mouse button"],
  ["Switch", "1 / 2 / 3"],
  ["Shop", "B"],
  ["Inventory", "I"],
  ["Menu", "Esc"],
];

export type Controls = {
  movement(): MovementInput;
  aimAngle(pivotX: number, pivotY: number): number;
  shootHeld(): boolean;
  inventoryPressed(): boolean;
  shopPressed(): boolean;
  pausePressed(): boolean;
  weaponSwitchPressed(): number | null;
};

type KeyMap = {
  w: Phaser.Input.Keyboard.Key;
  a: Phaser.Input.Keyboard.Key;
  s: Phaser.Input.Keyboard.Key;
  d: Phaser.Input.Keyboard.Key;
  up: Phaser.Input.Keyboard.Key;
  down: Phaser.Input.Keyboard.Key;
  left: Phaser.Input.Keyboard.Key;
  right: Phaser.Input.Keyboard.Key;
  inventory: Phaser.Input.Keyboard.Key;
  shop: Phaser.Input.Keyboard.Key;
  pause: Phaser.Input.Keyboard.Key;
  one: Phaser.Input.Keyboard.Key;
  two: Phaser.Input.Keyboard.Key;
  three: Phaser.Input.Keyboard.Key;
};

/**
 * Build a {@link Controls} facade over a scene. Each method re-reads
 * live input state — nothing is cached across ticks so callers always
 * see the latest values.
 */
export function createControls(scene: Phaser.Scene): Controls {
  const kb = scene.input.keyboard;
  if (!kb) {
    // Keyboard plugin disabled — return a no-op facade so callers
    // don't crash when asked for input.
    return {
      movement: () => ({ dx: 0, dy: 0 }),
      aimAngle: () => 0,
      shootHeld: () => false,
      inventoryPressed: () => false,
      shopPressed: () => false,
      pausePressed: () => false,
      weaponSwitchPressed: () => null,
    };
  }

  const keys: KeyMap = {
    w: kb.addKey(Phaser.Input.Keyboard.KeyCodes.W, false),
    a: kb.addKey(Phaser.Input.Keyboard.KeyCodes.A, false),
    s: kb.addKey(Phaser.Input.Keyboard.KeyCodes.S, false),
    d: kb.addKey(Phaser.Input.Keyboard.KeyCodes.D, false),
    up: kb.addKey(Phaser.Input.Keyboard.KeyCodes.UP, false),
    down: kb.addKey(Phaser.Input.Keyboard.KeyCodes.DOWN, false),
    left: kb.addKey(Phaser.Input.Keyboard.KeyCodes.LEFT, false),
    right: kb.addKey(Phaser.Input.Keyboard.KeyCodes.RIGHT, false),
    inventory: kb.addKey(Phaser.Input.Keyboard.KeyCodes.I, false),
    shop: kb.addKey(Phaser.Input.Keyboard.KeyCodes.B, false),
    pause: kb.addKey(Phaser.Input.Keyboard.KeyCodes.ESC, false),
    one: kb.addKey(Phaser.Input.Keyboard.KeyCodes.ONE, false),
    two: kb.addKey(Phaser.Input.Keyboard.KeyCodes.TWO, false),
    three: kb.addKey(Phaser.Input.Keyboard.KeyCodes.THREE, false),
  };

  const justDown = Phaser.Input.Keyboard.JustDown;

  return {
    movement(): MovementInput {
      let dx = 0;
      let dy = 0;
      if (keys.a.isDown || keys.left.isDown) dx -= 1;
      if (keys.d.isDown || keys.right.isDown) dx += 1;
      if (keys.w.isDown || keys.up.isDown) dy -= 1;
      if (keys.s.isDown || keys.down.isDown) dy += 1;
      const mag = Math.hypot(dx, dy);
      if (mag > 1) {
        dx /= mag;
        dy /= mag;
      }
      return { dx, dy };
    },
    aimAngle(pivotX: number, pivotY: number): number {
      const p = scene.input.activePointer;
      // Prefer world coords so camera scroll doesn't warp the angle.
      const worldX = Number.isFinite(p.worldX) ? p.worldX : p.x;
      const worldY = Number.isFinite(p.worldY) ? p.worldY : p.y;
      return Math.atan2(worldY - pivotY, worldX - pivotX);
    },
    shootHeld(): boolean {
      return scene.input.activePointer.leftButtonDown();
    },
    inventoryPressed(): boolean {
      return justDown(keys.inventory);
    },
    shopPressed(): boolean {
      return justDown(keys.shop);
    },
    pausePressed(): boolean {
      return justDown(keys.pause);
    },
    weaponSwitchPressed(): number | null {
      if (justDown(keys.one)) return 1;
      if (justDown(keys.two)) return 2;
      if (justDown(keys.three)) return 3;
      return null;
    },
  };
}
