# NUS Greyhats DEFCON Dungeon Speedrun — Specifications Index

A single-player dungeon-crawler speedrun with a shared leaderboard. Players clear rooms of enemies, traverse doors to new single-screen maps, and post completion times. The **real competition** is exploiting protocol-level bugs to shave seconds off a run. Each intentional bug is a small, bounded advantage — stacking them is the path to #1.

**Format:** Individual speedrun + exploitation
**Stack:** Go backend, Phaser 3 frontend, WebSocket transport

> **Looking to run it?** [`README.md`](README.md) has quick-start, the env-var reference, and the dev / test / prod-cutover deployment recipes. This file is the spec index for designers and implementers.

## The real spec lives in [`docs/specs/`](docs/specs/README.md)

This file is a thin index. Start at [`docs/specs/README.md`](docs/specs/README.md) for orientation and reading order. Individual components are broken down into small, topic-focused files so a human or agent can implement one piece without paging through the whole design.

## Top-level table of contents

| File                                                                                 | One-line summary                                                   |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| [docs/specs/README.md](docs/specs/README.md)                                                   | Orientation, reading order, "how to add a feature" recipe          |
| [docs/specs/bugs.md](docs/specs/bugs.md)                                                       | Central index of all 11 intentional bugs + stacking / synergy maps |
| [docs/specs/foundations/01-architecture.md](docs/specs/foundations/01-architecture.md)         | Two-service topology, prod deployment                              |
| [docs/specs/foundations/02-protocol.md](docs/specs/foundations/02-protocol.md)                 | WebSocket envelope + packet registry + BUG-09 parser half          |
| [docs/specs/foundations/03-identity-auth.md](docs/specs/foundations/03-identity-auth.md)       | player_id, bcrypt, AES, token allowlist                            |
| [docs/specs/foundations/04-lobby.md](docs/specs/foundations/04-lobby.md)       | Lobby service, slot polling, allowlist writes                |
| [docs/specs/foundations/05-server-runtime.md](docs/specs/foundations/05-server-runtime.md)     | Goroutines, tick loop, BUG-02 / BUG-06 / BUG-11                    |
| [docs/specs/foundations/06-game-state-conventions.md](docs/specs/foundations/06-game-state-conventions.md) | State blocks + ownership + bug-critical type choices   |
| [docs/specs/foundations/07-determinism-seeding.md](docs/specs/foundations/07-determinism-seeding.md) | MobRNG + consumption invariant replay relies on            |
| [docs/specs/foundations/08-leaderboard-replay.md](docs/specs/foundations/08-leaderboard-replay.md) | CompletedRun, checksum, replay engine + streaming `/ws/replay` |
| [docs/specs/foundations/09-ci-cd-deploy.md](docs/specs/foundations/09-ci-cd-deploy.md)         | Pipelines, source stripping, build tags that ship BUG-11           |
| [docs/specs/foundations/10-client-architecture.md](docs/specs/foundations/10-client-architecture.md) | Phaser scenes, net sync, packet→scene mapping                |
| [docs/specs/features/combat-weapons.md](docs/specs/features/combat-weapons.md)                 | Damage pipeline, HP, cooldowns (BUG-04, BUG-07, BUG-08)            |
| [docs/specs/features/mob-ai.md](docs/specs/features/mob-ai.md)                                 | Spawn, patrol, aggro, BUG-01 seed optimization                     |
| [docs/specs/features/rooms-doors.md](docs/specs/features/rooms-doors.md)                       | Room traversal, door state machine (BUG-03)                        |
| [docs/specs/features/pickups.md](docs/specs/features/pickups.md)                               | Pickups + interact range (BUG-05)                                  |
| [docs/specs/features/inventory-items.md](docs/specs/features/inventory-items.md)               | Inventory, item parsing (BUG-10, BUG-09 dispatch half)             |
| [docs/specs/client/11-replay-viewer.md](docs/specs/client/11-replay-viewer.md)                 | Replay viewer client (third Phaser bundle, plays back stored runs) |

## Open questions

Tracked across sub-specs but collected here for visibility.

- Exact room count beyond 10, map art, tileset finalization.
- Boss mechanics, phases, and debuff system (needed for BUG-08).
- Full weapon stats table (reload timings, bullet speeds, DPS).
- Item and inventory details beyond the skeleton (needed for BUG-09, BUG-10).
- Canonical entity serialization format for map files.
- Critical-hit formula (`X` and per-weapon variance).
- Death penalty: checkpoint or full restart?
- Monorepo vs two separate repos (spec targets two; current repo is mono).
- Leaderboard webapp stack: React, vanilla JS, Go templates?
- Replay viewer: embedded Phaser canvas or separate page?
- VPS provider for dev/test.
- `go/ast` stripping tool — custom or existing?
- Queue notification: SSE, WebSocket from lobby, or client polls?
- Admin panel auth: basic auth? IP whitelist?
- Are 11 bugs too many? Should we cut to 8–9 for scope?
- BUG-11 non-determinism — is that fun or frustrating?
- Replay tolerance window for non-deterministic bugs.
- Should the debug items (BUG-09) be discoverable only through source-code reading, or also hinted in-game?
- Display name uniqueness — enforce or allow duplicates?
