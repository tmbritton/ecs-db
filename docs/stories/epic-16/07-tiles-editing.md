# Story 7: TILES tile metadata editing

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** ✅ Complete
**Depends on:** Stories 1, 5 and 6

## Contract

TILES edits only the selected writable `.tsx` through its one shared lossless
`TilesetDocument`. The first supported edits are a tile's `class`/`type` and
typed tile properties (including `entityType` art-template metadata); sheet
tiles can gain a first explicit `<tile>`, while a collection exposes only IDs
it already declares. An image source remains an inspected filename until the
image-import workflow; polygon collision shapes and Tiled animations are not
gameplay authoring controls.

## Acceptance criteria

- [x] A selected TSX tile can change/clear class without losing its existing
      spelling (`class` versus `type`), unknown XML, collision objects,
      animations, wangsets or terrain. Typed properties can be added/updated,
      preserving unrelated metadata; no-op edits leave the session clean.
      Refuse absent collection IDs, invalid local IDs, invalid property names,
      unsafe XML characters and unsupported/ambiguous types visibly.
- [x] The inspector shows the working metadata immediately and the tile grid
      reflects class changes. Grid and inspector are disjoint patch targets
      with stable IDs and remain stable over a page-stream update. A `.tsj`
      and unavailable TSX have no mutation controls; a crafted route cannot
      write either or target an unreferenced file.
- [x] Dirty state and the footer follow the selected shared TSX. Save writes
      the lossless file once, Discard restores the saved snapshot, Reload takes
      an external change, and conflict offers Reload/Overwrite for *that* file.
      Other mode footers report unsaved tileset work. Both MAP previews of a
      shared TSX show its working metadata before Save; saving takes effect
      in the game only on the next `ecs-db run`.
- [x] Browser checks exercise class/property mutations by real controls and
      POSTs, save/discard/reload/conflict effects, shared MAP previews, clean
      read-only behavior and accessibility. Go tests cover validation and
      copy-on-write XML preservation. `make test`, builds, both lint tag sets
      and `make e2e` pass; deliberately break one edit handler to prove the
      browser spec fails.

## Playwright steps — browser-only evidence, before code

1. On a shared TSX, focus the class field for tile 13 and change its class.
   Observe exactly one POST, the selected inspector/grid change, a dirty
   footer and both referencing MAP previews using the unsaved class.
2. Add/change a typed property to an implicitly authored sheet tile. Observe
   the correct value/type immediately, Save writes just the selected TSX,
   Reload after Save preserves it, and Discard restores the saved metadata.
3. Edit the TSX in Tiled after opening it; press Save to see conflict, then
   Reload to take disk or Overwrite to keep the working value. Confirm the
   affected path is the one on screen and unrelated work survives.
4. Try a `.tsj`, an unknown ID, a bad property and a guessed TSX path. Read-
   only offers no editable input; refusals preserve the current bytes and
   explain why. Include an explicit `accessibility` block for named inputs,
   keyboard submit and conflict resolutions.

## Verification

- `make test` (race tests and tagged vet), both builds, both direct linter tag
  sets and `make e2e` pass (**327 browser checks**). The nested mise call in
  `make lint` selects a mismatched Go compiler; its two linter commands were
  run directly with the pinned Go 1.26.7.
- Deliberately replacing the class edit with an empty value made the browser
  spec fail on the unchanged class. Restored before the full suite.
- Statement coverage: `internal/forge/tilelinks` **82.6%**,
  `internal/forge/server` **86.8%**, `internal/forge/tilesets` **86.4%**.
