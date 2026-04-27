import { startSpectator } from "./game/spectate-main";

// URL shape: /spectate/:run_id?name=<display_name>. run_id is the public
// per-run identifier minted by the game server in initRun — used in
// place of player_id, which is the WS bearer credential (issue 71 #1).
function parseRoute(): { runId: string; displayName: string } {
  const path = window.location.pathname;
  const m = path.match(/^\/spectate\/([^/?#]+)/);
  const runId = m ? decodeURIComponent(m[1]) : "";
  const params = new URLSearchParams(window.location.search);
  const displayName = params.get("name") ?? "";
  return { runId, displayName };
}

const route = parseRoute();
startSpectator({
  parentId: "game-container",
  runId: route.runId,
  displayName: route.displayName,
});
