import Phaser, { Scene } from "phaser";
import Controls from "./utils/Controls";
import Player from "../entities/Player";
import Enemy, { RobotType } from "../entities/Enemy";
import Bullet from "../entities/Bullet";
import Coin from "../entities/Coin";
import {
  ROOM_WIDTH,
  ROOM_HEIGHT,
  WS_URL,
  STATE_SYNC_INTERVAL_MS,
  WIN_ROOM,
} from "../constants";
import { getPlayerName } from "./MainMenu";
import GameStateSync from "../net/GameStateSync";
import { getWeaponStats, type WeaponId } from "../weapons";
import { KEYCODE_BACKTICK } from "../shop";

const DOOR_COLOR = 0x6b4423;

function getObjProp(
  obj: Phaser.Types.Tilemaps.TiledObject,
  name: string,
): string | undefined {
  if (!obj.properties) return undefined;
  if (Array.isArray(obj.properties)) {
    const prop = (obj.properties as { name: string; value: string }[]).find(
      (p) => p.name === name,
    );
    return prop?.value;
  }
  return (obj.properties as Record<string, string>)[name];
}

export class Game extends Scene {
  camera: Phaser.Cameras.Scene2D.Camera;
  controls: Controls;
  player: Player;
  enemies: Phaser.GameObjects.Group;
  bullets: Phaser.GameObjects.Group;
  coins: Phaser.GameObjects.Group;

  private currentMap: Phaser.Tilemaps.Tilemap | null = null;
  private groundLayer: Phaser.Tilemaps.TilemapLayer | null = null;
  private wallsLayer: Phaser.Tilemaps.TilemapLayer | null = null;
  private wallColliders: Phaser.Physics.Arcade.Collider[] = [];
  private doorBlockerGroup: Phaser.Physics.Arcade.StaticGroup | null = null;
  private doorBlocker: Phaser.GameObjects.Rectangle | null = null;
  private exitZone: Phaser.GameObjects.Rectangle | null = null;
  private exitDoorCollider: Phaser.Physics.Arcade.Collider | null = null;
  private exitZoneData: {
    x: number;
    y: number;
    w: number;
    h: number;
  } | null = null;
  private doorOpen = false;
  private lastShotTime = 0;
  private killCount = 0;
  private nextEnemyId = 0;
  private gameOver = false;
  private exitThrottle = false;
  inventoryOpen = false;
  shopOpen = false;
  playerName = "Player";
  private stateSync: GameStateSync | null = null;

  constructor() {
    super("Game");
  }

  create() {
    this.killCount = 0;
    this.gameOver = false;
    this.exitThrottle = false;
    this.inventoryOpen = false;
    this.shopOpen = false;
    this.playerName = getPlayerName();

    this.wallColliders = [];
    this.exitDoorCollider = null;
    this.exitZone = null;
    this.exitZoneData = null;
    this.doorBlocker = null;
    this.doorBlockerGroup = null;
    this.groundLayer = null;
    this.wallsLayer = null;
    this.currentMap = null;

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

    this.physics.add.overlap(this.player, this.coins, (_playerObj, coinObj) =>
      this.onPlayerPickupCoin(coinObj as Coin),
    );

    this.camera = this.cameras.main;
    this.camera.setBackgroundColor(0x1a1a2e);
    this.cameras.main.setBounds(0, 0, ROOM_WIDTH, ROOM_HEIGHT);
    this.cameras.main.startFollow(this.player, true, 0.1, 0.1);

    this.scene.launch("GameHUD");
    this.loadRoom();
    this.events.emit("coin-count-changed", this.player.coins);
    this.events.emit(
      "weapon-changed",
      getWeaponStats(this.player.equippedWeaponId).displayName,
    );

    const kb = this.input.keyboard!;
    const K = Phaser.Input.Keyboard.KeyCodes;
    kb.addKey(K.I).on("down", () => this.tryOpenInventory());
    kb.addKey(K.TAB).on("down", () => this.tryOpenInventory());
    kb.addKey(KEYCODE_BACKTICK).on("down", () => this.tryOpenShop());

    kb.on("keydown-ESC", () => {
      if (this.gameOver || this.inventoryOpen || this.shopOpen) return;
      this.scene.pause();
      this.scene.launch("PauseMenu", { gameOver: false });
    });

    this.events.on("resume", () => {
      this.input.keyboard!.resetKeys();
    });

    this.stateSync = new GameStateSync(this, WS_URL, STATE_SYNC_INTERVAL_MS);
    this.stateSync.start();

    this.events.once("shutdown", () => {
      this.stateSync?.stop();
      this.stateSync = null;
    });
  }

  getKillCount(): number {
    return this.killCount;
  }

  getEnemyStates(): Array<{
    id: number;
    x: number;
    y: number;
    hp: number;
    type: string;
  }> {
    const result: Array<{
      id: number;
      x: number;
      y: number;
      hp: number;
      type: string;
    }> = [];
    for (const child of this.enemies.getChildren()) {
      const e = child as Enemy;
      if (!e.active) continue;
      result.push({
        id: e.syncId,
        x: Math.round(e.x * 100) / 100,
        y: Math.round(e.y * 100) / 100,
        hp: e.health,
        type: e.texture.key,
      });
    }
    return result;
  }

  rollbackEnemies(
    enemyStates: Array<{
      id: number;
      x: number;
      y: number;
      hp: number;
      type: string;
    }>,
  ) {
    const existingMap = new Map<number, Enemy>();
    for (const child of this.enemies.getChildren()) {
      const e = child as Enemy;
      if (e.active) existingMap.set(e.syncId, e);
    }

    const rolledBackIds = new Set<number>();
    for (const es of enemyStates) {
      rolledBackIds.add(es.id);
      const existing = existingMap.get(es.id);
      if (existing) {
        existing.setPosition(es.x, es.y);
        existing.health = es.hp;
      } else {
        const enemy = new Enemy(
          this,
          es.x,
          es.y,
          es.type as RobotType,
          this.player,
        );
        enemy.syncId = es.id;
        enemy.health = es.hp;
        this.enemies.add(enemy);
      }
    }

    for (const [id, enemy] of existingMap) {
      if (!rolledBackIds.has(id)) {
        enemy.destroy();
      }
    }
  }

  applyRollbackState(
    currentRoom: number,
    kills: number,
    enemyStates: Array<{
      id: number;
      x: number;
      y: number;
      hp: number;
      type: string;
    }>,
  ) {
    if (this.player.progress.currentRoom !== currentRoom) {
      this.player.progress.currentRoom = currentRoom;
      this.exitThrottle = false;
      this.loadRoom();
    }

    this.killCount = kills;
    this.events.emit("kill-count-changed", this.killCount);

    this.rollbackEnemies(enemyStates);
    this.checkRoomCleared();
  }

  tryOpenInventory() {
    if (this.gameOver || this.inventoryOpen || this.shopOpen) return;
    this.inventoryOpen = true;
    this.input.keyboard!.enabled = false;
    this.scene.pause();
    this.scene.launch("InventoryMenu");
  }

  tryOpenShop() {
    if (this.gameOver || this.shopOpen || this.inventoryOpen) return;
    this.shopOpen = true;
    this.input.keyboard!.enabled = false;
    this.scene.pause();
    this.scene.launch("ShopMenu");
  }

  tryPurchaseShopWeapon(id: WeaponId, price: number): boolean {
    if (this.player.ownsWeapon(id) || this.player.coins < price) return false;
    this.player.coins -= price;
    if (!this.player.acquireWeapon(id)) {
      this.player.coins += price;
      return false;
    }
    this.events.emit("coin-count-changed", this.player.coins);
    return true;
  }

  playerEquipWeapon(id: WeaponId): boolean {
    if (!this.player.equipWeapon(id)) return false;
    this.events.emit("weapon-changed", getWeaponStats(id).displayName);
    return true;
  }

  // ---- Room lifecycle (tilemap-based) ----

  private clearRoomGeometry() {
    for (const c of this.wallColliders) c.destroy();
    this.wallColliders = [];

    if (this.exitDoorCollider) {
      this.exitDoorCollider.destroy();
      this.exitDoorCollider = null;
    }

    this.exitZone?.destroy();
    this.exitZone = null;
    this.exitZoneData = null;

    this.doorBlockerGroup?.clear(true, true);
    this.doorBlocker = null;

    this.groundLayer?.destroy();
    this.wallsLayer?.destroy();
    this.currentMap?.destroy();
    this.groundLayer = null;
    this.wallsLayer = null;
    this.currentMap = null;

    for (const e of this.enemies.getChildren().slice()) (e as Enemy).destroy();
    for (const b of this.bullets.getChildren().slice()) (b as Bullet).destroy();
    for (const c of this.coins.getChildren().slice()) (c as Coin).destroy();
  }

  private loadRoom() {
    this.clearRoomGeometry();
    this.doorOpen = false;

    const roomNum = this.player.progress.currentRoom;

    const map = this.make.tilemap({ key: `room-${roomNum}` });
    const tileset = map.addTilesetImage("defcon_tileset", "defcon-tiles")!;

    this.groundLayer = map.createLayer("Ground", tileset)!;
    this.groundLayer.setDepth(-5);

    this.wallsLayer = map.createLayer("Walls", tileset)!;
    this.wallsLayer.setDepth(0);
    this.wallsLayer.setCollisionByExclusion([-1, 0]);

    this.currentMap = map;

    this.doorBlockerGroup = this.physics.add.staticGroup();

    this.wallColliders.push(
      this.physics.add.collider(this.player, this.wallsLayer),
      this.physics.add.collider(this.enemies, this.wallsLayer),
      this.physics.add.collider(this.bullets, this.wallsLayer, (b) => {
        (b as Bullet).destroy();
      }),
      this.physics.add.collider(this.player, this.doorBlockerGroup!),
      this.physics.add.collider(this.enemies, this.doorBlockerGroup!),
      this.physics.add.collider(this.bullets, this.doorBlockerGroup!, (b) => {
        (b as Bullet).destroy();
      }),
    );

    const objects = map.getObjectLayer("Objects")?.objects ?? [];

    const ps = objects.find((o) => o.type === "PlayerSpawn");
    if (ps) this.player.setPosition(ps.x!, ps.y!);
    this.player.setVelocity(0, 0);

    const db = objects.find((o) => o.type === "DoorBlocker");
    if (db && db.width && db.height) {
      this.doorBlocker = this.add.rectangle(
        db.x! + db.width / 2,
        db.y! + db.height / 2,
        db.width,
        db.height,
        DOOR_COLOR,
      );
      this.doorBlocker.setDepth(0);
      this.physics.add.existing(this.doorBlocker, true);
      this.doorBlockerGroup!.add(this.doorBlocker);
    }

    const ez = objects.find((o) => o.type === "Exit");
    if (ez && ez.width && ez.height) {
      this.exitZoneData = {
        x: ez.x!,
        y: ez.y!,
        w: ez.width,
        h: ez.height,
      };
    }

    for (const es of objects.filter((o) => o.type === "EnemySpawn")) {
      const enemyType = (getObjProp(es, "enemyType") ||
        "robot-grunt") as RobotType;
      const enemy = new Enemy(this, es.x!, es.y!, enemyType, this.player);
      enemy.syncId = this.nextEnemyId++;
      this.enemies.add(enemy);
    }

    for (const cs of objects.filter((o) => o.type === "CoinSpawn")) {
      const in_wall = getObjProp(cs, "in_wall") === "true";
      const coin = new Coin(this, cs.x!, cs.y!, in_wall);
      this.coins.add(coin);
    }

    this.events.emit("room-changed", roomNum);
  }

  // ---- Door / exit ----

  private tryAdvanceThroughDoor() {
    if (!this.doorOpen || this.gameOver || this.exitThrottle) return;
    this.exitThrottle = true;
    this.player.progress.currentRoom += 1;

    if (this.player.progress.currentRoom > WIN_ROOM) {
      this.gameOver = true;
      this.stateSync?.sendStateNow();
      this.player.setVelocity(0);
      this.physics.pause();
      this.scene.launch("WinScreen");
      return;
    }

    this.time.delayedCall(200, () => {
      this.exitThrottle = false;
    });
    this.loadRoom();
  }

  private openExitDoor() {
    if (this.doorOpen) return;
    this.doorOpen = true;

    if (this.doorBlocker) {
      this.doorBlockerGroup!.remove(this.doorBlocker, true, true);
      this.doorBlocker = null;
    }

    if (!this.exitZoneData) return;
    const z = this.exitZoneData;

    this.exitZone = this.add.rectangle(
      z.x + z.w / 2,
      z.y + z.h / 2,
      z.w,
      z.h,
      0x66ff99,
      0.2,
    );
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

    if (this.exitDoorCollider) this.exitDoorCollider.destroy();
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

  // ---- Combat ----

  private shootBullet() {
    const w = getWeaponStats(this.player.equippedWeaponId);
    if (this.time.now < this.lastShotTime + w.cooldownMs) return;
    this.lastShotTime = this.time.now;

    const angle = this.player.getAimAngle();

    const spawnX = this.player.x + Math.cos(angle) * 20;
    const spawnY = this.player.y + Math.sin(angle) * 20;

    const bullet = new Bullet(this, spawnX, spawnY, angle, {
      speed: w.bulletSpeed,
      damage: w.damage,
      textureKey: w.bulletTextureKey,
      bodyRadius: w.bulletBodyRadius,
    });
    this.bullets.add(bullet);

    this.player.playShootAnimation();

    const flash = this.add
      .circle(spawnX, spawnY, 6, 0xffff00, 0.8)
      .setDepth(12);
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
    const died = enemy.takeDamage(bullet.damage);

    if (died) {
      this.killCount++;
      this.events.emit("kill-count-changed", this.killCount);

      const explosion = this.add.circle(ex, ey, 16, 0xff6600, 0.7).setDepth(15);
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

  private onPlayerPickupCoin(coin: Coin) {
    if (!coin.active || this.gameOver) return;
    coin.destroy();
    this.player.coins += 1;
    this.events.emit("coin-count-changed", this.player.coins);
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
      if ((bullet as Bullet).active) (bullet as Bullet).update(_time, delta);
    }

    this.checkRoomCleared();
  }
}
