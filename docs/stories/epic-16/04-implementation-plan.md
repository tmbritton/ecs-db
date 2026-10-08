# Story 4 — MAP occupant authoring: implementation plan

## Verified starting seams

- Story 3's `LoadMap` resolves TSX `entityType` templates and materializes
  owned referenced art/rules for every nonempty Tile. `TileLink.layerID` and
  optional JSON `TileLink.cells` on a typed TMX object connect a shared spawn
  to one or several absolute target cells. The importer validates links before
  writing, then persists `TileReferences` after spawning the objects. There is
  no MAP link-editing UI yet.
- `forge/paint.Apply` is a pure, nonmutating stroke over one layer's GID data;
  MAP's route applies the result to a `tiled.Document` in a working session.
  A link-aware erase must reconcile the authored object metadata before the
  document is published. The engine rejects a dangling TileLink that points
  to an empty cell.
- `tiled.Document` provides lossless `SetObjectProperty`, `RemoveObjectComponent`,
  `MoveObject`, `SetLayerData`, `AddObject` and `RemoveObject`. A link edit should
  use those methods on a working copy so a refusal never half-mutates the map.
- MAP renders every painted layer cell in `mapCanvas` and marks object spawns.
  The active layer, stamp, tool and zoom are client-owned signals. The URL's
  `MapView` currently selects a map and optional spawn; `MapInspectorRegion`
  shows only a selected spawn. Stable selected-cell identity needs layer **ID**
  plus `(x,y)` in the URL, never a layer array index or DB entity ID.
- Browser attributes have failed silently before. Add test IDs to Go template
  tests and assert network requests plus changed TMX/session state in Playwright.

## Choices that preserve the engine contract

1. A painted cell's TSX template is displayed as a generated owned reference,
   not an editable object-layer spawn. The user links existing typed objects
   via the `TileLink` metadata contract. The editor never writes DB IDs into
   TMX or copies the shared River per cell.
2. Resolve link footprints from one shared tilemap projection rather than
   independently interpreting `TileLink.cells` in Forge. A link to another
   layer, an empty cell, duplicate IDs or invalid offsets is refused before
   touching the working document. A candidate edit must validate against a
   parsed copy before `session.Edit` publishes it; use existing `Document`
   serialization to keep unknown XML and no-op byte fidelity.
3. The first MAP action links/unlinks one existing object ID at a selected
   layer/cell. Its metadata's offsets remain relative to its object anchor;
   a one-cell link can use the implicit anchor only when they coincide. A
   multi-cell footprint is encoded as explicit unique offsets, ordered by
   map row and column so identical operations are no-ops.
4. A link object's anchor and absolute footprint must not silently diverge.
   Moving an object with links must preserve target cells by rewriting offsets
   in the same document edit. Erasing a cell must remove it from every linked
   object's metadata (or refuse before the stroke if an edit is ambiguous),
   without deleting other references. Unlinking the last cell removes the
   `TileLink` metadata but does not delete the object spawn.
5. A shared River with distinct per-cell art is represented by per-cell
   template visual references and one restriction-only River link. A
   same-type shared visual spanning different TSX slices is refused by the
   importer; MAP surfaces that refusal instead of promising different art.

## Red → green sequence

1. Add table-driven tests for reading a selected layer ID/cell and its
   generated template and authored links (including stacked layers, empty
   cells, invalid/missing IDs and bad TSX metadata). Add MapView URL and
   inspector tests first, then render the read-only cell selection. Pin test
   IDs from the Go side and write Playwright steps above before browser code.
2. Add `forge/tilelinks` domain tests for linking one object to several cells,
   a Tile linking several objects, unlinking one/all targets, no-op byte
   stability, duplicate object IDs, wrong layer/empty cells, invalid object
   coordinates and output that the real `LoadMap` imports. Then implement
   surgical document edit and hook the route into the map session. Keep
   object IDs and properties stable.
3. Test link-preserving object moves and link-aware erasure/replacement through
   the existing spawn/paint routes; invalid edits leave the working document
   and dirty bit unchanged. Test saved/reloaded/discarded output and active-map
   imports, traversal, sight and layered art with a restriction-only River.
4. Add and break-on-purpose Playwright link/selection/edit specs with an
   explicit `accessibility` block; run `make test`, tagged builds, both lint
   sets and `make e2e`. Fresh-context staged review, fix findings, commit and
   push; mark Story 4 and `docs/plan.md` complete with coverage figures.

## Implemented checkpoint

Stable layer-ID/cell URL selection exposes the resolved TSX template and
object-layer links. The `tilelinks` adapter writes explicit relative cells to
the existing `TileLink` metadata without cloning shared entities. Edit routes
preflight a complete candidate: object schema and required fields, link
footprints, resolved shared artwork, and any erased/replaced Tile. Moves
preserve linked cells; erase drops only affected links. The MAP inspector and
spawn markers surface invalid authored links. Headless integration covers the
real importer and the browser suite tests network writes, save/reload/discard,
selection, erasure and accessible controls.
