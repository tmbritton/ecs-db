# Story 5: Statechart canvas — direct manipulation

**Epic:** 13 — Forge: AGENTS mode  
**Status:** ✅ Complete  
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

- [x] Dragging a node moves it, and on release the position is persisted where
      Story 1 decided and the canvas is redrawn from the server
- [x] A drag that is cancelled — Escape, or released outside the canvas — leaves
      the machine and the layout untouched
- [x] Dragging from a node's port to another node creates a transition, and the
      server decides what event it carries rather than the client inventing one
- [x] A connection dropped on empty space, or on the node it started from, is
      discarded without a request
- [x] Double-clicking empty space adds a state at that position, with a name
      that does not collide
- [x] Right-click opens the prototype's menus: rename, set as initial, delete on
      a node; delete on an edge; add state on the canvas
- [x] Deleting a state that other transitions target asks first and says which
      transitions will dangle
- [x] Every mutation goes through the Story 2 session. The canvas may not write
      a file, and may not call `EmitMachine`
- [x] Nothing is lost when two edits arrive close together: the second is applied
      to the result of the first, not to the state the client last saw
- [~] The canvas is operable without a pointer, or its absence is stated
      explicitly with what replaces it — **the menus are; dragging is not, and
      says so.** See *What a pointer is still needed for* below
- [x] With JavaScript unavailable the canvas still renders and still selects,
      because Story 4 made both server-side
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/13-canvas-editing.spec.js`. This is the story whose failures are
invisible everywhere else: a drag handler that never binds renders perfectly and
does nothing, which is precisely the failure mode the suite was built for.

- [x] Dragging a node changes its persisted position — asserted from the file on
      disk, not from a CSS transform
- [x] Dragging and pressing Escape changes nothing on disk
- [x] Dragging from a port to another node creates a transition that appears in
      the file
- [x] Dropping a connection on empty space creates nothing, and issues no request
- [x] Double-clicking empty space adds a state, and it appears in the file
- [x] Right-clicking a node opens the menu, and "set as initial" moves the `◉`
- [x] Deleting a state that is a transition target warns, and cancelling keeps it
- [x] Ten drags in a row leave ten consistent positions, not nine and a lost one
- [x] Exactly one request per completed drag — a handler bound twice is
      indistinguishable from one bound once until you count
- [x] The page reports no console errors throughout, which the suite's shared
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

## As Implemented

One JS module, 162 lines, and it turned out to need less than expected. Three of
the five interactions need none of it: double-click, both context menus and
every menu item are Datastar expressions, because `evt` is already in scope
there. Only the two drags need pointer state.

### The boundary, settled by a probe rather than by argument

The module owns pointer state and stops. It never fetches, never renders, and
does not know what a machine is; on drop it dispatches one `CustomEvent`
carrying the *result*, and a `data-on:canvasdrop` expression turns that into the
same `@post` every other control in Forge uses.

That shape was chosen after checking in a browser that Datastar binds a custom
event at all and that `evt.detail` survives it. It does — hyphenated names too,
and floats and strings arrive whole. Without that, the alternatives were JS
calling `fetch` and then either ignoring the SSE patches in the response or
reimplementing the part of Datastar that applies them.

**Bound once, from the document.** The mode content is replaced by the page
stream whenever anything changes, so a listener attached to a canvas element
dies with it — and a module that re-attached on each patch is how you get two
handlers and two requests per drag. The browser test counts requests for exactly
that reason: a handler bound twice is indistinguishable from one bound once
until you count.

### The drag posts a delta

A node's rendered position is its recorded one plus up to three offsets: the
chart's own normalisation shift, and for a nested node its parent's inner margin
and title height. The client knows none of that and should not learn it.

So the drag posts `dx`/`dy`, and the server adds it to what the file records.
The chart gained `Node.RecordedX/RecordedY` — the coordinate that, written into
`meta`, reproduces where the node is now — captured before either offset is
applied, so a node the fallback grid placed has one too and its first drag
writes a position rather than starting from zero.

### What the browser caught that Go could not

**The right-click menu blocked the whole application.** Its backdrop is
`fixed; inset: 0`, so clicking anywhere closes it — and the menu is per-server
state, like the edit-problem banner. One left open in another tab covered this
page's mode rail, machine list and menu bar the moment it loaded, with no way
through but to click it away. It is cleared on a full page load now, on exactly
the terms a refused edit already was.

**I reintroduced Story 4's defect with a different element.** Double-click and
right-click needed a full-canvas target, so I added one — `position:absolute;
inset:0` above the ground link, which then swallowed every click meant for it and
broke deselection. That is the same bug the node layer had, found the same way,
one story later. They are one element now: a link when there is a selection to
clear, a plain box when there is not, so an empty canvas still offers nothing to
anything that reads the page aloud.

### What the review caught

Eight defects, and two of them made an acceptance criterion false while it was
ticked.

**Two drags arriving together lost one.** The move handler read the recorded
position through one acquisition of the session lock and wrote position+delta
through another — a read-modify-write across two critical sections. Eight
concurrent drags lost five of them. The plan's own verified table said the
interleaving criterion "is satisfied by using `Edit` and nothing else", and the
move path was precisely the one that did not: it read first. `Session.MoveBy`
now does the read and the write inside one edit, and a test reproduces the old
shape losing fourteen drags out of twenty-four. `AddState` had the same shape
for the canvas's own offset and was fixed the same way.

**A move released outside the canvas persisted.** The criterion says Escape *or*
a release outside the canvas leaves the machine and the layout untouched; only
the Escape half existed. Dropping a node on the mode rail wrote a large negative
coordinate, which then made the chart shift everything to bring it back on
screen — moving every other node on the canvas as a side effect of a drag that
was meant to be abandoned.

**A connection could only be dropped on a target's title bar.** The node layer
lets clicks through everywhere except a header and a port, so
`elementFromPoint` answered "the canvas ground" for the rest of every box — and
for the entire interior of a compound state, which could not be connected to at
all. The browser test dropped at `y + 18`, inside the 26-pixel header: a fixture
tuned to the one region that worked. `pathAt` falls back to the boxes' own
rectangles now, innermost first.

**Renaming a compound state left every path through it dangling.** `retarget`
matched the old name and the old path exactly, so a target of
`combat.attacking` survived `combat` being renamed to `battle` and stopped
resolving. The rewrite is done by **where each transition resolved before the
rename** rather than by how it was spelled — which also scopes it, so a
transition that meant a different state of the same name is left alone. The test
that was meant to cover the second case had a false premise, and finding that
out was worth more than the test: *this engine resolves a bare target from the
machine root*, not relative to the source as real XState does. The property is
now stated as it actually is — every transition still resolves where it resolved
before — and the divergence from XState is written down.

**The right-click menu blocked the whole application for two seconds.** Its
backdrop was `position: fixed; inset: 0`, and dismissing it answered 204 and
waited for a stream tick — so the click that dismissed it was swallowed and the
mode rail, machine list and menu bar were unreachable until the redraw arrived.
There is no backdrop now: `data-on:click__window` and `data-on:keydown__window`
close it without covering anything.

**The keyboard claim was overstated.** Right-clicking is reachable — browsers
fire `contextmenu` for the menu key on the focused element — but focus never
moved into the menu, and Escape did nothing, so reaching "Set as initial" meant
tabbing past every remaining node and edge label with no way to back out. Both
work now. `autofocus` turned out to be inert here: the browser honours it only
for elements the HTML parser inserted, and everything the page stream delivers
is built with DOM APIs, so the attribute rendered perfectly and did nothing —
the house failure mode, in a new place. `static/js/autofocus.js` makes it mean
what it says in patched content.

**Cancelling the rename prompt raised an error.** `prompt(...) || ''` posted the
empty string, which the name check refused, so backing out of a dialog produced
a red banner and a line in the log.

**A `pointercancel` left the drag running.** Touch and pen input, a
browser-initiated scroll, and the OS taking the pointer all end a gesture that
way; without handling it the node went on following the cursor and the next
click anywhere posted the accumulated move. The comment beside the listeners
also claimed pointer capture that was never taken — it is taken now, on the
first movement rather than on pointer-down, because capturing straight away
retargets the events and the click that selects a state never fires.

### What the mutation battery caught

Five test gaps and two assertions looking at the wrong thing. Three of the five
share a shape worth naming: **a fixture where two different rules give the same
answer**.

- The rename test renamed the *last* state and checked it was still last, which
  cannot tell "kept in place" from "moved to the end".
- The rename-follows-targets test used a top-level state, where the bare name and
  the dotted path are the same string — so the branch that rewrites bare targets
  was never exercised, and deleting it changed nothing.
- The refusal test for an unmergeable `meta` asserted only that it errored.
  Reading the key order of a non-object errors too, so a version with no idea why
  it failed passed it. It now asserts the sentence that says what to do.

The other two were simply missing: nothing covered adding a state to a machine
with no initial, or deleting the state the initial names. Both leave a machine
that will not load, from an edit the author did mean.

And one assertion was on the whole page rather than on the element: the edge
menu's four parameters appear in every edge's own context-menu action too, so
searching the document passed with the menu carrying none of them.

The battery ended at 58 mutations, all caught. One more finding came out of it  rather than out of the code: a mutation
that moved a read inside the lock that already held it **deadlocked**, and the
harness waited out the Go test timeout for it. Each run is bounded now, and a
hang is reported as a mutation caught — which it is.

### What a pointer is still needed for

The AC marked `[~]`. Right-clicking is reachable without a pointer — browsers
fire `contextmenu` for the keyboard menu key on the focused element, and every
node head is a link — so rename, set-as-initial and delete are all operable from
the keyboard, which matters because until Story 6 they live nowhere else.

What is not: **dragging**. Moving a node has no keyboard equivalent, and neither
does drawing a connection. Moving is cosmetic — a position is layout and nothing
in the machine depends on it. Drawing a connection is not, and its keyboard
route arrives with Story 7's transition inspector, where a target is a dropdown
of the machine's states. Until then, creating a transition needs a pointer, and
that is stated here rather than implied by silence.

Adding a state is reachable both ways: double-click empty space, or the canvas
menu's "Add state here" from the keyboard menu key.

## Left for later

- **The menu is per-server, not per-page.** Two browsers on one Forge see each
  other's menu open and close. It is cleared on page load, and it no longer
  covers anything, so a stray one is now a curiosity rather than a blockage —
  but two tabs held open still share one. The same limitation `editProblem` has
  carried since Story 2; both want the same fix.
- **Double-clicking a node's body adds a state on top of it.** The coordinate is
  where the pointer was, which is inside the box that was clicked. Same root as
  the parenting note below: the canvas has no notion of adding *into* something.
- **`freeEventName` scans `on` keys only.** XState allows a named delay as an
  `after` key, which would be a second namespace to avoid — but this engine runs
  every `after` key through `ParseDurationMs`, so a machine with one never
  loads and the session never holds it. A guard for it would be code no test
  could reach.
- **A move is refused on a state whose `meta` is not an object**, rather than
  silently replacing it. The refusal explains itself; there is no way to fix
  such a state from the canvas, only from a text editor.
- **An edit lands on screen within a stream tick**, up to two seconds, because
  the handlers answer 204 and the page stream redraws. The dragged box stays
  where it was dropped in the meantime, so a move looks immediate; an added
  state does not appear until the tick.
- **`AddState` places top-level states only.** Double-clicking inside a compound
  state adds a sibling of that compound, not a child of it. The coordinate is
  right; the parent is not.
- **A transition's actions and guard are not editable here** — Story 7's job.
  The canvas creates a bare transition and names its event.
- **`DeleteState` leaves the transitions that targeted it dangling**, on
  purpose: Story 4 draws a dangling edge rather than dropping it, and removing
  the transition would take its actions and its guard with it as a side effect of
  deleting something else. The confirmation names them first.
- **Rename is a `prompt()`**, like the confirmations are `confirm()`. Epic 17's
  dialog set replaces both.
