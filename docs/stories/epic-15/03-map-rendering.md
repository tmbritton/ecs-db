# Story 3: The map renders

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** 🔲 Not started  
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

- [ ] The map draws: every tile layer in file order, each cell showing the tile
      its gid names, from the tileset image
- [ ] A route serves project image files, and it **refuses any path outside the
      project** — `..`, absolute paths, and symlinks that leave it. Path
      traversal is the whole risk of this route and it is tested for, not
      assumed
- [ ] Only image types are served, and nothing else in the project is reachable
      through it
- [ ] The layer panel lists the layers the file has, in file order, with the
      active one marked and a visibility toggle per layer
- [ ] **Layer order is drawn as the engine reads it** — topmost non-empty tile
      wins (`tilemap.cellAt`) — so what the canvas shows and what the game loads
      are the same map
- [ ] Hiding a layer changes only what Forge draws. It is not written to the
      file and it does not change what the engine imports, because the engine
      does not consult `visible` for tiles — the panel says so where a user can
      see it
- [ ] The tileset palette shows each of the map's tilesets and lets one tile be
      selected; the selection is in the URL
- [ ] A collection tileset — no sheet, sparse ids — renders as its individual
      tile images, because Epic 14 Story 2 supports both kinds and a palette
      that only handles sheets is broken for half of them
- [ ] The map tab strip lists what Story 2 discovered; switching maps is
      navigation
- [ ] The `AUTHORED / ● LIVE / REPLAY` lens control renders in the toolbar, with
      LIVE and REPLAY disabled and each naming the epic that delivers it
- [ ] The status line under the canvas shows the hovered cell and the map's size
      and tile size
- [ ] A gid no tileset holds draws as a visible unresolved cell rather than as
      empty — a tile you cannot see is one you cannot fix, and it is exactly what
      the engine will refuse to load (Story 9 explains it)
- [ ] Two renders of an unchanged map are byte-identical, so the page stream's
      identical-patch suppression works and the canvas does not flicker
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/15-map-rendering.spec.js`.

- [ ] The shipped `level1.tmx` draws 20×15 cells and the tileset image loads
- [ ] The wall cells and the floor cells are visibly different
- [ ] The layer panel lists `ground`; toggling its eye hides the tiles and
      leaves the file untouched
- [ ] Selecting a tile in the palette marks it, changes the URL, and survives a
      reload
- [ ] LIVE and REPLAY are present and not clickable, and say which epic owns them
- [ ] Nothing on the canvas moves between two renders of an unchanged map —
      count `datastar-patch-elements` frames and assert the count stops growing,
      as `06-engine-status.spec.js` does
- [ ] The image route refuses `../../etc/passwd` and anything outside the project

## Notes

- **Draw from the file, not from the database.** That is the engine's own rule
  since Epic 14 Story 5: `comp_tile` holds one row per cell and cannot express a
  stack, and a stack is most of what layers are for. It also means MAP mode
  needs no database at all in AUTHORED, which is what lets it work with no game
  ever having run.
- **Cell size on screen is not tile size in the file.** Zoom is a view concern
  and belongs to the client; the coordinates the server reasons about are always
  cells. Getting that boundary right here is what keeps Story 5's pointer maths
  from having two coordinate systems to be wrong about.
- 20×15 is 300 cells and a real map is thousands. Whether cells are elements or
  one canvas is an implementation decision, but it is worth taking with the
  patch-stream in mind: a full-map redraw on every stamp is a lot of HTML.
  Measure before optimising, and if the answer is a `<canvas>`, note that this
  story stays server-authoritative regardless — Story 5's JS owns pointer state
  and nothing else.
