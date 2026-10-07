# Story 9 — Inline map validation: implementation plan

## Verified before planning

- `tilemap.LoadMap` calls `readTiled`, which rejects non-orthogonal maps, fails
  at the **first** unresolved tileset, then calls `tilesOfTiled`. Its private
  `checkShape`, `cellAt`, `tileOf` and `stateOf` produce the engine's cell and
  shape refusals. `SyncSpawns` continues past individual spawn refusals and
  warnings, but needs a database, so calling it just to validate in Forge is
  wrong. `tilemap.ValidateSpawn` is its database-free, shared property/contract
  path; it returns an error for the first malformed property and a
  `world.ValidationResult` for the entity type's strict/warning contract.
- `tiled.Parse` itself rejects malformed layer dimensions/data. Such a map is
  already named with its reason in `maps.Session.Problems` and has no editable
  canvas. A valid in-memory map with a contradictory layer size still reaches
  `checkShape`; cover that path without making the parser silently accept a
  broken file.
- `maps.Session.Resolved` parses fresh and resolves *all* tilesets in sequence,
  stopping at the first error. The current `addMapData` returns immediately on
  that error: it shows a project problem but renders no canvas or spawns. To
  show all errors on their owners, Forge needs a read-only partial preview of
  the working document that resolves each tileset separately. Do not resolve
  into `Document.Map()`'s memoized value or lose its copy-on-write contract.
- The map's `mapId` is a custom property; `SyncSpawns` warns (in a helpful
  sentence) when it is absent and otherwise adopts that ID as spawn identity.
  `tiled.Document` has no map-property mutator, so a visible control and a
  byte-preserving mutator are required for the Playwright add/fix step.
- `maps.Session` knows every open map and keeps its *working* documents.
  Comparing `mapId`s from disk would miss an unsaved edit; use `Session.Read`
  for each path, without nesting session locks. A second map with an unreadable
  TMX already has a project problem and contributes no guessed ID.
- `mapcanvas.Build` emits unresolved cells and keeps one draw-layer index per
  cell; the template already has `map-cell-unresolved` and spawn marker IDs.
  `map-inspector` is a separate patch region. The page stream patches regions
  only when their bytes change, so validation order must be deterministic.

## Design

1. Add a non-mutating `maps.Session.Preview(path)` returning a copy of the
   working `tiled.Map`, with each tileset resolved independently and its
   original loader error attached to that tileset. Reuse `tiled.ResolveTilesets`
   on a one-ref copy, not a second path resolver. Continue to render the other
   tilesets, cells and spawns if one fails. Keep `Resolved`'s strict behavior
   for callers that need a fully loadable map.
2. Add a database-free validation projection under
   `internal/forge/mapvalidation`: a sorted `Report` of owner-keyed problems
   (map, tileset, layer index, cell/layer/coordinate, spawn object plus group
   index), severity (warning/error), and the engine's message. Extract narrow,
   exported engine helpers **only where necessary** so the engine and Forge
   cannot drift: map identity warning, shape/cell reasons and spawn parser
   sentences. A single invalid object may have several malformed properties;
   validate them individually through the shared parser, then validate its
   contract with the valid properties so one typo does not mask missing
   required components. Distinguish warning-level contracts from errors.
3. Detect duplicate `mapId` values across the session's open working maps.
   Name both files and mark the selected map. Detect duplicate spawn object
   IDs across object groups, name both objects, and mark both on the canvas;
   an ID alone is not enough to identify either duplicate, so marker test IDs
   need an unambiguous suffix only for the duplicate case.
4. Surface a mapId field and warning in `map-head`, layer/cell/spawn marks on
   their owners and a single complete problem summary there. Retain the
   selected spawn's existing engine-backed inspector feedback, adding the
   owner mark even before selection. CSS and ARIA must say invalid/warning,
   not only colour. No validation may block Save: a broken map is precisely
   what Forge is for, and saving never loads it into the game automatically.
5. On an edit or external reload, the same server-side report is rebuilt for
   SSE regions. On the next push the repaired problem disappears without a
   full page navigation. Because all maps are checked per render, benchmark a
   representative several-map fixture and keep the scan proportional to maps
   and authored cells, not page size squared.

## TDD and browser steps

1. First test the map-level property mutator for exact round-trip fidelity and
   document invalidation; add the minimal setter and route. Test that the
   warning disappears for a working-map edit and returns on Discard.
2. First test partial preview with two broken tilesets and another valid one;
   confirm neither error hides the valid cells/spawns, and the strict
   `Resolved` path still refuses the same file.
3. Table-drive the validation projection with no/duplicate map IDs,
   overlapping duplicate object IDs, unknown class, strict/warning missing
   required components, malformed/unknown/Position properties, all three
   invalid-gid cases, multiple layer shapes and unresolved tilesets. Assert
   exact engine messages and owner keys, all mistakes at once and stable order.
4. Write `e2e/specs/15-map-validation.spec.js` before markup wiring. Pin all
   new `data-testid`s from Go. Cover the story's Playwright steps, assert the
   owner mark and effect of a fix as well as summary text, and include an
   explicit accessibility block. Break one browser handler intentionally and
   observe a failing spec before trusting it.
5. Run `make test`, both lint tag sets, both builds and `make e2e`; fresh-context
   review of the staged diff, fix any findings, rerun affected checks, record
   coverage in the story and `docs/plan.md`, then commit.

## Review follow-up

- The engine validates the *topmost non-empty* tile in a stacked cell. A bad
  covered tile is not a current load refusal; inspect the full layer stack and
  mark the layer whose tile the engine actually selected.
- Two TMX objects sharing an ID cannot be selected or edited by ID. Both get
  findable, separate, non-actionable canvas markers. A hand-typed deep link
  reports ambiguity rather than selecting the last one; document move/delete
  now refuse duplicate IDs before touching either object.
- Count spawn IDs once per canvas render, not once per marker.
- A typed spawn's ID is also ambiguous when an untyped Tiled object claims it.
  Validate mapId text as XML before changing the working TMX, and bound the
  duplicate-ID summary per object so a file with hundreds of missing IDs does
  not grow a quadratic report.
