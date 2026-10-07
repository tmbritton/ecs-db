# Story 6 — Spawn placement: implementation plan

## Verified against the code before implementation

- `tiled.Document.AddObject(group, Object)` already allocates from the higher of
  `nextobjectid` and the greatest existing id, and increments the counter;
  `RemoveObject(id)` already deletes without recycling it. There is **no move
  operation**. Do not re-create an object to move it: the id is the engine's
  `(mapId, object_id)` identity and every other attribute must survive.
- `Document.objectGroups()` and `Map.ObjectGroups` flatten folder nesting in
  the same file order. Expose the chosen object group in the page and send its
  index to the route; never silently choose one of several.
- `maps.Session.Edit` holds the lock but does not roll a document back when a
  callback fails. Validate group, type, coordinates and id *before* calling a
  mutator; each document edit must validate before changing its tree.
- `tilemap.spawnCell` floors the object's pixel coordinates to a cell and
  subtracts one pixel from a tile object's Y. An ordinary new spawn can be a
  point object (`GID=0`) at the cell's top-left; a moved tile object must keep
  its `gid` and use the bottom-left convention.
- The map page has four disjoint SSE regions, with `mode-list` holding the tile
  palette and `map-canvas-region` holding the canvas. Client view state lives in
  `MapSignals`, never in the stream subscription URL. The map session and
  `refuseMapEdit` already provide the route and error pattern.
- `paint.js` is the second hand-written canvas module; it owns pointer
  coordinates, not map data. Add placement/movement pointer gestures there (or
  native drag events whose Datastar handlers can express the same operation),
  but do not add a third model or duplicate object positions in JS.
- The fixture `e2e-level.tmx` is 6×4, tile size 16×16, with one empty
  `<objectgroup name="spawns"/>` and `nextobjectid=1`; it can prove allocation,
  placement, move, deletion and a saved engine import end to end.

## Design decisions

1. A spawn palette is derived from the *editable* schema, not a static list.
   A type is offered only when it declares `Position` (required or optional),
   except `Tile`, which is importer-owned; unspawnable types remain visible with
   their reason. Recheck on the server: browser signals are not authority.
2. The object-group selector is a browser signal seeded from the first visible
   group; the selected group is labelled in the panel and accompanies a create
   request. An empty map with no object group must explain why it cannot place.
3. New objects are point objects: class = entity type, x/y = cell's top-left
   in map pixels, gid=0. They therefore round-trip through the engine's
   `spawnCell`. A move changes *only* x/y of the existing XML object and keeps
   its shape, attributes, unknown children and id. If an existing tile object
   is moved, its y is the bottom edge of the target cell.
4. Place/move/delete are one session edit and one notification each; selecting
   an object uses a URL with the map path and object id, so reload/deep links
   restore the inspector target. A missing/deleted id clears selection with an
   explanation rather than selecting another object.
5. Snap is view state. When on, positions go to the target cell's origin; when
   off, the pixel offset within that cell is kept for objects that can move
   freely, and the browser's displayed coordinate must match what the engine
   imports. Placement can remain cell-aligned until free-position gestures
   exist; a toggle that never changes an operation must not be displayed.
6. Saving a map does **not** hot-reload it. Deleting an object tells the author
   that the engine removes its entity on the next `ecs-db run`.

## TDD sequence

1. In `internal/tiled/document_test.go`, first prove moving an object keeps its
   id, unknown XML, attributes and the next-id counter; refuses missing ids and
   malformed/out-of-bounds destinations without changing bytes. Add the
   minimal `Document.MoveObject` implementation.
2. Add table-driven domain tests for spawnability, point/tile pixel origins,
   group choice, map bounds and preserved identity in a `internal/forge/spawn`
   package. Implement the smallest pure operation functions to pass.
3. Add `server` tests for create, move, delete, forged type/group/id, save and
   reload, and error visibility. Implement the routes using `maps.Session.Edit`
   and the domain operations. Assert real parsed TMX and `nextobjectid` values,
   not just HTTP 204.
4. Add rendering tests for palette, selector, overlay, URL selection and
   accessibility, then implement the Templ/Datastar wiring, CSS and pointer
   gesture in the existing canvas module. Pin every new browser test ID in
   `internal/forge/templates/testid_test.go`.
5. Write `e2e/specs/15-spawn-placement.spec.js` before the browser wiring.
   Drag a spawn from the palette, save/reload it, place two then delete/place
   again, drag an existing one to another cell with the same id, select it via
   the URL and reload, and check the unspawnable explanation. Include a named
   `accessibility` block and prove the spec fails against a deliberate broken
   handler. Test the saved configured map through the engine import.
6. Run `make test`, linter on both tag sets, `make build`,
   `make build-headless`, `make e2e`; review the staged diff with a fresh-context
   sub-agent before committing. Mark Story 6 and the roadmap complete only
   when the acceptance criteria and browser steps are met.

## As implemented

- `Document.MoveObject` changes only the existing object's coordinate
  attributes. Point objects use the top-left cell origin; an existing tile
  object retains its gid and uses the bottom-left origin. The id, counter,
  properties and unknown XML stay untouched.
- `spawn.Place` seeds each required non-Position component with the same
  zero-value convention the generated SQL columns use. The engine inserts
  *every* object property explicitly, including required fields, so writing a
  point object with only Position made a new Goblin that the engine refused.
  A required entity reference has no valid automatic target and is shown as
  unspawnable rather than inventing an entity id; Story 7 can author one.
- HTML native drag-and-drop carries an opaque type or object id to the existing
  `paint.js` module. The module converts drop pixels to a cell and dispatches
  one event; Datastar picks the operation and the Go domain mutates the map.
  No new hand-written client module or client-side copy of the object list.
- The selected group is an explicit browser signal, the selected object a URL
  address. The stream subscription retains its object id; without that, the
  inspector appeared on navigation and vanished on the very next patch.
- The native drag gesture lands at a cell origin. Snap stays absent rather than
  offering a toggle that currently changes nothing; free-pixel placement needs
  its own gesture and server coordinate contract before a toggle has meaning.
- The browser suite was run with the drop event deliberately suppressed and
  the placement spec failed at the missing object, proving the test checks the
  actual request-and-patch path rather than the presence of draggable markup.

## Review follow-up

- Entity-type keys are JSON strings, not necessarily SQL identifiers; `O'Brien`
  broke the Datastar drag expression. The type payload is now quoted as a JS
  string literal and the browser test drags that name successfully.
- A map with duplicate object ids, including a classless marker before a typed
  spawn, is ambiguous. Move and delete refuse it before touching the document;
  the domain test checks both operations leave the bytes unchanged.
- When an object disappears, the selection stays addressable in the stream and
  renders an explicit missing-object message with a link back to the map,
  rather than leaving a dead `?spawn=` URL and an empty inspector.
- The save/reload browser test now runs the engine's real `SyncSpawns` against
  the saved fixture TMX in a fresh SQLite database and asserts that object 1
  became a TestGoblin at cell 2,1. The fixture has no Tile entity type, so this
  calls the spawn importer from `LoadMap` rather than the full tile loader.
- Datastar also rewrites `@post(` *inside a quoted literal*. A type named
  `O'Brien@post(x)` broke the drag expression despite JavaScript quoting; the
  palette now uses the existing `quoteJS` helper that also protects authored
  layer names, and a browser drag pins the combined apostrophe/action case.
- Review found the group selector changed its signal and aria state without
  changing its visible highlight. Its active class now follows `$group`, and a
  two-group browser test asserts both directions before placing the object.
