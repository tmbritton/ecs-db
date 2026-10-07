# Story 5 — Painting, the pointer surface: implementation plan ✅ Complete

## Verified before planning

| The story says | The code says |
|---|---|
| `canvas.js` is the precedent: pointer state only, one `CustomEvent` on release, bound once from the document | True — `canvas.js:1-17` states exactly that rule, `:215` dispatches `canvasdrop`, `:218` binds `pointerdown` on `document` |
| A Datastar expression turns that event into the same `@post` every other control uses | True — `canvas.templ:21` `data-on:canvasdrop={ dropAction(data) }`, `canvas.go:279` branches on `evt.detail.action` |
| The arithmetic is a pure exported function tested away from the browser | Half true — `dragResult` is exported and pure, but it is tested by importing the module *inside Playwright* (`13-canvas-editing.spec.js:82`), not by node. There is no JS unit-test runner in this repo and no `*.test.js` anywhere. The precedent to copy is the Playwright module import |
| `data-cell-w`/`data-cell-h` are there for the pointer arithmetic | True — `map.templ` binds both on `.map-canvas` from `$zoom` |
| The canvas is hit-testable at map scale | True, with a catch: `.map-canvas` is the *zoomed* box (`forge.css:2577`), `.map-canvas__scaled` inside it is the map-pixel box under a `scale()`. Pixels→cells must measure against `.map-canvas`, whose rect is already zoomed, using the `data-cell-*` attributes |
| Tool shortcuts `B` `R` `E` `M`, `Q` rotate, `X` flip match the prototype's tooltips | Three of six. The prototype (`Forge Editor.dc.html:98-104`) has Stamp (B), Rect fill (R), Eraser (E), **Select (M)**, Rotate stamp (Q), Flip stamp (X). `Tools()` has stamp/fill/erase and `paint.Kind` has three values — **there is no select tool** and nothing on the server it could talk to |
| The ghost stamp "shows the selected tile with its current rotation, as the prototype does" | The prototype's ghost (`Forge Editor.dc.html:132-133`) is a **dashed amber box with a ⟳ glyph and a mono label reading `stamp · 90°`** — not a picture of the tile. "As the prototype does" is the operative clause |
| Grid and snap toggles are view state | Grid, yes — the prototype's `gridOn` with shortcut `G` (`:1756`). **Snap has nothing to snap.** Every tile stroke lands on a cell by construction; snapping is about free pixel positions, which arrive with spawn objects in Story 6 |

## The defect this story surfaces

`paint.cells` (`paint.go:150`) says:

> A rectangle for all three tools, because a stamp dragged across the map is a
> rectangle one cell tall or wide and a click is a rectangle of one cell.

That is true of a strictly horizontal or vertical drag and false of every other
one. Until this story nothing could drag diagonally, so it cost nothing. With a
pointer it costs two things at once:

- A diagonal stamp drag across four cells paints **sixteen**.
- `stamp` and `fill` become the same operation, which makes one of the two
  toolbar buttons a lie.

So `Op` gains the cells a gesture actually covered, and `Fill` keeps the
rectangle. That is the difference between the two tools, and it is what the
story's own acceptance criteria already assume by listing "drag paints a stroke"
and "rect fill previews its rectangle" separately.

## What this story ships

1. **`paint.Op.Cells`** — the cells a pointer-driven stroke covered. `Fill`
   ignores it and spans `From`..`To`; `Stamp` and `Erase` use it when it is
   there and fall back to the rectangle for a click. Every cell is bounds-checked
   before anything is written, in place of the two end checks.
2. **`internal/forge/web/static/js/paint.js`** — the pointer surface, to
   `canvas.js`'s contract. Three pure exported functions (`cellAt`, `lineCells`,
   `trail`) plus `strokeResult`, and handlers bound once from the document that
   dispatch one `paintstroke` event per gesture.
3. **The ghost and the marquee** — server-rendered elements inside `.map-canvas`,
   positioned by the JS through CSS custom properties, styled and labelled by
   Datastar from `$tool` and the orientation flags. The JS writes numbers; it
   never learns what a tile is.
4. **`$grid`** — the grid toggle, as a signal, with its button and the `G`
   shortcut.
5. **Keyboard shortcuts** — `B` `R` `E` for the tools, `Q` to turn, `X` to
   mirror, `G` for the grid, on `data-on:keydown__window`.

### What it deliberately does not ship, and why

- **`M` — Select.** There is no marquee tool: no `paint.Kind`, no route, no
  server operation to give a selection meaning. A shortcut that selects a tool
  which does not exist is worse than no shortcut, and inventing the tool is a
  story of its own (copy, cut, paste, move a region). Recorded for the epic.
- **Snap.** Nothing in this story has a sub-cell position to snap. Spawn objects
  in Story 6 do — Tiled writes their `x`/`y` in pixels — and that is where a
  snap toggle first means something. Adding a control now that changes nothing
  is how a toolbar stops being trustworthy.
- **A ghost showing the tile's own picture.** The prototype does not, and doing
  it would need a gid→background lookup for every palette tile inside the canvas
  region — 216 tiles across the fixture's two tilesets, roughly 10 KB of
  expression added to the one region this epic worked hardest to keep small.
  The dashed box carries the orientation in words instead, which is what the
  prototype shows and what a rotation actually needs to be readable.

## Design

### The split, restated

`canvas.js`'s opening comment is the contract. Replace "machine" with "map":

> it owns pointer state and stops there. It never fetches, never renders, and
> does not know what a map is.

Concretely, `paint.js` may read: the canvas element's client rect, its
`data-cell-w`/`data-cell-h`, and which element the pointer is over. It may write:
CSS custom properties and classes on that element. It may dispatch one event. It
may not know a gid, a layer, a tool or a tileset — every one of those is a signal
the server wrote an expression over.

That is what decides where each acceptance criterion is implemented:

| | who does it |
|---|---|
| which cell the pointer is in | `paint.js`, from pixels and cell size |
| which cells a drag covered | `paint.js`, `trail` over the sampled path |
| whether the gesture is a rectangle or a trail | the Datastar expression, from `$tool` |
| what the ghost looks like and says | Datastar, from `$tool` and the flip signals |
| whether the marquee is shown | CSS, from a class the JS sets and a class Datastar sets |
| what a stroke does to the map | `internal/forge/paint`, server-side |

### The wire

`POST /forge/map/paint` keeps `?x=&y=` as the gesture's start and `?x2=&y2=` as
its end. It gains `?cells=x,y,x,y,…` — a flat list, used by `stamp` and `erase`.
`strokeAction` picks between them:

```
$tool === 'fill'
  ? @post('…&x=' + d.x + '&y=' + d.y + '&x2=' + d.x2 + '&y2=' + d.y2)
  : @post('…&x=' + d.x + '&y=' + d.y + '&cells=' + d.cells)
```

The tool lives in the expression rather than in the JS, and the JS sends all
three descriptions of the gesture with every stroke. The list is bounded by the
map: `trail` emits each cell once, so a stroke can never name more cells than the
map has.

### Interpolation is the client's, and this is the one place the split bends

A pointer sampled at frame rate skips cells when it moves fast — a 100 px jump at
1× zoom crosses six 16 px cells and reports one. Something has to join them.

It is done in `paint.js` rather than on the server because the alternative is
worse in both directions: sending the raw samples means the server must
interpolate, which makes the request describe *where the pointer was* rather than
*what to paint*, and it makes the emitted list unbounded (a stroke could name the
same cell a hundred times). `trail` dedupes, which is only sound once the joining
has already happened.

The arithmetic is a line between two cells — pixels and cell size, nothing about
maps — and it is a pure exported function with its own tests, which is what the
story asks for.

### Where the state lives

- `$grid` — new, seeded `true` in `MapSignals`.
- `$tool`, `$tile`, `$layer`, `$flipH/V/D`, `$hide<N>`, `$zoom` — already there.
- The ghost's position, the marquee's rectangle, and whether either is shown:
  **not signals.** They change on every pointer move, and a signal write per
  frame is a re-evaluation of every expression bound to it. They are inline
  custom properties and classes on `.map-canvas`, written directly.

  A patch replacing the canvas region drops them. That is correct and harmless:
  the next pointer move puts them back, and a stroke that was mid-flight when the
  map changed underneath it should not finish.

## Files

**New**

- `internal/forge/web/static/js/paint.js`
- `e2e/specs/15-paint-pointer.spec.js`

**Changed**

- `internal/forge/paint/paint.go` — `Op.Cells`, `cells()` chooses, bounds-check
  every cell.
- `internal/forge/paint/paint_test.go`
- `internal/forge/server/paintedit.go` — read `?cells=`.
- `internal/forge/server/paintedit_test.go`
- `internal/forge/templates/modes/map.templ` — ghost, marquee, the paint
  binding, the grid button, the shortcut binding.
- `internal/forge/templates/modes/mapmode.go` — `strokeAction`, `shortcutAction`,
  `ghostNoteExpr`, the grid expressions.
- `internal/forge/templates/modes/pages.go` — `"grid":true`.
- `internal/forge/templates/modes/turn_test.go` / a new `pointer_test.go`
- `internal/forge/templates/layout.templ` — load the module.
- `internal/forge/web/embed_test.go` — pin the new file.
- `internal/forge/web/static/css/forge.css` — ghost, marquee, grid off.

## Verification

- `go test ./...`, `make lint` on both tag sets, `make build`, `make build-headless`.
- The pure functions, through a module import in Playwright, the way `dragResult`
  is: cells from pixels at several zooms, a point off the canvas, a diagonal
  line, a trail that revisits a cell, a drag released outside.
- `make e2e` — the whole suite, plus the new spec's steps.
- Mutation battery over `paint.js`'s arithmetic, `cells()` and the new
  expressions, because every one of them is the kind of thing that stays green
  while being wrong.
- Fresh-context `reviewer` on the staged diff before committing (`AGENTS.md:211`).
- By hand in a browser: a diagonal stamp drag paints a diagonal, and the same
  drag with the fill tool paints the rectangle.

## Carried in from Story 4

- `signalsFrom` coerces rather than refuses: a `layer` or `tile` arriving as a
  JSON string reads as 0. Now that a real page sends them, decide whether to
  refuse.
- The hidden-layer guard **fails open** when a `hide<N>` key is absent from what
  the page sends, rather than merely renamed. A real click is the first thing
  that sends the whole signal set, so this is the story that can tell.

## As implemented

**`paint.Op.Cells`, and every cell of it checked.** `cells()` returns the trail
for the tools that follow the pointer and the rectangle for `Fill`, which is now
the only difference between the two toolbar buttons that had none. The bounds
check moved from the two ends to every cell, before the copy, so a trail that
leaves the map paints none of itself rather than the part that came first.

**`trailFrom`** reads `?cells=x,y,x,y,…`. A missing parameter is a click and no
error; an *empty* one is refused, because a stroke that says where it went and
went nowhere is not the same as one that never said. Odd lengths and
non-numbers are refused rather than coerced.

**`paint.js`**, to `canvas.js`'s contract: `cellAt`, `lineCells`, `trail` and
`strokeResult` are pure and exported, the handlers bind once from the document,
and the module dispatches one `paintstroke` event. It knows the canvas rect, the
drawn cell size, and which element the pointer is over. It does not know a gid,
a layer, a tool or a tileset.

**The ghost and the marquee** are server-rendered elements inside `.map-canvas`,
outside the scaled box so their borders stay 2px at 8×. `paint.js` writes
`--ghost-x/y` and `--sel-x/y/w/h`; Datastar decides what they say and whether the
marquee applies at all. The two halves of "show the rectangle" come from opposite
sides — the pointer says *dragging*, the signal says *fill* — and CSS is where
they meet.

**`$grid`**, its button, and `B R E Q X G`. The shortcuts are `data-on:keydown__window`
on the toolbar, guarded against modifier keys and against firing while a field
has the focus.

## Design notes the work turned up

**The interpolation is the client's, and that is the one place the split bends.**
A pointer sampled at frame rate skips cells whenever it moves faster than one
per frame. Joining them on the server would make the request describe *where the
pointer was* rather than *what to paint*, and would leave the list unbounded — a
stroke could name one cell a hundred times. `trail` joins first and deduplicates
second, which is the only sound order: deduplicating the raw samples and letting
the server join what was left would draw a line between two cells the pointer
never travelled between.

**The ghost is the prototype's, not a picture of the tile.** The story says
"shows the selected tile with its current rotation, as the prototype does", and
the prototype's ghost is a dashed amber box with a ⟳ and a mono label reading
`stamp · 90°`. Drawing the tile itself would need a gid→background lookup for
every palette tile inside the canvas region — the one region this epic worked
hardest to keep small — plus an eight-entry matrix table for the rotation. The
label says the orientation in words instead, and the status line beside it
already says which tile is in hand.

**Two of the prototype's controls are not here, and neither is an oversight:**

- **Select (M)** has no tool behind it. There is no `paint.Kind`, no route, and
  nothing a selection could mean to the server. A shortcut that chooses a tool
  which does not exist is worse than no shortcut; the tool itself is a story
  (copy, cut, paste, move a region), not a keybinding.
- **Snap** has nothing to snap. Every tile stroke lands on a cell by
  construction. Spawn objects in Story 6 carry pixel coordinates, and that is
  where the toggle first changes anything.

## Found while verifying: the ghost was drawn and invisible

The ghost rendered correctly, carried the right label, moved to the right cell,
and could not be seen. Everything inside `.map-canvas` is positioned with
`z-index: auto`, so tree order decides what covers what — and the ghost has to
come *before* `.map-canvas__scaled` in the markup to escape its transform and
keep a 2px border at 2px. Tree order therefore painted it underneath every tile
it was over.

The same was already true of the grid, and had been since Story 3. On a map
whose ground layer is fully tiled — which the fixture is, and the shipped level
is — the grid was not drawn at all. That went unnoticed until this story put a
toggle beside the zoom and made it a control for something invisible.

**Nothing that reads the DOM could have caught either.** `toBeVisible` is about
`display`, size and `visibility`; it says nothing about what is painted on top.
The bounding box was right. The attributes were right. The Go render tests were
right. The three tests that do catch it compare screenshots of the canvas with
the overlay on and off, which is the only assertion that is actually about being
drawn.

The fix is `z-index` on the three overlays, ordered tiles → grid → marquee →
ghost, and the ghost's label moved onto a chip: amber text over 4× pixel art was
legible only where the map happened to be dark.

## And a second flake the cleanup guard caught

Two tests painted and then ended without waiting. The `afterEach` read which
maps were dirty *from the page*, and the dirty flag arrives on the SSE stream —
so the cleanup raced the stroke it existed to undo, and lost in the direction
that leaves the session dirty for every test after it. It passed twice before
failing.

Both halves were wrong and both are fixed: the cleanup now discards every map in
the project rather than the ones the page is currently marking, and a test that
paints waits for its own stroke to land before it ends.

## Verified

- `go test ./...`, `make lint` on both tag sets, `make build`, `make build-headless`
- The full browser suite, including the pointer tests in `15-paint-pointer.spec.js`
- Mutation battery, Go half: over `paint.go`, `paintedit.go`, `mapmode.go`,
  `pages.go` and the generated template
- Mutation battery, browser half: over `paint.js` and the stylesheet, run
  against the spec, because no Go test can see either
- By hand in a browser: a diagonal stamp drag paints a diagonal and the same
  drag with the fill tool paints the rectangle

## What the batteries caught that the tests did not

Five Go mutations survived the first run, and each was a test asserting less
than it looked like it did:

- `TestPaint_FollowsTheTrailAndNotTheRectangle` used the trail `0,0,1,1`, which
  is **symmetric** — so reading the pairs y-first was invisible. Replaced with a
  cell off the diagonal.
- Nothing pinned that the shortcut keys are compared in lower case, so every
  shortcut could quietly stop working with shift held.
- Nothing pinned the grid's tooltip, which is the only thing that says which way
  the toggle goes.
- `len(op.Cells) > 0` and `op.Cells != nil` were indistinguishable, because the
  route refuses the one case that tells them apart.
- One mutation was a no-op: its anchor had the wrong indentation, which is the
  battery's own failure mode and the reason no-ops are counted.

## Left for Story 6

- **Snap**, with the first thing that has a sub-cell position to snap to.
- `signalsFrom` still coerces rather than refuses: a `layer` or `tile` arriving
  as a JSON string reads as 0. The page never sends one, so nothing is wrong
  today.
- The hidden-layer guard still **fails open** when a `hide<N>` key is absent
  from what the page sends rather than merely renamed. A real click sends the
  whole signal set, so the case is now reachable only by a hand-made request.

## Review follow-up

- A far-out rectangle endpoint was expanded before bounds checks and could
  overflow an allocation. Both endpoints are now validated before expanding a
  rectangle or following a supplied trail; a trail must start at its stated
  origin. The trail's last cell is not required to equal its destination: a
  stroke may revisit an earlier cell, which the client sends only once.
- A drag that leaves the canvas and comes back starts a new segment rather than
  connecting across cells the pointer never crossed. Pointer IDs prevent a
  second touch from taking over or completing the first pointer's stroke.
- The fill browser test now checks the actual painted rectangle, and the
  marquee screenshot is taken against a baseline with the ghost already shown.
- A second review found that a stream patch could detach the canvas while it
  held pointer capture, leaving an unfinished gesture blocking the next one.
  Lost capture cancels the gesture, and a disconnected canvas is discarded
  before the next pointerdown. A no-op click also restores the hover ghost on
  release rather than requiring a mouse wiggle.
- The marquee screenshot holds the ghost on the same cell in both frames. A
  temporary `z-index: -1` on the marquee left it present and `toBeVisible`, but
  made the screenshot comparison fail, confirming it sees the painted overlay.
