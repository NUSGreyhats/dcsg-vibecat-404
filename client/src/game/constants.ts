export const ROOM_WIDTH = 1280;
export const ROOM_HEIGHT = 768;
export const ROOM_COUNT = 11;
export const WIN_ROOM = 11;

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

// Mirrors server `HoldFireCooldownMult` (server/internal/game/constants.go).
// Applied to the per-shot cooldown when a SHOOT is sent with `released=false`
// (a held continuation). Must match the server value or the local throttle
// will queue shots the server then rejects. See issue #62.
export const HOLD_FIRE_COOLDOWN_MULT = 2;

export const WALK_ANIM_FRAMERATE = 8;
