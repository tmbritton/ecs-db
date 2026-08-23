# Story 4: The loader writes tiles from a parsed map

**Epic:** 14 — Tiled map & tileset formats  
**Status:** 🔲 Not started  
**Priority:** High — connects the parsers to the game

**Depends on:** Stories 1, 2 and 3

## Context

Stories 1 and 2 produce a parsed map and its tilesets; Story 3 decides how tiles
reach the database a second time. This story is the loader that uses all three:
it replaces `LoadMap`'s TOML reader and its `'.'`/`'#'` switch with a Tiled map
and per-tile properties, and it is where `passable` stops being a character and
becomes something a tileset declares.

`TileGrid` needs no change. `Rebuild` reads `comp_tile.passable` and always has,
which is why this is the last place the character format survives.

Multiple tile layers raise the one question the ASCII format never had: **which
layer decides passability?** A map with a floor layer and a wall layer above it
has two tiles in the same cell. The answer has to be stated rather than fallen
into.

## Acceptance Criteria

- [ ] `LoadMap` takes a Tiled map and produces the same `*TileGrid` the rest of
      the engine already reads
- [ ] `comp_tile.passable` comes from the tile's property rather than from a
      character
- [ ] `comp_tile.tile_type` carries something the renderer and the existing
      queries can still use
- [ ] Which layer decides a cell's passability is stated in the code, and it is
      the same answer every run
- [ ] An empty cell — gid `0` — is not a tile, and creates no entity
- [ ] A tile whose gid resolves to no tileset is refused by position rather than
      silently skipped
- [ ] Re-import goes through Story 3 rather than a second copy of it
- [ ] The wandering goblin still wanders: `ecs-db run` against the migrated map
      pathfinds, sees and moves exactly as before
- [ ] `go test ./...` passes

## Notes

- The tile's *global* id is what identifies its appearance and is what Story 5
  draws from. `comp_tile` may want it as well as `tile_type`, which is a schema
  change and therefore a migration.
- Keep `LoadMap`'s signature if it can be kept. Every caller of it is a
  composition root, and a story that changes the loader and its callers at once
  is two stories.
