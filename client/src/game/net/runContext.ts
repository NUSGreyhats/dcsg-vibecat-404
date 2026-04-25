import { GameStateSync } from "./GameStateSync";
import { SpectatorSync } from "./SpectatorSync";
import type { GameState } from "./GameState";

let sync: GameStateSync | undefined;
let spectatorSync: SpectatorSync | undefined;

// activeState is the GameState that scenes shared between player and
// spectator (currently just GameHUD) should read from. Set by the boot
// flow before launching shared scenes; cleared on reset.
let activeState: GameState | undefined;

export function getSync(): GameStateSync {
  if (!sync) sync = new GameStateSync();
  return sync;
}

export function resetSync(): void {
  sync?.close();
  sync = undefined;
}

export function getSpectatorSync(): SpectatorSync {
  if (!spectatorSync) spectatorSync = new SpectatorSync();
  return spectatorSync;
}

export function resetSpectatorSync(): void {
  spectatorSync?.close();
  spectatorSync = undefined;
}

export function setActiveState(state: GameState): void {
  activeState = state;
}

export function getActiveState(): GameState {
  if (!activeState) {
    // Fall back to the player sync's state so existing player flows that
    // don't know about setActiveState still work. Spectator flows must
    // call setActiveState explicitly because their state belongs to
    // SpectatorSync, not GameStateSync.
    return getSync().state;
  }
  return activeState;
}
