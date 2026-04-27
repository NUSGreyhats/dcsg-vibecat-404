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

/**
 * Read-only sibling of Game. Renders the same world from the spectator
 * sync's GameState. No Controls, no input handlers, no send calls.
 *
 * Mirrors Game.ts on the reconcile side so we render a faithful copy of
 * what the player sees — including intentional-bug artifacts (kill-log
 * garbage, pre-settle shots, time-warp drift). The viewer is the show.
 *
 * See docs/specs/client/09-spectator-mode.md.
 */
export class SpectateGame extends Phaser.Scene {
  player?: Player;
  enemies: Map<number, Enemy> = new Map();
  projectiles: Map<number, Bullet> = new Map();
  pickups: Map<number, Pickup> = new Map();
  doors: Map<number, Door> = new Map();
  backgroundObjs: Phaser.GameObjects.GameObject[] = [];
  private displayName = "";

  constructor() {
    super({ key: "SpectateGame" });
  }

  init(data: { displayName?: string }): void {
    this.displayName = data.displayName ?? "";
  }

  create(): void {
    const state = getActiveState();
    const roomData = state.roomData;
    if (!roomData) {
      this.scene.start("SpectateEnd", {
        displayName: this.displayName,
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
    // useSnapshotAim — server-authoritative aim instead of local cursor.
    this.player = new Player(this, px, py, { useSnapshotAim: true });
    this.add.existing(this.player);

    // Spectator badge — top-center, semi-muted so it doesn't fight the HUD.
    // Cap the name length so a 60-char display name doesn't overflow the
    // canvas; wordWrap+maxLines is the belt-and-braces clamp.
    const rawName = this.displayName || "anonymous";
    const trimmedName =
      rawName.length > 28 ? rawName.slice(0, 27) + "…" : rawName;
    this.add
      .text(this.scale.width / 2, 4, `SPECTATING: ${trimmedName}`, {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_CAPTION,
        color: hex(ACCENT_PRIMARY),
        wordWrap: { width: this.scale.width - 16, useAdvancedWrap: true },
        maxLines: 1,
      })
      .setOrigin(0.5, 0)
      .setScrollFactor(0)
      .setDepth(10000);

    state.on("state", this.onServerState, this);
    state.on("roomLoad", this.onRoomLoad, this);
    state.on("doorUnlocked", this.onDoorUnlocked, this);
    state.on("entityDied", this.onEntityDied, this);
    state.on("runComplete", this.onRunComplete, this);
    state.on("error", this.onError, this);
    state.on("disconnect", this.onDisconnect, this);

    this.events.on("shutdown", this.cleanup, this);
  }

  update(_time: number, delta: number): void {
    const dt = delta / 1000;
    this.player?.tick(dt);
    for (const e of this.enemies.values()) e.tick(dt);
    for (const b of this.projectiles.values()) b.tick(dt);
    // No input handling — that's the whole point.
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

    // Catchup reconcile: spectators who attach mid-room never received
    // the S2C_ENTITY_DIED events for kills that happened before they
    // joined, so deadEntities is empty for those. The server filters
    // dead enemies and consumed pickups out of S2C_STATE, so absence
    // from the seen sets is an authoritative "this entity is gone now."
    // Run the prune unconditionally — it's O(n) and the player code
    // doesn't hit it because the player's deadEntities path already
    // tore each one down on the same tick the server filtered it out.
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
    this.scene.start("SpectateEnd", {
      displayName: this.displayName,
      outcome,
      totalMs,
      rank,
      reason: "Run complete",
    });
  }

  private onError(err: { code: string; message: string }): void {
    this.scene.stop("GameHUD");
    this.scene.start("SpectateEnd", {
      displayName: this.displayName,
      reason: `${err.code}: ${err.message}`,
    });
  }

  private onDisconnect(): void {
    // If runComplete already fired, the run-end transition takes precedence.
    const state = getActiveState();
    if (state.runFinished) return;
    this.scene.stop("GameHUD");
    this.scene.start("SpectateEnd", {
      displayName: this.displayName,
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

export default SpectateGame;
