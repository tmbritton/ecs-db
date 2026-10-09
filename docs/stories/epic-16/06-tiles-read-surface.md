# Story 6: TILES read surface

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** ✅ Complete
**Depends on:** Story 5 (one shared project TSX session and project-scoped image route)

## Contract

The selected tileset's *working* reading model supplies a usable sheet or
collection grid and an enlarged selected tile. This is inspection, not a new
file writer. File selection belongs to Story 5; tile selection is URL-addressed
so a deep link, back/forward, refresh and one page-level stream agree.

## Acceptance criteria

- [x] A sheet shows its `tilecount` implicit local IDs using the tileset's own
      columns, margin, spacing and tile size. It uses the existing project-
      contained image route; a missing/unsafe image appears as an explicit
      missing-art problem, not a guessed filesystem URL. Huge invalid counts
      do not turn a page request into an unbounded HTML allocation.
- [x] A collection shows only explicit, sorted local IDs, including sparse
      gaps, from the individual tile images. No phantom cells, sheet math or
      contiguous re-numbering.
- [x] Selecting a tile displays a larger, properly cropped image, local ID,
      tile-level class/type and typed properties, plus tileset-level name,
      class, dimensions, image/offset/spacing and source file. An implicit
      sheet tile may have no explicit metadata; say so. Metadata is displayed
      as authored, without claiming that artwork properties alone govern
      movement or visibility.
- [x] Unknown/invalid `?tile=` does not silently retarget to a different tile;
      an explicit problem is shown. No tiles, unreadable files and read-only
      `.tsj` stay usable states. Tile selection does not change dirty state
      or write a TSX/TMJ; switching MAP↔TILES retains the selected file.
- [x] The browser suite checks image loading and real crop pixels for a sheet
      and a sparse collection, keyboard selection, URL/back/forward behavior,
      no mutation requests, absent artwork and explicit accessibility.
      Test IDs are pinned on the Go side. Run `make test`, both builds/lint tag
      sets and `make e2e`; deliberately break the image crop or tile selection
      and confirm the browser check fails.

## Playwright steps — browser-only evidence, before code

1. On the e2e sheet, click local IDs 0 and 13. The selected marker, URL and
   larger image change; a sampled pixel differs, and the image request returns
   a project image with `image/png`. Back/forward restores the correct tile.
2. Select a sparse collection with nonconsecutive IDs. Its rows list those
   exact IDs and clicking a tile uses its own image, not the sheet image.
3. Deep-link to an unknown local ID, then a valid one. The former reports a
   problem and selects nothing; the latter renders the requested tile without
   changing `save-footer` dirty state or posting a mutation.
4. With an unreadable image or unsafe symlink, the grid and inspector show
   missing art and the asset route refuses the image. Include an explicit
   `accessibility` block for named grid, keyboard links, selected state and
   meaningful image alternative.

## Verification

- `make test` including race checks and tagged vet, both builds, both direct
  linter tag sets and `make e2e` pass (**321 browser checks**). The `make lint`
  wrapper's nested mise selects a mismatched Go compiler, so its two linter
  commands were invoked with the pinned Go 1.26.7 and linter directly.
- Deliberately replacing the sheet's source rectangle with `(0,0)` caused the
  browser pixel-difference check to fail; restoring the crop passed.
- Statement coverage: `internal/forge/tilesurface` **97.0%**,
  `internal/forge/maps` **87.0%**, `internal/forge/server` **86.7%**.
