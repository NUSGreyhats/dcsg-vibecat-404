export type WeaponId = "pistol" | "rifle" | "shotgun";

export interface WeaponStats {
  id: WeaponId;
  serverId: number;
  displayName: string;
  cooldownMs: number;
  bulletSpeed: number;
  damage: number;
  bulletTextureKey: string;
  bulletBodyRadius: number;
  spriteKey: string;
  ammoMax: number;
}

export const WEAPON_STATS: Record<WeaponId, WeaponStats> = {
  pistol: {
    id: "pistol",
    serverId: 1,
    displayName: "Pistol",
    cooldownMs: 150,
    bulletSpeed: 600,
    damage: 1,
    bulletTextureKey: "bullet",
    bulletBodyRadius: 4,
    spriteKey: "weapon_pistol",
    ammoMax: 9999,
  },
  rifle: {
    id: "rifle",
    serverId: 2,
    displayName: "Rifle",
    cooldownMs: 75,
    bulletSpeed: 700,
    damage: 2,
    bulletTextureKey: "bullet_lmg",
    bulletBodyRadius: 5,
    spriteKey: "weapon_rifle",
    ammoMax: 30,
  },
  shotgun: {
    id: "shotgun",
    serverId: 3,
    displayName: "Shotgun",
    cooldownMs: 520,
    bulletSpeed: 480,
    damage: 6,
    bulletTextureKey: "bullet_matador",
    bulletBodyRadius: 7,
    spriteKey: "weapon_shotgun",
    ammoMax: 8,
  },
};

export const ALL_WEAPON_IDS: WeaponId[] = ["pistol", "rifle", "shotgun"];

export function getWeaponStats(id: WeaponId): WeaponStats {
  return WEAPON_STATS[id];
}

// Map the server's numeric weapon_id to its client-side WeaponId. Previous
// code indexed into ALL_WEAPON_IDS directly which silently aliased server
// ids to the wrong slot when ids are non-contiguous.
const SERVER_ID_TO_WEAPON: Map<number, WeaponId> = new Map(
  (Object.values(WEAPON_STATS) as WeaponStats[]).map((s) => [s.serverId, s.id]),
);

export function weaponFromServerId(serverId: number): WeaponId {
  return SERVER_ID_TO_WEAPON.get(serverId) ?? "pistol";
}
