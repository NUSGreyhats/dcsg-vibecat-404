import { startGame } from "./game/main";

startGame("game-container");

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
