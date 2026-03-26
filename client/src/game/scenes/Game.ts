import { Scene } from "phaser";
import Controls from "./utils/Controls";
import Player from "../entities/Player";
import Enemy, { RobotType } from "../entities/Enemy";
import Bullet from "../entities/Bullet";
import {
  ENEMY_SPAWN_INTERVAL,
  ENEMY_MAX_COUNT,
  ENEMY_SPAWN_DISTANCE_MIN,
  ENEMY_SPAWN_DISTANCE_MAX,
  SHOOT_COOLDOWN,
} from "../constants";

export class Game extends Scene {
  camera: Phaser.Cameras.Scene2D.Camera;
  controls: Controls;
  player: Player;
  enemies: Phaser.GameObjects.Group;
  bullets: Phaser.GameObjects.Group;
  private lastShotTime: number = 0;
  private mapWidth: number = 0;
  private mapHeight: number = 0;
  private killCount: number = 0;
  private gameOver: boolean = false;

  constructor() {
    super("Game");
  }

  create() {
    this.killCount = 0;
    this.gameOver = false;

    this.controls = new Controls(this);
    this.player = new Player(this, 50, 50, this.controls);

    const map = this.make.tilemap({ key: "map" });
    const tileset = map.addTilesetImage("top_down_shooter", "topdown-tiles")!;

    map.createLayer("Base", tileset, 0, 0);
    const decor_layer = map.createLayer("Decor", tileset, 0, 0)!;

    decor_layer.setCollisionByProperty({ collides: true });

    this.mapWidth = map.widthInPixels;
    this.mapHeight = map.heightInPixels;

    this.enemies = this.add.group();
    this.bullets = this.add.group();

    this.physics.add.collider(this.player, decor_layer);
    this.physics.add.collider(this.enemies, decor_layer);
    this.physics.add.collider(this.enemies, this.enemies);

    this.physics.add.collider(this.bullets, decor_layer, (bulletObj) => {
      (bulletObj as Bullet).destroy();
    });

    this.physics.add.overlap(
      this.bullets,
      this.enemies,
      (bulletObj, enemyObj) =>
        this.onBulletHitEnemy(bulletObj as Bullet, enemyObj as Enemy),
    );

    this.physics.add.overlap(
      this.player,
      this.enemies,
      (_playerObj, enemyObj) => this.onEnemyHitPlayer(enemyObj as Enemy),
    );

    this.camera = this.cameras.main;
    this.camera.setBackgroundColor(0x00ff00);
    this.cameras.main.setBounds(0, 0, this.mapWidth, this.mapHeight);
    this.cameras.main.startFollow(this.player);

    this.time.addEvent({
      delay: ENEMY_SPAWN_INTERVAL,
      callback: this.spawnEnemy,
      callbackScope: this,
      loop: true,
    });

    this.scene.launch("GameHUD");

    this.input.keyboard!.on("keydown-ESC", () => {
      if (this.gameOver) return;
      this.scene.pause();
      this.scene.launch("PauseMenu", { gameOver: false });
    });

    this.events.on("resume", () => {
      this.input.keyboard!.resetKeys();
    });

    this.input.keyboard!.once("keydown-M", (_: KeyboardEvent) => {
      this.physics.world.createDebugGraphic();

      const graphics = this.add.graphics().setAlpha(0.75).setDepth(20);
      decor_layer.renderDebug(graphics, {
        tileColor: null,
        collidingTileColor: new Phaser.Display.Color(243, 134, 48, 255),
        faceColor: new Phaser.Display.Color(40, 39, 37, 255),
      });
    });
  }

  private spawnEnemy() {
    if (this.enemies.countActive() >= ENEMY_MAX_COUNT) return;

    const angle = Phaser.Math.FloatBetween(0, Math.PI * 2);
    const distance = Phaser.Math.FloatBetween(
      ENEMY_SPAWN_DISTANCE_MIN,
      ENEMY_SPAWN_DISTANCE_MAX,
    );

    let x = this.player.x + Math.cos(angle) * distance;
    let y = this.player.y + Math.sin(angle) * distance;

    x = Phaser.Math.Clamp(x, 32, this.mapWidth - 32);
    y = Phaser.Math.Clamp(y, 32, this.mapHeight - 32);

    const type: RobotType =
      Math.random() < 0.25 ? "robot-heavy" : "robot-grunt";
    const enemy = new Enemy(this, x, y, type, this.player);
    this.enemies.add(enemy);
  }

  private shootBullet() {
    if (this.time.now < this.lastShotTime + SHOOT_COOLDOWN) return;
    this.lastShotTime = this.time.now;

    const pointer = this.input.activePointer;
    pointer.updateWorldPoint(this.cameras.main);

    const angle = Phaser.Math.Angle.Between(
      this.player.x,
      this.player.y,
      pointer.worldX,
      pointer.worldY,
    );

    const spawnX = this.player.x + Math.cos(angle) * 20;
    const spawnY = this.player.y + Math.sin(angle) * 20;

    const bullet = new Bullet(this, spawnX, spawnY, angle);
    this.bullets.add(bullet);

    const flash = this.add
      .circle(spawnX, spawnY, 6, 0xffff00, 0.8)
      .setDepth(11);
    this.tweens.add({
      targets: flash,
      alpha: 0,
      scale: 2,
      duration: 80,
      onComplete: () => flash.destroy(),
    });
  }

  private onBulletHitEnemy(bullet: Bullet, enemy: Enemy) {
    if (!bullet.active || !enemy.active) return;

    const ex = enemy.x;
    const ey = enemy.y;

    bullet.destroy();
    const died = enemy.takeDamage(1);

    if (died) {
      this.killCount++;
      this.events.emit("kill-count-changed", this.killCount);

      const explosion = this.add
        .circle(ex, ey, 16, 0xff6600, 0.7)
        .setDepth(15);
      this.tweens.add({
        targets: explosion,
        alpha: 0,
        scale: 2.5,
        duration: 300,
        onComplete: () => explosion.destroy(),
      });
    }
  }

  private onEnemyHitPlayer(enemy: Enemy) {
    if (!enemy.active || this.gameOver) return;

    const dmg = enemy.damage;
    enemy.destroy();

    const damaged = this.player.takeDamage(dmg);
    if (!damaged) return;

    this.events.emit(
      "player-health-changed",
      this.player.health,
      this.player.maxHealth,
    );

    if (this.player.health <= 0) {
      this.gameOver = true;
      this.player.setVelocity(0);
      this.physics.pause();
      this.scene.launch("PauseMenu", { gameOver: true });
    }
  }

  update(_time: number, delta: number) {
    if (this.gameOver) return;

    this.player.update();

    if (this.input.activePointer.isDown) {
      this.shootBullet();
    }

    for (const enemy of this.enemies.getChildren().slice()) {
      if ((enemy as Enemy).active) (enemy as Enemy).update();
    }

    for (const bullet of this.bullets.getChildren().slice()) {
      if ((bullet as Bullet).active)
        (bullet as Bullet).update(_time, delta);
    }
  }
}
