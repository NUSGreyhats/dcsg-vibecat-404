import type { WeaponId } from "./weapons";

export type ShopDeal = {
  weaponId: WeaponId;
  price: number;
  shopLabel: string;
};

export const SHOP_DEALS: ShopDeal[] = [
  { weaponId: "maximi", price: 5, shopLabel: "Ultimax 100" },
  { weaponId: "matador", price: 200, shopLabel: "MATADOR" },
];

/** US QWERTY backtick / tilde physical key (shared with ~). */
export const KEYCODE_BACKTICK = 192;
