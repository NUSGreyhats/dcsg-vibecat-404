# 04 — Lobby Service

## Purpose

Separate service (Repo B) that manages FIFO turn-taking in production, serves the public leaderboard + queue webapp, **serves the stripped game-server source tree to players**, owns player registration, and drives the `allowlist.json` that gates the game server's WebSocket under `AUTH_MODE=token`.

## Public endpoints

| Endpoint                       | Method | Purpose                                                 |
| ------------------------------ | ------ | ------------------------------------------------------- |
| `/api/register`                | POST   | `{ display_name, contact, password }` → `{ player_id }` |
| `/api/queue/join`              | POST   | `{ player_id, password }` → `{ position, est_wait_ms }` |
| `/api/queue/status/:player_id` | GET    | Current position                                        |
| `/api/queue/:player_id`        | DELETE | `{ password }` — leave queue                            |
| `/api/leaderboard`             | GET    | Rankings from `game.db`                                 |
| `/api/replay/:replay_id`       | GET    | Replay data from `game.db`                              |
| `/source/*`                    | GET    | Static file serving of the stripped game-server source tree under `/data/stripped-source/`. Read-only; no traversal outside the tree. See [09-ci-cd-deploy.md](./09-ci-cd-deploy.md#source-code-publishing-served-by-lobby). |
| `/api/health`                  | GET    | Service health                                          |

See [03-identity-auth.md](./03-identity-auth.md) for the registration / verification semantics shared with this service.

## Admin panel (auth-protected)

- `/admin/queue` — reorder, kick, advance, pause.
- `/admin/leaderboard` — flag, remove entries.
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

## Data flow

- **Join:** `POST /api/queue/join { player_id, password }` → bcrypt verify → append to queue (FIFO in `lobby.db`).
- **Advance:** every 2 s tick — poll `/internal/slot-status`. If free + queue non-empty: pop head → write to `allowlist.json` → notify front-of-queue client.
- **Play:** player's WS connects to game server; game server reads `allowlist.json` to validate `player_id`.
- **Leaderboard display:** webapp reads `game.db` (read-only) to render rankings.

`lobby.db` is owned by this service. `game.db` is read-only from here (it's owned by the game server). `allowlist.json` is written here, read by game server. `/data/stripped-source/` is populated by CI/CD and served read-only from here.

## Implementation notes

- Package layout (target): `cmd/admin/`, `internal/queue/`, `internal/identity/`, `internal/admin/`, `web/`.
- Queue storage: SQLite `lobby.db`, single `queue_entries` table: `(player_id, joined_at, position)`.
- Notification to waiting clients: TBD — SSE, WebSocket, or short polling (see open questions).
- `/internal/slot-status` is game server → lobby only; not exposed on public interface (bind to loopback or require shared secret).

## Implementation status

**Greenfield.** The lobby service does not exist yet — the current repo is monolithic and hosts identity endpoints inside `server/main.go` + `server/identity.go`. Splitting into a separate service is later work.

## Intentional bugs in this area

None — queue semantics must remain correct. The lobby is also the host of the public leaderboard webapp; bugs here affect trust in results.

## Cross-references

- [01-architecture.md](./01-architecture.md) — two-service topology, deployment.
- [03-identity-auth.md](./03-identity-auth.md) — registration and verification.
- [08-leaderboard-replay.md](./08-leaderboard-replay.md) — `game.db` read path.
- [09-ci-cd-deploy.md](./09-ci-cd-deploy.md) — env matrix, prod deployment.
