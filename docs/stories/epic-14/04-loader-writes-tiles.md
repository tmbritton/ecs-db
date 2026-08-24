# Story 4: The loader writes tiles from a parsed map

**Epic:** 14 — Tiled map & tileset formats  
**Status:** ✅ Complete  
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

- [x] `LoadMap` takes a Tiled map and produces the same `*TileGrid` the rest of
      the engine already reads
- [x] `comp_tile.passable` comes from the tile's property rather than from a
      character
- [x] `comp_tile.tile_type` carries something the renderer and the existing
      queries can still use
- [x] Which layer decides a cell's passability is stated in the code, and it is
      the same answer every run
- [x] An empty cell — gid `0` — is not a tile, and creates no entity
- [x] A tile whose gid resolves to no tileset is refused by position rather than
      silently skipped
- [x] Re-import goes through Story 3 rather than a second copy of it
- [x] The wandering goblin still wanders: `ecs-db run` against the migrated map
      pathfinds, sees and moves exactly as before
- [x] `go test ./...` passes

## Notes

- The tile's *global* id is what identifies its appearance and is what Story 5
  draws from. `comp_tile` may want it as well as `tile_type`, which is a schema
  change and therefore a migration.
- Keep `LoadMap`'s signature if it can be kept. Every caller of it is a
  composition root, and a story that changes the loader and its callers at once
  is two stories.

## As Implemented

`LoadMap` keeps its signature, sniffs the file, and dispatches: a Tiled map goes
through `readTiled`, and the character format goes through the reader it always
had. Both produce the same thing — a `map[Point]TileState` and a size — and both
hand it to Story 3's `SyncTiles`, so re-import is one implementation and not two.

### The decision the story asked for

> **The topmost non-empty tile in a cell is the cell's tile, and it decides both
> `passable` and `tile_type`.**

Not a preference. `comp_tile` holds one row per cell, so the loader has to pick
one tile out of the stack, and the one the author drew on top is the one they
see there. Layer order is the file's order, which is now the same order in both
serialisations — see below.

Three decisions come with it:

- **A hidden layer still counts.** Visibility is what the editor shows you, not
  what the map is. An author who hides the wall layer to look at the floor
  underneath it, saves, and finds every wall gone has been robbed by a checkbox.
  Story 5 honours `Visible` for drawing, which is what it is for.
- **A tile whose tileset declares no `passable` is passable**, and a tileset may
  declare `passable` itself to change that for every tile in it. A map is a
  floor with obstacles on it: a default that made a 200-tile decoration set
  unwalkable until every tile in it was declared is a default that gets scripted
  around. The asymmetry with an empty cell — impassable, because `TileGrid` has
  no entry for it — is deliberate. Nothing to stand on is not the same as a
  floor nobody described.
- **`tile_type` is the tile's class, then the tileset's, then empty.** The
  renderer draws anything that is not `"wall"` as floor, so a tile with no class
  draws rather than disappears.

### The defect this story found in Story 1

`collectXML` read a map's direct tile layers first and only then descended into
its `<group>` layer folders, so a folder written *above* a plain layer came out
*below* it. `collectJSON` walks one list and got it right. Measured on the same
one-cell map in both serialisations:

```
tmx order: [over under]
tmj order: [under over]
```

Invisible until something depends on layer order — which is this story's whole
rule. The same map exported both ways would have disagreed about which tile is
on top, against a package whose doc comment claims both "parse into the value
below, so nothing above this package learns which was on disk".

The XML reader now walks a map's and a folder's children in document order,
through one `xml:",any"` union node type. `encoding/xml` fills such a field with
every child element no other field claimed, in the order it read them, and that
is the only place the file's order survives — three slices are three passes.

### What the review found

Ten findings, two of them the same class as Story 3's: a valid file that loads
as fewer cells than it has, which `SyncTiles` then turns into deletions.

1. **A map with no declared size deleted every tile.** The parser refuses a
   negative size and accepts a map with no dimensions at all, deliberately —
   nothing below it had a reason to care. Under a diff, zero cells means every
   tile in the database is a cell the file dropped: the level gone, no error.
   Exactly the `rowz` bug from Story 3, in the format that replaces it, and
   reachable from the caller this code was written for — Forge's MAP mode builds
   maps rather than reading them. `checkShape` now refuses a map of no size, and
   a layer smaller than its map for the same reason: `Layer.TileAt` answers
   "empty" out of range, which is right for a renderer walking a viewport and
   here would quietly turn the missing part of a layer into cells the file lost.
2. **`TileCount` was the wrong membership test, and it failed both ways.** A
   *collection* of images — one file per tile, the obvious way to author prop and
   creature art — has ids that are not consecutive, because deleting a tile in
   the editor leaves a gap. So a three-tile collection's last tile is id 3, which
   a count bound refuses; and id 1 may be the hole, which a count bound accepts
   and `stateOf` reads as the zero `TilesetTile`: a passable, typeless floor,
   silently. The tileset parser already knew this and says so in `Tileset.check`;
   the loader reintroduced the rejection that comment exists to avoid. `holds`
   now asks the question each kind of tileset can answer — membership for a
   collection, range for a sheet.

The rest, in short: a `passable` that is declared and will not read as a boolean
— an `int` `0`, a string `"no"`, both of which Tiled's property editor makes easy
— fell back to the default and drew a wall the player walks through, invisible
until somebody tests the geometry, and is now refused; the `tile_type` fallback
order was untested and could be written backwards with the suite still green,
because no fixture put a tileset class and a tile class on the same tile; a map
declaring no tilesets at all was reported as a renumbering; `"is an %s map"` was
ungrammatical for two of Tiled's four orientations; error prefixes mixed
`LoadMap:` with the `tiled:` the parser uses, and `LoadMap:` was wrong anyway for
the in-memory caller the file plans for, so this package's own refusals now say
`tilemap:`; and the `,any` comment claimed only a folder ever has children, which
an `<imagelayer>`'s `<image>` disproves.

Four tests asserted less than their names claimed and were rewritten: an
orientation test whose only real assertion was the fixture's own `Fatalf`, a
"neither format" test whose fixture never reached the reader it was named for, an
unparseable-file test that passed just as well when the refusal came from the
shape check downstream, and an object-layer order test that made the
both-serialisations point in only one serialisation.

### Verification

- 60/60 mutations caught, 0 survived.
- `go test ./... -race` clean; `golangci-lint` clean.
- Coverage: `tiled` 93.4%, `tilemap` 96.8%.
- **The goblin criterion, discharged directly.** The Tiled equivalent of
  `mods/map/level1.toml` was generated and both were loaded against the real
  `schema.json`: 300 tiles each, and every cell agrees on passability, on whether
  a tile is there at all, and on `tile_type`. A*, line of sight and reachability
  read the grid and nothing else, so identical grids means they cannot tell the
  two maps apart — which is a stronger statement than watching the goblin wander,
  and it is what that criterion means.

### Left for later

- **`comp_tile` does not carry the tile's global id.** Story 5 draws from
  tilesets and needs it; whether it wants the gid alone, or the gid with the
  tileset it resolves through and the flip flags, is Story 5's question, and the
  answer is a column and therefore a migration. Adding it now would be guessing
  at the shape.
- **The character reader is still here.** It goes when `game.toml` stops pointing
  at `level1.toml`, which is Story 7's — a story that changed the loader and
  migrated the map at once would be two stories.
- **Flip flags are parsed, masked before lookup, and drawn by nobody.** A flipped
  tile loads as its tile, which is all this story needs; Story 5 decides whether
  the renderer honours them.
- **The tileset opener is not sandboxed.** A `source` that climbs out of the
  project is handed to `os.ReadFile` as written. Carried from Story 2.
