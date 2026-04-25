export const C2SType = {
  AUTH: 0x01,
  START_RUN: 0x02,
  INPUT: 0x03,
  SHOOT: 0x04,
  SWITCH_WEAPON: 0x05,
  INTERACT: 0x06,
  ENTER_DOOR: 0x07,
  USE_ITEM: 0x08,
  SHOP_PURCHASE: 0x09,
} as const;
export type C2SType = (typeof C2SType)[keyof typeof C2SType];

export const S2CType = {
  AUTH_OK: 0x81,
  RUN_STARTED: 0x82,
  STATE: 0x83,
  ROOM_LOAD: 0x84,
  RUN_COMPLETE: 0x85,
  LEADERBOARD: 0x86,
  ERROR: 0x87,
  DOOR_UNLOCKED: 0x88,
  ENTITY_DIED: 0x89,
} as const;
export type S2CType = (typeof S2CType)[keyof typeof S2CType];

export type C2SEnvelope<T extends C2SType = C2SType> = {
  type: T; // 0x01–0x08, uint8 range
  seq: number; // uint32, monotonic per-connection, starts at 1
  ts: number; // client wallclock ms (Date.now())
  data: C2SDataFor<T>;
};

export type S2CEnvelope<T extends S2CType = S2CType> = {
  type: T; // 0x81–0x89
  seq: number; // uint32, server-side monotonic
  server_ts: number; // server wallclock ms
  data: S2CDataFor<T>;
};

export type C2SDataFor<T extends C2SType> = T extends typeof C2SType.AUTH
  ? { player_id: string } // 32-char hex
  : T extends typeof C2SType.START_RUN
    ? { start_time: number } // client ms
    : T extends typeof C2SType.INPUT
      ? { dx: number; dy: number } // float32, normalized
      : T extends typeof C2SType.SHOOT
        ? { aim_angle: number } // radians
        : T extends typeof C2SType.SWITCH_WEAPON
          ? { weapon_id: number } // uint8
          : T extends typeof C2SType.INTERACT
            ? { target_id: number; claimed_type: number } // claimed_type: BUG-12 substrate
            : T extends typeof C2SType.ENTER_DOOR
              ? { door_id: number }
              : T extends typeof C2SType.USE_ITEM
                ? { item_name: string }
                : T extends typeof C2SType.SHOP_PURCHASE
                  ? { item_id: number } // uint8, 1-indexed into ShopCatalog
                  : never;

export type S2CDataFor<T extends S2CType> = T extends typeof S2CType.AUTH_OK
  ? { display_name: string }
  : T extends typeof S2CType.RUN_STARTED
    ? { seed: number; room: RoomData }
    : T extends typeof S2CType.STATE
      ? {
          elapsed_ms: number;
          player: PlayerSnapshot;
          entities: EntitySnapshot[];
          projectiles: ProjSnapshot[];
          pickups: PickupSpawn[];
        }
      : T extends typeof S2CType.ROOM_LOAD
        ? { room_index: number; room: RoomData; split_ms: number }
        : T extends typeof S2CType.RUN_COMPLETE
          ? {
              outcome: RunOutcome;
              total_ms: number;
              rank: number;
              splits: SplitData[];
              replay_id?: string;
              seed?: number;
            }
          : T extends typeof S2CType.LEADERBOARD
            ? { entries: LeaderboardEntry[] }
            : T extends typeof S2CType.ERROR
              ? { code: string; message: string }
              : T extends typeof S2CType.DOOR_UNLOCKED
                ? { door_id: number }
                : T extends typeof S2CType.ENTITY_DIED
                  ? { entity_id: number }
                  : never;

export type RoomData = {
  room_index: number; // uint8
  tilemap_id: string; // maps to Phaser tilemap key, e.g. "room-3"
  enemies: EnemySpawn[];
  pickups: PickupSpawn[];
  doors: DoorDef[];
};

export type EnemySpawn = {
  entity_id: number;
  type: number;
  x: number;
  y: number;
  hp: number;
};
export type PickupSpawn = {
  entity_id: number;
  type: number;
  x: number;
  y: number;
};
export type DoorDef = {
  door_id: number;
  x: number;
  y: number;
  target_room: number;
  locked: boolean;
};

export type PlayerSnapshot = {
  x: number;
  y: number;
  hp: number; // int16 on server — client just mirrors
  weapon_id: number; // uint8
  ammo: number;
  inventory: string[];
  coins: number; // int32, shop currency
  speed_stacks: number; // uint8, +10% move speed each
  fire_rate_stacks: number; // uint8, cooldown / (1 + 0.1 * stacks)
  damage_stacks: number; // uint8, +10% damage each
};

export type EntitySnapshot = {
  entity_id: number;
  type: number;
  x: number;
  y: number;
  hp: number;
  state: number; // enum: Idle/Patrol/Aggro/Dead (uint8)
};

export type ProjSnapshot = {
  proj_id: number;
  x: number;
  y: number;
  angle: number;
  owner: number; // entity_id; 0 = player
};

export type SplitData = { room_index: number; time_ms: number; kills: number };

// Terminal state of a run on the S2C_RUN_COMPLETE packet. Matches the
// Go-side protocol.RunOutcome constants. Use the named members instead of
// bare string literals.
export const RunOutcome = {
  COMPLETED: "completed",
  FAILED: "failed",
} as const;
export type RunOutcome = (typeof RunOutcome)[keyof typeof RunOutcome];

export type LeaderboardEntry = {
  rank: number;
  display_name: string;
  total_ms: number;
  splits: SplitData[];
  seed: number;
  replay_id: string;
};
