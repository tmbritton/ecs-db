# Story 2: TSX tileset parser

**Epic:** 14 — Tiled map & tileset formats  
**Status:** 🔲 Not started  
**Priority:** High — the tile properties the engine actually reads

**Depends on:** Story 1

## Context

A tileset says how to cut an image into tiles and what each tile means. The
image reference, tile size, margin and spacing decide where a tile is in the
sheet; the per-tile custom properties are what the game reads — collision,
terrain, class — and they are the reason this epic exists at all, because the
ASCII format could only ever say "wall" or "not wall".

`.tsx` is XML and an embedded tileset is the same element inside the map, so one
parser serves both. Resolving a map's external references belongs here too:
Story 1 deliberately left them unresolved so that it could be tested from a
string.

**This is where passability comes from.** The roadmap said `TileGrid.Rebuild`
keys off `'#'`; it does not — it reads `comp_tile.passable` and always has. The
character switch lives in `LoadMap` and dies with the TOML format. What replaces
it is a per-tile property from here, which Story 4 writes into `comp_tile`.

## Acceptance Criteria

- [ ] `.tsx` parses, and an embedded `<tileset>` in a map parses through the
      same code
- [ ] Name, tile width and height, tile count, columns, margin and spacing
- [ ] The image source, resolved relative to the tileset file rather than the
      working directory
- [ ] Per-tile custom properties, typed as Tiled types them
- [ ] Tileset-level properties, and a tile inheriting nothing it did not declare
- [ ] The source rectangle for a local tile id, from columns, margin and spacing
- [ ] A map's external tileset references resolve, relative to the map file
- [ ] A gid resolves to a tileset plus a local id, across several tilesets
- [ ] A reference that does not exist, and a tileset whose tile count disagrees
      with its columns, are refused by name
- [ ] `go test ./...` passes

## Notes

- Tiled's property types are `string`, `int`, `float`, `bool`, `color`, `file`,
  `object` and `class`. The engine needs the first four; the rest should survive
  as strings rather than being dropped, on the same argument Epic 13 Story 1
  made about unknown machine fields.
- `passable` is a convention this epic invents, not something Tiled knows. Name
  it in one place and let Story 4 read it there.
