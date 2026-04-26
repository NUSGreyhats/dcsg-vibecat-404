import type Phaser from "phaser";

/**
 * Design tokens for VIBECAT 404 — see docs/specs/client/10-ui-components.md.
 *
 * Every hex and numeric constant a scene or component needs should come
 * from this module. Scenes that inline new colors or sizes are the
 * source of drift; prefer adding a token here instead.
 */

// -- Brand blues (sampled from art/title.png) --
export const SKY_BLUE = 0x9ec5e9;
export const MID_BLUE = 0x5c8fbf;
export const NAVY = 0x2b3d5c;
export const DEEP_NAVY = 0x1a2338;
export const MIDNIGHT = 0x0d1222;

// -- Cast palette (sampled from sprite work) --
export const FEDORA_GREY = 0x8a8a8a;
export const FEDORA_SHADOW = 0x4a4a4a;
export const CAT_BLACK = 0x1c1c1c;
export const BONE_WHITE = 0xf5f0e6;
export const BLOOD_RED = 0xc4303b;
export const MASK_RED = 0xe64a55;
export const TRACER_YELLOW = 0xffdd33;
export const COIN_GOLD = 0xf5c03a;

// -- HUD / functional palette --
export const TEXT_PRIMARY = BONE_WHITE;
export const TEXT_SECONDARY = 0xc8c0ae;
export const TEXT_MUTED = 0x8a8679;
export const TEXT_DIM = 0x5a564c;
export const ACCENT_PRIMARY = TRACER_YELLOW;
export const ACCENT_WARM = MASK_RED;
export const SUCCESS = 0x8de77a;
export const WARNING = COIN_GOLD;
export const ERROR = BLOOD_RED;

// -- HP bar thresholds --
export const HP_GREEN = 0x5aa94a;
export const HP_AMBER = COIN_GOLD;
export const HP_RED = BLOOD_RED;

// -- Structural --
export const CANVAS_BG = MIDNIGHT;
export const PANEL_FILL = DEEP_NAVY;
export const PANEL_STROKE = MID_BLUE;
export const BUTTON_BG = NAVY;
export const BUTTON_BG_HOVER = MID_BLUE;
export const TEXT_STROKE = MIDNIGHT;

/**
 * Convert a 24-bit RGB integer to a `#rrggbb` CSS string. Most Phaser
 * Text styles want the string form; fill rectangles want the number
 * form. Keep both available rather than repeating the math inline.
 */
export function hex(value: number): string {
  return "#" + value.toString(16).padStart(6, "0");
}

// -- Spacing (px) --
export const GUTTER_SMALL = 8;
export const GUTTER_MEDIUM = 12;
export const GUTTER_LARGE = 22;
export const BUTTON_PADDING = { left: 16, right: 16, top: 8, bottom: 8 } as const;
export const MODAL_PADDING = 24;
export const PIXEL_GRID = 32;

// -- Typography --
export const FONT_FAMILY = "monospace";
export const FONT_SIZE_TITLE = "48px";
export const FONT_SIZE_HEADING = "28px";
export const FONT_SIZE_SUBTITLE = "26px";
export const FONT_SIZE_SECTION = "22px";
export const FONT_SIZE_BODY_EM = "20px";
export const FONT_SIZE_BODY = "18px";
export const FONT_SIZE_SMALL = "16px";
export const FONT_SIZE_CAPTION = "14px";
export const FONT_SIZE_MICRO = "12px";

export const STROKE_THICKNESS_HUD = 3;
export const STROKE_THICKNESS_LABEL = 2;

/**
 * Standard HUD text style: Bone white on a 3 px Midnight stroke for
 * legibility against the pixel-art world.
 */
export const HUD_TEXT_STYLE: Phaser.Types.GameObjects.Text.TextStyle = {
  fontFamily: FONT_FAMILY,
  fontSize: FONT_SIZE_BODY_EM,
  color: hex(TEXT_PRIMARY),
  stroke: hex(TEXT_STROKE),
  strokeThickness: STROKE_THICKNESS_HUD,
};

/**
 * Standard button text style. Scenes override color on pointerover /
 * pointerout — see {@link BUTTON_HOVER_COLOR}.
 */
export const BUTTON_TEXT_STYLE: Phaser.Types.GameObjects.Text.TextStyle = {
  fontFamily: FONT_FAMILY,
  fontSize: FONT_SIZE_HEADING,
  color: hex(TEXT_PRIMARY),
  backgroundColor: hex(BUTTON_BG),
  padding: BUTTON_PADDING,
};

export const BUTTON_HOVER_COLOR = hex(ACCENT_PRIMARY);
export const BUTTON_IDLE_COLOR = hex(TEXT_PRIMARY);

// -- Depth layers (see 10-ui-components.md § Depth layers) --
export const DEPTH_BG_FALLBACK = -100;
export const DEPTH_WALL_FALLBACK = -50;
export const DEPTH_BADGE = -40;
export const DEPTH_TILEMAP = -30;
export const DEPTH_PLAYER = 10;
export const DEPTH_FX = 20;
export const DEPTH_SPLASH = -10;
export const DEPTH_HUD_BG = 1000;
export const DEPTH_HUD_FILL = 1001;
export const DEPTH_MODAL_TEXT = 1002;
