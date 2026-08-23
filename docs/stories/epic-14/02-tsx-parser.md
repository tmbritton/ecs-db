# Story 2: TSX tileset parser

**Epic:** 14 — Tiled map & tileset formats  
**Status:** ✅ Complete  
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

- [x] `.tsx` parses, and an embedded `<tileset>` in a map parses through the
      same code
- [x] Name, tile width and height, tile count, columns, margin and spacing
- [x] The image source, resolved relative to the tileset file rather than the
      working directory
- [x] Per-tile custom properties, typed as Tiled types them
- [x] Tileset-level properties, and a tile inheriting nothing it did not declare
- [x] The source rectangle for a local tile id, from columns, margin and spacing
- [x] A map's external tileset references resolve, relative to the map file
- [x] A gid resolves to a tileset plus a local id, across several tilesets
- [x] A reference that does not exist, and a tileset whose tile count disagrees
      with its columns, are refused by name
- [x] `go test ./...` passes

## Notes

- Tiled's property types are `string`, `int`, `float`, `bool`, `color`, `file`,
  `object` and `class`. The engine needs the first four; the rest should survive
  as strings rather than being dropped, on the same argument Epic 13 Story 1
  made about unknown machine fields.
- `passable` is a convention this epic invents, not something Tiled knows. Name
  it in one place and let Story 4 read it there.


## As Implemented

`ParseTileset(data, name, dir)` reads a `.tsx`, a `.tsj`, or the element a map
embeds — one function for all three, because an embedded tileset is the same
bytes as a file, which is only true because Story 1's review fixed what
`TilesetRef.Embedded` captured.

`Map.ResolveTilesets(dir, open)` takes an injected `Opener` rather than reaching
for the filesystem, which is what keeps every test in this package a string.

### Two kinds of tileset, not one

A **sheet** is one image cut into a grid. A **collection** is one file per tile
with no sheet at all, which Tiled writes with `columns="0"` — and which is the
obvious way to author the prop and creature art Story 6 spawns.

The first draft refused every collection, and blamed a `columns` the author
never chose. It also applied a divisibility rule that is simply false for one:
a collection's `columns` is a display width somebody dragged in the editor, and
its tile ids are sparse because deleting a tile leaves a gap. Both checks are
now a sheet's, `Collection()` says which kind a tileset is, and `SourceRect`
reports false for a collection because there is no grid for a rectangle to be
in.

### What review against the format changed

Nine findings; three of my own suspicions were among them and all three held.

- **No JSON tileset support at all.** `ParseTileset` called `xml.Unmarshal`
  unconditionally, so a `.tmj` project loaded its map and then died on its
  tilesets with a bare `EOF`. That falsified this package's own doc comment,
  which claims both serialisations parse into one value so that nothing above it
  learns which was on disk — true of maps, and not of tilesets.
- **Image collections were refused**, above.
- **`tilecount % columns` was not a rule Tiled keeps**, above.
- **An absolute image path became a relative one.** `path.Join` does not treat a
  leading slash as anchoring, so `/home/tom/art/dungeon.png` came out as
  `maps/home/tom/art/dungeon.png` — and Tiled writes an absolute path the first
  time art comes from outside the project.
- **`<tileoffset>` was dropped**, which draws every tile of a taller-than-grid
  tileset in the wrong place. `SourceRect` is documented as the arithmetic a
  renderer should trust, so Story 5 would have trusted it.
- **`<image trans>`**, the colour key a magenta-keyed sheet needs, was dropped.
- **The resolution refusal named neither the map nor the path actually opened** —
  the one fact worth having when a relative reference is wrong. `Map.Name` is
  recorded at parse time so it can.
- **Tileset-level `class`** was missing while a tile's was present.
- **Two tiles with one id** silently last-won.

### On the tests

79/79 mutations before review, which was better than Story 1 managed — but the
reviewer found a refusal test asserting only that *an* error occurred, which is
the exact weakness Story 1's own As Implemented writes up. Writing a lesson down
did not stop me repeating it one story later.

Two more of the same shape came out of the second battery: a collection fixture
with `columns="0"` cannot tell the collection check from the `columns <= 0`
check behind it, and a "not a tileset" fixture with no tile size is refused by
the size check before the type check is reached. Both needed a fixture built to
distinguish rather than merely to fail.

97/97 caught; 92.8% statement coverage.

### Left for later

- **Per-tile `<objectgroup>` collision shapes and `<animation>` frames** are
  dropped. This story's own Context names both, so this is a gap rather than a
  decision — nothing in the engine reads either yet, and Story 5 is where
  animation would first mean something.
- **Wangsets and terrain** are dropped, and nothing plans to read them.
- **Object alignment and `probability`** likewise.
- **`Image.Path` is resolved at parse time now**, from the `dir` argument, so a
  caller reading a `.tsx` directly gets it. `ResolveTilesets` passes the
  tileset's own directory, which is what makes an image relative to its tileset
  rather than to the map.
- **A `source` that climbs out of the project** (`../../../etc/x.tsx`) is
  resolved and handed to the caller's `Opener`. The injection means a caller can
  sandbox it; nothing here says it must, and Forge will accept uploaded
  projects.
