export type WeaponId = "pistol" | "rifle" | "shotgun";

export interface WeaponStats {
  id: WeaponId;
  displayName: string;
  cooldownMs: number;
  bulletSpeed: number;
  damage: number;
  bulletTextureKey: string;
  bulletBodyRadius: number;
  spriteKey: string;
}

export const WEAPON_STATS: Record<WeaponId, WeaponStats> = {
  pistol: {
    id: "pistol",
    displayName: "Pistol",
    cooldownMs: 150,
    bulletSpeed: 600,
    damage: 1,
    bulletTextureKey: "bullet",
    bulletBodyRadius: 4,
    spriteKey: "weapon_pistol",
  },
  rifle: {
    id: "rifle",
    displayName: "Rifle",
    cooldownMs: 220,
    bulletSpeed: 700,
    damage: 2,
    bulletTextureKey: "bullet_lmg",
    bulletBodyRadius: 5,
    spriteKey: "weapon_rifle",
  },
  shotgun: {
    id: "shotgun",
    displayName: "Shotgun",
    cooldownMs: 520,
    bulletSpeed: 480,
    damage: 6,
    bulletTextureKey: "bullet_matador",
    bulletBodyRadius: 7,
    spriteKey: "weapon_shotgun",
  },
};

export const ALL_WEAPON_IDS: WeaponId[] = ["pistol", "rifle", "shotgun"];

export function getWeaponStats(id: WeaponId): WeaponStats {
  return WEAPON_STATS[id];
}
