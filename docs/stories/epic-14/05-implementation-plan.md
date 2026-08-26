# Story 5 — implementation plan: tileset rendering

**Story:** [`05-tileset-rendering.md`](05-tileset-rendering.md)

## What the code says

Verified before planning, because the story brief and the roadmap disagree with
each other about where a drawn tile comes from.

**The renderer today reads the database.** `tilemap_renderer.go` joins
`entities` to `comp_tile` and fills a rectangle per row: grey 60 for
`tile_type == "wall"`, grey 180 otherwise. Confirmed as written.

**`comp_tile` cannot say which tile to draw.** Its properties are `x`, `y`,
`passable`, `tile_type` — no global id, no tileset. And it holds **one row per
cell**: Story 4 decided the topmost non-empty tile *is* the cell's tile,
because the component's shape forces the choice.

That second fact settles the story's central question, which the brief states
without arguing for:

> Layers draw in file order, so a wall layer covers the floor beneath it.

A database with one row per cell has no layers to draw in any order. So **the
renderer draws from the parsed map, not from the database**, and this story
needs no schema change and no migration.

I had said this story "opens with a migration, because the tile's global id
needs to reach `comp_tile`". That was wrong, and worth writing down because it
was wrong in a specific way: storing a gid per cell would let the database name
one tile per cell, and it would still not let it name a *stack*. (It was said in
conversation, not in `docs/plan.md`, whose entry for this story is a single
line with no such claim — an earlier draft of this plan attributed the quote to
that file, which the review caught.)

`comp_tile` stays what Story 4 made it: the **gameplay** projection of a map.
The file stays the **appearance**. That is also the split the Forge design
handoff already assumes — `EPICS.md` has the editor reading and writing "real
engine files: `schema.json`, `mods/*/behaviors/*.json`, TMX/TMJ maps, `.tsx`
tilesets" and opening `world.sqlite` **read-only**. An editor that cannot write
to the database could not author appearance if appearance lived there.

**What it costs**, which is the part worth knowing before someone is surprised
by it: after this story, nothing in the database can alter a drawn pixel of a
Tiled map. Before it, changing `comp_tile.tile_type` and rebuilding would have.
Live tile editing, or a scripted wall collapsing, needs something the renderer
reads that the simulation can write — a story, not a tweak.

**Story 2 already did the hard arithmetic.** `Tileset.SourceRect(local)` returns
the rectangle for a tile from columns, margin and spacing, and reports false for
a collection, which has no grid. `Tileset.Collection()`, `TileOffsetX/Y`,
`TilesetTile.Image` and `Map.ResolveTilesets` are all there.

**Flags survive the parser.** `Layer.TileAt(x, y)` returns a `tiled.Tile` with
`GID` masked clean and `FlipH/FlipV/FlipD/RotatedHex` split out. Nothing
downstream reads them yet.

**The image cache is usable and silent.** `ImageCache.Get` returns
`(nil, false)` for a path that will not open *and* for one that will not decode,
with no error and no name. Called per tile per frame it would say nothing, a lot.

**One curiosity, recorded rather than fixed.** `Invalidate()` exists so
`setTilePassable` can redraw an opened door — but that action writes `passable`
and the renderer colours by `tile_type`, so the rebuilt image is identical to
the one it replaces. The invalidation path has never changed a pixel. Out of
scope here; worth knowing before someone relies on it.

## Design

### Everything that can be got wrong goes where it can be tested

The renderer is behind `//go:build ebitengine` and needs a display, so the plan
is to leave almost nothing in it. A new **untagged** file, `internal/tiled/draw.go`,
turns a resolved map into an ordered list of draw instructions:

```go
// Draw is one tile, placed.
type Draw struct {
    Image                string // the file to draw from
    SX, SY, SW, SH       int    // source rectangle within it
    DX, DY               int    // destination in pixels
    FlipH, FlipV, FlipD  bool
}

func (m *Map) DrawList() ([]Draw, []DrawProblem)
```

Pure arithmetic over the parsed format: no image decoding, no Ebitengine, no
database. It belongs in `tiled` rather than `tilemap` because every input is the
format's and `SourceRect` is already there.

Decisions it owns, each testable without a display:

| decision | rule |
|---|---|
| layer order | file order, back to front — the reverse of `cellAt`, which wants the topmost |
| visibility | an invisible layer contributes nothing, which is what `Visible` is *for*; `cellAt` deliberately ignores it, and the comment there already says so |
| empty cells | gid 0 contributes nothing, rather than a blank tile |
| which tileset | the ref with the greatest `FirstGID` not above the gid |
| tall tiles | bottom-left aligned to the cell, so a 32px tile in a 16px grid rises out of it — Tiled's rule, and the one thing here that looks like a layout bug when wrong |
| tile offset | `TileOffsetX/Y` added to every destination |
| collections | the tile's own `Image`, whole, rather than a rectangle of a sheet that does not exist |

`DrawProblem` carries what cannot be drawn — a gid no tileset holds, a tileset
with no resolved image path — once each, with the layer and cell that caused it.

### The renderer becomes a loop

`TilemapRenderer` gains the map and an `ImageCache`, and `rebuild` becomes:
load each distinct `Draw.Image` once, report the ones that will not load by
name, then walk the list calling `DrawImage`. The `tile_type` colouring stays as
the path taken when the map has no drawable tilesets — which is every current
test fixture and the TOML map until Story 7.

Flips are drawn, since `GeoM` makes it three lines; `FlipD` is the antidiagonal
transpose and is the one worth a test of its own.

### Wiring

`LoadMap` returns the parsed `*tiled.Map` alongside the grid, so `run.go` hands
the renderer what it already read rather than parsing the file twice. The
character format returns nil, and a nil map is the fallback path.

## Work

1. `internal/tiled/draw.go` + tests — `DrawList`, `DrawProblem`, the seven rules
   above. All untagged, all testable.
2. `LoadMap` returns the map; `readMap` already has it. Update the one caller.
3. `TilemapRenderer` takes `*tiled.Map` and an `ImageCache`; `rebuild` draws the
   list, falls back to colours, reports missing images once by name.
4. `make build-headless` still builds with no tags and no CGO — the guard that
   the new file pulled in no image decoder.

## Verification

- `go test ./...`, `-count=2`, `-race`, `golangci-lint`
- `make build-headless` with `CGO_ENABLED=0` and no tags
- Mutation battery over `draw.go` and the loader change
- Fresh-context review before committing
- `ecs-db run` against a map with a real tileset — the first time the map looks
  like a map, and the only part of this that a test cannot see
