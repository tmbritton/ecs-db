# Story 3: Map re-import semantics

**Epic:** 14 — Tiled map & tileset formats  
**Status:** 🔲 Not started  
**Priority:** High — decides whether Forge's MAP mode can work at all

**Depends on:** Story 1

## Context

`LoadMap` is a one-time bootstrap. It counts `Tile` entities and creates none if
any exist, so after the first `ecs-db run` the **database is the source of truth
and the file is decoration**. A map edited in Forge would save successfully and
change nothing — no error, no warning, nothing to notice.

Epic 15 is a map editor. It cannot be built on that.

This story settles it before anything is built on top, which is the same call
Epic 13 Story 1 made about the machine round trip and for the same reason: a
format that cannot be re-read is a format nothing above it can trust.

The shape of the answer is a **diff**, not a reload. Tiles carry state the file
does not own — a door someone opened at runtime is `comp_tile.passable` differing
from the tileset's property, and `setTilePassable` is a registered action that
exists to do exactly that. Truncating and recreating would silently discard it,
and would also churn every entity id, which `comp_tile.entity_id` and the
`TileGrid`'s own index both depend on.

## Acceptance Criteria

- [ ] Loading a map whose tiles already exist updates them rather than skipping
- [ ] A tile whose file position is unchanged keeps its entity id
- [ ] A tile the file no longer has is removed
- [ ] A tile the file has and the database does not is created
- [ ] Nothing is written when the file and the database already agree, so a
      restart with no edit touches no rows
- [ ] The decision is per tile, not per map: one changed cell does not rewrite
      the other 299
- [ ] Runtime state a tile carries that the file does not describe is stated one
      way or the other — either preserved with a reason, or overwritten with a
      reason
- [ ] A map that fails to parse leaves the database exactly as it was
- [ ] `go test ./...` passes

## Notes

- The identity of a tile is its position, not its row in the table. Two tiles
  cannot share a cell, and that is what makes the diff possible.
- Watch the transaction boundary. A partial re-import is a map with a hole in
  it, and the engine reads the grid on the next tick.
- This is the story most likely to want a decision recorded rather than a rule
  invented: "the file wins" and "the database wins" are both defensible for
  `passable`, and only one of them can be true.
