import Phaser from "phaser";
import {
  S2CType,
  RunOutcome,
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
  runOutcome?: RunOutcome;
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
  /**
   * server_now − client_now in ms, sampled from the most recent source we
   * have (S2C envelope `server_ts`, or HTTP `Date` header via
   * setServerTime). Positive = server clock is ahead of the client clock.
   *
   * Diagnostic only — the client must NOT subtract this from outgoing
   * envelope `ts` (per docs/specs/client/02-net-sync.md:100-101, the
   * stock client sends raw Date.now() so exploiting BUG-02 requires a
   * custom client). UI uses this to warn the user when their clock has
   * drifted past the ±2s server window.
   */
  serverOffsetMs?: number;

  constructor() {
    super();
  }

  /**
   * Record a server-time observation. Called from envelope.apply() with
   * each S2C envelope's server_ts, and from the HTTP login flow with the
   * response's Date header (coarse but available before WS auth, so
   * MainMenu can warn the user before AUTH is even attempted).
   */
  setServerTime(serverNowMs: number, clientNowMs: number = Date.now()): void {
    if (!Number.isFinite(serverNowMs) || serverNowMs <= 0) {
      return;
    }
    this.serverOffsetMs = serverNowMs - clientNowMs;
    this.emit("serverTime", this.serverOffsetMs);
  }

  /**
   * Apply a server envelope. Mutates state and emits the appropriate event.
   */
  apply(envelope: S2CEnvelope): void {
    // Track the server's wallclock on every inbound packet. Cheap, and
    // the latest sample is the freshest; later UI consumers read it via
    // serverOffsetMs.
    if (typeof envelope.server_ts === "number") {
      this.setServerTime(envelope.server_ts);
    }
    switch (envelope.type) {
      case S2CType.AUTH_OK: {
        const data = envelope.data as { display_name: string };
        this.displayName = data.display_name;
        this.emit("authOk", data.display_name);
        break;
      }
      case S2CType.RUN_STARTED: {
        const data = envelope.data as {
          seed: number;
          room: RoomData;
          display_name?: string;
        };
        this.seed = data.seed;
        this.roomData = data.room;
        this.currentRoom = data.room.room_index;
        // Spectator catchup: AUTH_OK is player-only, so the name only
        // arrives here. The server tags every RUN_STARTED so a spectator
        // who deep-linked /spectate/:id without ?name= still gets the
        // display name without a separate /api/live fetch.
        if (data.display_name) {
          this.displayName = data.display_name;
        }
        this.runStarted = true;
        this.runFinished = false;
        this.runOutcome = undefined;
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
        // ROOM_LOAD is emitted before the next STATE tick, so mirror the
        // authoritative split time into elapsedMs immediately. The HUD's
        // left timer and right split row then agree on the transition frame.
        this.elapsedMs = data.split_ms;
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
        const data = envelope.data as {
          door_id: number;
          tile_x: number;
          tile_y: number;
          unlocked_gid: number;
        };
        this.unlockedDoors.add(data.door_id);
        this.emit(
          "doorUnlocked",
          data.door_id,
          data.tile_x,
          data.tile_y,
          data.unlocked_gid,
        );
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
          outcome?: RunOutcome;
          total_ms: number;
          rank: number;
          splits: SplitData[];
          replay_id?: string;
          seed?: number;
        };
        // Older server builds may omit `outcome`; default to COMPLETED so
        // existing replays / in-flight runs keep rendering the WinScreen.
        const outcome: RunOutcome = data.outcome ?? RunOutcome.COMPLETED;
        this.runFinished = true;
        this.runOutcome = outcome;
        this.totalMs = data.total_ms;
        this.elapsedMs = data.total_ms;
        this.rank = data.rank;
        this.splits = data.splits;
        this.emit(
          "runComplete",
          outcome,
          data.total_ms,
          data.rank,
          data.splits,
        );
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
