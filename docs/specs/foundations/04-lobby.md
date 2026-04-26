# 04 — Lobby Service

## Purpose

Separate service (Repo B) that manages FIFO turn-taking in production, serves the public leaderboard + queue webapp, **hosts the live-spectating selector and viewer**, **serves the stripped game-server source tree to players**, owns player registration, and drives the `allowlist.json` that gates the game server's WebSocket under `AUTH_MODE=token`.

## Public endpoints

| Endpoint                       | Method | Purpose                                                 |
| ------------------------------ | ------ | ------------------------------------------------------- |
| `/api/register`                | POST   | `{ display_name, contact, password }` → `{ player_id }` |
| `/api/queue/join`              | POST   | `{ player_id, password }` → `{ position, est_wait_ms }` |
| `/api/queue/status/:player_id` | GET    | Current position                                        |
| `/api/queue/:player_id`        | DELETE | `{ password }` — leave queue                            |
| `/api/leaderboard`             | GET    | Rankings from `game.db`                                 |
| `/api/live`                    | GET    | JSON list of currently-playing games. See [Live spectating](#live-spectating). |
| `/spectate`                    | GET    | HTML selector page: lists live games, links into viewer. |
| `/spectate/:player_id`         | GET    | HTML viewer page that hosts the client in spectator mode. |
| `/source/*`                    | GET    | Static file serving of the stripped game-server source tree under `/data/stripped-source/`. Read-only; no traversal outside the tree. See [09-ci-cd-deploy.md](./09-ci-cd-deploy.md#source-code-publishing-served-by-lobby). |
| `/api/health`                  | GET    | Service health                                          |

See [03-identity-auth.md](./03-identity-auth.md) for the registration / verification semantics shared with this service.

## Admin panel (auth-protected)

- `/admin/queue` — reorder, kick, advance, pause.
- `/admin/leaderboard` — flag, remove entries.
- `/admin/replay/:replay_id` — download replay JSON (input log + seed). Moderators-only; not exposed publicly so input logs can't be data-mined for the intentional bugs.
- `/admin/winners` — decrypt + export contact info.
- `/admin/slot` — force-free slot, adjust `MAX_CONCURRENT`.

Admin auth: basic auth or IP whitelist (TBD — see open questions in [../../../SPECIFICATIONS.md](../../../SPECIFICATIONS.md)).

## Queue ↔ game server handoff

Lobby polls `GET /internal/slot-status` on the game server every 2 s. When a slot is free **and** a player is at the front of the queue, lobby writes that `player_id` into the shared `allowlist.json`. The game server reads `allowlist.json` when a new WebSocket connection sends `C2S_AUTH` under `AUTH_MODE=token`.

## Queue rules

| Rule                  | Detail                            |
| --------------------- | --------------------------------- |
| **TTL per turn**      | 5 min. Must start run within 60 s.|
| **Max runs per turn** | 3 attempts. Best time kept.       |
| **Re-queue**          | After turn → back of queue.       |
| **AFK kick**          | No run within 60 s → forfeit.     |
| **Disconnect grace**  | 15 s before slot freed.           |

## Live spectating

The lobby is the public front door for spectators. Under production `MAX_CONCURRENT=4`, up to four runs can be in progress at once, so the lobby exposes a selector (`/spectate`) that lists the live games and hands the viewer off to a read-only WebSocket served by the game server.

### Selector

- `/spectate` — HTML page. Fetches `/api/live` on load (and every 2 s while open) and renders a tile per live game showing `display_name`, `room_index`, `elapsed_ms`, and `spectator_count`. Clicking a tile navigates to `/spectate/:player_id`.
- `/api/live` — JSON. Entries are freshly-cached from the game server (see below).
  ```jsonc
  [
    {
      "player_id": "a3f9…",        // hex; matches what the client will pass on the spectator WS
      "display_name": "aluminum-fox",
      "room_index": 3,
      "elapsed_ms": 42718,
      "spectator_count": 7,
      "started_at": 1745000000000  // server wallclock ms; clients can render "started 42s ago"
    }
  ]
  ```
  Ordering: by `elapsed_ms` ascending (newest runs first). Stale up to ~2 s (cache window).

### Viewer

- `/spectate/:player_id` — HTML page that hosts the same Phaser client in **spectator mode** (see [../client/09-spectator-mode.md](../client/09-spectator-mode.md)). The client opens a WebSocket to `/ws/spectate?player_id=<id>` on the same public origin. The reverse proxy routes `/ws/spectate` → game server `:8080`, identically to `/ws`.
- The spectator WS is served by the **game server**, not the lobby — the game server owns the live state and already writes `S2C_STATE` on its tick. Fan-out is a new outCh in the player's gameLoop. See [05-server-runtime.md#spectator-fan-out](./05-server-runtime.md#spectator-fan-out) and [02-protocol.md#spectator-protocol](./02-protocol.md#spectator-protocol).

### Data flow

- Lobby polls `GET /internal/live-games` on the game server every 2 s. May be combined with `/internal/slot-status` into one call; both are loopback-only / shared-secret-gated.
- Response is cached in-process (2 s TTL) and served verbatim to `/api/live`. No auth on `/api/live` — this is intentionally public.
- `display_name` is included — we're not pseudonymizing. Privacy posture: by entering the queue a player consents to their display name being visible to spectators during their run and on the leaderboard after.
- Viewer WS → game server directly, using the `player_id` from the URL. No C2S packets are permitted from a spectator connection; the server closes the WS on any client frame. See [02-protocol.md#spectator-protocol](./02-protocol.md#spectator-protocol).

### Caps and abuse resistance

Spectating is public-internet exposed, so the game server enforces:

| Cap                          | Default | Rationale                                               |
| ---------------------------- | ------- | ------------------------------------------------------- |
| Spectators per game          | 50      | Bounds outbound bandwidth per gameLoop. Drop-on-full at outCh (no back-pressure to game logic). |
| Global spectator connections | 200     | Hard ceiling across all 4 slots.                        |
| New spectator connects / IP  | 5 / min | Reverse proxy rate limit. Prevents connect-storm DoS.   |
| Spectator WS idle timeout    | 60 s    | Close spectators that haven't sent a pong in 60 s. No app-level pings from spectators; rely on WebSocket control frames. |

When a cap is hit, the WS upgrade returns `503` with `Retry-After`. Caps are configurable via env (`SPECTATOR_PER_GAME`, `SPECTATOR_GLOBAL`).

### What spectators see

Spectators receive a near-identical packet stream to the player — enough to render the run faithfully — **minus any packet that's authentication-scoped**. Full list in [02-protocol.md#spectator-protocol](./02-protocol.md#spectator-protocol). Spectators do see intentional-bug artifacts (e.g. BUG-11 kill-log garbage, BUG-02 drift) as they happen, which is the point: part of the show is watching a run get exploited.

### Selector ↔ run lifecycle

- **Run starts:** player's `S2C_RUN_STARTED` fires → gameLoop registers itself in the spectator registry → next `/internal/live-games` poll surfaces it to the selector.
- **Run ends or player disconnects:** gameLoop sends `S2C_RUN_COMPLETE` (or an error) to all attached spectators, then closes each spectator WS. The selector removes the tile on the next poll.
- **Spectator disconnects:** gameLoop decrements `spectator_count` via a deregister channel; no impact on the player.

## Data flow

- **Join:** `POST /api/queue/join { player_id, password }` → bcrypt verify → append to queue (FIFO in `lobby.db`).
- **Advance:** every 2 s tick — poll `/internal/slot-status`. If free + queue non-empty: pop head → write to `allowlist.json` → notify front-of-queue client.
- **Play:** player's WS connects to game server; game server reads `allowlist.json` to validate `player_id`.
- **Spectate:** spectator loads `/spectate`, picks a live game, viewer opens `/ws/spectate?player_id=...` to the game server.
- **Leaderboard display:** webapp reads `game.db` (read-only) to render rankings.

`lobby.db` is owned by this service. `game.db` is read-only from here (it's owned by the game server). `allowlist.json` is written here, read by game server. `/data/stripped-source/` is populated by CI/CD and served read-only from here.

## Implementation notes

- Package layout (target): `cmd/admin/`, `internal/queue/`, `internal/identity/`, `internal/admin/`, `web/`.
- Queue storage: SQLite `lobby.db`, single `queue_entries` table: `(player_id, joined_at, position)`.
- Notification to waiting clients: TBD — SSE, WebSocket, or short polling (see open questions).
- `/internal/slot-status` and `/internal/live-games` are game-server endpoints the lobby polls; neither is exposed on the public interface (bind to loopback or require shared secret).
- The live-games cache (2 s TTL) lives in the lobby process, not on disk. If the game server is down, `/api/live` returns `[]` rather than erroring.

## Implementation status

Lobby split rolling out in five phases under epic #26:

- **Phase 1 (#21, merged):** `lobby/` service stood up; identity HTTP serving (`/api/register`, `/api/login`, `/api/verify`, `/api/health`, `/register`) extracted from the monolith. Caddy routes identity endpoints to lobby.
- **Phase 2 (#22, merged):** public leaderboard + replay download. Lobby owns `/api/leaderboard`, `/api/replay/:replay_id`, and the `/leaderboard` HTML page; reads `game.db` over WAL alongside the game-server writer. Replay download verifies the stored checksum on read.
- **Phase 3 (#23, in flight):** live spectating. Lobby owns `/api/live` and the `/spectate` selector page; `/spectate/:player_id` viewer URLs serve a static spectator client bundle from the client container. Game server adds `/internal/live-games` (loopback) and `/ws/spectate` upgrade with caps + per-IP rate limit + read-only enforcement; per-engine spectator fan-out with non-blocking drop-on-full + catchup snapshot on register.
- **Phase 4 (#24, not started):** source serving + non-queue admin (`/source/*`, `/admin/leaderboard`, `/admin/winners`).
- **Phase 5 (#25, not started):** FIFO queue + slot handoff via `allowlist.json`. Only required when `MAX_CONCURRENT=4` for prod.

The two-repo target in [01-architecture.md](./01-architecture.md) was relaxed to a single repo with three modules (`server/`, `lobby/`, `pkg/`) coordinated via `go.work` + `replace` directives. Both binaries open the same `/data/identity.db` (SQLite WAL).

## Intentional bugs in this area

None — queue semantics must remain correct. The lobby is also the host of the public leaderboard webapp; bugs here affect trust in results.

## Cross-references

- [01-architecture.md](./01-architecture.md) — two-service topology, deployment.
- [03-identity-auth.md](./03-identity-auth.md) — registration and verification.
- [08-leaderboard-replay.md](./08-leaderboard-replay.md) — `game.db` read path.
- [09-ci-cd-deploy.md](./09-ci-cd-deploy.md) — env matrix, prod deployment.
