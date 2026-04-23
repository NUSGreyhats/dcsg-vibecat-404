# 10 — Client Architecture

> **Moved.** The client foundation specs have been split out of this file into a dedicated `specs/client/` tree at the same depth as the server foundations. Follow the links below.

## New location

| Area                              | Spec                                               |
| --------------------------------- | -------------------------------------------------- |
| Overall client architecture       | [../client/01-architecture.md](../client/01-architecture.md) |
| Network sync + packet layer       | [../client/02-net-sync.md](../client/02-net-sync.md) |
| Client-side state + event bus     | [../client/03-client-state.md](../client/03-client-state.md) |
| Scene graph + lifecycle           | [../client/04-scenes.md](../client/04-scenes.md)   |
| Entity rendering + interpolation  | [../client/05-entities-rendering.md](../client/05-entities-rendering.md) |
| Rooms + tilemaps                  | [../client/06-world-rooms.md](../client/06-world-rooms.md) |
| Input & controls                  | [../client/07-input-controls.md](../client/07-input-controls.md) |
| Assets pipeline + Vite            | [../client/08-assets-pipeline.md](../client/08-assets-pipeline.md) |
| Spectator mode (greenfield)       | [../client/09-spectator-mode.md](../client/09-spectator-mode.md) |
| UI components design              | [../client/10-ui-components.md](../client/10-ui-components.md) |

## Why split

The single-file client spec had reached the depth where it was underdocumented relative to the server foundations. Splitting brings each client concern up to the same level of detail as its server counterpart (protocol, state conventions, runtime, etc.) and makes "where does this belong" easier to answer when adding a new feature.

## For incoming links

This stub exists so existing cross-references from other specs continue to resolve. Any new reference should point directly at the target file under `specs/client/`.
