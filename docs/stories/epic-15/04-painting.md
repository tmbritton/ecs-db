# Story 4: Painting, server-side

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** 🔲 Not started  
**Priority:** High — the thing the mode is for

**Depends on:** Story 3

## Context

Stamp, rectangle fill, erase, and the rotate/flip that the stamp carries. Every
one of them is an operation on the map session: given a map, a layer, a cell and
a tile, produce the map that results. That makes the whole of painting testable
in Go, against a map value, with no browser and no pointer — which is the point
of doing it before Story 5.

The tools are the four in the prototype's toolbar (`🖌` stamp, `▧` rect, `⌫`
eraser, `⛶` select) plus rotate and flip. Rotate and flip are not new tiles:
Tiled packs them into the top bits of the gid, and Epic 14's parser already
splits them out into `Tile.FlipH/FlipV/FlipD` — so painting a rotated tile is
writing a gid with flags, and the renderer already draws them (Epic 14 Story 5
fixed the diagonal-flip case).

## Acceptance Criteria

- [ ] Stamp writes the selected tile into a cell on the active layer
- [ ] Rect fill writes it across a rectangle, inclusive of both corners, in any
      drag direction
- [ ] Erase writes gid 0, which is a genuinely empty cell and not "the tile that
      looks like nothing"
- [ ] Rotate and flip set the transform flags on the gid, using Epic 14's four
      flag bits — including the hexagonal-rotation bit, which the widely quoted
      three-bit mask leaves in the id
- [ ] An edit is refused with a reason, never silently clamped, when the cell is
      outside the map or the layer does not exist
- [ ] An edit that changes nothing writes nothing and does not dirty the map —
      stamping the tile a cell already holds is a no-op, and a footer that says
      `● unsaved` after it teaches you to ignore the footer
- [ ] A drag arrives as **one** operation, not one per cell, so it is one entry
      in the session's history and one patch on the wire
- [ ] Painting a layer that the panel is hiding is refused rather than done
      invisibly
- [ ] The whole of painting is unit-tested against map values; the browser is
      Story 5's problem
- [ ] Undo/redo is decided explicitly. Discard reverts to the last save and is
      the coarse answer; if per-operation undo is out of scope, the story says
      so and the UI does not pretend otherwise
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/15-painting.spec.js` — thin, because the maths is proven in Go.

- [ ] Selecting the stamp tool marks it active and the others inactive
- [ ] Clicking a cell paints the palette's selected tile there and the footer
      flips to `● unsaved`
- [ ] The eraser clears a painted cell
- [ ] Rotate marks the stamp rotated and the painted cell renders rotated
- [ ] Save writes the file; re-opening the map shows the same picture
- [ ] Painting the tile a cell already holds leaves the footer clean

## Notes

- **The layer a stroke lands on is the active layer and nothing else.** The most
  common way to lose an hour in a tile editor is painting into the layer you
  were not looking at; the panel showing which one is active is not decoration.
- **Rect fill and the select tool are different gestures with the same drag.**
  Deciding which one a drag means belongs to the tool state, on the server, not
  to the JS — Story 5 reports "a drag happened from here to here" and this story
  decides what that means.
- Story 1's contract holds: a paint operation rewrites the layer's `<data>` and
  touches nothing else in the document.
