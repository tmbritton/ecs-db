# Story 3: The map renders

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** ✅ Complete  
**Priority:** High — the centre of the mode

**Depends on:** Story 2

## Context

Draw the map: the tile grid from the tileset images, the layer panel, the
tileset palette, the map tab strip and the source-lens control. Nothing here
edits anything, which is the same split Epic 13 used for the statechart — a
canvas that draws the wrong map and a canvas you cannot paint on fail in
completely different ways, and only the second needs a line of JavaScript.

Everything is therefore server-rendered and patched down the page stream, and
selection — active layer, selected tile, selected spawn — is a URL, the same as
choosing a component in SCHEMA. It survives a reload, it can be linked, and the
inspectors read it from the request rather than from client state.

One genuinely new thing: **Forge has to serve an image the user's project owns.**
`staticHandler` serves the embedded asset FS and nothing else. A tileset PNG
lives at whatever path a `.tsx` names, resolved relative to it, which makes this
the first route in Forge that reads a filesystem path out of a file the user
controls.

## Acceptance Criteria

- [x] The map draws: every tile layer in file order, each cell showing the tile
      its gid names, from the tileset image
- [x] A route serves project image files, and it **refuses any path outside the
      project** — `..`, absolute paths, and symlinks that leave it. Path
      traversal is the whole risk of this route and it is tested for, not
      assumed — including the three cases above named by a *tileset* rather than
      by the request, which is the half the first cut of this got wrong
- [x] Only image types are served, and nothing else in the project is reachable
      through it
- [x] The layer panel lists the layers the file has, in file order, with the
      active one marked and a visibility toggle per layer
- [x] **Layer order is drawn as the engine reads it** — topmost non-empty tile
      wins (`tilemap.cellAt`) — so what the canvas shows and what the game loads
      are the same map
- [x] Hiding a layer changes only what Forge draws. It is not written to the
      file and it does not change what the engine imports, because the engine
      does not consult `visible` for tiles — the panel says so where a user can
      see it
- [x] The tileset palette shows each of the map's tilesets and lets one tile be
      selected; the selection is in the URL
- [x] A collection tileset — no sheet, sparse ids — renders as its individual
      tile images, because Epic 14 Story 2 supports both kinds and a palette
      that only handles sheets is broken for half of them
- [x] The map tab strip lists what Story 2 discovered; switching maps is
      navigation
- [x] The `AUTHORED / ● LIVE / REPLAY` lens control renders in the toolbar, with
      LIVE and REPLAY disabled and each naming the epic that delivers it
- [x] Zoom, added after the story shipped — see *The zoom control* below
- [~] The status line under the canvas shows ~~the hovered cell and~~ the map's
      size and tile size — **hover moved to Story 5.** Hover is pointer state,
      and pointer state is the story that introduces this epic's only JS. The
      line shows size, tile size, layer count, the active layer and the selected
      tile
- [x] A gid no tileset holds draws as a visible unresolved cell rather than as
      empty — a tile you cannot see is one you cannot fix, and it is exactly what
      the engine will refuse to load (Story 9 explains it)
- [x] Two renders of an unchanged map are byte-identical, so the page stream's
      identical-patch suppression works and the canvas does not flicker
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/15-map-rendering.spec.js`.

- [x] The fixture map draws a cell per tile and the tileset image loads — the
      *fixture*, not the shipped `level1.tmx`, which the browser suite has never
      pointed at. It gained real CC-BY art and a second layer for this story, so
      a sheet's rows and columns are exercised rather than two flat colours
- [x] The wall cells and the floor cells draw different slices of the sheet
- [x] The layer panel lists `ground` and `props`; toggling an eye hides that
      layer's tiles and leaves the file untouched. The row is two controls — the
      eye and the name — so looking at a layer is not the same as painting into it
- [x] Selecting a tile in the palette marks it, changes the URL, and survives a
      reload
- [x] LIVE and REPLAY are present and not clickable, and say which epic owns them
- [x] Nothing on the canvas moves between two renders of an unchanged map —
      count `datastar-patch-elements` frames and assert the count stops growing,
      as `06-engine-status.spec.js` does
- [x] The image route refuses `../../etc/passwd` and anything outside the project
- [x] Zoom is a control, is in the URL, and survives a reload — added after the
      story shipped, with the map really drawn at the size the control says
- [x] Zoom travels with every other link: choosing a tile or hiding a layer does
      not reset it
- [x] The palette does not follow the canvas zoom — the rail is a fixed width
      and at 8× a 32px tile became a 256px swatch that flex then squashed into a
      distorted crop of itself

## Notes

- **Draw from the file, not from the database.** That is the engine's own rule
  since Epic 14 Story 5: `comp_tile` holds one row per cell and cannot express a
  stack, and a stack is most of what layers are for. It also means MAP mode
  needs no database at all in AUTHORED, which is what lets it work with no game
  ever having run.
- **Cell size on screen is not tile size in the file.** The coordinates the
  server reasons about are always cells; the pixels are a rendering of them.
  ~~Zoom belongs to the client~~ — it turned out to belong in the URL with every
  other part of the view, and the boundary this note is about is kept by
  `Canvas.CellW()` being the only place the product is taken.
- 20×15 is 300 cells and a real map is thousands. Whether cells are elements or
  one canvas is an implementation decision, but it is worth taking with the
  patch-stream in mind: a full-map redraw on every stamp is a lot of HTML.
  Measure before optimising, and if the answer is a `<canvas>`, note that this
  story stays server-authoritative regardless — Story 5's JS owns pointer state
  and nothing else.

## As Implemented

**`DrawList` became a projection of a fuller answer.** `tiled.Placements()` is
one entry per non-empty cell, carrying the `Draw` when it could be placed and a
`Problem` when it could not, plus the cell and the layer. `DrawList` is that,
filtered and aggregated. One walk, so the engine and the editor cannot disagree
about layer order, render order, tile offsets or which tileset owns an id — and
the alternative, Forge re-asking "can this gid be drawn", duplicates exactly the
predicate that took Epic 14 four review findings to get right.

It also fixed a bug it exposed. `drawOf` used to file its reason into the
aggregated list and the cell read it back off the end — but that list keeps one
entry per *distinct* reason, so a cell repeating an earlier fault added no entry
and was handed whichever reason happened to be last.
`TestPlacements_EachRefusedCellCarriesItsOwnReason` pins it.

**`internal/forge/mapcanvas` is the view model**, a sibling of
`internal/forge/chart`: positioned cells with a CSS matrix, unresolved markers,
the layer rows and the palette. No HTTP, no filesystem, no templates. Image
paths become URLs through a function the caller supplies, so the package cannot
invent a route.

**The asset route is an allow-list *and* a containment check.** The first gate
is the set of files the project's own tilesets name, filled by every resolve —
so a path the *request* invented is unreachable without the route doing any path
arithmetic, and the browser can only ask for pictures the page it was given told
it about. The second gate resolves symlinks and requires the result to be inside
the project.

Both are needed, and the first cut shipped only the first — which review broke
in three ways, each proved against the running server: a `.tsx` with
`source="../outside.png"`, an absolute `source=`, and a `fixture.png` that is a
symlink to `/etc/passwd`, all served 200. A tileset is a file in the project and
a project can be a clone of somebody else's repository, so "the project names
it" is not "we may serve it". The comments said the opposite in as many words,
and the tests were shaped so they could not notice: every denied path in them
was one no tileset named.

The cost is now stated rather than hidden: **art outside the project directory
cannot be previewed**, and Tiled writes an absolute source the moment art lives
elsewhere. Reaching it wants a deliberate opt-in naming the other directory.

**The layer row is two controls.** The eye decides what Forge draws; the name
decides where a stroke lands. Conflating them means you cannot look at a layer
without also painting into it.

**Layer visibility is the URL's answer, not the file's.** `?hide=` is the
complete set, seeded from the map's own `visible` attributes when the parameter
is absent. The first cut OR'd the two, which gave a layer hidden in Tiled an eye
you could click that changed the URL, flipped nothing on screen and never
changed its own state — so the one place an author would go to look at a hidden
layer could not show it.

Also found by review:

- **`Build` mutated the map it was given**, so a second `Build` with nothing
  hidden reported that the file hid a layer it does not — a false statement about
  the `.tmx` on the one row whose job is telling view state and file state apart.
  Safe only because `Resolved` re-parses, and the plan's own Risks section
  proposes caching that. The layers are copied now.
- **A layer at opacity 0 was reported as drawn.** Tiled's other way of making a
  layer invisible, and one `visible` says nothing about — `Draw.Alpha`'s own doc
  says "a layer at opacity 0 is hidden as surely as an unchecked one", and the
  panel was the one place that did not know.
- **The no-flicker browser test subscribed to the wrong stream**, passing
  `document.title` where a map path goes. It fell back to the default
  subscription, so the very change it looked like it covered — the stream
  carrying the whole view — was untested.
- `X-Content-Type-Options: nosniff`, and regular files only: a FIFO named by a
  tileset blocks `os.Open` forever, and the server deliberately has no write
  timeout because of SSE.

## Notes

- **The active layer was missing and is not a deferral.** The AC asked for it,
  Story 4's notes depend on it, and it was simply absent from the plan — unlike
  the hover-cell criterion, which was noticed, reasoned about and written down.
  That asymmetry is why it was a finding rather than a scope call.

### Left undone, deliberately

- **The hovered cell.** Hover is pointer state, and pointer state is Story 5's.
- ~~**Zoom is fixed at 3×**, chosen for 16px art.~~ **Done, after the fact —
  see *The zoom control*.** Deferring it was the wrong call: the number was
  chosen for the fixture and shipped against the engine's own map, where it
  drew 96px cells and a canvas of 1920×1440.
- **The canvas is an element per cell, and the ceiling is now measured** rather
  than assumed. Through the real template: 400 cells is 89 KB of HTML, 10,000 is
  2.2 MB, 250,000 is 57 MB — about 222 bytes a cell. The mode stream also
  renders content to a string and compares it to the last one every tick, so a
  100×100 map means building a 2.2 MB string every two seconds per open browser,
  on top of `Resolved`'s re-parse. Suppression saves the wire, not the work.
  Nothing in this engine has a map near that; the number is here so the next
  person does not have to find it.
- **`cellID` is not unique** and cannot be: Tiled permits two layers with one
  name. It is for a test to point at and a person to read. Story 5 keys pointer
  state on the layer *index*, which is what the panel and the paint routes
  already address a layer by.

### The zoom control

Added after the story shipped, because the fixed 3× it shipped with was chosen
for 16px art and the engine's own map is 20×15 at 32px — which came to 96px
cells and a canvas of 1920×1440 that does not fit on a screen. The story had
recorded that as a deferral to Story 5, on the argument that zoom and pointer
arithmetic are the same question. That argument is still true and it was still
the wrong call: a canvas you cannot see the map on is not a canvas, and the
deferral shipped one.

**`mapcanvas.FitScale` is the default**: the largest step that keeps the whole
map inside a 900×640 budget, capped at 4 and floored at 1. Both dimensions bind
— a tall, narrow map has width to spare and no height. The engine's map gets 1×
and the e2e fixture gets 4×, so the common case is "open a map, see the map"
with no interaction. The budget is not the viewport, which the server cannot
know; it is a size comfortably inside a laptop window beside Forge's two panels.

**The steps are 1, 2, 3, 4, 6 and 8, and they are integers.** A fractional scale
puts a tile boundary between device pixels and pixel art either blurs or gains a
seam depending on which way the browser rounds. `?zoom=` takes only a step the
control offers, so a hand-typed 137 cannot draw one tile the size of the panel.

The control marks the scale **in force**, not the parameter: a view that has
asked for nothing is at the fitted scale, and a control that highlighted nothing
in that case would be worse than no control. Review found the one case where
that could still happen — a map whose tilesets do not resolve renders with a
zero canvas — and the control is absent there rather than marking none of its
six steps.

**The palette keeps its own scale**, also found by review. Zoom is a canvas
concern and a 212px rail structurally cannot follow it: at 8× a 32px tile became
a 256px swatch, flex shrank each one's width to fit while its height and its
background-size did not, and every tile in the palette became a distorted crop
of itself. Swatches are sized for the rail — about 32px — which is legible for
8px art and does not magnify art that is already big enough.

### The battery

51 mutations over `Placements`, `mapcanvas`, the asset route, the allow-list and
the view-state helpers, run three times.

The first pass caught 28 of 38, and nine of the ten survivors were one gap:
`MapView` had no unit tests at all, only whole-page `strings.Contains` through
the server, which is too loose to see a link dropping half the view or a CSS
matrix written in Go's column order rather than CSS's.

The third pass — after the review's fixes and thirteen more mutations for them —
caught 50 of 51. Two things it found that the tests as written could not: a
palette ordering test that passed on luck, because Go randomises where a map
walk starts rather than the order within it, so six keys can come out sorted for
a whole run; and the FIFO guard, whose own test *hung*, because the first
version of the fix checked the file mode after the `os.Open` that blocks.

One accepted survivor: the `slug != "map"` guard that stops the canvas being
built for every mode is a cost guard, and removing it changes no output at all.

A fourth pass of 13 mutations covered the zoom control, 12 caught first time.
The survivor was a fit that only measured width — every fixture in the test was
a square map, so height never bound on its own, and the property test beside the
table had the same blind spot.
