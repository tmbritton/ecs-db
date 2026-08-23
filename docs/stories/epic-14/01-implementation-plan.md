# Epic 14 Story 1: TMX/TMJ parser — Implementation Plan

**Goal:** a Tiled map, read from either serialisation, as a Go value the rest of
the epic can be written against — with nothing read from disk beyond the file
itself and nothing decoded that the engine will not use.

---

## Verified before planning

| Claim | Verified |
|---|---|
| Nothing in the repo parses XML, base64 or a compressed stream today | True — no import of `encoding/xml`, `encoding/base64`, `compress/zlib` or `compress/gzip` anywhere in `internal/` or `cmd/`. |
| There is no Tiled dependency | True — `go.mod` has none, and this story adds none. |
| `internal/tilemap` holds the grid, A*, line-of-sight, reachability and the TOML loader | True. The parser is a *format reader* and belongs beside it rather than inside it, so `tilemap` can depend on it and not the reverse. |
| `mods/map/level1.toml` is 20×15 | True — the fixture Story 7 migrates. |
| The gid's flags are the top three bits | **False as of Tiled 1.9**: four. Horizontal, vertical, diagonal, and a hexagonal 120° rotation. The clear mask is `0x0FFFFFFF`; `0x1FFFFFFF` leaves the hex bit in the id. |

---

## Design

### A sibling package, not a subpackage

`internal/tiled`. It knows about files and nothing about entities, databases or
grids, so it can be tested from a string; `internal/tilemap` will depend on it
in Story 4 and never the other way round.

### One value, two serialisations

`.tmx` is XML and `.tmj` is JSON, and they describe the same thing. Two sets of
wire structs unmarshal into one exported `Map`, so nothing above this package
can tell which was on disk — the AC's phrasing, and the reason `Parse` dispatches
on content rather than on extension where it can.

### Raw gids, decoded on demand

`Layer.Data` is `[]uint32` of exactly `Width*Height`, row-major, holding the
**raw** gid with its flags still in the top bits. `Layer.TileAt(x, y)` returns
the decomposed `Tile{GID, FlipH, FlipV, FlipD}`.

Raw in the slice because it is lossless and because the mask is a Tiled version
question rather than a fact — a caller that wants the bits can have them.
Decomposed through an accessor because every caller in this epic wants the id
and would otherwise mask it themselves, which is the copy that goes wrong.

### Encodings

| `encoding` | `compression` | Decoder |
|---|---|---|
| `csv` | — | split on commas, `ParseUint` |
| `base64` | — | `base64.StdEncoding` |
| `base64` | `zlib` | + `compress/zlib` |
| `base64` | `gzip` | + `compress/gzip` |
| absent (XML `<tile gid=…>`) | — | the child elements, in order |
| TMJ `data` as an array | — | the numbers |
| TMJ `data` as a string | as above | the same decoders |

`zstd` is Tiled's fourth option and needs a dependency; it is refused by name
rather than mis-decoded.

### What is kept that this story does not use

Object groups are parsed and carried, because Story 6 reads them and a parser
that dropped them would have to be reopened. Properties are typed as Tiled types
them — `string`, `int`, `float`, `bool` — and anything else survives as a string,
on the argument Epic 13 Story 1 made about unknown machine fields: a reader that
silently drops what it does not model is a reader nothing above it can trust.

External tilesets are **not** resolved. `TilesetRef` keeps `FirstGID` and either
`Source` or an embedded tileset; resolving needs the TSX parser and a filesystem,
and both would make this untestable from a string.

### Refusals

A file that is not a map, a size that is not a size, an infinite map, a layer
whose decoded length disagrees with `Width*Height`, a layer that is not the
shape of the map it is in, and an unsupported encoding or compression — each
named with the file and what was wrong.

The length checks are the ones that matter: a short layer is a map with a hole
in it and every later story would read past the end.

*(An earlier draft of this line also promised to refuse "a gid whose id exceeds
any tileset's range". Nothing here can: a range needs `tilecount` from a `.tsx`
this package deliberately does not resolve. `Map.TilesetFor` answers which
tileset owns a gid, and the range check belongs to Story 2, where the tile
counts are.)*

---

## Files

| File | What |
|---|---|
| `internal/tiled/tiled.go` | the exported `Map`, `Layer`, `Tile`, `ObjectGroup`, `Object`, `TilesetRef`, `Properties` |
| `internal/tiled/tmx.go` | the XML wire structs and their conversion |
| `internal/tiled/tmj.go` | the JSON wire structs and their conversion |
| `internal/tiled/data.go` | the five decoders and the gid masks |
| `internal/tiled/*_test.go` | parsed from strings, both serialisations asserted to agree |
