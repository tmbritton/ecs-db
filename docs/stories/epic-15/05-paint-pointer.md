# Story 5: Painting, the pointer surface

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** ✅ Complete
**Priority:** High — without it the mode is unusable, with it the mode is done

**Depends on:** Story 4

## Context

The second and last hand-written client-JS surface in Forge. It exists for the
reason `canvas.js` does and no other: a stamp under a dragging pointer has to
follow it, and a round trip per cell is not a design anyone would defend.

`internal/forge/web/static/js/canvas.js` is the precedent and the contract to
copy. Its opening comment states the rule this story inherits: *it owns pointer
state and stops there. It never fetches, never renders, and does not know what a
machine is.* Replace "machine" with "map". On stroke end it dispatches one
`CustomEvent` carrying where the stroke started and ended, and a Datastar
expression turns that into the same `@post` every other control in Forge uses.

It also inherits `canvas.js`'s testability split: the arithmetic — pixels to
cells, a drag to a rectangle — is a pure exported function tested in Go's
neighbour, `node`, leaving Playwright to prove the wiring rather than the sums.

## Acceptance Criteria

- [ ] Click paints one cell; drag paints a stroke and posts once on release
- [ ] The ghost stamp follows the pointer and shows the selected tile with its
      current rotation, as the prototype does
- [ ] Rect fill previews its rectangle while dragging
- [ ] The JS holds no model of the map — it knows pixels, cell size and element
      roles, and nothing about tiles, layers or gids
- [ ] Pixel→cell conversion is a pure exported function with its own tests, the
      way `dragResult` is
- [ ] A drag released outside the canvas is a drag abandoned: nothing posted,
      nothing changed, exactly as Escape does
- [ ] A drag that starts and ends in the same cell with no movement is a click,
      not a zero-length stroke that dirties the file
- [ ] Bound once from the document, not per patch — the mode content is replaced
      by the page stream whenever anything changes, and a module that re-attached
      on each patch is how you get two handlers and two requests per stroke
- [ ] Grid and snap toggles work and are view state, not file state
- [ ] Keyboard shortcuts for the tools (`B` `R` `E` `M`, `Q` rotate, `X` flip)
      match the prototype's tooltips
- [ ] `go test ./...` and the JS unit tests pass

## Playwright steps

`e2e/specs/15-paint-pointer.spec.js`.

- [ ] Dragging across four cells paints four and issues exactly one request
- [ ] The ghost stamp appears on hover and tracks the pointer
- [ ] A drag released over the layer panel paints nothing
- [ ] Rect fill previews while dragging and commits on release
- [ ] After a patch arrives on the stream, the next drag still paints once — the
      re-attachment bug, asserted rather than hoped for
- [ ] The tool shortcuts select the tools
- [ ] Painting works after switching maps and back

## Notes

- **Two hand-written surfaces is the budget.** The README of Epic 13 said this
  one was coming and named it; nothing after this epic gets a third without a
  reason as concrete as "pointer speed".
- The canvas keeping its own model of the map is the failure to guard against —
  it is a second source of truth, and the first thing it will disagree with is
  the file on disk. Every acceptance criterion above that looks like a style
  preference is that rule wearing a different hat.
