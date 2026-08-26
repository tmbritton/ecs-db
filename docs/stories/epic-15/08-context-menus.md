# Story 8: Context menus

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** 🔲 Not started  
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

- [ ] A layer menu: rename, toggle visibility, move up, move down, delete
- [ ] Layer order changes are file changes, because order decides which tile
      wins — the panel and the engine read it the same way
- [ ] A canvas menu on a cell: what it offers is decided by what a cell can
      have done to it that no toolbar button already does
- [ ] A spawn menu: at least delete, and duplicate if duplication can allocate an
      id safely — a duplicate that reuses an id retargets a live entity
- [ ] The entity-type palette menu is offered only if it has something real to
      do; an empty menu is worse than no menu
- [ ] `QUERY LAYERS` has no menu, because it has no feature — it is Epic 20
- [ ] The menu is server state, on Epic 13's pattern, and closes on Escape, on a
      click elsewhere, and on the action it performed
- [ ] Every entry either does something or is not there. No greyed-out rows
      standing in for later epics
- [ ] Deleting a layer says what it costs before doing it: the engine deletes
      every tile the map stops describing, so removing a layer can empty the
      world
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/15-context-menus.spec.js`.

- [ ] Right-clicking a layer opens its menu; Escape closes it
- [ ] Renaming a layer through the menu dirties the map and the panel updates
- [ ] Moving a layer up changes which tile the canvas draws in a stacked cell
- [ ] Right-clicking a spawn and deleting it removes it from the canvas
- [ ] Clicking elsewhere closes an open menu without performing anything
- [ ] Two menus are never open at once

## Notes

- Reuse `templates/components`' menu primitives rather than writing a second
  menu. If the statechart's menu is not reusable as it stands, making it so is
  cheaper than the drift.
- The one genuinely new hazard here is layer deletion. Every other entry is
  reversible with Discard; that one is too, but only until save, and the effect
  at load is 300 tiles rather than the one layer the author thought they removed.
