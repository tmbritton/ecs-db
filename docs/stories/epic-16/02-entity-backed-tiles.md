# Story 2: Every authored tile is an entity

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** ✅ Implemented
**Priority:** High — resolves the ECS/rendering contradiction before TILES

## Confirmed product rule

Every rendered tile in every layer is a Tile entity. A layer hidden in Tiled
still has authored tiles and Tile entities; referenced entities' visual
components determine what they draw. A TMX/TSX is an **authoring source** for importing those entities,
not a second live model the renderer reads behind the database's back. Images
remain files/assets; which image, source rectangle, transform and draw order
an entity uses are database-backed component state.

Story 3 moved the interim Tile-owned visuals to referenced entities. Wall and
water entities are independent **occupants** placed on the grid. A
wall's movement restriction does not come from a tile's class or art. A single
water/river entity can occupy many cell positions without becoming many water
entities; that is Story 3. This story makes the tile/art side of the rule true.

## Contradiction this story resolved

The old loader took only the topmost nonempty tile from stacked TMX layers.
Its single-cell `SyncTiles` plan maintained one Tile entity per cell, while
`renderer.TilemapRenderer` drew **all** layer cells from the parsed map file
and never queried those entities. Thus a lower tile could be visible without
an entity, and deleting or changing an entity could leave the picture
unchanged. The game now imports and draws every layer's own Tile entities.

## Acceptance criteria

- [x] Every nonempty tile on every authored layer imports as exactly one Tile
      entity. Identity includes map identity, stable positive layer ID and cell
      coordinate; a reorder changes draw order without replacing entity IDs.
- [x] The schema-backed Tile and referenced entities' visual components contain the data the renderer
      needs: source image/rectangle, position, transform, order, opacity and
      authored visibility. Unknown TMX/TSX XML stays preserved by the writers.
- [x] A hidden layer's Tile entities exist but do not draw. Two nonempty tiles
      stacked in one cell both exist and draw in layer order.
- [x] Saving a tile edit then re-importing updates the existing entity for
      that `(map,layer,cell)`; removing a tile removes **that** entity, not the
      one below it. Runtime-only components on surviving Tile entities remain
      when the project's schema permits attaching them.
- [x] The game renderer draws tile instances from database component state,
      with image files used only as assets. A runtime visual-component change
      or deletion changes the rendered picture, rather than waiting for a TMX
      reload. Forge's unsaved MAP canvas can still preview its working TMX.
- [x] No general `passable` toggle is presented as tile artwork metadata.
      Existing movement stays functional only as an explicit short-lived
      transition until Story 3 replaces the Boolean altogether.
- [x] Tests prove database entity counts/IDs and draw order for two stacked
      layers, reorder/edit/delete, visibility and a visual change at runtime.
      `make test`, both builds and Forge browser checks pass.

## Playwright steps

No new browser UI is added. MAP's existing Playwright tests must still prove
layer stacking and edits after the importer change. A tagged renderer test
must prove the displayed tile comes from an entity's current component state,
not from a static TMX snapshot. Since invalid lower-layer tiles now fail
import even when covered or hidden, a MAP Playwright step checks that the
validation count and message appear on the page and the underlying cell is
marked. A tile whose source rectangle exceeds its image likewise names the
image and marks the cell. The spec keeps an explicit accessibility block.

## Implementation notes

`TileLayer` stores `(map_id, layer_id, layer_order, draw_order)` and
`TileVisual` stores the resolved image path, source rectangle, pixel position,
flips, opacity and authored visibility. `mapId` is read from the map property,
with the cleaned path as fallback, as for spawns. Schema version is now 4.
`AllPlacements` projects hidden and transparent tiles for import; the normal
Forge canvas projection still respects their visual state. A missing or
duplicate layer ID refuses the import before writing anything.

`SyncLayerTiles` diffs by map/layer/cell in one transaction and preserves
surviving entity IDs and schema-permitted runtime-only components. The bundled
Tile type has no optional components; a mod may declare one. A malformed
partial tile refuses re-import rather than silently replacing its identity.
The game renderer queries `TileVisual` on each draw and caches image assets
only; its tagged test records the actual `DrawImage` source rectangle after a
database edit and confirms hidden tiles stop producing draw calls. Adding a `mapId`
adopts path-keyed tiles without replacing their IDs. Pre-layer-identity rows
without any map owner cannot be assigned safely and refuse import with an
explicit migration/recreate message instead of being deleted. MAP's
engine-issue projection reports bad tiles on covered lower layers and invalid
image source rectangles as import refusals.

The topmost tile still supplies `TileGrid`'s old movement Boolean and tile-ID
lookup. The former one-cell `SyncTiles` API remains only for its legacy tests;
Story 3 removes it with `Tile.passable`, `setTilePassable` and the Boolean grid.
`make test`, both builds, both lint tag sets and all 299 Forge browser checks
passed. `internal/tilemap` statement coverage: 91.2%; `internal/tiled`: 94.2%.
