import Phaser from "phaser";
import { getActiveState } from "../net/runContext";
import { ROOM_WIDTH, ROOM_HEIGHT } from "../constants";
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
import type {
  RoomData,
  EntitySnapshot,
  ProjSnapshot,
  PlayerSnapshot,
  PickupSpawn,
  RunOutcome,
} from "../../net/packet";
import { formatTime } from "../ui/formatters";
import {
  ACCENT_PRIMARY,
  FONT_FAMILY,
  FONT_SIZE_CAPTION,
  hex,
} from "../ui/tokens";

type StatePayload = {
  elapsed_ms: number;
  player: PlayerSnapshot;
  entities: EntitySnapshot[];
  projectiles: ProjSnapshot[];
  pickups: PickupSpawn[];
};

type ReplayMeta = {
  display_name: string;
  total_ms: number;
};

/**
 * Read-only sibling of SpectateGame. Renders the replayed world from
 * the replay sync's GameState. No Controls, no input handlers, no game-
 * affecting send calls. Identical reconciliation logic to SpectateGame
 * because the protocol is the same; the only behavioural delta is the
 * top badge ("REPLAY: name — total_ms") and that we listen for
 * runStarted to reset on Restart.
 *
 * See docs/specs/client/11-replay-viewer.md.
 */
export class ReplayGame extends Phaser.Scene {
  player?: Player;
  enemies: Map<number, Enemy> = new Map();
  projectiles: Map<number, Bullet> = new Map();
  pickups: Map<number, Pickup> = new Map();
  doors: Map<number, Door> = new Map();
  backgroundObjs: Phaser.GameObjects.GameObject[] = [];
  private meta: ReplayMeta = { display_name: "", total_ms: 0 };

  constructor() {
    super({ key: "ReplayGame" });
  }

  init(data: { meta?: ReplayMeta }): void {
    if (data.meta) this.meta = data.meta;
  }

  create(): void {
    const state = getActiveState();
    const roomData = state.roomData;
    if (!roomData) {
      this.scene.start("ReplayEnd", {
        meta: this.meta,
        reason: "missing room data",
      });
      return;
    }

    this.backgroundObjs = paintRoomBackground(this, roomData);
    const built = buildRoom(this, roomData);
    this.enemies = built.enemies;
    this.pickups = built.pickups;
    this.doors = built.doors;

    const px = state.player?.x ?? ROOM_WIDTH / 2;
    const py = state.player?.y ?? ROOM_HEIGHT - 100;
    this.player = new Player(this, px, py, { useSnapshotAim: true });
    this.add.existing(this.player);

    const badge = this.meta.display_name
      ? `REPLAY: ${this.meta.display_name} — ${formatTime(this.meta.total_ms)}`
      : "REPLAY";
    this.add
      .text(this.scale.width / 2, 4, badge, {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_CAPTION,
        color: hex(ACCENT_PRIMARY),
      })
      .setOrigin(0.5, 0)
      .setScrollFactor(0)
      .setDepth(10000);

    state.on("state", this.onServerState, this);
    state.on("roomLoad", this.onRoomLoad, this);
    state.on("doorUnlocked", this.onDoorUnlocked, this);
    state.on("entityDied", this.onEntityDied, this);
    state.on("runComplete", this.onRunComplete, this);
    state.on("runStarted", this.onRunStarted, this);
    state.on("error", this.onError, this);
    state.on("disconnect", this.onDisconnect, this);

    this.events.on("shutdown", this.cleanup, this);
  }

  update(_time: number, delta: number): void {
    const dt = delta / 1000;
    this.player?.tick(dt);
    for (const e of this.enemies.values()) e.tick(dt);
    for (const b of this.projectiles.values()) b.tick(dt);
  }

  /**
   * Restart sends C2S_REPLAY_CONTROL{action:"restart"}; the server
   * replays from T0 and emits a fresh RUN_STARTED. Tear down our
   * scene-local entity maps so the next ROOM_LOAD / STATE pass builds
   * cleanly. GameState.apply() resets its internal mirror on RUN_STARTED
   * already, so we don't need to clear `state.*` here.
   */
  private onRunStarted(_seed: number, room: RoomData): void {
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

    this.backgroundObjs = paintRoomBackground(this, room);
    const built: BuiltRoom = buildRoom(this, room);
    this.enemies = built.enemies;
    this.pickups = built.pickups;
    this.doors = built.doors;
  }

  private onServerState(payload: StatePayload): void {
    if (this.player && payload.player) {
      this.player.applySnapshot(payload.player);
    }

    const seenEntityIds = new Set<number>();
    for (const snap of payload.entities) {
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
    for (const [id, bullet] of this.projectiles) {
      if (!seenProjIds.has(id)) {
        bullet.destroy();
        this.projectiles.delete(id);
      }
    }

    const state = getActiveState();
    const seenPickupIds = new Set<number>();
    if (payload.pickups) {
      for (const snap of payload.pickups) {
        seenPickupIds.add(snap.entity_id);
        if (state.deadEntities.has(snap.entity_id)) continue;
        if (this.pickups.has(snap.entity_id)) continue;
        const pickup = new Pickup(this, snap);
        this.pickups.set(snap.entity_id, pickup);
      }
    }

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

    // Absence-based prune — same as spectator. The replay engine
    // produces a clean stream from T0 so this is normally a no-op,
    // but it correctly handles the (edge-case) Restart path where
    // entity_died events may not have time to flush before the new
    // RUN_STARTED rebuild.
    for (const [id, enemy] of this.enemies) {
      if (!seenEntityIds.has(id)) {
        enemy.destroy();
        this.enemies.delete(id);
      }
    }
    for (const [id, pickup] of this.pickups) {
      if (!seenPickupIds.has(id)) {
        pickup.destroy();
        this.pickups.delete(id);
      }
    }
  }

  private onRoomLoad(_roomIndex: number, room: RoomData, _splitMs: number): void {
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

    this.backgroundObjs = paintRoomBackground(this, room);
    const built: BuiltRoom = buildRoom(this, room);
    this.enemies = built.enemies;
    this.pickups = built.pickups;
    this.doors = built.doors;

    const state = getActiveState();
    const px = state.player?.x ?? ROOM_WIDTH / 2;
    const py = state.player?.y ?? ROOM_HEIGHT - 100;
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

  private onRunComplete(outcome: RunOutcome, totalMs: number, rank: number): void {
    this.scene.stop("GameHUD");
    this.scene.stop("ReplayControls");
    this.scene.start("ReplayEnd", {
      meta: this.meta,
      outcome,
      totalMs,
      rank,
      reason: "Replay finished",
    });
  }

  private onError(err: { code: string; message: string }): void {
    this.scene.stop("GameHUD");
    this.scene.stop("ReplayControls");
    this.scene.start("ReplayEnd", {
      meta: this.meta,
      reason: `${err.code}: ${err.message}`,
    });
  }

  private onDisconnect(): void {
    const state = getActiveState();
    if (state.runFinished) return;
    this.scene.stop("GameHUD");
    this.scene.stop("ReplayControls");
    this.scene.start("ReplayEnd", {
      meta: this.meta,
      reason: "Connection lost",
    });
  }

  private cleanup(): void {
    const state = getActiveState();
    state.off("state", this.onServerState, this);
    state.off("roomLoad", this.onRoomLoad, this);
    state.off("doorUnlocked", this.onDoorUnlocked, this);
    state.off("entityDied", this.onEntityDied, this);
    state.off("runComplete", this.onRunComplete, this);
    state.off("runStarted", this.onRunStarted, this);
    state.off("error", this.onError, this);
    state.off("disconnect", this.onDisconnect, this);

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

export default ReplayGame;
