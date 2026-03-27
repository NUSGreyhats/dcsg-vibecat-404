import { Scene } from "phaser";
import Controls from "./utils/Controls";
import Player from "../entities/Player";
import Enemy, { RobotType } from "../entities/Enemy";
import Bullet from "../entities/Bullet";
import Coin from "../entities/Coin";
import {
  ROOM_WIDTH,
  ROOM_HEIGHT,
  WALL_THICKNESS,
  DOOR_GAP,
  ROOM_MIN_INTERIOR_WALLS,
  ROOM_MAX_INTERIOR_WALLS,
  ROOM_MIN_ENEMIES,
  ROOM_MAX_ENEMIES,
  ROOM_SPAWN_SAFE_W,
  ROOM_SPAWN_SAFE_H,
  ENEMY_SPAWN_BODY_RADIUS,
  COIN_SPAWN_BODY_RADIUS,
  SHOOT_COOLDOWN,
} from "../constants";
import {
  type DoorSide,
  type WallSpec,
  buildPerimeterWalls,
  getDoorBlockerSpec,
  getExitZoneCenter,
  getPlayerSpawnForEntry,
  oppositeSide,
  generateInteriorWalls,
  isClearOfWalls,
} from "../world/roomLayout";

const WALL_COLOR = 0x4a5568;
const DOOR_COLOR = 0x6b4423;
const FLOOR_COLOR = 0x252540;

export class Game extends Scene {
  camera: Phaser.Cameras.Scene2D.Camera;
  controls: Controls;
  player: Player;
  enemies: Phaser.GameObjects.Group;
  bullets: Phaser.GameObjects.Group;
  coins: Phaser.GameObjects.Group;
  private wallGroup!: Phaser.Physics.Arcade.StaticGroup;
  private floorBg: Phaser.GameObjects.Rectangle | null = null;
  private doorBlocker: Phaser.GameObjects.Rectangle | null = null;
  private exitZone: Phaser.GameObjects.Rectangle | null = null;
  private exitDoorCollider: Phaser.Physics.Arcade.Collider | null = null;
  private doorOpen = false;
  private currentDoorSide: DoorSide = "top";
  private lastShotTime = 0;
  private killCount = 0;
  private gameOver = false;
  private exitThrottle = false;

  constructor() {
    super("Game");
  }

  create() {
    this.killCount = 0;
    this.gameOver = false;
    this.exitThrottle = false;

    this.wallGroup = this.physics.add.staticGroup();
    this.enemies = this.add.group();
    this.bullets = this.add.group();
    this.coins = this.add.group();

    this.controls = new Controls(this);
    this.player = new Player(
      this,
      ROOM_WIDTH / 2,
      ROOM_HEIGHT / 2,
      this.controls,
    );

    this.physics.add.collider(this.player, this.wallGroup);
    this.physics.add.collider(this.enemies, this.wallGroup);
    this.physics.add.collider(this.bullets, this.wallGroup, (bulletObj) => {
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

    this.physics.add.overlap(
      this.player,
      this.coins,
      (_playerObj, coinObj) => this.onPlayerPickupCoin(coinObj as Coin),
    );

    this.camera = this.cameras.main;
    this.camera.setBackgroundColor(0x1a1a2e);
    this.cameras.main.setBounds(0, 0, ROOM_WIDTH, ROOM_HEIGHT);
    this.cameras.main.startFollow(this.player, true, 0.1, 0.1);

    this.scene.launch("GameHUD");
    this.loadRoom(null);
    this.events.emit("coin-count-changed", this.player.coins);

    this.input.keyboard!.on("keydown-ESC", () => {
      if (this.gameOver) return;
      this.scene.pause();
      this.scene.launch("PauseMenu", { gameOver: false });
    });

    this.events.on("resume", () => {
      this.input.keyboard!.resetKeys();
    });
  }

  private clearRoomGeometry() {
    if (this.exitDoorCollider) {
      this.exitDoorCollider.destroy();
      this.exitDoorCollider = null;
    }
    this.exitZone?.destroy();
    this.exitZone = null;
    this.doorBlocker = null;
    this.floorBg?.destroy();
    this.floorBg = null;
    this.wallGroup.clear(true, true);

    for (const e of this.enemies.getChildren().slice()) {
      (e as Enemy).destroy();
    }
    for (const b of this.bullets.getChildren().slice()) {
      (b as Bullet).destroy();
    }
    for (const c of this.coins.getChildren().slice()) {
      (c as Coin).destroy();
    }
  }

  private addStaticWall(spec: WallSpec, color: number): Phaser.GameObjects.Rectangle {
    const r = this.add.rectangle(spec.cx, spec.cy, spec.w, spec.h, color);
    r.setDepth(0);
    this.physics.add.existing(r, true);
    this.wallGroup.add(r);
    return r;
  }

  private loadRoom(entryFrom: DoorSide | null) {
    this.clearRoomGeometry();
    this.doorOpen = false;

    this.floorBg = this.add.rectangle(
      ROOM_WIDTH / 2,
      ROOM_HEIGHT / 2,
      ROOM_WIDTH,
      ROOM_HEIGHT,
      FLOOR_COLOR,
    );
    this.floorBg.setDepth(-6);

    const sides: DoorSide[] = ["top", "bottom", "left", "right"];
    this.currentDoorSide = sides[Phaser.Math.Between(0, sides.length - 1)];

    const spawn = getPlayerSpawnForEntry(
      ROOM_WIDTH,
      ROOM_HEIGHT,
      entryFrom,
      WALL_THICKNESS + 8,
    );
    this.player.setPosition(spawn.x, spawn.y);
    this.player.setVelocity(0, 0);

    const perimeter = buildPerimeterWalls(
      ROOM_WIDTH,
      ROOM_HEIGHT,
      WALL_THICKNESS,
      DOOR_GAP,
      this.currentDoorSide,
    );
    const interior = generateInteriorWalls(
      ROOM_WIDTH,
      ROOM_HEIGHT,
      WALL_THICKNESS,
      DOOR_GAP,
      this.currentDoorSide,
      ROOM_MIN_INTERIOR_WALLS,
      ROOM_MAX_INTERIOR_WALLS,
      spawn.x,
      spawn.y,
      ROOM_SPAWN_SAFE_W,
      ROOM_SPAWN_SAFE_H,
    );

    for (const spec of perimeter) {
      this.addStaticWall(spec, WALL_COLOR);
    }
    for (const spec of interior) {
      this.addStaticWall(spec, WALL_COLOR);
    }

    const blockerSpec = getDoorBlockerSpec(
      ROOM_WIDTH,
      ROOM_HEIGHT,
      WALL_THICKNESS,
      DOOR_GAP,
      this.currentDoorSide,
    );
    this.doorBlocker = this.addStaticWall(blockerSpec, DOOR_COLOR);

    const enemyTarget =
      Phaser.Math.Between(ROOM_MIN_ENEMIES, ROOM_MAX_ENEMIES) +
      Math.floor((this.player.progress.currentRoom - 1) / 2);
    const capped = Phaser.Math.Clamp(enemyTarget, ROOM_MIN_ENEMIES, 14);

    const spawnCheckWalls: WallSpec[] = [
      ...perimeter,
      ...interior,
      blockerSpec,
    ];
    this.spawnRoomEnemies(capped, spawn.x, spawn.y, spawnCheckWalls);
    this.spawnRoomCoins(spawn.x, spawn.y, spawnCheckWalls);

    this.events.emit("room-changed", this.player.progress.currentRoom);
  }

  private spawnRoomEnemies(
    count: number,
    px: number,
    py: number,
    walls: WallSpec[],
  ) {
    const margin = 80;
    let placed = 0;
    let tries = 0;
    const maxTries = count * 100;

    while (placed < count && tries < maxTries) {
      tries++;
      const ex = Phaser.Math.Between(margin, ROOM_WIDTH - margin);
      const ey = Phaser.Math.Between(margin, ROOM_HEIGHT - margin);
      if (Phaser.Math.Distance.Between(ex, ey, px, py) < 130) continue;
      if (!isClearOfWalls(ex, ey, ENEMY_SPAWN_BODY_RADIUS, walls)) continue;

      const type: RobotType =
        Math.random() < 0.22 ? "robot-heavy" : "robot-grunt";
      const enemy = new Enemy(this, ex, ey, type, this.player);
      this.enemies.add(enemy);
      placed++;
    }
  }

  private spawnRoomCoins(px: number, py: number, walls: WallSpec[]) {
    const count = Phaser.Math.Between(1, 5);
    const margin = 80;
    const minFromPlayer = 96;
    const minCoinSep = 32;
    const minFromEnemy = 40;
    const placed: { x: number; y: number }[] = [];

    for (let i = 0; i < count; i++) {
      let found = false;
      for (let tries = 0; tries < 100 && !found; tries++) {
        const cx = Phaser.Math.Between(margin, ROOM_WIDTH - margin);
        const cy = Phaser.Math.Between(margin, ROOM_HEIGHT - margin);
        if (Phaser.Math.Distance.Between(cx, cy, px, py) < minFromPlayer)
          continue;
        if (!isClearOfWalls(cx, cy, COIN_SPAWN_BODY_RADIUS, walls)) continue;

        let blocked = false;
        for (const p of placed) {
          if (Phaser.Math.Distance.Between(cx, cy, p.x, p.y) < minCoinSep) {
            blocked = true;
            break;
          }
        }
        if (blocked) continue;

        for (const e of this.enemies.getChildren()) {
          const en = e as Enemy;
          if (!en.active) continue;
          if (Phaser.Math.Distance.Between(cx, cy, en.x, en.y) < minFromEnemy) {
            blocked = true;
            break;
          }
        }
        if (blocked) continue;

        const coin = new Coin(this, cx, cy);
        this.coins.add(coin);
        placed.push({ x: cx, y: cy });
        found = true;
      }
    }
  }

  private onPlayerPickupCoin(coin: Coin) {
    if (!coin.active || this.gameOver) return;
    coin.destroy();
    this.player.coins += 1;
    this.events.emit("coin-count-changed", this.player.coins);
  }

  private tryAdvanceThroughDoor() {
    if (!this.doorOpen || this.gameOver || this.exitThrottle) return;
    this.exitThrottle = true;
    this.player.progress.currentRoom += 1;
    const cameFrom = oppositeSide(this.currentDoorSide);
    this.time.delayedCall(200, () => {
      this.exitThrottle = false;
    });
    this.loadRoom(cameFrom);
  }

  private openExitDoor() {
    if (this.doorOpen) return;
    this.doorOpen = true;

    if (this.doorBlocker) {
      this.wallGroup.remove(this.doorBlocker, true, true);
      this.doorBlocker = null;
    }

    const z = getExitZoneCenter(
      ROOM_WIDTH,
      ROOM_HEIGHT,
      WALL_THICKNESS,
      this.currentDoorSide,
    );
    this.exitZone = this.add.rectangle(z.cx, z.cy, z.w, z.h, 0x66ff99, 0.2);
    this.exitZone.setDepth(-3);
    this.physics.add.existing(this.exitZone, true);
    const eb = this.exitZone.body as Phaser.Physics.Arcade.StaticBody;
    eb.updateFromGameObject();

    this.tweens.add({
      targets: this.exitZone,
      alpha: { from: 0.12, to: 0.35 },
      duration: 550,
      yoyo: true,
      repeat: -1,
    });

    if (this.exitDoorCollider) {
      this.exitDoorCollider.destroy();
    }
    this.exitDoorCollider = this.physics.add.overlap(
      this.player,
      this.exitZone,
      () => this.tryAdvanceThroughDoor(),
    );
  }

  private checkRoomCleared() {
    if (this.doorOpen || this.gameOver) return;
    let alive = 0;
    for (const c of this.enemies.getChildren()) {
      if ((c as Enemy).active) alive++;
    }
    if (alive === 0) {
      this.openExitDoor();
    }
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

    this.checkRoomCleared();
  }
}
