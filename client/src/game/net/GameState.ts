import Phaser from "phaser";
import {
  S2CType,
  type S2CEnvelope,
  type RoomData,
  type PlayerSnapshot,
  type EntitySnapshot,
  type ProjSnapshot,
  type SplitData,
  type LeaderboardEntry,
} from "../../net/packet";

export type GameStateError = { code: string; message: string };

export class GameState extends Phaser.Events.EventEmitter {
  displayName?: string;
  seed?: number;
  runStarted = false;
  runFinished = false;
  elapsedMs = 0;
  currentRoom?: number;
  roomData?: RoomData;
  player?: PlayerSnapshot;
  entities: EntitySnapshot[] = [];
  projectiles: ProjSnapshot[] = [];
  unlockedDoors = new Set<number>();
  deadEntities = new Set<number>();
  totalMs?: number;
  rank?: number;
  splits: SplitData[] = [];
  leaderboard: LeaderboardEntry[] = [];
  lastError?: GameStateError;

  constructor() {
    super();
  }

  /**
   * Apply a server envelope. Mutates state and emits the appropriate event.
   */
  apply(envelope: S2CEnvelope): void {
    switch (envelope.type) {
      case S2CType.AUTH_OK: {
        const data = envelope.data as { display_name: string };
        this.displayName = data.display_name;
        this.emit("authOk", data.display_name);
        break;
      }
      case S2CType.RUN_STARTED: {
        const data = envelope.data as { seed: number; room: RoomData };
        this.seed = data.seed;
        this.roomData = data.room;
        this.currentRoom = data.room.room_index;
        this.runStarted = true;
        this.runFinished = false;
        this.elapsedMs = 0;
        this.entities = [];
        this.projectiles = [];
        this.unlockedDoors = new Set<number>();
        this.deadEntities = new Set<number>();
        this.splits = [];
        this.totalMs = undefined;
        this.rank = undefined;
        this.emit("runStarted", data.seed, data.room);
        break;
      }
      case S2CType.STATE: {
        const data = envelope.data as {
          elapsed_ms: number;
          player: PlayerSnapshot;
          entities: EntitySnapshot[];
          projectiles: ProjSnapshot[];
        };
        this.elapsedMs = data.elapsed_ms;
        this.player = data.player;
        this.entities = data.entities;
        this.projectiles = data.projectiles;
        this.emit("state", data);
        break;
      }
      case S2CType.ROOM_LOAD: {
        const data = envelope.data as {
          room_index: number;
          room: RoomData;
          split_ms: number;
        };
        // Server-side split semantics: split_ms is the cumulative
        // elapsed time at the moment the player FINISHED the previous
        // room (entered this one). Record it under the previous room's
        // index so the HUD can show one row per cleared room.
        const finishedRoom = this.currentRoom ?? data.room_index - 1;
        if (finishedRoom >= 1) {
          this.splits.push({
            room_index: finishedRoom,
            time_ms: data.split_ms,
            kills: 0,
          });
        }
        this.currentRoom = data.room_index;
        this.roomData = data.room;
        this.entities = [];
        this.projectiles = [];
        this.unlockedDoors = new Set<number>();
        this.deadEntities = new Set<number>();
        this.emit("roomLoad", data.room_index, data.room, data.split_ms);
        break;
      }
      case S2CType.DOOR_UNLOCKED: {
        const data = envelope.data as { door_id: number };
        this.unlockedDoors.add(data.door_id);
        this.emit("doorUnlocked", data.door_id);
        break;
      }
      case S2CType.ENTITY_DIED: {
        const data = envelope.data as { entity_id: number };
        this.deadEntities.add(data.entity_id);
        this.emit("entityDied", data.entity_id);
        break;
      }
      case S2CType.RUN_COMPLETE: {
        const data = envelope.data as {
          total_ms: number;
          rank: number;
          splits: SplitData[];
          replay_id?: string;
          seed?: number;
        };
        this.runFinished = true;
        this.totalMs = data.total_ms;
        this.rank = data.rank;
        this.splits = data.splits;
        this.emit("runComplete", data.total_ms, data.rank, data.splits);
        break;
      }
      case S2CType.LEADERBOARD: {
        const data = envelope.data as { entries: LeaderboardEntry[] };
        this.leaderboard = data.entries;
        this.emit("leaderboard", data.entries);
        break;
      }
      case S2CType.ERROR: {
        const data = envelope.data as { code: string; message: string };
        this.lastError = { code: data.code, message: data.message };
        this.emit("error", this.lastError);
        break;
      }
      default: {
        // Exhaustive: unknown envelope type. Swallow silently so a future
        // server-side message doesn't crash the client.
        break;
      }
    }
  }
}
