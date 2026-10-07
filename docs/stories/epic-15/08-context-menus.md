# Story 8: Context menus

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** ✅ Complete
**Priority:** Medium — the prototype's right-click affordances

**Depends on:** Stories 4, 6 and 7

## Context

Right-click menus on layers, on the canvas and on spawns. The prototype hangs
one off almost everything (`ctxLayer`, `ctxQuery`, `ctxEntType`, `ctxTile`,
`ctxSpawn`, `ctxLiveEnt`), and the mode is meaningfully harder to use without
them — most of the operations they carry have no other home in the layout.

Epic 13 Story 5 already built this pattern for the statechart canvas, including
the part that is easy to get wrong: the open menu is server state, so the menu
and the thing it acts on cannot disagree, and closing it is a request like any
other. That story's `Server.canvasMenu` also recorded a real limitation
honestly — it is per server rather than per page, so two browsers on one Forge
see each other's menu. The same limitation applies here and gets the same
treatment: recorded, not dressed up.

## Acceptance Criteria

- [x] A layer menu: rename, toggle visibility, move up, move down, delete
- [x] Layer order changes are file changes, because order decides which tile
      wins — the panel and the engine read it the same way
- [x] A canvas menu on a cell: what it offers is decided by what a cell can
      have done to it that no toolbar button already does
- [x] A spawn menu: at least delete, and duplicate if duplication can allocate an
      id safely — a duplicate that reuses an id retargets a live entity
- [x] The entity-type palette menu is offered only if it has something real to
      do; an empty menu is worse than no menu
- [x] `QUERY LAYERS` has no menu, because it has no feature — it is Epic 20
- [x] The menu is server state, on Epic 13's pattern, and closes on Escape, on a
      click elsewhere, and on the action it performed
- [x] Every entry either does something or is not there. No greyed-out rows
      standing in for later epics
- [x] Deleting a layer says what it costs before doing it: the engine deletes
      every tile the map stops describing, so removing a layer can empty the
      world
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/15-context-menus.spec.js`.

- [x] Right-clicking a layer opens its menu; Escape closes it
- [x] Renaming a layer through the menu dirties the map and the panel updates
- [x] Moving a layer up changes which tile the canvas draws in a stacked cell
- [x] Right-clicking a spawn and deleting it removes it from the canvas
- [x] Clicking elsewhere closes an open menu without performing anything
- [x] Two menus are never open at once

## Notes

- Verified: `make test`, both lint tag sets, both builds and `make e2e` (287
  passing browser checks). Statement coverage: `internal/tiled` 94.2%,
  `internal/forge/server` 86.8%, `internal/forge/templates/modes` 70.7%,
  `internal/forge/mapcanvas` 94.3%.
- Menus use the shared `components.ContextMenu`; a canvas cell picks its
  topmost visible tile and its orientation. An empty cell offers nothing. Layer
  eye changes remain view-only; layer rename, order and deletion edit the TMX.
  Duplication clones the object XML, allocating a new ID and keeping unknown
  authored content. Stable layer IDs keep the eye and paint selection attached
  through a move; an absent selected layer refuses the next stroke. Legacy maps
  without unique layer IDs can paint and rename, but refuse reorder/delete
  until Tiled supplies identities. A stale
  menu cannot mutate a renamed, reordered or removed layer.

- Reuse `templates/components`' menu primitives rather than writing a second
  menu. If the statechart's menu is not reusable as it stands, making it so is
  cheaper than the drift.
- The one genuinely new hazard here is layer deletion. Every other entry is
  reversible with Discard; that one is too, but only until save, and the effect
  at load is 300 tiles rather than the one layer the author thought they removed.
