# Story 4 — Painting, server-side: implementation plan ✅ Complete

## Verified before planning

| The story says | The code says |
|---|---|
| Epic 14's parser splits the flags into `Tile.FlipH/FlipV/FlipD` | True — `tiled.go:80-91`, and there is a fourth field `RotatedHex` the story's prose omits but its ACs remember |
| Four flag bits, including the hexagonal one the common three-bit mask leaves behind | True — `tiled.go:96-104`, `gidMask = 0x0FFFFFFF` |
| The renderer already draws the flips | True — `draw.go:65-75` applies FlipD then FlipH/FlipV, in that order |
| A paint operation rewrites the layer's `<data>` and touches nothing else | True — `Document.SetLayerData` (`document.go:170`) edits that element and re-renders the rest verbatim |
| Layer data is one raw gid per cell, row-major | True — `Layer.Data`, documented as raw on purpose |

**The gap.** `TileOf` splits a raw gid into a `Tile`; nothing puts one back. Every
operation in this story ends by writing a gid, so the inverse is the first thing
to build, and it has to carry all four flags or a rotated tile silently loses its
rotation on the next save.

## Two places the story has been overtaken

**Tool state is not the server's.** The story says *"deciding which one a drag
means belongs to the tool state, on the server, not to the JS"*. That was written
before Story 0 moved MAP's view state into Datastar signals for a reason that
applies here exactly: a stream's subscription is fixed when the page loads, so
anything the server renders from it is frozen there. The tool, its rotation and
its flips are view state — the browser owns them, and Datastar sends them with
the paint request like every other signal. The server still *decides what a drag
means*; it is simply told the tool rather than remembering it.

**Its Playwright section is Story 5's.** "Clicking a cell paints the palette's
selected tile there" is the pointer surface, and Story 5's first acceptance
criterion is "click paints one cell; drag paints a stroke and posts once on
release". Those steps move. What stays here is the toolbar — choosing a tool and
rotating the stamp — which needs no pointer.

## What this story ships

1. **`tiled.Tile.Raw()`** — the inverse of `TileOf`, all four flags.
2. **`internal/forge/paint`** — the operations, as functions from a map value and
   an operation to the gids that result. No session, no HTTP, no browser:
   - `Stamp` one cell, `Fill` a rectangle inclusive of both corners in any drag
     direction, `Erase` (gid 0, a genuinely empty cell), and `Turn` — rotate and
     flip, which are flag arithmetic on the tile in hand rather than new tiles.
   - Refusals with reasons: outside the map, no such layer, a layer the view is
     hiding. Never a silent clamp.
   - A no-op is detected here, not left to the file layer: stamping the tile a
     cell already holds returns "nothing changed" and the caller writes nothing.
3. **`POST /forge/map/paint`** — one route, one operation per request, reading
   the tool, the tile, the layer and the flips from the request's signals. A
   drag arrives as one operation with two corners, so it is one entry in the
   session's history and one patch on the wire.
4. **The toolbar** — stamp, rect, eraser, and rotate/flip, as signals.

## Undo/redo, decided

Out of scope, explicitly. Discard reverts to the last save and is the coarse
answer the mode already has; the toolbar will not grow an undo button that does
something narrower than the footer's Discard. Per-operation undo needs a history
the session does not keep, and inventing one here would be a second source of
truth about what the map is.

## Verification

- Unit tests over the operations against map values, including the flag
  arithmetic for every rotation and both flips.
- A mutation battery over `paint` and the route.
- `e2e/specs/15-painting.spec.js`, thin: the toolbar marks the chosen tool,
  rotate marks the stamp rotated, and a paint through the route flips the footer
  to `● unsaved` while painting the same tile again leaves it clean.
- The engine loads a painted map: `ecs-db run` reads what Forge wrote.

## As Implemented

**Where the map comes from.** The route grew its own resolver, `mapShown`. An
empty `?map=` means the map the page is showing, resolved by `selectMap` — the
same call the page itself makes. It has to be the same rule rather than merely a
reasonable one: a route defaulting differently from the page is exactly how a
stroke lands on a file nobody was looking at. Reload and discard keep the
resolvers that insist on a name; both throw work away, and defaulting a
destructive route to whatever happened to be first is a different kind of
mistake from refusing one. The first attempt loosened the shared `mapPath` and
took `reload` with it, which a test caught.

**The rotation was wrong, and the review caught it.** `RotateCW` was written as
a case per angle with a `default` for "270 → upright, and anything else lands
back at square". The eight flag combinations are the eight symmetries of a
square and a quarter turn permutes all of them — there is no "anything else". Of
the four states the default swallowed, every one is a *mirrored* stamp, so
mirroring the stamp and then turning it silently threw the mirror away. The fix
is a closed form over all eight: `D' = !D, H' = !V, V' = H`.

Nothing could see it. `TestTurn_FourTimesIsIdentity`, `TestTurnTables_FourTurns`
and the e2e rotation test all start from upright, which is in the orbit that
worked. `TestTurn_MatchesWhatTheRendererDraws` claimed to check the cycle
"against the transform the renderer actually applies" but asserted only `TX`/
`TY` — and every rotation shares its translation with a reflection: `(0,0)` is
both upright and the diagonal transpose, `(16,0)` is both 90° and a horizontal
mirror. It passed against the mirror cycle as readily as the rotation one. It is
replaced by three tests that assert the **full matrix** over all eight states,
that a quarter turn is a bijection (the shape of this bug: four states
collapsing onto one), and that four turns is identity *from every orientation*.
A fourth pins the matrix convention itself, because reading `A/B/C/D`
transposed makes all of them pass against the wrong thing — a mistake made once
while diagnosing this.

**The orientation labels are generated too.** Two of the eight were swapped, and
the test could not see it: counting eight distinct non-empty strings is all a
test of a literal map can check. `turnLabels` now walks `RotateCW` around both
orbits — the unmirrored one from upright, the mirrored one from a horizontal
mirror — so each label's angle is the number of quarter turns it took to get
there. A plain vertical mirror therefore reads "mirrored 180°", which is exact:
a top-to-bottom mirror is a left-to-right one turned half way round. No naming
avoids that, because each reflected state is reachable by either button from a
different angle.

**Rotation lives in the browser, and is generated from the server's copy.** The
plan said "rotate and flip, as flag arithmetic on the tile in hand", which read
as a server operation. Phase 4 had already moved every other view control to
signals, and orientation is view state by the same argument, so the stamp's
flags travel with the stroke rather than being remembered. That leaves two
copies of the rotation cycle — `paint.RotateCW` and a JavaScript expression —
and a quarter turn changes two flags, not one, so a hand-written copy would
drift silently: the browser would send flags the server accepts as valid, and
nothing would report the disagreement except a tile pointing the wrong way on
screen. `turnTables` therefore *enumerates* `RotateCW` over all eight flag
combinations at render time and emits the three arrays the browser indexes. What
is left to test is the index arithmetic, which `turn_test.go` pins from both
ends.

**The mirrors are actions, not toggles.** They were built with an on-state, and
the browser showed why that is wrong: `$flipH` is a raw flag, not "is it
mirrored", and a quarter turn sets it — so the mirror button lit up merely
because the stamp was turned. They lost the on-state; the note beside them
reports the orientation in words, over all eight combinations rather than the
four the rotation cycle visits.

**`paint.FlipH` and `paint.FlipV` were deleted.** Written to match the plan's
"and both flips", they ended up with no caller once the browser held the
orientation, and the mutation battery found their test could not fail (mirroring
vertically instead of horizontally still satisfies "its own inverse").
`RotateCW` stayed, because `turnTables` reads it.

**A no-op is not a refusal.** Painting the tile a cell already holds returns
`Changed: false` and the route writes nothing, so the footer keeps meaning
something. It is not reported as a problem either — it is a legal stroke that
happened to change nothing.

## Verified

- `go test ./...`, `make lint` on both tag sets, `make build`, `make build-headless`.
- 235 browser tests, including `e2e/specs/15-painting.spec.js` (12, of which
  4 are the accessibility block AGENTS asks for).
- A mutation battery of 35 over `paint`, `Tile.Raw`, the route and the toolbar:
  **35/35 caught, no no-ops**. Five survived the first pass; four were real gaps
  and got tests, the fifth was the dead mirror helpers. One survivor is worth
  recording: a drag's far corner past the right-hand edge indexes *into the next
  row* rather than out of the array, so the inner bounds guard does not catch it
  and the stroke would land a row lower than it was drawn. `inside(layer, op.To)`
  is what stops it.
- `internal/forge/paint` at **100% statement coverage**. The last gap was the
  index guard inside the write loop, whose comment called it unreachable. It is
  not: a layer off disk can declare a size larger than its data, and a cell
  inside the declared bounds is then past the end of it. The comment was wrong
  and is now a test.
- The `hide<N>` signal name was being built in three places across two packages.
  A disagreement there does not fail — the lookup returns the zero value, the
  hidden-layer refusal quietly stops happening, and strokes land on a layer
  nobody can see. It is now one exported `modes.HideSignal`, with a test that
  reads the signals off the **rendered page** and checks the route looks up a
  name the page really seeds.
- The engine reads what Forge writes: a quarter-turned tile painted through the
  route, saved, and re-parsed with `tiled.Parse` comes back with its flags
  (`TestPaint_WhatIsSavedIsWhatTheEngineReads`). Confirmed by hand against the
  fixture project too — `0xA000007B` in the CSV, which is gid 123 with FlipH and
  FlipD.

**A test that could not fail, found by mutating it.** `turnLabels` renders the
labels into a JavaScript array literal through `quoteJS`. The test asserting
that used a regexp, and a regexp re-synchronises on the next quote — so it
passed with `quoteJS` bypassed *and* a label carrying an apostrophe. It is
replaced by a scan that reads the literal the way a parser does and decodes the
escapes back, so a string that closes its own quote shows up as a malformed
literal rather than as two elements.

Mutating it then showed the remaining gap: none of the eight real labels
contains a character `quoteJS` escapes, so bypassing it produces byte-identical
output and there is nothing to detect. The rendering moved into `jsStringArray`,
which a test exercises with strings that do need escaping — an apostrophe, a
backslash, a comma, a newline, and an `@post(...)` that Datastar would otherwise
rewrite from inside the literal.

## Design notes the review raised, and where they landed

- **`paint.Op.Hidden` carries view state into a domain operation**, and a future
  caller that forgets to populate it loses the refusal silently. Kept, because
  the risk that actually mattered was the signal *name* drifting across the
  package boundary, and `modes.HideSignal` plus a test spanning the seam closes
  that. Worth revisiting if a second caller ever appears.
- **`signalsFrom` coerces rather than refuses.** A `layer` or `tile` arriving as
  a JSON string reads as 0, so a stroke would land on layer 0 with an empty hand
  instead of being refused. Both are seeded as numbers, so it does not arise.
- **The hidden-layer guard fails open.** A `hide<N>` key simply absent from what
  the page sends reads as false, and the refusal does not fire. The seam test
  covers the name drifting, not the key being missing. Worth revisiting in
  Story 5, when the page rather than a test is doing the sending.
- **A gid is written without checking it resolves to a tileset the map
  references.** The palette is the only thing that sets `$tile`, so it does not
  arise in practice, but the route would accept one. An unresolvable gid shows
  as a problem marker in Forge and makes `ecs-db run` refuse the map — loud, not
  silent, which is why it is recorded rather than fixed here.

## Left for Story 5

The pointer surface: click-to-paint, drag-to-fill, and the cell arithmetic that
turns a pointer position into a cell. With it comes the assertion this story
could not make — that the page *sends* the tool and orientation it is showing.
Datastar keeps no readable signal store, so nothing in the browser can check
that until a real click starts a stroke; `15-painting.spec.js` posts its signals
explicitly and says so.

Also unchanged and still true: view transitions cost ~140 ms of swallowed
pointer input per navigation. Painting is not a navigation, so a stroke is not
affected — but a drag that crosses a navigation would be.
