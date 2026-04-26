import Phaser from "phaser";
import { getSync } from "../net/runContext";
import type { GameState } from "../net/GameState";
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
import {
  cameraShake,
  FX_COLOR,
  spawnImpactBurst,
  spawnMuzzleFlash,
  triggerHitstop,
  WEAPON_FX,
} from "../fx";
import { maybeRecordPB } from "../prefs";
import { spawnPickupToast } from "../ui/PickupToast";
import { BONE_WHITE, COIN_GOLD, HP_GREEN } from "../ui/tokens";
import { WEAPON_STATS, weaponFromServerId } from "../weapons";
import { RunOutcome as RunOutcomeValue } from "../../net/packet";
import { createControls, type Controls } from "./utils/Controls";
import type {
  RoomData,
  EntitySnapshot,
  ProjSnapshot,
  PlayerSnapshot,
  PickupSpawn,
  SplitData,
  RunOutcome,
} from "../../net/packet";

type StatePayload = {
  elapsed_ms: number;
  player: PlayerSnapshot;
  entities: EntitySnapshot[];
  projectiles: ProjSnapshot[];
  pickups: PickupSpawn[];
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
  private nextShootAt = 0;
  private requestedPickupIds = new Set<number>();
  private requestedDoorIds = new Set<number>();
  /** Read by fx.isHitstopped — entity ticks halt while non-zero. */
  hitstopUntil = 0;
  /** Last seen player HP, used to detect damage and play hurt fx. */
  private prevPlayerHp = -1;
  /** Positions where enemies took damage this tick — used to suppress
   *  redundant wall sparks at projectile despawn. */
  private hitPositionsThisFrame: { x: number; y: number }[] = [];
  /** GameState captured at create() so cleanup detaches from the same
   *  instance, even after resetSync() swaps in a new sync/state. */
  private boundState?: GameState;

  constructor() {
    super({ key: "Game" });
  }

  create(): void {
    const sync = getSync();
    this.controls = createControls(this);
    this.requestedPickupIds.clear();
    this.requestedDoorIds.clear();

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

    this.boundState = sync.state;
    sync.state.on("state", this.onServerState, this);
    sync.state.on("roomLoad", this.onRoomLoad, this);
    sync.state.on("doorUnlocked", this.onDoorUnlocked, this);
    sync.state.on("entityDied", this.onEntityDied, this);
    sync.state.on("runComplete", this.onRunComplete, this);
    sync.state.on("error", this.onSyncError, this);

    // Enemies emit this on HP decrease so we can play impact fx at the
    // hit location without the entity needing a back-pointer here.
    this.events.on("enemyHit", this.onEnemyHit, this);

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

    if (this.controls.shootHeld()) {
      const sp = sync.state.player;
      const now = this.time.now;
      if (this.player && sp && now >= this.nextShootAt) {
        // Out of ammo: skip the visuals AND the network message — the
        // server already rejects empty-mag SHOOT packets, but a flash
        // + shake on a no-op press would mislead the player into
        // thinking the shot landed.
        if ((sp.ammo ?? 0) > 0) {
          const angle = this.controls.aimAngle(this.player.x, this.player.y);
          sync.sendShoot(angle);
          this.player.triggerShootAnim();
          const id = weaponFromServerId(sp.weapon_id);
          const baseCd = WEAPON_STATS[id].cooldownMs;
          const stacks = sp.fire_rate_stacks ?? 0;
          this.nextShootAt = now + baseCd / (1 + 0.1 * stacks);

          // Muzzle flash + camera shake fire on every shot. The
          // nextShootAt gate above already throttles this — no
          // separate fx cooldown needed.
          const fx = WEAPON_FX[id];
          const barrelLen = 22;
          const bx = this.player.x + Math.cos(angle) * barrelLen;
          const by = this.player.y + Math.sin(angle) * barrelLen;
          spawnMuzzleFlash(this, bx, by, angle, {
            length: fx.muzzleLength,
            thickness: fx.muzzleThickness,
          });
          cameraShake(this, fx.shakeMs, fx.shakeIntensity);
        }
      }
    } else {
      // Release resets the gate so the next click fires instantly.
      this.nextShootAt = 0;
    }

    const slot = this.controls.weaponSwitchPressed();
    if (slot !== null) sync.sendSwitchWeapon(slot);

    if (this.player) {
      const nearest = this.nearestPickup(this.player.x, this.player.y, 24);
      if (nearest && !this.requestedPickupIds.has(nearest.id)) {
        this.requestedPickupIds.add(nearest.id);
        // Honest client: forward the pickup type as rendered. The server
        // *trusts* this field (BUG-12) — exploits live in the wire layer.
        sync.sendInteract(nearest.id, nearest.type);
      }

      const doorId = this.nearestUnlockedDoorId(
        this.player.x,
        this.player.y,
        28,
      );
      if (doorId !== null && !this.requestedDoorIds.has(doorId)) {
        this.requestedDoorIds.add(doorId);
        sync.sendEnterDoor(doorId);
      }
    }

    if (this.controls.inventoryPressed()) {
      this.scene.launch("InventoryMenu");
      this.scene.pause();
    }

    if (this.controls.shopPressed()) {
      this.scene.launch("ShopMenu");
      this.scene.pause();
    }

    if (this.controls.pausePressed()) {
      this.scene.launch("PauseMenu");
      this.scene.pause();
    }
  }

  private onServerState(payload: StatePayload): void {
    // Reset per-tick fx accumulators before re-emitting events.
    this.hitPositionsThisFrame = [];

    // Player damage detection — first snapshot just primes prevPlayerHp.
    if (this.player && payload.player) {
      const newHp = payload.player.hp;
      if (this.prevPlayerHp >= 0 && newHp < this.prevPlayerHp) {
        triggerHitstop(this, 60);
      }
      this.prevPlayerHp = newHp;
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
    // Despawn missing projectiles. If the despawn doesn't line up with a
    // same-frame enemy hit, treat it as a wall absorb and spark there.
    for (const [id, bullet] of this.projectiles) {
      if (!seenProjIds.has(id)) {
        if (!this.bulletEndedOnHit(bullet.x, bullet.y)) {
          spawnImpactBurst(this, bullet.x, bullet.y, {
            color: FX_COLOR.spark,
            count: 4,
            speed: 60,
            lifeMs: 180,
          });
        }
        bullet.destroy();
        this.projectiles.delete(id);
      }
    }

    // Spawn newly-revealed pickups (coin drops appear mid-room). Pickups
    // that vanish from the snapshot are torn down via the deadEntities
    // path below — same code that handles consumed health/ammo crates.
    const state = getSync().state;
    if (payload.pickups) {
      for (const snap of payload.pickups) {
        if (state.deadEntities.has(snap.entity_id)) continue;
        if (this.pickups.has(snap.entity_id)) continue;
        const pickup = new Pickup(this, snap);
        this.pickups.set(snap.entity_id, pickup);
      }
    }

    // Drop enemies/pickups that the server marked dead.
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
    this.requestedPickupIds.clear();
    this.requestedDoorIds.clear();

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
    this.game.events.emit("doorUnlockedToast", doorId);
  }

  private onEntityDied(entityId: number): void {
    const enemy = this.enemies.get(entityId);
    if (enemy) {
      // Beefier burst on a kill so it reads as more than a damage tick.
      spawnImpactBurst(this, enemy.x, enemy.y, {
        color: FX_COLOR.blood,
        count: 12,
        speed: 140,
        lifeMs: 320,
        sizeMin: 2,
        sizeMax: 4,
      });
      enemy.markDead();
    }
    const pk = this.pickups.get(entityId);
    if (pk && this.requestedPickupIds.has(entityId)) {
      this.spawnPickupToastFor(pk);
    }
    this.requestedPickupIds.delete(entityId);
    pk?.setConsumed();
  }

  private spawnPickupToastFor(pk: Pickup): void {
    const HEALTH = 1;
    const AMMO = 2;
    const COIN = 3;

    let text: string;
    let color: number;
    switch (pk.pickupType) {
      case HEALTH:
        text = "+20 Health";
        color = HP_GREEN;
        break;
      case AMMO: {
        const weaponId = getSync().state.player?.weapon_id ?? 0;
        const weapon = WEAPON_STATS[weaponFromServerId(weaponId)];
        text = `+20 ${weapon.displayName} ammo`;
        color = BONE_WHITE;
        break;
      }
      case COIN:
        text = "+1 Coin";
        color = COIN_GOLD;
        break;
      default:
        return;
    }
    spawnPickupToast(this, pk.x, pk.y, text, color);
  }

  private onEnemyHit(x: number, y: number, _entityId: number): void {
    spawnImpactBurst(this, x, y, {
      color: FX_COLOR.blood,
      count: 7,
      speed: 100,
    });
    triggerHitstop(this, 40);
    this.hitPositionsThisFrame.push({ x, y });
  }

  private bulletEndedOnHit(bx: number, by: number): boolean {
    const r2 = 40 * 40;
    for (const p of this.hitPositionsThisFrame) {
      const dx = p.x - bx;
      const dy = p.y - by;
      if (dx * dx + dy * dy <= r2) return true;
    }
    return false;
  }

  private onRunComplete(
    outcome: RunOutcome,
    totalMs: number,
    _rank: number,
    splits: SplitData[],
  ): void {
    if (outcome === RunOutcomeValue.COMPLETED) {
      maybeRecordPB(totalMs, splits);
    }
    this.endRun(outcome);
  }

  private onSyncError(err: { code: string; message: string }): void {
    // Mid-run WS drop without a RUN_COMPLETE — most commonly the death
    // path before the server fully drained, but also any network blip.
    // Fall back to the WinScreen as a failed run so the player isn't
    // staring at a frozen room with no way out.
    if (err.code !== "disconnected") return;
    const state = getSync().state;
    if (state.runFinished) return; // RUN_COMPLETE already handled.
    // RUN_COMPLETE never landed, so totalMs is undefined — seed it from
    // the most recent STATE snapshot's elapsed counter. Mark the run as
    // finished to mirror the normal completion path (and prevent re-entry
    // if the error event fires more than once).
    state.totalMs = state.elapsedMs;
    state.runFinished = true;
    state.runOutcome = RunOutcomeValue.FAILED;
    this.endRun(RunOutcomeValue.FAILED);
  }

  /**
   * Tear down every overlay scene that may be sitting on top of the
   * gameplay (HUD, pause menu, inventory) before launching the WinScreen.
   * Without stopping PauseMenu, a player who dies mid-pause sees the
   * pause modal layered over the run-failed screen.
   */
  private endRun(outcome: RunOutcome): void {
    // Stop PauseMenu first while the Game + GameHUD cameras are still
    // live so its blur post-FX teardown finds something to remove.
    this.scene.stop("PauseMenu");
    this.scene.stop("InventoryMenu");
    this.scene.stop("GameHUD");
    this.scene.start("WinScreen", { outcome });
  }

  private nearestPickup(
    x: number,
    y: number,
    radius: number,
  ): { id: number; type: number } | null {
    let best: { id: number; type: number } | null = null;
    let bestDist = radius * radius;
    for (const [id, pk] of this.pickups) {
      const dx = pk.x - x;
      const dy = pk.y - y;
      const d2 = dx * dx + dy * dy;
      if (d2 <= bestDist) {
        bestDist = d2;
        best = { id, type: pk.pickupType };
      }
    }
    return best;
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
    // Use the captured state reference rather than getSync().state — the
    // sync may have been reset (e.g. PauseMenu Restart Run), in which
    // case getSync() now returns a fresh state and our listeners on the
    // OLD state would otherwise leak. Stale listeners would fire on the
    // old WS's onclose and falsely trigger the run-failed flow.
    const state = this.boundState;
    if (state) {
      state.off("state", this.onServerState, this);
      state.off("roomLoad", this.onRoomLoad, this);
      state.off("doorUnlocked", this.onDoorUnlocked, this);
      state.off("entityDied", this.onEntityDied, this);
      state.off("runComplete", this.onRunComplete, this);
      state.off("error", this.onSyncError, this);
    }
    this.boundState = undefined;
    this.events.off("enemyHit", this.onEnemyHit, this);
    this.hitstopUntil = 0;
    this.prevPlayerHp = -1;
    this.requestedPickupIds.clear();
    this.requestedDoorIds.clear();

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
