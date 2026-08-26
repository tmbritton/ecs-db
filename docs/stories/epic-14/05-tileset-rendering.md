# Story 5: Tileset rendering

**Epic:** 14 — Tiled map & tileset formats  
**Status:** ✅ Complete  
**Priority:** Medium — the first time the map looks like a map

**Depends on:** Stories 2 and 4

## Context

`tilemap_renderer.go` fills a rectangle per tile: grey 60 for `tile_type ==
"wall"`, grey 180 for everything else. That is all the ASCII format could
support and it is the last thing in the engine that still assumes it.

This story draws each tile from its tileset image instead, using the source
rectangle Story 2 computes from columns, margin and spacing. Layers draw in file
order, so a wall layer covers the floor beneath it.

The renderer already has an image cache (`image_cache.go`) and it is behind the
`ebitengine` build tag, so nothing here can be tested without a display —
which is the argument for putting every decision that *can* be tested in Story 2
and leaving this one as thin as possible.

## Acceptance Criteria

- [x] Tiles draw from the tileset image at the source rectangle for their gid
- [x] Multiple layers draw in file order
- [x] An invisible layer does not draw
- [x] An empty cell draws nothing rather than a blank tile
- [x] The tileset image loads through the existing cache rather than a second one
- [x] A missing image file is reported once, by name, rather than per tile per
      frame
- [x] Nothing outside `//go:build ebitengine` gains a dependency on an image
      decoder
- [x] `go test ./...` passes, and `make build-headless` still builds with no
      tags and no CGO
- [ ] `ecs-db run` shows the migrated map drawn from its tileset — **not met, and
      not meetable here.** There is no migrated map: `level1.toml` becomes
      `level1.tmx` in Story 7, and until then the only map the engine loads has
      no tileset and takes the colour fallback. This is the criterion to check
      when Story 7 lands, on a machine with a display

## As Implemented

The renderer draws from the **parsed map**, not from the database: `comp_tile`
holds one row per cell, so it cannot express the layers this story's own
acceptance criteria are about. No schema change, no migration. See the
implementation plan for the argument and for what the split costs.

Almost nothing ended up in the tagged renderer. Everything decidable —
layer order, visibility, which tileset owns a global id, sheets against
collections, tile offsets, bottom-left alignment, and the flip composition —
is in `tiled.DrawList` and `Draw.Transform`, which no build tag hides.

Found by review, all of them the same shape: parsed, then silently ignored.

- **A map with no `tilewidth`/`tileheight`** was accepted and put every tile of
  every layer at x=0, one tile above the top of the window — the whole map in a
  single stack, off screen, no error. The reader already refuses a map with no
  size in tiles; this is the same thing one attribute over, and is refused now.
- **`renderorder` was parsed and dropped.** Invisible for square tiles and the
  whole point for the tall ones this story exists to draw: it decides which of
  two overlapping tiles ends up on top. All four orders are honoured.
- **`opacity` was parsed and dropped**, so a layer hidden by setting it to zero
  drew at full strength — the other way Tiled hides a layer, which the visible
  flag says nothing about.
- **A source rectangle past the end of the tileset's image** drew nothing:
  Ebiten intersects the rectangle with the image and returns an empty one, so
  no pixels, no error, and a hole nobody could account for. Checked against the
  size the tileset declares, when it declares one.
- **A diagonally flipped non-square tile landed a row out.** The transpose swaps
  the extent, and the bottom-left alignment was computed from the source height
  rather than the drawn one.
- **`Invalidate`'s comment was false**: nothing calls it, and `setTilePassable`
  never touched the renderer. The plan's own diagnosis was weaker and wrong —
  it blamed the `tile_type` coupling, which is true but is not why nothing
  redraws.
- **The map's tile size and `window.tileSize` could disagree.** Tiles are placed
  from the file now and entities still from the config; before this they could
  not disagree because both came from the config. Refused at startup, naming
  both numbers.

One gap the battery left and the review closed: every transform test used a
square tile, so a mutation flipping vertically about the *width* survived the
whole suite. A square tile cannot tell the two apart.

And two lint findings that had been sitting in the tagged half of the tree,
which `golangci-lint run` never compiles. Both fixed, and `make lint` now runs
both tag sets — which is what would have caught them.

## Notes

- Flip flags are parsed in Story 1 and this is the only place they mean
  anything. Drawing them is optional; dropping them silently is not — if they
  are ignored, say so where the drawing happens.
- The existing `tile_type` colouring is worth keeping as the fallback for a map
  with no tileset, which is what every test fixture will be.
- **The fallback is narrower than it sounds.** `readTiled` refuses a map whose
  tilesets will not resolve, and `Drawable` is true as soon as one did — so a
  Tiled map whose PNGs are all missing is a black screen and a log line naming
  each file, not grey rectangles. The colours are for a map with no tilesets at
  all, which means the character format.
- **A tile placed on an object layer is not drawn.** Object layers are Story 6's,
  and until then a prop dropped on one vanishes with nothing logged.
