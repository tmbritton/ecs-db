# Story 5: Tileset rendering

**Epic:** 14 — Tiled map & tileset formats  
**Status:** 🔲 Not started  
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

- [ ] Tiles draw from the tileset image at the source rectangle for their gid
- [ ] Multiple layers draw in file order
- [ ] An invisible layer does not draw
- [ ] An empty cell draws nothing rather than a blank tile
- [ ] The tileset image loads through the existing cache rather than a second one
- [ ] A missing image file is reported once, by name, rather than per tile per
      frame
- [ ] Nothing outside `//go:build ebitengine` gains a dependency on an image
      decoder
- [ ] `go test ./...` passes, and `make build-headless` still builds with no
      tags and no CGO
- [ ] `ecs-db run` shows the migrated map drawn from its tileset

## Notes

- Flip flags are parsed in Story 1 and this is the only place they mean
  anything. Drawing them is optional; dropping them silently is not — if they
  are ignored, say so where the drawing happens.
- The existing `tile_type` colouring is worth keeping as the fallback for a map
  with no tileset, which is what every test fixture will be.
