# Epic 14 Story 2: TSX tileset parser — Implementation Plan

**Goal:** a tileset, from a `.tsx` file or from a map that embeds one, with the
per-tile properties the engine actually reads and the source rectangle a
renderer needs — plus resolving a map's external references, which Story 1
deliberately left undone.

---

## Verified before planning

| Claim | Verified |
|---|---|
| An embedded `<tileset>` is the same element as a `.tsx` root | True, and Story 1's review is why it is usable: `TilesetRef.Embedded` now holds the whole element, attributes and all, rather than what was between its tags. |
| Story 1 keeps `FirstGID` and leaves references unresolved | True — `TilesetRef{FirstGID, Source, Embedded}`, and `Map.TilesetFor` already answers which tileset owns a gid. |
| Tiled's property types are `string, int, float, bool, color, file, object, class` | True, confirmed against the format reference during Story 1's review; `Properties` already keeps every one, with `class` carrying its nested block and `PropertyType` naming the custom type. |
| `TileGrid.Rebuild` reads `comp_tile.passable`, so passability is already property-driven | True. The character switch is in `LoadMap` and this story is what replaces it. |
| `passable` is a convention this epic invents rather than something Tiled knows | True. Nothing in Tiled names it. |

---

## Design

### One parser, two callers

`ParseTileset(data []byte, name string) (*Tileset, error)`. A `.tsx` file and an
embedded element are the same bytes, so the map path feeds `TilesetRef.Embedded`
straight into it — which is only possible because Story 1's review fixed what
`Embedded` captured.

### Resolution takes a reader, not a filesystem

```go
type Opener func(name string) ([]byte, error)
func (m *Map) ResolveTilesets(dir string, open Opener) error
```

`dir` is the map file's directory, because Tiled writes `source` relative to the
map. An injected `Opener` keeps every test a string, which is the property that
made Story 1 cheap to get right — and the caller in Story 4 passes
`os.ReadFile`.

An embedded tileset resolves without the opener being called at all.

### The image path is relative to the tileset, not the map

`<image source="../img/dungeon.png"/>` inside `tilesets/dungeon.tsx` is
`img/dungeon.png` from the project root, and the difference only shows when the
two files are in different directories — which is exactly how Tiled lays a
project out. Both the raw attribute and the resolved path are kept: the first is
what the file says and the second is what opens.

### Per-tile properties, and the one the engine reads

`Tileset.Tiles` is keyed by **local** id and holds only the tiles that declare
something — a 256-tile sheet with two walls has two entries, not 256.

`Passable(localID) (bool, bool)` is the accessor Story 4 calls, and it is the
one place the convention is named. The second result separates "this tileset
says nothing" from "this tileset says no", which decides whether Story 4 falls
back to a default or obeys.

**A tile inherits nothing.** Tiled has no per-tile inheritance from the
tileset's own properties, and inventing it would make a tileset-level `passable`
silently override a tile that had been set deliberately.

### The source rectangle

`SourceRect(localID) (x, y, w, h int, ok bool)` from `columns`, `margin` and
`spacing` — the arithmetic a renderer would otherwise write itself, and the last
thing in this epic that anyone would get subtly wrong by hand. `ok` is false for
an id the tileset does not hold.

### Refusals

A `source` that does not open, a tileset that is not a tileset, and one whose
`tilecount` and `columns` disagree with each other — each naming the file. The
last is the one that matters: a wrong `columns` puts every tile after the first
row at the wrong place in the image, and nothing downstream could tell.

---

## Files

| File | What |
|---|---|
| `internal/tiled/tileset.go` | `Tileset`, `Image`, `TilesetTile`, `ParseTileset`, `SourceRect`, `Passable` |
| `internal/tiled/resolve.go` | `Opener`, `Map.ResolveTilesets` |
| `internal/tiled/tileset_test.go` | parsed from strings, resolution through a fake opener |
