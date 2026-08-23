# Story 1: TMX/TMJ parser

**Epic:** 14 — Tiled map & tileset formats  
**Status:** ✅ Complete  
**Priority:** High — nothing else in the epic has an input without it

**Depends on:** nothing

## Context

Tiled writes two interchangeable serialisations of the same map: `.tmx` (XML)
and `.tmj` (JSON). Both carry the map's size and tile size, an ordered list of
layers, and a reference to one or more tilesets. A tile layer's data is an array
of **global tile ids** — one per cell, row-major, `0` meaning empty — encoded
either as CSV or as base64, optionally compressed with zlib or gzip.

This story parses that into Go and stops. It reads no images, touches no
database and creates no entities: the point is a value the rest of the epic can
be written against, and a parser that can be tested without a display or a
schema.

Two details decide whether later stories are possible at all. The **global tile
id carries flip flags in its top three bits**, so an id used as an index without
masking is wrong for any rotated tile — and Tiled writes them the moment someone
presses X in the editor. And a **tileset may be embedded in the map or external
via `<tileset source="…">`**; both are ordinary, so the parser has to represent
"which tileset does this gid belong to" by first-gid range rather than assuming
one.

## Acceptance Criteria

- [x] `.tmx` and `.tmj` both parse into the same Go value, so the rest of the
      engine never learns which was on disk
- [x] Map width, height, tile width and tile height
- [x] Multiple tile layers, in file order, each with its name and visibility
- [x] CSV, base64, base64+zlib and base64+gzip layer data all decode
- [x] A layer's data is a flat `[]uint32` of length width×height, row-major
- [x] Flip flags are separated from the tile id rather than left in it, and both
      are readable
- [x] Tileset references keep their `firstgid`, so a gid resolves to a tileset
      and a local id
- [x] Map-level custom properties, typed as Tiled types them
- [x] A file that is not a map, a layer whose data length disagrees with
      width×height, and an unsupported encoding are each refused with a message
      naming the file and what was wrong
- [x] Object layers are preserved rather than dropped — Story 6 reads them
- [x] `go test ./...` passes

## Notes

- The gid's top **four** bits are flags: horizontal, vertical and diagonal flip,
  and — since Tiled 1.9 — a hexagonal 120° rotation. The clear mask is therefore
  `0x0FFFFFFF`, not the `0x1FFFFFFF` that three flags would suggest; the older
  mask leaves the hex bit in the id and produces a tile index ~268 million too
  high for any map Tiled 1.9 wrote. The engine may ignore the flags, but losing
  them silently is what makes a rotated tile draw wrong later.
- Prefer `encoding/xml` and `encoding/json` over a dependency. The subset Tiled
  writes is small and a parser is easier to keep honest than a library's
  assumptions about which version wrote the file.
- Do not resolve external tilesets here. That needs the TSX parser, which is
  Story 2, and a parser that reaches for the filesystem is one that cannot be
  tested from a string.

## As Implemented

`internal/tiled` — a sibling of `internal/tilemap`, not a subpackage. It knows
about files and nothing else, so every one of its tests is a string, and
`tilemap` will depend on it in Story 4 and never the other way round.

### The gid mask, verified against the format

Tiled 1.9 uses the top **four** bits — horizontal, vertical and diagonal flip,
and a hexagonal 120° rotation — so the clear mask is `0x0FFFFFFF`. The
`0x1FFFFFFF` in wide circulation is the pre-1.9 three-flag mask and leaves the
hex bit in the id, producing a tile index 268 million too high for any map a
current editor wrote. Checked against Tiled's own reference during review, and
it holds.

`Layer.Data` keeps the **raw** gid and `TileAt` decomposes it, because which
bits are flags is a version question rather than a fact.

### What review against the real specification changed

Eleven findings. Three would have shipped a parser that reads real files wrong:

- **A `<group>` is a layer folder, and its contents were dropped.** Dragging two
  layers into a folder is a routine editor action, and a map that had been
  tidied loaded as *empty, with no error* — the exact failure this story argues
  against. Both serialisations now flatten folders recursively, and a hidden
  folder hides what is in it.
- **A negative `width` panicked `make`.** `cells` comes straight out of a file;
  `width="-1"` crashed the process instead of returning an error, and
  `width="100000" height="100000"` asked for 40 GB before a byte had been read.
- **`TilesetRef.Embedded` could not do what its own comment promised.** In JSON
  it was tagged `json:"-"` and was therefore always nil. In XML, `,innerxml`
  gives what is *between* the tags — so `name`, `tilewidth`, `tilecount` and
  `columns` were all gone and what was left was not a `<tileset>` element at
  all. Both now capture the whole element.

And two that would have produced a bad report rather than a bad read:

- **An infinite map** — one written in chunks — decoded to nothing and was
  refused as *"layer holds 0 tiles but is 1024 cells"*, which reads as a corrupt
  map and sends someone hunting for a hole that is not there. `Map.Infinite` was
  parsed and read by nothing; it is now the refusal.
- **Decompression was unbounded.** A 260 KB payload expanded to 256 MB and then
  asked for a 268 MB slice before any guard noticed the layer was one cell. The
  size is known before decompressing, so the bound was free.

The rest: a whole-numbered float read differently from the two serialisations
(the normalisation excluded exactly the case it was written for); a class-typed
property stored the whitespace between its tags; objects dropped `template`,
`rotation` and `visible`, and a **template** instance carries almost nothing
inline, so Story 6 would have seen nameless, typeless spawns; `Object.GID` was
raw and undocumented, so the obvious use of it as an index is wrong by
0x80000000 the moment somebody flips a spawn.

`Map.TilesetFor` was added — the first-gid range lookup this story's own Context
asks for. Without it every caller from Story 4 writes the descending scan
itself, which is the copy that goes wrong.

### On the tests

The suite passed on its first run, and the mutation battery caught 24 of 41.
Three lessons, all of which recurred:

- **Fixtures that cannot distinguish.** The flip test set two flags at once, so
  reading the diagonal bit as the vertical one passed. The encoding test used
  four small gids, which cannot catch a high-byte error.
- **Assertions weaker than their names.** Six refusal tests asserted only that
  *an* error occurred; every mutation still produced one, further downstream.
  Asserting the base64 refusal mentioned `"base64"` passed even when the parser
  fell through to a decompression error, because the wrapped stdlib message says
  *"illegal base64 data"* either way.
- **Comparing the fields you thought of.** The "both serialisations agree" test
  checked six fields and missed that `.tmj` dropped every embedded tileset. It
  is now `reflect.DeepEqual` over two fixtures carrying one of everything.

Three guards turned out to be genuinely unreachable and were deleted rather than
kept as decoration: the `!ok` lookups in `Int`/`Float`/`Bool` (an absent
property is the zero value, whose empty string parses as none of those types),
the root-element check in `parseTMX` (the `xml:"map"` tag already makes
`Unmarshal` refuse anything else), and a separate chardata field beside
`innerxml` (which carries text and markup alike).

63/63 mutations caught; 90.9% statement coverage.

### Left for later

- **Templates are captured, not resolved.** `Object.Template` names a `.tx` file
  and nothing reads it. Story 6 needs to, or a spawn placed from a template has
  no type.
- **Object shapes** — point, polygon, polyline, ellipse — are not modelled, so a
  point object is indistinguishable from a 0×0 rectangle. Nothing in this engine
  reads a shape yet.
- **Flip flags are parsed and drawn by nobody.** Story 5 is where they mean
  something.
- **`zstd` is refused by name** rather than decoded, because it needs a
  dependency. Re-export as zlib, gzip or CSV.
