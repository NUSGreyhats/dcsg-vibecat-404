import { startGame } from "./game/main";
import { loadConfig } from "./net/config";

// Fetch /api/config before the Phaser game spins up so scenes can
// synchronously branch on feature flags (notably the leaderboard
// toggle on WinScreen). loadConfig() never throws — on failure it
// keeps the safe default {leaderboard_enabled:true, queue_enabled:false}.
loadConfig().finally(() => {
  startGame("game-container");
});

const app = document.getElementById("app");
const fullscreenToggle = document.getElementById("fullscreen-toggle");

fullscreenToggle?.addEventListener("click", () => {
  if (document.fullscreenElement) {
    document.exitFullscreen();
  } else {
    app?.requestFullscreen();
  }
});

document.addEventListener("fullscreenchange", () => {
  if (!fullscreenToggle) return;
  const isFullscreen = Boolean(document.fullscreenElement);
  fullscreenToggle.setAttribute(
    "aria-label",
    isFullscreen ? "Exit fullscreen" : "Enter fullscreen"
  );
});
