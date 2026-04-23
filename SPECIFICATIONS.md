# NUS Greyhats DEFCON Dungeon Speedrun — Specifications Index

A single-player dungeon-crawler speedrun with a shared leaderboard. Players clear rooms of enemies, traverse doors to new single-screen maps, and post completion times. The **real competition** is exploiting protocol-level bugs to shave seconds off a run. Each intentional bug is a small, bounded advantage — stacking them is the path to #1.

**Format:** Individual speedrun + exploitation
**Stack:** Go backend, Phaser 3 frontend, WebSocket transport

## The real spec lives in [`specs/`](specs/README.md)

This file is a thin index. Start at [`specs/README.md`](specs/README.md) for orientation and reading order. Individual components are broken down into small, topic-focused files so a human or agent can implement one piece without paging through the whole design.

## Top-level table of contents

| File                                                                                 | One-line summary                                                   |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| [specs/README.md](specs/README.md)                                                   | Orientation, reading order, "how to add a feature" recipe          |
| [specs/bugs.md](specs/bugs.md)                                                       | Central index of all 11 intentional bugs + stacking / synergy maps |
| [specs/foundations/01-architecture.md](specs/foundations/01-architecture.md)         | Two-service topology, prod deployment                              |
| [specs/foundations/02-protocol.md](specs/foundations/02-protocol.md)                 | WebSocket envelope + packet registry + BUG-09 parser half          |
| [specs/foundations/03-identity-auth.md](specs/foundations/03-identity-auth.md)       | player_id, bcrypt, AES, token allowlist                            |
| [specs/foundations/04-queue-service.md](specs/foundations/04-queue-service.md)       | Queue admin service, slot polling, allowlist writes                |
| [specs/foundations/05-server-runtime.md](specs/foundations/05-server-runtime.md)     | Goroutines, tick loop, BUG-02 / BUG-06 / BUG-11                    |
| [specs/foundations/06-game-state-conventions.md](specs/foundations/06-game-state-conventions.md) | State blocks + ownership + bug-critical type choices   |
| [specs/foundations/07-determinism-seeding.md](specs/foundations/07-determinism-seeding.md) | MobRNG + consumption invariant replay relies on            |
| [specs/foundations/08-leaderboard-replay.md](specs/foundations/08-leaderboard-replay.md) | CompletedRun, checksum, replay engine                        |
| [specs/foundations/09-ci-cd-deploy.md](specs/foundations/09-ci-cd-deploy.md)         | Pipelines, source stripping, build tags that ship BUG-11           |
| [specs/foundations/10-client-architecture.md](specs/foundations/10-client-architecture.md) | Phaser scenes, net sync, packet→scene mapping                |
| [specs/features/combat-weapons.md](specs/features/combat-weapons.md)                 | Damage pipeline, HP, cooldowns (BUG-04, BUG-07, BUG-08)            |
| [specs/features/mob-ai.md](specs/features/mob-ai.md)                                 | Spawn, patrol, aggro, BUG-01 seed optimization                     |
| [specs/features/rooms-doors.md](specs/features/rooms-doors.md)                       | Room traversal, door state machine (BUG-03)                        |
| [specs/features/pickups.md](specs/features/pickups.md)                               | Pickups + interact range (BUG-05)                                  |
| [specs/features/inventory-items.md](specs/features/inventory-items.md)               | Inventory, item parsing (BUG-10, BUG-09 dispatch half)             |

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
- Queue notification: SSE, WebSocket from queue-admin, or client polls?
- Admin panel auth: basic auth? IP whitelist?
- Are 11 bugs too many? Should we cut to 8–9 for scope?
- BUG-11 non-determinism — is that fun or frustrating?
- Replay tolerance window for non-deterministic bugs.
- Should the debug items (BUG-09) be discoverable only through source-code reading, or also hinted in-game?
- Display name uniqueness — enforce or allow duplicates?
