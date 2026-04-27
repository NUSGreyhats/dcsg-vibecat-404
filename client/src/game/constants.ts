export const ROOM_WIDTH = 1280;
export const ROOM_HEIGHT = 768;
export const ROOM_COUNT = 10;
export const WIN_ROOM = 10;

export const PLAYER_MAX_HEALTH = 100;

export const WS_URL = (() => {
  if (typeof window !== "undefined" && window.location) {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    // In prod, the reverse proxy routes /ws → game-server :8080.
    // In dev, override with VITE_WS_URL to point at :8080 directly.
    const override = import.meta.env?.VITE_WS_URL as string | undefined;
    if (override) return override;
    return `${proto}//${window.location.host}/ws`;
  }
  return "ws://localhost:8080/ws";
})();

// Tick intervals (server runs at 20 Hz).
export const INPUT_SEND_INTERVAL_MS = 50;

// Client-side shoot rate multiplier applied when the fire button has
// been held since the last shot (no release in between). Spam-clicking
// fires at the weapon's full rate; holding self-throttles to
// HOLD_FIRE_RATE_MULT of the full rate. Server cooldown is unchanged —
// this is a local nerf so spam-clickers can keep a small (~15%) edge
// over hold-to-fire, not a server rule. See issue #62.
export const HOLD_FIRE_RATE_MULT = 0.85;

export const WALK_ANIM_FRAMERATE = 8;
