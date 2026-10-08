# Story 2 — Entity-backed tile layers: implementation plan

## Verified seams before implementation

- `tilemap.tilesOfTiled`/`cellAt` choose one topmost tile per `(x,y)`;
  `SyncTiles` diffs `map[Point]TileState`. `comp_tile` currently records
  `(x,y,passable,tile_type)` and `TileGrid.entityIDs` indexes one ID at a
  point. All four encode the one-tile-per-cell assumption. Import must walk
  every nonempty cell on every layer and key identity by `(mapId,layerID,x,y)`.
- Epic 15 Story 8 already requires unique positive layer IDs before moving or
  deleting layers. This story should validate that identity for import rather
  than invent a fallback index that reassigns entity IDs on reorder. No old
  map corpus needs migration, but tests/fixtures and the example schema do.
- `tiled.DrawList` already resolves image and flip/translation geometry in
  layer order. Prefer one projection of those facts into Tile visual component
  values, not renderer-side parsing of a second copy of the map. File-backed
  PNGs are assets; per-instance draw state must come from component rows.
- `renderer.TilemapRenderer` currently builds a static Ebiten image from
  `*tiled.Map`. `Game.Draw` reads actor sprites from Position/Sprite, but
  never reads Tile entities for its map picture. The renderer change has an
  `ebitengine` build tag, so plain `go test ./...` cannot prove it compiles.
- Movement and LOS still read `TileGrid.IsPassable`; Story 3 removes this. To
  keep Story 2 independently runnable, any interim Boolean projection must
  be explicitly keyed to the topmost tile and must not become a TILES UI
  contract. Keep TileGrid's bounds and Tile ID lookup distinct from Tile
  drawing and future occupant lookup.

## Red–green order

1. Table-drive import projection over a 2×2 TMX with two nonempty overlapping
   layers, one hidden layer, flips and a missing tile. Assert one authored
   nonempty tile means one planned entity, and no deduplication by `(x,y)`.
2. Test stable identity across painting, changing art, reordering layers and
   deleting one stacked tile. The surviving `(mapId,layerID,x,y)` keeps its
   entity ID and runtime-only components; the other layer is never silently
   retargeted. Then change `SyncTiles`, the Tile schema and data fixtures.
3. Test a database-backed draw projection: source image/rect, transform,
   opacity, visibility and layer order. Change/delete a Tile visual component
   **after** import and assert the next picture changes without rereading
   TMX. Adapt `TilemapRenderer` to consume the projection, retaining its
   image cache as an asset cache. Verify with tagged tests/build and a renderer
   smoke test, not only DOM markup.
4. Keep current player movement/guards working until Story 3's separate
   migration by explicitly choosing the current topmost entity where the old
   Boolean path still needs one. Test that temporary rule and remove it in
   Story 3; never let it decide which layer entities exist.
5. Run `make test`, both lint tag sets, both builds and `make e2e` for MAP
   regressions. Record coverage, request fresh-context staged review, fix
   findings, repeat affected checks and commit before Story 3.
