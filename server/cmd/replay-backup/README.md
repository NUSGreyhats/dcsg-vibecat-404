# replay-backup

Localhost-only operator tool for watching a single recorded run via the
fixed replay engine, without touching the production game server.

Use when:

- You need to verify a recording reproduces correctly under the PR #96
  fix on real prod data, _before_ redeploying the prod gameserver.
- The prod gameserver is buggy/unavailable but you have a JSON download
  of a run from the lobby admin endpoint.

## Build & run

The replay client is a Vite-built static bundle. Build it once:

```sh
cd ../../../client
npm install
npm run build      # produces client/dist/{replay.html, assets/, ...}
```

Then build and run the backup binary (lives inside the server module so
it can import `server/internal/replay` directly — that's how it
inherits the PR #96 inCh-sizing fix):

```sh
cd ../server/cmd/replay-backup
go build -o replay-backup .
./replay-backup -addr=127.0.0.1:8080 -client-dist=../../../client/dist
```

Or from the repo root: `go build -o /tmp/replay-backup ./server/cmd/replay-backup`.

Flags:

- `-addr` (default `127.0.0.1:8080`) — listen address. Defaults to
  loopback only; pass `0.0.0.0:8080` to expose on the LAN if needed.
- `-client-dist` (default `./dist`) — path to the directory holding
  `replay.html` and `assets/`.

## Usage

1. Download a run from the lobby admin endpoint:

   ```sh
   curl -b admin_session=<session-cookie> \
        https://<lobby-host>/admin/replay/<replay-id> > run.json
   ```

   The downloaded file is a JSON object with shape
   `{replay_id, player_id, display_name, total_ms, seed, input_log}`.
   `input_log` is base64-encoded JSON-marshaled `[]game.InputRecord`.
   Both encodings are handled by the upload endpoint — no manual
   transform required.

2. Open `http://localhost:8080`, upload `run.json`. You'll be
   redirected to `/admin/replay/<id>/view` and the replay starts.

## What this tool does NOT do

- No persistence: the in-memory store is cleared on restart.
- No admin auth: the binary is intended for localhost. The HMAC ticket
  scheme is preserved (so the existing Phaser client works unmodified)
  but the secret is generated fresh per process and never persisted.
- No splits in the end-screen: the JSON download omits `splits_json`,
  so the viewer end-screen shows an empty splits table. The replayed
  run still produces correct per-room timing internally.
- No leaderboard, no spectator, no live game. Replay only.
- No caps / per-IP rate limit / metrics. Single operator assumed.

## What it shares with prod

The WS path imports `server/internal/replay`, so the PR #96
inCh-sizing fix is automatically inherited. The protocol is
byte-identical to prod's `/ws/replay`, so the existing Phaser client
works against this binary without modification.

## Smoke test

End-to-end check that the fix works on a known-bursty run:

1. `npm run build` in `client/`.
2. Run `replay-backup` with `-client-dist=../../client/dist`.
3. Upload a run that previously desynced (player got stuck on a wall
   after a fast door transition).
4. Confirm the room transitions cleanly in the viewer.
5. Tail the binary's stderr — there should be no
   `replay: inCh full at tick X, dropping record` lines.
