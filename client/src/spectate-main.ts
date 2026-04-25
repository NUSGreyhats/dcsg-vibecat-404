import { startSpectator } from "./game/spectate-main";

// URL shape: /spectate/:player_id?name=<display_name>
function parseRoute(): { playerId: string; displayName: string } {
  const path = window.location.pathname;
  const m = path.match(/^\/spectate\/([^/?#]+)/);
  const playerId = m ? decodeURIComponent(m[1]) : "";
  const params = new URLSearchParams(window.location.search);
  const displayName = params.get("name") ?? "";
  return { playerId, displayName };
}

const route = parseRoute();
startSpectator({
  parentId: "game-container",
  playerId: route.playerId,
  displayName: route.displayName,
});
