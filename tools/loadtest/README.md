# loadtest

Synthetic-client driver for the DEFCON game server. Opens N concurrent
`/ws` connections, drives each through the full `C2S_AUTH` →
`C2S_START_RUN` → `C2S_INPUT` loop, and optionally attaches `/ws/spectate`
viewers to stress fan-out.

The point is to find the actual ceiling: where does the engine miss its
50 ms tick budget, and which resource (CPU, bandwidth, FD count, semaphore)
binds first. There is no benchmark in the repo otherwise — any ceiling
quoted without one is a guess.

## Quick start

```sh
# 1. Build/run the server in open auth mode (any player_id accepted).
docker compose run --rm --service-ports \
  -e AUTH_MODE=open \
  -e MAX_CONCURRENT=128 \
  -e SPECTATOR_PER_IP=200 \
  game-server

# 2. From another shell, run the load tester.
cd tools/loadtest
go run . -players 32 -spectators-per-game 4 -duration 60s -ramp 10s
```

Output ends with a one-shot summary including connect counts, message
rates, byte throughput, and the inter-`S2C_STATE` gap percentiles per
role (the server's tick budget is 50 ms — anything materially above that
means the engine is falling behind).

## Flags

| flag | default | meaning |
| --- | --- | --- |
| `-server` | `ws://127.0.0.1:8080` | WS base URL (no trailing path) |
| `-players` | `10` | concurrent synthetic players |
| `-spectators-per-game` | `0` | extra `/ws/spectate` viewers per player. Each viewer polls `/api/live` to discover a public `run_id` then attaches; no 1:1 mapping to a specific player (issue #71 #1). |
| `-ramp` | `5s` | linear ramp window for player connect launches |
| `-duration` | `30s` | total test duration; `SIGINT` also stops |
| `-input-hz` | `10` | `C2S_INPUT` rate per player |
| `-no-shoot` | `false` | suppress randomized `C2S_SHOOT` packets |
| `-id-prefix` | `loadtest-` | prefix for synthetic player_ids |
| `-report-interval` | `5s` | periodic stats line cadence (`0` disables) |
| `-spectator-lag` | `2s` | delay before each spectator polls `/api/live` for a run_id |

## Auth mode caveat

The server's `AUTH_MODE` controls what `player_id` values are accepted at
the WS handshake. The load tester generates random hex IDs, so:

- `AUTH_MODE=open` (the only realistic load-test posture) — accepts any
  ID. Use this.
- `AUTH_MODE=token` — only IDs in `/data/allowlist.json` are accepted.
  Every synthetic ID will be rejected unless you pre-seed the allowlist.
- `AUTH_MODE=identity` — only registered players (rows in
  `identity.db`). Same problem.

If you see all connections failing with the rejection code
`s2c_error:auth_failed`, the server is not in open mode.

## Rate-limit caveats

Defaults that will bite a single-host load test long before any
performance ceiling does:

- `SPECTATOR_PER_IP=5` and `SPECTATOR_GLOBAL=200` cap spectator accept
  rate. Raise both for fan-out tests from one host.
- The lobby's queue (`QUEUE_ENABLED=true`) does not gate `/ws` directly,
  but admin-set `max_concurrent` does. With the default of `4` the
  loadtester will see most connects rejected.

## What it measures

- **Connect outcomes**: attempted, ok, failed (split by HTTP-reject code,
  dial error, auth failure).
- **Server tick health**: distribution of inter-arrival gaps between
  consecutive `S2C_STATE` packets per role. The engine ticks at 20 Hz, so
  steady-state p50 should be ~50 ms; tail growth is the first symptom of
  CPU saturation.
- **Bandwidth**: bytes received (player + spectator).
- **Errors**: count and codes of received `S2C_ERROR` packets and
  unexpected disconnects.

What it does **not** do: real game play (the synthetic motion is a slow
circular wander — enough to exercise input dispatch and state
broadcast, not enough to actually clear rooms), input log replay
correctness, or graceful client reconnects.

## Reading the output

```
[18:55:53] conns=9/12 state=2182 (180/s) bytes=0.1MB/s pGap p95=50.7ms sGap p95=50.8ms err=0 disc=0
```

- `conns=ok/attempted`
- `state=total (rate over last interval)`
- `pGap p95` — player-WS inter-state-gap p95 (target 50 ms)
- `sGap p95` — spectator-WS inter-state-gap p95 (target 50 ms)
- `err` — `S2C_ERROR` packets received
- `disc` — connections that closed unexpectedly

If `pGap p95` climbs well above 50 ms while CPU on the server is high,
you've found the CPU ceiling for that workload mix. If it stays at
~50 ms but `disc` grows, the bottleneck is elsewhere (network, FD
exhaustion, etc.).
