import { GameStateSync } from "./GameStateSync";

let sync: GameStateSync | undefined;

export function getSync(): GameStateSync {
  if (!sync) sync = new GameStateSync();
  return sync;
}

export function resetSync(): void {
  sync?.close();
  sync = undefined;
}
