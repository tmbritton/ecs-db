# Story 1: Lossless TSX writer

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** ✅ Complete
**Priority:** High — every later TILES Save depends on preserving the source

## Context

`tiled.ParseTileset` is a reading model; it does not hold a whole TSX. The map
writer's copy-on-write `xtree` already preserves unknown XML verbatim, but its
public `Document` only accepts `<map>` roots. A tileset editor that marshals
`tiled.Tileset` back to disk would delete per-tile collision objects, animation
frames, wangsets, terrains, editor settings, comments and future Tiled fields.
This story makes an external `.tsx` safe to edit before exposing any TILES
save button.

## Acceptance criteria

- [x] Parse an editable external `.tsx` as both a lossless XML tree and a
      `tiled.Tileset` reading model. Unchanged `Bytes()` are byte-identical,
      including comments, spacing, unknown attributes and XML around the root
- [x] Reject `.tsj`, a non-tileset root, XML that cannot be preserved, and a
      tileset the engine's own parser rejects, with a useful reason; no silent
      conversion on Save
- [x] Read a tile by **local** ID, not a map's global firstgid. A sheet tile
      with no `<tile>` metadata can acquire one; sparse collections keep their
      authored IDs and images
- [x] Set a tile's `passable` property surgically, preserving other properties,
      per-tile `<objectgroup>`, `<animation>`, wangsets, terrain and siblings;
      an unchanged assignment writes no new bytes
- [x] Set a tile's type/class without confusing tileset-level class with the
      tile's own, preserving `type` versus `class` spelling where it exists
- [x] Refuse missing/duplicate IDs, invalid property shape, nested values the
      edit cannot preserve, and unsupported edit targets **before** mutation;
      `Tileset()` reads the new value after every successful change
- [x] Fresh Go unit tests include a TSX with unknown XML and a sparse image
      collection; `go test ./...` and the relevant linter/build checks pass

## Notes

- `TilesetDocument` reuses `xtree` and re-parses the engine's `Tileset` from
  working bytes after an edit. An unchanged save is exact; changing an authored
  property or class keeps the original attribute/content spelling and unknown
  descendants. New sheet tile metadata is inserted by local ID; collections
  never invent sparse IDs.
- The existing TMX object-property writer and new TSX tile-property writer now
  share the same nested-value refusals and XML character guard. A bad edit
  changes no bytes. No `.tsj` or embedded tileset is offered a TSX writer.
- Even noncanonical source tags and character references survive child edits;
  only the value that changed is respelled. Conflicting `type`/`class` and
  authored sheet tiles beyond `tilecount` refuse edits instead of silently
  changing a value the game cannot read. Multiple property blocks on one
  element also refuse instead of editing a block the parser does not use.
- Verified: `make test`, both lint tag sets, both builds and `make e2e` (295
  browser checks passing). `internal/tiled`: 94.3% statement coverage.

- This story is data-only. The TILES grid, sessions, routes, file Save and
  Playwright steps belong to the later stories. Editing `passable` in its Go
  tests proves surgical XML mutation of an existing Tiled property; it does
  **not** prescribe a universal passability toggle for the TILES mode. Story
  3's occupant-aware traversal contract supersedes that gameplay rule.
  No browser surface changes here.
- An external TSX may be shared by maps. The writer owns only its bytes; Story
  5 decides who holds it, who saves it and which map previews must refresh.
- TSX polygon collision and Tiled `<animation>` survive edits but have **no
  engine consumer**. Preserve them; do not interpret them as this game's
  collision or sprite-animation contract.
