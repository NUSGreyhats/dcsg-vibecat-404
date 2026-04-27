import { startReplay } from "./game/replay-main";

// URL shape: /admin/replay/:replay_id/view  (the /view suffix is what
// Caddy rewrites to /replay.html). The replay_id is a UUID.
function parseRoute(): { replayId: string } {
  const path = window.location.pathname;
  const m = path.match(/^\/admin\/replay\/([0-9a-fA-F-]{36})(?:\/view)?\/?$/);
  const replayId = m ? m[1] : "";
  return { replayId };
}

const route = parseRoute();
startReplay({
  parentId: "game-container",
  replayId: route.replayId,
});
