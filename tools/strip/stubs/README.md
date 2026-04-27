# DEFCON Game — Published Source

This tarball is the redacted, comment-free source served from the live
lobby at `/source.tar.gz`. It exists so you can study the runtime that
drives the deployed game and find the intentional bugs hidden in it.

The tree is intended for **reading and local experimentation**, not as a
drop-in replacement for the production deployment. A few things have
been deliberately stripped or stubbed; see "What is and isn't here"
below.

## Quickstart

Prerequisites:

- Go 1.25 or newer
- (optional, for the client) Node 18+ and Yarn

### Run the game server

```sh
cd server
go run ./cmd/server
```

Defaults: listens on `:8080`, `AUTH_MODE=open` (no auth — any
`player_id` works), uses an in-memory identity store from the stub.
Override via env vars:

```sh
LISTEN_ADDR=":9000" AUTH_MODE=identity go run ./cmd/server
```

WebSocket endpoint: `ws://localhost:8080/ws?player_id=<any-hex-id>`.
Spectator endpoint: `ws://localhost:8080/ws/spectate?player_id=<id>`.

### Build (don't run) the client

```sh
cd client
yarn install
npx vite build
```

The included `package.json` references a Vite config that lives outside
the published tree (build infra, not gameplay). `npx vite build` with no
config falls back to Vite defaults and is enough to get a static bundle
out for inspection. To play against your local server, point the
deployed client's WebSocket URL at it via your browser's devtools, or
host the built `dist/` behind any static server.

## What is and isn't here

Included:

- `server/` — the entire game-server runtime, including the file that
  intentionally races on `KillLog`. This is the puzzle.
- `client/src`, `client/index.html`, `client/spectate.html`,
  `client/public/`, `client/style.css`, `client/package.json` — the
  Phaser app source the browser already ships you.
- `pkg/identity/stub.go` — a no-auth stub that lets `server/` compile.
  See below.

Not included:

- `lobby/` — the lobby HTTP service (queue, leaderboard, source serving,
  admin panel). Not part of the in-game puzzle.
- The real `pkg/identity` — replaced by a stub. The stub treats every
  `player_id` as a registered player whose `display_name` equals the
  `player_id`, performs no password check, and prints a `STUB IDENTITY`
  warning at startup. **Do not deploy.**
- `tools/`, `docs/`, `.github/`, `*.Dockerfile`, `docker-compose.yml`,
  `client/Caddyfile`, `client/vite/*` — build, deploy, and CI plumbing.
- All comments (`//`, `/* */`), doc strings, and `_test.go` /
  `*.test.ts` files. Build-tag comments are also stripped.

## Why the stub

Identity is shared infrastructure — login, rate limiting, the
account-to-display-name mapping that the leaderboard trusts. Publishing
the real implementation would let any player attack other players'
accounts or the leaderboard ledger, which isn't the puzzle anyone signed
up for. The HTTP API surface (`POST /api/register`, `/api/login`,
`/api/verify`) is plenty if you want to script registration against the
live server.

## Found a bug? Keep it.

The game is intentionally buggy. Protocol quirks, off-by-ones, races,
unchecked state — every weird thing you find in here is in scope, and
exploiting them is how you climb the leaderboard. Don't report ordinary
bugs to us; they're the puzzle.

The two things we *do* want to hear about:

- **Infra problems** — the site is down, you can't register, the
  leaderboard won't load, your run dropped because the server crashed.
- **Unintended game-breakers** — an instant-solve that skips the run
  entirely, or anything that breaks the leaderboard for everyone else.

If you hit one of those, ping an organizer through whatever channel was
announced for the event. Include the file path and line number from
this tarball — line numbers won't match the upstream repo, so quoting
the stripped copy makes triage easier.
