export type WeaponId = "sar21" | "maximi" | "cs-lr4" | "matador";

export interface WeaponStats {
  id: WeaponId;
  displayName: string;
  cooldownMs: number;
  bulletSpeed: number;
  damage: number;
  bulletTextureKey: string;
  bulletBodyRadius: number;
}

export const WEAPON_STATS: Record<WeaponId, WeaponStats> = {
  sar21: {
    id: "sar21",
    displayName: "SAR 21",
    cooldownMs: 150,
    bulletSpeed: 600,
    damage: 1,
    bulletTextureKey: "bullet",
    bulletBodyRadius: 4,
  },
  maximi: {
    id: "maximi",
    displayName: "Ultimax 100",
    cooldownMs: 220,
    bulletSpeed: 700,
    damage: 2,
    bulletTextureKey: "bullet_lmg",
    bulletBodyRadius: 5,
  },
  "cs-lr4": {
    id: "cs-lr4",
    displayName: "CS/LR4",
    cooldownMs: 450,
    bulletSpeed: 950,
    damage: 4,
    bulletTextureKey: "bullet_dmr",
    bulletBodyRadius: 6,
  },
  matador: {
    id: "matador",
    displayName: "MATADOR",
    cooldownMs: 520,
    bulletSpeed: 480,
    damage: 6,
    bulletTextureKey: "bullet_matador",
    bulletBodyRadius: 7,
  },
};

export const ALL_WEAPON_IDS: WeaponId[] = [
  "sar21",
  "maximi",
  "cs-lr4",
  "matador",
];

export function getWeaponStats(id: WeaponId): WeaponStats {
  return WEAPON_STATS[id];
}
