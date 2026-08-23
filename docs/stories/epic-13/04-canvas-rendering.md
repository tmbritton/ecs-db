# Story 4: Statechart canvas — rendering

**Epic:** 13 — Forge: AGENTS mode  
**Status:** ✅ Complete  
**Priority:** High — the centre of the mode

**Depends on:** Stories 1 and 3

## Context

The canvas draws the machine: a node per state, an edge per transition, the
initial state tagged, entry actions listed under the node name, and the whole
thing laid out at the coordinates Story 1 decided where to keep.

This story renders it and lets you select things. It does not let you move
anything. That split is deliberate: a chart that draws the wrong edges and a
chart you cannot drag fail in completely different ways, and only the second
needs a line of JavaScript. Getting the rendering right first means the story
that adds pointer handling is adding it to something already known to be
correct — and if the chart is wrong, that is found in `go test` rather than
through a browser.

Everything here is therefore server-rendered and patched down the page stream,
like every other panel in Forge. Selection is a URL, the same as choosing a
component in SCHEMA: it survives a reload, it can be linked, and the inspectors
in Stories 6 and 7 read it from the request rather than from client state.

## Acceptance Criteria

- [x] A node per state, drawn from the machine, with its name and its entry
      actions
- [x] The initial state carries the prototype's `◉` tag
- [x] An edge per transition, labelled with its event; `after` transitions
      labelled with their duration, since that is what the key is
- [x] A transition with a `cond` is visually distinguishable from one without —
      a guard is the difference between "this happens" and "this might"
- [x] Compound states render their children nested, because the machine is a
      tree and a flat drawing of it is a different machine
- [x] History nodes are drawn as history nodes, not as ordinary states
- [x] Layout comes from wherever Story 1 put it; a machine with no layout yet is
      laid out by a deterministic fallback, so two renders of an unpositioned
      machine agree
- [x] Selecting a node or an edge is a URL parameter, resolved server-side, and
      survives a reload
- [x] Exactly one thing is selected at a time, and what is selected is readable
      from the DOM rather than only visible
- [x] Two renders of one machine are byte-identical — the chart is on a 2-second
      stream with identical patches suppressed, so any map-iteration order
      leaking into the output both flickers the canvas and defeats the
      suppression
- [~] A machine that does not validate still draws. This is the editor for
      fixing it — **the canvas does; the project resolver still will not open a
      machine that fails validation on disk.** See *A machine broken on disk
      cannot be opened at all* below
- [x] A transition whose target does not exist draws as a dangling edge rather
      than being dropped — a transition you cannot see is one you cannot fix
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/13-canvas-rendering.spec.js`.

- [x] The fixture's machine draws its two states and the transitions between
      them, and the node count matches the machine
- [x] The initial state is marked, and it is the one the file names
- [x] Clicking a node selects it, the URL changes, and a reload keeps it selected
- [x] Clicking an edge selects the edge, not the node under it
- [x] Selecting one thing deselects the other
- [x] An edit made elsewhere redraws the canvas on the stream, without a reload
- [x] A machine with a transition to a state that does not exist still renders,
      and the dangling edge is visible
- [x] Nothing on the canvas moves between two renders of an unchanged machine —
      count `datastar-patch-elements` frames and assert the count stops growing,
      the way `06-engine-status.spec.js` does

## Notes

- **Draw the chart from the machine, not from the layout.** Layout says where a
  node sits; it must never be able to say *whether* a node exists. A state
  missing from the layout file needs a position, not an omission — otherwise a
  stale layout silently hides part of someone's machine.
- The reverse too: a layout entry for a state that has been deleted is ignored,
  not drawn. Neither direction may be an error, because both are ordinary
  results of editing the file in another tool.
- SVG or positioned HTML is an open choice. SVG gives edge routing and markers
  for free; HTML gives the design system's borders, fonts and hover states for
  free. Whichever, the nodes must be real elements with test ids — the suite
  selects by test id, and a canvas painted into a `<canvas>` element is opaque
  to every assertion in this file.
- Edge routing is the part most likely to eat time. Straight lines with a
  midpoint label are acceptable for this story; the design's curves are worth
  nothing if the wrong states are connected.
- Do not add zoom or pan here. They are pointer features and belong with the
  pointer story, and adding them now means adding them twice.

## As Implemented

The canvas is `internal/forge/chart` — a package with no HTML in it — rendered
by `modes/canvas.templ` as positioned HTML nodes over one SVG edge layer.
Everything is server-rendered and patched down the page stream; there is not a
line of JavaScript, which is the split the story asked for and the reason Story
5 will be adding pointer handling to something already known to be correct.

### Three engine findings, and the canvas is what surfaced them

None of them was introduced here. Both were found by checking the story's claims
against the code rather than trusting them, and both are fixed.

**`StateNode.ID` cannot identify a node.** Unless a state declares an `id` of
its own, `parseStateNode` gives it the machine id plus its own name at every
depth, so a state named `alert` is `goblin.alert` whether it sits at the root or
three levels in — two compound states with a same-named child collide on one ID.
The chart therefore keys a node on its **dotted path from the machine root**
(`combat.attacking`), which is stable under everything but a rename, readable in
a URL, and the same notation XState targets are written in.

That path is unique as long as no state name contains a dot — which XState
already requires, since the dot is its path separator and a state named `a.b` is
ambiguous to the engine's own resolver too. Nothing rejects such a name, here or
in the engine; a test pins what happens instead of the comment claiming it
cannot, and rejecting it belongs with Story 8, where a machine's problems are
reported.

**A dotted-path target was resolvable at runtime and refused at load.**
`ValidateMachine` checked membership of `collectStateIDs`, which holds bare
state keys and `StateNode.ID`s and therefore no dotted path at all, while the
interpreter's `findState` traverses one segment by segment before anything else.
So `"target": "combat.attacking"` — XState v4's own notation, and what Stately
Studio writes — made a machine fail validation, which made the loader refuse it,
which meant it could not run *and* could not be opened in Forge to be fixed.
`FindState` is now exported and both the interpreter and the validator use it,
so there is one answer to "where does this transition go" rather than two.

**And that resolver was non-deterministic.** Its last fallback ranged a map, so
a target naming a state that exists in two subtrees — `alert` under both
`patrol` and `combat` — resolved to whichever one Go's randomised iteration
reached first. The same file made the running game behave differently between
two launches. It descends in authored order now. Ambiguity in a file is the
author's to fix; picking a different answer each time is not a way to tell them
about it.

### What the review caught

Five defects, all of them in the half of the work that renders rather than the
half that computes.

**Fanned edges ended beside the boxes rather than on them.** The fan was applied
by clipping from each box's true centre and then translating the result by the
offset — which returns a point on the box, moved off it. On an axis-aligned
layout the perpendicular runs along a box edge and it is invisible, which is why
the fixture never showed it; place two states diagonally and an arrowhead floats
25px above the target or stops short pointing at nothing. Each end is now walked
out from its own offset point to its own box. The test that named this property
had been picking an edge whose group had one member — the one case where the fan
is zero and the bug cannot appear.

**A self-transition on any unpositioned state was drawn off the top of the
canvas.** A loop arcs above the node it loops over, the first row of the fallback
grid sits 14px from the origin, and nothing scrolls into negative overflow — so
the arc and its label were painted outside the clip. Since a label is the only
pointer target an edge has, that transition had quietly stopped existing, in the
default state of every machine created from the skeleton. The same hole swallowed
a negative recorded position. The chart now shifts its contents just far enough
to bring anything negative back on, and reports the shift so Story 5 can subtract
it; the breathing room around the canvas is the stylesheet's, because an inset
baked into the geometry would make every node's rendered position depend on where
the others are.

**`num` did the opposite of what its comment said.** It claimed to fix
coordinates to one decimal and called `FormatFloat` with `-1`, which is
shortest-round-trip. A compound state at x 0.1 holding a child at x 0.2 sized the
canvas to `224.29999999999998px`. Deterministic, so the stream's suppression was
never at risk — but it is noise in every frame on the wire, and Story 5 writes
dragged coordinates into exactly the field it comes from.

**The stream's `?sel=` branch was pinned by nothing.** The browser test that
looked like it covered it rebuilt the subscription URL from the address bar
rather than reading the one the page opens, so deleting the branch passed every
Go and Playwright test while, in a real browser, the first stream frame would
re-render the mode content with nothing selected. It now reads the `data-init`
attribute, and a Go test asserts the subscription directly.

**Two guards were deleted rather than kept.** The battery could not detect
either, and code a mutation cannot reach is a claim nothing backs. `round()`
kept the chart's geometry to a tenth of a pixel "because the markup is compared
byte for byte" — which stopped being true the moment `num` was fixed to round on
the way out, so removing it changed nothing anywhere. And `extent` widened the
canvas for edge labels as well as endpoints, which cannot bind: a label sits at
the midpoint of its own edge, so it is inside the two points the same function
already counts. The far side — a loop's label, above the node it hangs from — is
real, and `normalise` is what handles it. Story 5 rounds what it writes back,
which is where rounding belongs.

**Three more tests asserted the neighbour of the property.** A loop test checked
that two arcs differ *and* that their labels differ, which is the same fact twice
since a label sits at its own arc height. A fan test checked label positions,
which `separateLabels` fixes whether the fan ran or not — the exact mistake its
own comment said had been removed, still present a few lines below the fix. And
the nested-layout test asserted only that a child was somewhere inside its
parent, which is a tautology of how absolute positions are computed; it now pins
that a nested node's rendered position is its recorded one plus the parent's
margin and title height, which is the offset Story 5 has to undo.

### What the browser caught that Go could not

**The node layer swallowed every click meant for the canvas ground.**
`.chart__nodes` is `position:absolute; inset:0` at `z-index:2`, so it covered
the whole canvas: the ground link underneath was unreachable everywhere, and
clicking empty space to deselect did nothing at all. No Go test can see which
element is on top. The layer is `pointer-events:none` now and only
`.chart-node__head` takes the pointer back — which also means the empty area
inside a compound state behaves like the empty space it is, ready for Story 5's
double-click-to-add.

**Edges were unreadable before they were wrong.** At the first spacing the label
chip covered the entire line between two states, and three transitions between
the same pair stacked their labels into one illegible word. Two passes fix it,
both deterministic: edges sharing a pair of states are fanned perpendicular to
the line they share (unordered, so `A→B` and `B→A` fan apart together rather
than each being centred in its own group of one), and any labels still landing
on top of each other are pushed apart in edge order.

### Layout

Story 1's decision, read: `meta.forge.{x,y}` on each state, looked up as
`Extra["meta"]` — the key exactly as authored, since `extraFields` matches the
modelled set case-insensitively but stores keys verbatim. Read-only here; Story
5 writes, and has to merge rather than replace.

Nothing about a malformed `meta` is an error. It is hand-editable, it is shared
with Stately Studio, and users keep their own keys in it, so a note where an
object was expected is a state with no recorded position and it gets a slot. The
two directions the story names both fall out of walking the machine rather than
the layout: a state with no `meta` is still drawn, and a `meta.forge` for a
state that has been deleted is never consulted because nothing asks for it.

**The fallback slot is keyed on authored index, and the grid cell on what is at
that level.** Neither depends on which states are already placed — otherwise
positioning one node in Story 5 would shuffle every unpositioned node after it,
and a layout that moves things you did not touch never settles.

Coordinates come in two systems, named apart: `X`/`Y` relative to the parent,
because that is what the DOM needs and it is what makes a compound state's
children move with it; `AbsX`/`AbsY` absolute, because the edges are one SVG
layer spanning the whole canvas. Both are worked out in the same walk.

### Selection

One URL parameter, `?sel=`, carrying `state:<path>` or `edge:<id>`. One
parameter rather than two, so "exactly one thing is selected" is structural
rather than a rule about which of two wins. Resolved server-side against the
chart that was just built, and a `sel` naming nothing is dropped in silence —
that is what a bookmark becomes the moment someone renames a state, and a 404
for it would be absurd. `streamQuery` carries the resolved value, not the raw
parameter, for the same reason it already does that for `?machine=`.

### A machine broken on disk cannot be opened at all

The AC marked `[~]`. The canvas draws an invalid machine — that is pinned by
tests over a machine with no initial state, an unregistered action, a context
key naming nothing and a target that does not exist. What cannot happen is
*reaching* it: `project.loadMachines` resolves machines through `agent.Loader`,
which validates before it registers, so a file that is broken on disk when Forge
opens never becomes a `project.Machine` and has no row to click. It is reported
by name in "Files that did not load", with every reason — and there is no way to
open it and fix it in the tool that exists to fix it.

That is not this story's to fix, and the reason is worth writing down rather
than working around: `project.Machine` currently means both "this resolved" and
"the engine will load this", and `validation.Check` depends on the second
meaning — `bindingProblems` reports a binding to a machine the loader rejected
precisely by looking for it in `Machines`. Splitting the two is a change to
`project`, `validation`, the ENTS binding dropdowns and the machine list, and
doing it hastily inside a rendering story is how a save-blocking rule quietly
stops firing. It belongs with Story 8, which is the inline-validation story for
this mode.

The dangling-edge half of the same problem *is* reachable, and the route is
itself a finding: **renaming a machine's id rewrites its states' derived ids and
does not rewrite the transitions that name them**, so every fully-qualified
target in the file stops resolving the moment you rename. That is a defect in
Story 2's `RenameID`, it is what `13-canvas-rendering.spec.js` uses to produce a
dangling edge through the UI, and until this story there was nothing on screen
that could have shown it — which is the whole argument for drawing a broken
transition rather than dropping it.

### Verification

- `internal/forge/chart` at 97% statement coverage, unit tests over nesting,
  initial tagging at every level, edge enumeration and identity, `after`
  labelling, guards, dangling and internal transitions, history nodes, the
  layout reader and its malformed inputs, slot stability, fanning, label
  separation, selection resolution, and fifty rebuilds compared for equality.
- Render tests in `modes` over the markup: nesting by balanced-element match
  rather than "appears somewhere on the page", escaping of a state name
  containing `"><script>`, both truth values of every boolean attribute, and
  fifty full renders compared byte for byte.
- `e2e/specs/13-canvas-rendering.spec.js`, 9 tests including the z-order one
  that states its own premise before relying on it, and an accessibility block.
- A 63-mutation battery over the chart, the render helpers, the interpreter and
  the validator, all caught. It earned its cost twice: the first run found seven
  tests that could not fail, and the run after the review fixes found two more —
  including one the review fixes had just introduced, where dropping a
  "redundant" assertion left only the one it implied.
- `BenchmarkBuild`, which is what keeps the per-tick cost honest.

## Left for later

- **The empty area of a compound state deselects rather than selecting the
  compound.** Only its title bar selects it. That is the right hit area while
  the inside is where children live, and Story 5 wants the empty space for
  double-click-to-add, but it is a choice rather than an obvious truth.
- **The fallback grid sprawls when a machine mixes placed and unplaced states.**
  The cell is sized for the widest state at that level, so one compound sibling
  pushes every unplaced state a long way right. Correct, non-overlapping, and
  ugly; it matters least in the case the fallback exists for, which is a machine
  with no layout at all.
- **No zoom or pan**, deliberately: they are pointer features and belong with the
  pointer story, and adding them now means adding them twice. Wide machines
  scroll.
- **Edges are straight lines**, with the story's explicit permission. The
  design's curves are worth nothing if the wrong states are connected.
- **`Position` reads; nothing writes.** Story 5 adds the writer, and it must
  merge into `meta` rather than replace it — and it must undo three offsets to
  get back to a recorded coordinate: the chart's `OffsetX`/`OffsetY`, and for a
  nested state its parent's inner margin and title height. All three are
  documented on the fields they belong to and pinned by tests.
- **`Loader`'s reconcile hook still uses the narrower state-id set.**
  `collectStateIDs` is no longer what validates a target, but `ReloadFile` still
  passes it to the `ReconcileFunc` as "the valid states", so a hot-reload
  reconcile will not recognise a state named only by a dotted path. Found by the
  review; it belongs with whoever wires the reconciler for real, since nothing
  reads that set today.
- **`separateLabels` is quadratic in edges** and gives up after a bounded number
  of attempts, so a machine with a dozen mutually-colliding transitions still
  ends with some overlap. It is what dominates `BenchmarkBuild` at 400 states.
- **Story 5 must round the coordinates it writes.** The chart no longer rounds
  its own geometry — `num` rounds for display and nothing else observed it — so
  a dragged position taken straight off a `Node` can carry a full-precision
  float into someone's `meta`.
- **The fallback grid can overlap a state the user placed.** The slot is keyed
  on authored index by design, which is what stops placing one state moving
  another, and the price is that it cannot route around one.
- **A parallel state's regions are drawn as ordinary children** with a dashed
  border on the parent. The dividing lines between regions are not drawn.
- **The chart is rebuilt on every render**, including every stream tick, and it
  is O(distinct targets × states). `BenchmarkBuild` keeps the number honest: a
  400-state machine is ~20 ms and 0.9 MB per build, 100 states ~1.7 ms, 20
  states ~0.3 ms. It started at 192 ms and 17.8 MB for 400 states — the review
  caught that, and three things fixed it: resolving each distinct target once
  per machine rather than once per transition, working out a level's authored
  order once per resolver rather than at every level of every search, and not
  building a path prefix to descend into a state that has no children. The
  benchmark is in the tree because all three are easy to lose in a refactor that
  looks harmless, and because Epic 18 puts much busier traffic on the same
  stream. What is left is `separateLabels`, which is quadratic in edges.
