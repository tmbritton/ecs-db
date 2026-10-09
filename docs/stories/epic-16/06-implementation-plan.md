# Story 6 — TILES read surface: implementation plan ✅ Complete

## Seams already present

- `tilesets.Session.Describe` reads one held lossless TSX or referenced TSJ;
  `tiled.Tileset.SourceRect` owns sheet arithmetic. `tiled.Tileset.Tiles` holds
  the sparse explicit metadata and per-image collection paths.
- MAP's `/forge/asset?path=` serves only allow-listed, project-contained
  images. Reuse it; do not add a general file server or duplicate image
  decoding. Missing assets need honest visible fallback.
- TILES already has a URL-selected file, read-only status, one stream and a
  shell footer. Extend its data/renderer without creating a new save path.

## Test-first order

1. Write table-driven view-model tests for a sheet with margin/spacing,
   implicit cells and authored overrides; sparse collection with IDs 1 and
   21; no tiles; missing image; and a bounded large declared count. Use
   `Tileset.SourceRect` rather than a second arithmetic implementation.
2. Add server tests for selected `?tile=`, invalid/unknown IDs, selected-file
   changes and unchanged session dirty bytes. Keep query selection through
   `streamQuery` as for `?file=`.
3. Add the documented browser checks and Go-side ID pins. Render a scrollable
   grid with anchor selection, a larger art crop and explicit typed metadata;
   add CSS only for the grid and inspector. The sheet source image must pass
   through the existing asset route. For read-only TSJ, no write controls.
4. Deliberately break selection or crop and watch the browser spec fail; run
   `make test`, both builds, both linter tag sets and `make e2e`. Fresh-context
   staged review before commit, then update story/roadmap with coverage and
   browser figures.

## Out of scope

Story 7 supplies tile class/property editing and the grid/inspector patch
split. Story 8 supplies comprehensive image/metadata validation. Neither
Tiled polygon collision shapes nor Tiled animation are game-authoring controls
here. SPRT and `animations.toml` follow in Stories 9–11.
