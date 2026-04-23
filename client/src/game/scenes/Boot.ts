import Phaser from "phaser";

/**
 * Minimal bootstrap scene. Loads a tiny 1x1 fallback pixel so the Preloader
 * always has at least one usable texture even if every other asset 404s.
 */
export class Boot extends Phaser.Scene {
  constructor() {
    super({ key: "Boot" });
  }

  preload(): void {
    // 1x1 transparent PNG encoded as a data URI. Guarantees at least one
    // valid texture exists before we move on to the Preloader.
    const PX =
      "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=";
    this.load.image("__fallback_pixel", PX);
  }

  create(): void {
    this.scene.start("Preloader");
  }
}

export default Boot;
