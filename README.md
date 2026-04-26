# NUS Greyhats DEFCON Dungeon Speedrun

A single-player dungeon-crawler speedrun with a shared leaderboard. Players clear 11 rooms, kill the boss, and post their time. The real competition is exploiting protocol-level bugs to shave seconds off a run — every intentional bug is a load-bearing design requirement, not a mistake.

- **Players:** http://how-to-play in [`docs/how-to-play.md`](docs/how-to-play.md).
- **Designers / implementers:** the spec lives in [`docs/specs/`](docs/specs/README.md). Start there.
- **Bug index:** [`docs/specs/bugs.md`](docs/specs/bugs.md).

## Stack

| Service       | Tech                                  | Default published port |
| ------------- | ------------------------------------- | ---------------------- |
| `client`      | Caddy + Phaser 3 / TypeScript bundle  | host `:80` → `:3000`   |
| `lobby`       | Go (identity, leaderboard, queue, admin) | `127.0.0.1:3001`     |
| `game-server` | Go (WebSocket game loop)              | `127.0.0.1:8080`       |

The browser only sees `client` on port 80 — Caddy reverse-proxies `/api/*`, `/admin/*`, `/register`, `/leaderboard`, `/spectate`, `/source.tar.gz` to the lobby and `/ws`, `/ws/spectate` to the game server. See [`docs/specs/foundations/01-architecture.md`](docs/specs/foundations/01-architecture.md) for the full topology.

## Quick start (local dev)

```sh
docker compose up -d --build
open http://localhost/
```

`docker compose down` to stop. Default config is the safest possible: `AUTH_MODE=identity` (anyone registered can play), `MAX_CONCURRENT=64`, queue + admin disabled.

## Deployment modes

### 1. Dev — single user / playtest

The repo's checked-in defaults. Set nothing, just `docker compose up -d --build`. Everything works except the queue and admin panel (both return `503` deliberately).

### 2. Test — per-branch worktrees on a shared host

We run multiple feature branches on the same machine. Each gets its own compose project + port-override file under `/tmp/<branch>-port.yml`, e.g.:

```yaml
# /tmp/feat-lobby-port.yml
services:
  game-server:
    ports: !override
      - "127.0.0.1:8092:8080"
  lobby:
    ports: !override
      - "127.0.0.1:3092:3001"
  client:
    ports: !override
      - "8002:3000"
```

```sh
docker compose -p feat-lobby \
  -f docker-compose.yml \
  -f /tmp/feat-lobby-port.yml \
  up -d --build
```

Existing assignments to avoid clashing:

| Project              | Public client | Lobby (loopback) | Game server (loopback) |
| -------------------- | ------------- | ---------------- | ---------------------- |
| `defcon-splash`      | `:80`         | `:3001`          | `:8080`                |
| `fix-mob-movement`   | `:8000`       | `:3091`          | `:8090`                |
| `hold-to-shoot`      | `:8001`       | `:3081`          | `:8081`                |
| `feat-lobby` (this)  | `:8002`       | `:3092`          | `:8092`                |

Tear down with `docker compose -p <project> down`.

### 3. Prod cutover — FIFO queue + Telegram admin

Set the env vars below in your shell (or a sourced `.envrc`) before `docker compose up`. The compose file uses `${VAR:-default}` interpolation so anything left unset stays at the dev default.

```sh
# Game server: gate WS auth on the allowlist.
export AUTH_MODE=token
export MAX_CONCURRENT=4

# Lobby: turn on the queue + Telegram-OTP admin.
export QUEUE_ENABLED=true
export TELEGRAM_BOT_TOKEN='<bot token from @BotFather>'
export TELEGRAM_ADMIN_CHAT_ID='<numeric chat id, e.g. -100123456789 for groups>'
export ADMIN_SESSION_SECRET='<≥16 random bytes; rotate to invalidate sessions>'

docker compose up -d --build
```

What you get:

- `/api/queue/{join,status,leave}` is live; the lobby's advancer ticks every 2 s, pops the queue head when a slot frees, and rewrites `/data/allowlist.json`.
- The game server rejects WS `C2S_AUTH` for any `player_id` not in the allowlist. See [`docs/specs/foundations/03-identity-auth.md`](docs/specs/foundations/03-identity-auth.md).
- `/admin/login` renders the Telegram-OTP login form; `/admin/*` is gated by an HMAC-signed session cookie. See [`docs/specs/foundations/04-lobby.md`](docs/specs/foundations/04-lobby.md#admin-auth-telegram-otp).

To verify after `up -d`:

```sh
docker logs <project>-lobby-1 | tail -1
# expected: ... admin=on (telegram chat=...) queue=on (db=/data/lobby.db allowlist=/data/allowlist.json)
```

## Environment variables

### `game-server`

| Var                | Default                | Purpose                                                                  |
| ------------------ | ---------------------- | ------------------------------------------------------------------------ |
| `LISTEN_ADDR`      | `:8080`                | Bind addr.                                                               |
| `AUTH_MODE`        | `identity`             | `open` / `identity` / `token`. `token` requires the queue + allowlist.   |
| `MAX_CONCURRENT`   | `64`                   | WS slot semaphore on the game server (absolute backstop). Set to `4` for prod. The queue admission cap lives on the lobby (`/admin/` → Concurrency cap, default seed `4`); keep it `≤` this env value or admitted players will hit `server full`. |
| `GAME_DB`          | `/data/game.db`        | SQLite path for completed runs (writer).                                 |
| `IDENTITY_DB`      | `/data/identity.db`    | SQLite path for `players` (read-only on this side).                      |
| `ALLOWLIST_PATH`   | `/data/allowlist.json` | File watched under `AUTH_MODE=token`. Read-only here; lobby writes it.   |

### `lobby`

| Var                       | Default                | Purpose                                                                  |
| ------------------------- | ---------------------- | ------------------------------------------------------------------------ |
| `LISTEN_ADDR`             | `:3001`                | Bind addr.                                                               |
| `IDENTITY_DB`             | `/data/identity.db`    | Shared with the game server (WAL).                                       |
| `GAME_DB`                 | `/data/game.db`        | Shared (WAL); read-only here.                                            |
| `LOBBY_DB`                | `/data/lobby.db`       | Lobby-owned: queue + admin OTP state.                                    |
| `ALLOWLIST_PATH`          | `/data/allowlist.json` | Lobby writes; game server reads.                                         |
| `GAME_SERVER_URL`         | `http://game-server:8080` | Used for `/internal/slot-status` and `/internal/live-games` polls.    |
| `SOURCE_TARBALL`          | `/app/source.tar.gz`   | Stripped source baked into the image at build time.                      |
| `QUEUE_ENABLED`           | `false`                | Turns on `/api/queue/*` + the slot advancer + allowlist writer.          |
| `TELEGRAM_BOT_TOKEN`      | _(empty)_              | From [@BotFather](https://t.me/BotFather). Empty disables `/admin/*` (503). |
| `TELEGRAM_ADMIN_CHAT_ID`  | _(empty)_              | Where OTPs are sent. Group or single user.                               |
| `ADMIN_SESSION_SECRET`    | _(empty)_              | HMAC key for admin session cookies; ≥ 16 raw bytes. Rotate to log everyone out. |

## Telegram bot setup (one-time)

1. **Create a bot.** DM [@BotFather](https://t.me/BotFather), `/newbot`, follow prompts. Save the token (e.g. `123456:ABC-DEF...`).
2. **Pick or create the admin chat.** Either DM the bot directly, or create a group and add the bot as a member. The bot does not need admin rights.
3. **Find the chat ID.** Send any message in the chat, then:
   ```sh
   curl "https://api.telegram.org/bot<TOKEN>/getUpdates" | jq '.result[].message.chat'
   ```
   Use the numeric `id` (negative for groups, positive for DMs).
4. **Set env vars** as in the prod cutover example above. The bot will post `VIBECAT 404 admin login code: 123456` to the chat whenever a moderator hits `/admin/login`.
5. **Rotate** by setting a new `ADMIN_SESSION_SECRET` and restarting — every issued session cookie is invalidated immediately. Or revoke the bot via BotFather to disable login while leaving cookies alive until expiry.

## Useful endpoints

Replace `:80` with whatever port your project is published on.

| URL                            | What                                                          |
| ------------------------------ | ------------------------------------------------------------- |
| `/`                            | Game (login → admission → play).                              |
| `/register`                    | Player registration.                                          |
| `/leaderboard`                 | Public leaderboard.                                           |
| `/spectate`                    | Live-games selector.                                          |
| `/source.tar.gz`               | Stripped game-server source download.                         |
| `/admin/login`                 | Telegram-OTP admin login.                                     |
| `/admin/`                      | Cookie-gated moderation panel.                                |
| `/api/queue/join` (POST)       | `{player_id, password}` → `{position, est_wait_ms, admitted}` |
| `/api/queue/status/:id` (GET)  | Queue status. `admitted=true && position=0` ⇒ open the WS.    |
| `/api/leaderboard` (GET)       | JSON rankings.                                                |
| `/api/live` (GET)              | JSON list of live runs.                                       |
| `/api/health` (GET)            | Lobby health probe.                                           |

## Repo layout

```
client/                Phaser 3 + Vite frontend served by Caddy
lobby/                 Go: identity, leaderboard, queue, admin, source serving
server/                Go: WebSocket game loop, leaderboard writer, spectator fan-out
pkg/identity/          Shared Go package: player + password storage (used by both binaries)
docs/specs/            The actual spec — start at docs/specs/README.md
docs/how-to-play.md    Player-facing guide
docker-compose.yml     Two-service stack (game-server + lobby) plus Caddy client
art/                   Source PSDs and reference assets (see art/README.md)
```

## Contributing

Read [`docs/specs/README.md`](docs/specs/README.md) — it has the recommended reading order, the "how to add a feature" recipe, and the rule that intentional bugs (`BUG-XX` markers in code) are load-bearing and should not be fixed without coordination.
