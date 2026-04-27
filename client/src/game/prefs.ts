import type { SplitData } from "../net/packet";

/**
 * Tiny localStorage-backed bag for preferences and the PB record. Kept
 * separate from the game state because none of this is server-driven —
 * if storage is unavailable (private browsing) every getter no-ops and
 * the rest of the game doesn't notice.
 */

const PB_KEY = "vibecat:pb";
const TIMINGS_KEY = "vibecat:timings-visible";
const PLAYER_ID_KEY = "vibecat:player-id";

type StoredPB = {
  total_ms: number;
  splits: SplitData[];
};

function safeGet(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function safeSet(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Out of quota or storage disabled — non-fatal.
  }
}

export function getStoredPB(): StoredPB | null {
  const raw = safeGet(PB_KEY);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as StoredPB;
    if (typeof parsed?.total_ms !== "number" || !Array.isArray(parsed.splits)) {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

/**
 * Record a finished run if it improves on the stored PB. Returns true
 * when the PB was updated. Splits are stored verbatim — the HUD reads
 * them back to compute per-room deltas.
 */
export function maybeRecordPB(totalMs: number, splits: SplitData[]): boolean {
  const current = getStoredPB();
  if (current && current.total_ms <= totalMs) return false;
  const next: StoredPB = { total_ms: totalMs, splits: splits.slice() };
  safeSet(PB_KEY, JSON.stringify(next));
  return true;
}

export function isTimingsVisible(): boolean {
  // Default to visible. Stored as "0" / "1" so the value survives
  // boolean-as-string round-trips. Controls only the speedrun timings
  // (run timer + splits column) — HP / weapon / room counter ignore it.
  const raw = safeGet(TIMINGS_KEY);
  if (raw === "0") return false;
  return true;
}

export function setTimingsVisible(visible: boolean): void {
  safeSet(TIMINGS_KEY, visible ? "1" : "0");
}

/**
 * The most recently authenticated player_id. Kept only as local session
 * state so explicit logout can clear stale credentials; MainMenu still
 * performs WS auth only after a fresh username/password login (and queue
 * admission when enabled). Cleared on auth_failed so a stale id doesn't
 * trap the user in a loop.
 */
export function getCachedPlayerId(): string | null {
  const v = safeGet(PLAYER_ID_KEY);
  return v && v.length > 0 ? v : null;
}

export function setCachedPlayerId(playerId: string): void {
  if (!playerId) return;
  safeSet(PLAYER_ID_KEY, playerId);
}

export function clearCachedPlayerId(): void {
  try {
    localStorage.removeItem(PLAYER_ID_KEY);
  } catch {
    // ignore
  }
}
