# Story 5: Statechart canvas — direct manipulation

**Epic:** 13 — Forge: AGENTS mode  
**Status:** 🔲 Not started  
**Priority:** High — the interaction the mode exists for

**Depends on:** Story 4

## Context

The prototype's caption is the specification: *drag a state to move · drag the
amber port → another state to connect · double-click empty space to add ·
right-click for options*.

This is the first hand-written client-JS surface in Forge, and it is here for a
reason that does not generalise: a node under the pointer has to move at pointer
speed, and a server round trip per pixel is not a design anyone would defend.
That is the whole of the exception. **The JS owns pointer state and nothing
else.** Where a node is *while you are dragging it* is the client's business;
what the machine *is* stays on the server and comes back as a patch, exactly
like every other panel. A canvas that keeps its own model of the statechart is a
second source of truth, and the first thing it will disagree with is the file on
disk.

The practical shape: pointer-down starts a drag and the node follows the cursor
locally; pointer-up posts the result once and the server answers with the
redrawn canvas. Between those two events nothing is authoritative on the client
except a pair of coordinates.

## Acceptance Criteria

- [ ] Dragging a node moves it, and on release the position is persisted where
      Story 1 decided and the canvas is redrawn from the server
- [ ] A drag that is cancelled — Escape, or released outside the canvas — leaves
      the machine and the layout untouched
- [ ] Dragging from a node's port to another node creates a transition, and the
      server decides what event it carries rather than the client inventing one
- [ ] A connection dropped on empty space, or on the node it started from, is
      discarded without a request
- [ ] Double-clicking empty space adds a state at that position, with a name
      that does not collide
- [ ] Right-click opens the prototype's menus: rename, set as initial, delete on
      a node; delete on an edge; add state on the canvas
- [ ] Deleting a state that other transitions target asks first and says which
      transitions will dangle
- [ ] Every mutation goes through the Story 2 session. The canvas may not write
      a file, and may not call `EmitMachine`
- [ ] Nothing is lost when two edits arrive close together: the second is applied
      to the result of the first, not to the state the client last saw
- [ ] The canvas is operable without a pointer, or its absence is stated
      explicitly with what replaces it — the mode rail, the menus and the
      inspectors are all keyboard-reachable today and this is the one surface
      that could quietly stop being
- [ ] With JavaScript unavailable the canvas still renders and still selects,
      because Story 4 made both server-side
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/13-canvas-editing.spec.js`. This is the story whose failures are
invisible everywhere else: a drag handler that never binds renders perfectly and
does nothing, which is precisely the failure mode the suite was built for.

- [ ] Dragging a node changes its persisted position — asserted from the file on
      disk, not from a CSS transform
- [ ] Dragging and pressing Escape changes nothing on disk
- [ ] Dragging from a port to another node creates a transition that appears in
      the file
- [ ] Dropping a connection on empty space creates nothing, and issues no request
- [ ] Double-clicking empty space adds a state, and it appears in the file
- [ ] Right-clicking a node opens the menu, and "set as initial" moves the `◉`
- [ ] Deleting a state that is a transition target warns, and cancelling keeps it
- [ ] Ten drags in a row leave ten consistent positions, not nine and a lost one
- [ ] Exactly one request per completed drag — a handler bound twice is
      indistinguishable from one bound once until you count
- [ ] The page reports no console errors throughout, which the suite's shared
      fixture already enforces

## Notes

- **Post the result, not the journey.** One request on pointer-up, carrying the
  final coordinates. A request per pointer-move is a request per pixel and will
  also arrive out of order.
- Datastar actions post the page's signals. A drag's coordinates are per-node
  and transient, which is exactly the shape that does not belong in a signal —
  Epic 12's fields table learned the same lesson from the other direction. Send
  them explicitly.
- The `evt` and `el` bindings are in scope in every Datastar expression, which
  is enough for click and context-menu wiring; only the drag itself needs real
  JS. Keep the boundary there rather than writing a canvas controller because
  one part of it needed a listener.
- Whatever JS is written is vendored and served from the embedded asset tree,
  like the Datastar bundle. Forge works offline; nothing here may reach for a
  CDN.
- Test the JS. A module with a pure "given a drag from A to B, what should be
  posted" function is testable without a browser, and leaves Playwright to prove
  the wiring rather than the arithmetic.
