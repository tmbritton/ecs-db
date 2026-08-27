# Story 3 — implementation plan

**Story:** [The map renders](03-map-rendering.md)

## What was verified first

| Claim | Verified |
|---|---|
| Nothing serves a project file over HTTP | True — `staticHandler` (`server.go`) serves the embedded asset FS and refuses directory listings; there is no other file route. |
| The engine already computes the drawing | True, and it is the find of this story: `tiled.DrawList` (`draw.go:125`) returns every tile as an image path, a source rectangle, a destination in pixels, its flip flags and its layer's opacity — bottom-left aligned, render-order aware, tile-offset aware. `Draw.Transform` gives the 2×3 matrix. All untagged, all tested by Epic 14 Story 5. |
| `DrawList` honours layer visibility and the loader does not | True (`draw.go:138`, `tilemap/tiled.go:119`) — and the two disagree deliberately. |
| A tileset image is never opened by the parser | True — `ParseTileset` records `Image.Path` and `check` never stats it. So a map can resolve with an image that is not there, and that is a Story 3 problem rather than a Story 2 one. |
| Collection tilesets have per-tile images | True — `source()` (`draw.go:259`) already handles both kinds, so the palette gets collections for free if it goes through the same code. |
| Inline positioning is the established pattern | True — `canvas.go`'s `nodeStyle`/`labelStyle` return `templ.SafeCSS`. A per-tile position cannot be a class. |
| Selection lives in the URL | True across SCHEMA, ENTS and AGENTS. |

## `DrawList` becomes a projection of a fuller answer

`DrawList` **skips** a cell it cannot place and records one aggregated problem
per distinct reason. That is right for the engine — a renderer has nothing to
draw for such a cell — and wrong here: the story wants an unresolved cell to be
visible, because a tile you cannot see is one you cannot fix.

So `tiled` gains `Placements()`: one entry per non-empty cell, carrying the
`Draw` when it could be placed and a `Problem` when it could not, plus the cell
and the layer. `DrawList` is then `Placements()` filtered and aggregated — one
implementation, no second copy of the layer-order, render-order, tile-offset or
bottom-left rules, and Epic 14's existing `draw_test.go` keeps it honest.

It is engine surface added for Forge, which is worth saying out loud. The
alternative — Forge walking the layers itself and re-asking "can this gid be
drawn" — duplicates exactly the predicate that took Epic 14 four review findings
to get right.

## The image route is an allow-list, not a prefix check
*(Amended after review: an allow-list **and** a containment check. See below.)*

The story asks for a route that "refuses any path outside the project". A prefix
check is the obvious implementation and it is the weaker one: it has to get
`..`, absolute paths, symlinks out of the tree, and case-insensitive
filesystems all right, and a tileset may legitimately name an absolute path
(`resolvePath` handles it, and Epic 14's comment says Tiled writes one the
moment art lives outside the project directory).

So the route serves **only paths the open maps' resolved tilesets actually
reference**. The set is derived from the files, not from the request, so a path
nobody's tileset names is not served whatever it looks like — traversal is
structurally impossible rather than filtered. Extension is still checked, as a
second gate and so the route cannot be talked into serving a `.tsx` as an image.

**Corrected after review, and this was wrong in the way that matters.** The
allow-list makes traversal impossible *for the request*. It does nothing about
traversal by the *project*: a `.tsx` naming `../../../etc/passwd`, an absolute
source, or a `fixture.png` that is a symlink out of the tree is a path the
project names, and all three were served. A tileset is a file in the project and
a project can be a clone of somebody else's repository. Containment —
`EvalSymlinks` on both sides, then require the target under the project root —
is the second gate the acceptance criterion actually asked for, and the cost is
that absolute out-of-project art cannot be previewed. See the story's As
Implemented.

## Shape

- **`internal/tiled/draw.go`** — `Placements()`, `DrawList()` rewritten over it.
- **`internal/forge/mapcanvas`** — the view model, a sibling of
  `internal/forge/chart`: `Build(m *tiled.Map, opts Options) Canvas` producing
  positioned cells with a CSS matrix, an image URL, unresolved markers, the
  layer rows and the palette. No HTTP, no templates, no `os`.
- **`internal/forge/server/asset.go`** — `GET /forge/asset` and the allow-list.
- **`templates/modes/map.templ`** — canvas, layer panel, palette, lens control,
  status line.

## Decisions taken up front

- **Layer visibility is view state in the URL** (`?hide=`), initialised from the
  file's own `visible` attribute. *(The first cut OR'd the two instead of
  initialising from it, which made a file-hidden layer impossible to look at.
  The seeding happens in the server, because only a URL can be authoritative.)* Toggling changes what Forge draws and never
  the file, which is what the story asks for and is *not* what Tiled's eye does
  — so the row says when Forge's state and the file's differ, and the panel
  states the engine's rule: tiles import whether or not a layer is hidden.
- **Cells are absolutely positioned elements, not a `<canvas>`.** A canvas needs
  JS to draw and this is the story that has none. The ceiling is real and gets
  measured and recorded rather than assumed: 20×15 is 300 elements and a
  500×500 map is a quarter of a million. **Measured:** ~222 bytes of HTML per
  cell — 89 KB at 400 cells, 2.2 MB at 10,000, 57 MB at 250,000.
- **Zoom is not in this story.** Cell size on screen equals tile size in the
  file, so there is exactly one coordinate system for Story 5 to be wrong about.

## Missed, not deferred

The AC "with the active one marked" was not in this plan at all, and was not
built until review found it. Story 4's own notes depend on it. Recorded here
because the deferral below was reasoned about and written down, and this was
simply absent — which is the difference between a scope call and an omission.

## An acceptance criterion this story cannot meet

*"The status line under the canvas shows the hovered cell"* — hover is pointer
state, and pointer state is Story 5's, which is the story that introduces the
only JS this epic gets. The status line here shows the map's size, its tile
size, the layer count and the selected tile; the hovered cell moves to Story 5.

## Risks

- **Byte-identical re-render.** The canvas is on a 2-second stream and the
  patch suppression depends on two renders agreeing. Anything ranging a map —
  the palette, the tileset list — has to be ordered explicitly.
- **The allow-list is rebuilt per request.** It comes from the session's
  resolved maps, which means parsing. Cache keyed on nothing yet; measure.
