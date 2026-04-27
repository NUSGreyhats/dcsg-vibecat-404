// Public client-facing config served by the lobby at /api/config.
// Fetched once at boot (see main.ts) so scenes can synchronously read
// feature flags without waiting on a network round-trip mid-render.
//
// Spec: docs/specs/foundations/04-lobby.md.

export type ClientConfig = {
  leaderboard_enabled: boolean;
  queue_enabled: boolean;
  // Only present when leaderboard_enabled is false.
  leaderboard_disabled_message?: string;
};

// Default keeps current behavior on any failure (offline boot, lobby
// down, malformed JSON) — the leaderboard pane renders as before.
const DEFAULT_CONFIG: ClientConfig = {
  leaderboard_enabled: true,
  queue_enabled: false,
};

let cached: ClientConfig = DEFAULT_CONFIG;
let loaded = false;

export async function loadConfig(): Promise<ClientConfig> {
  try {
    const res = await fetch("/api/config", { cache: "no-store" });
    if (!res.ok) {
      cached = DEFAULT_CONFIG;
    } else {
      const body = (await res.json()) as Partial<ClientConfig>;
      cached = {
        leaderboard_enabled: body.leaderboard_enabled ?? true,
        queue_enabled: body.queue_enabled ?? false,
        leaderboard_disabled_message: body.leaderboard_disabled_message,
      };
    }
  } catch {
    cached = DEFAULT_CONFIG;
  }
  loaded = true;
  return cached;
}

// Synchronous read — callers must ensure loadConfig() has resolved
// (main.ts awaits it before constructing the Phaser game). Returns the
// last-loaded config, or the default if loadConfig() was skipped.
export function getConfig(): ClientConfig {
  return cached;
}

export function isConfigLoaded(): boolean {
  return loaded;
}
