import Phaser from "phaser";
import { getSync } from "../net/runContext";
import {
  ROOM_WIDTH,
  ROOM_HEIGHT,
  INPUT_SEND_INTERVAL_MS,
} from "../constants";
import { Player } from "../entities/Player";
import { Enemy } from "../entities/Enemy";
import { Bullet } from "../entities/Bullet";
import { Pickup } from "../entities/Pickup";
import { Door } from "../entities/Door";
import {
  paintRoomBackground,
  buildRoom,
  type BuiltRoom,
} from "../world/roomLayout";
import { createControls, type Controls } from "./utils/Controls";
import type {
  RoomData,
  EntitySnapshot,
  ProjSnapshot,
  PlayerSnapshot,
  SplitData,
} from "../../net/packet";

type StatePayload = {
  elapsed_ms: number;
  player: PlayerSnapshot;
  entities: EntitySnapshot[];
  projectiles: ProjSnapshot[];
};

/**
 * The main gameplay scene. Owns entity maps keyed by server ID, reconciles
 * each server "state" snapshot, and forwards local input.
 */
export class Game extends Phaser.Scene {
  player?: Player;
  enemies: Map<number, Enemy> = new Map();
  projectiles: Map<number, Bullet> = new Map();
  pickups: Map<number, Pickup> = new Map();
  doors: Map<number, Door> = new Map();
  backgroundObjs: Phaser.GameObjects.GameObject[] = [];
  controls!: Controls;
  inputSendAcc = 0;

  constructor() {
    super({ key: "Game" });
  }

  create(): void {
    const sync = getSync();
    this.controls = createControls(this);

    const roomData = sync.state.roomData;
    if (!roomData) {
      this.scene.start("MainMenu");
      return;
    }

    this.backgroundObjs = paintRoomBackground(this, roomData);
    const built = buildRoom(this, roomData);
    this.enemies = built.enemies;
    this.pickups = built.pickups;
    this.doors = built.doors;

    const px = sync.state.player?.x ?? ROOM_WIDTH / 2;
    const py = sync.state.player?.y ?? ROOM_HEIGHT - 100;
    this.player = new Player(this, px, py);
    this.add.existing(this.player);

    sync.state.on("state", this.onServerState, this);
    sync.state.on("roomLoad", this.onRoomLoad, this);
    sync.state.on("doorUnlocked", this.onDoorUnlocked, this);
    sync.state.on("entityDied", this.onEntityDied, this);
    sync.state.on("runComplete", this.onRunComplete, this);

    this.events.on("shutdown", this.cleanup, this);
  }

  update(_time: number, delta: number): void {
    if (!this.controls) return;
    const dt = delta / 1000;
    this.player?.tick(dt);
    for (const e of this.enemies.values()) e.tick(dt);
    for (const b of this.projectiles.values()) b.tick(dt);

    this.handleInput(delta);
  }

  private handleInput(delta: number): void {
    const sync = getSync();
    const mv = this.controls.movement();
    this.inputSendAcc += delta;
    if (this.inputSendAcc >= INPUT_SEND_INTERVAL_MS) {
      sync.sendInput(mv.dx, mv.dy);
      this.inputSendAcc = 0;
    }

    if (this.controls.shootPressed() && this.player) {
      const angle = this.controls.aimAngle(this.player.x, this.player.y);
      sync.sendShoot(angle);
      this.player.triggerShootAnim();
    }

    const slot = this.controls.weaponSwitchPressed();
    if (slot !== null) sync.sendSwitchWeapon(slot);

    if (this.controls.interactPressed() && this.player) {
      const id = this.nearestPickupId(this.player.x, this.player.y, 80);
      if (id !== null) {
        sync.sendInteract(id);
      } else {
        const doorId = this.nearestUnlockedDoorId(
          this.player.x,
          this.player.y,
          80,
        );
        if (doorId !== null) sync.sendEnterDoor(doorId);
      }
    }

    if (this.controls.inventoryPressed()) {
      this.scene.launch("InventoryMenu");
      this.scene.pause();
    }

    if (this.controls.pausePressed()) {
      this.scene.launch("PauseMenu");
      this.scene.pause();
    }
  }

  private onServerState(payload: StatePayload): void {
    // Player
    if (this.player && payload.player) {
      this.player.applySnapshot(payload.player);
    }

    const seenEntityIds = new Set<number>();
    for (const snap of payload.entities) {
      // state === 3 is AIDead on the server. The server already filters
      // these out, but stay defensive: a dead snap for an enemy we've
      // already torn down must not respawn it.
      if (snap.state === 3) continue;
      seenEntityIds.add(snap.entity_id);
      let enemy = this.enemies.get(snap.entity_id);
      if (!enemy) {
        enemy = new Enemy(this, snap);
        this.add.existing(enemy);
        this.enemies.set(snap.entity_id, enemy);
      }
      enemy.applySnapshot(snap);
    }

    // Reconcile projectiles.
    const seenProjIds = new Set<number>();
    for (const p of payload.projectiles) {
      seenProjIds.add(p.proj_id);
      let bullet = this.projectiles.get(p.proj_id);
      if (!bullet) {
        bullet = new Bullet(this, p);
        this.add.existing(bullet);
        this.projectiles.set(p.proj_id, bullet);
      }
      bullet.applySnapshot(p);
    }
    // Despawn missing projectiles.
    for (const [id, bullet] of this.projectiles) {
      if (!seenProjIds.has(id)) {
        bullet.destroy();
        this.projectiles.delete(id);
      }
    }

    // Drop enemies/pickups that the server marked dead.
    const state = getSync().state;
    for (const id of state.deadEntities) {
      const e = this.enemies.get(id);
      if (e) {
        e.markDead();
        this.enemies.delete(id);
      }
      const pk = this.pickups.get(id);
      if (pk) {
        pk.setConsumed();
        this.pickups.delete(id);
      }
    }
  }

  private onRoomLoad(
    _roomIndex: number,
    room: RoomData,
    _splitMs: number,
  ): void {
    // Wipe the old room.
    for (const obj of this.backgroundObjs) obj.destroy();
    this.backgroundObjs = [];
    for (const e of this.enemies.values()) e.destroy();
    this.enemies.clear();
    for (const b of this.projectiles.values()) b.destroy();
    this.projectiles.clear();
    for (const p of this.pickups.values()) p.destroy();
    this.pickups.clear();
    for (const d of this.doors.values()) d.destroy();
    this.doors.clear();

    // Rebuild with the new room data.
    this.backgroundObjs = paintRoomBackground(this, room);
    const built: BuiltRoom = buildRoom(this, room);
    this.enemies = built.enemies;
    this.pickups = built.pickups;
    this.doors = built.doors;

    // Reposition the persistent player at the server-provided start.
    const sync = getSync();
    const px = sync.state.player?.x ?? ROOM_WIDTH / 2;
    const py = sync.state.player?.y ?? ROOM_HEIGHT - 100;
    if (this.player) {
      this.player.setPosition(px, py);
    }
  }

  private onDoorUnlocked(doorId: number): void {
    this.doors.get(doorId)?.setUnlocked(true);
  }

  private onEntityDied(entityId: number): void {
    this.enemies.get(entityId)?.markDead();
    this.pickups.get(entityId)?.setConsumed();
  }

  private onRunComplete(
    _totalMs: number,
    _rank: number,
    _splits: SplitData[],
  ): void {
    this.scene.stop("GameHUD");
    this.scene.start("WinScreen");
  }

  private nearestPickupId(
    x: number,
    y: number,
    radius: number,
  ): number | null {
    let bestId: number | null = null;
    let bestDist = radius * radius;
    for (const [id, pk] of this.pickups) {
      const dx = pk.x - x;
      const dy = pk.y - y;
      const d2 = dx * dx + dy * dy;
      if (d2 <= bestDist) {
        bestDist = d2;
        bestId = id;
      }
    }
    return bestId;
  }

  private nearestUnlockedDoorId(
    x: number,
    y: number,
    radius: number,
  ): number | null {
    const unlocked = getSync().state.unlockedDoors;
    let bestId: number | null = null;
    let bestDist = radius * radius;
    for (const [id, door] of this.doors) {
      if (!unlocked.has(id)) continue;
      const dx = door.x - x;
      const dy = door.y - y;
      const d2 = dx * dx + dy * dy;
      if (d2 <= bestDist) {
        bestDist = d2;
        bestId = id;
      }
    }
    return bestId;
  }

  private cleanup(): void {
    const sync = getSync();
    sync.state.off("state", this.onServerState, this);
    sync.state.off("roomLoad", this.onRoomLoad, this);
    sync.state.off("doorUnlocked", this.onDoorUnlocked, this);
    sync.state.off("entityDied", this.onEntityDied, this);
    sync.state.off("runComplete", this.onRunComplete, this);

    for (const obj of this.backgroundObjs) obj.destroy();
    this.backgroundObjs = [];
    for (const e of this.enemies.values()) e.destroy();
    this.enemies.clear();
    for (const b of this.projectiles.values()) b.destroy();
    this.projectiles.clear();
    for (const p of this.pickups.values()) p.destroy();
    this.pickups.clear();
    for (const d of this.doors.values()) d.destroy();
    this.doors.clear();

    this.player?.destroy();
    this.player = undefined;
  }
}

export default Game;
