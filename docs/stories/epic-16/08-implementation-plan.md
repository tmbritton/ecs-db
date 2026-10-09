# Story 8 — TILES validation: implementation plan ✅ Complete

1. Write failing table-driven `tilesetvalidation` tests for image availability
   and source rectangles, sparse collection missing art, unknown entityType,
   deprecated passable and malformed typed property values. Reuse the same
   `tilesurface.View` the grid renders. Keep work bounded to the visible page
   plus selected tile.
2. Add a lossless `TilesetDocument` metadata inspection for authored Tiled-only
   XML features using its parsed tree rather than string searching comments.
   Test bytes survive Story 7 mutations; TSJ gets a general read-only warning.
3. Wire validation from the selected session value and current schema to TILES
   grid/inspector/header, with stable IDs and status text. Preserve failed-file
   discovery from Story 5, retain Save for repairable files and associate edit
   refusals with class/property controls.
4. Add the browser checks above, pin IDs in Go, break a warning deliberately
   and watch the browser check fail. Run `make test`, both builds, both lint
   sets, `make e2e`; obtain a fresh staged review, update roadmap and commit.
