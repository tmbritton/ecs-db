# Story 4: Author Tile references in MAP

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** ✅ Implemented
**Priority:** High — the editor must author the same references the engine reads

## Product contract

Each nonempty painted layer cell imports as one positioned `Tile`. The tile's
resolved TSX `entityType` and properties are a template for its **owned**
referenced entity; painting a Wall already creates a Wall on the next
`ecs-db run`. Erasing removes the Tile and that owned reference. MAP must show
this consequence in the cell inspector rather than stamping one object-layer
Wall per painted cell or persisting database IDs in the TMX.

Additional references are authored through the existing object-layer
`TileLink.layerID` and optional `TileLink.cells` metadata. A Tile can link
several existing object-layer entities; several Tiles can link one River ID.
Those links place the referenced entity's artwork, restrictions and sight
rules at the Tiles' Positions. An object with a link can be a restriction-only
River when its visual varies by cell: the per-cell referenced art still owns
each segment. Neither a polygon nor a `Tile.passable` switch is involved.

MAP edits the working TMX in the existing map session. A save takes effect on
the next `ecs-db run`, not on a running interpreter. The object ID is an
authoring ID, **not** an entity ID; linked DB IDs are resolved on import.

## Acceptance criteria

- [x] Selecting a painted cell identifies its stable layer ID and coordinate,
      displays its TSX entity template and authored object links separately,
      and explains that the actual entity IDs appear after import. Stacked
      layers can select different Tiles at the same `(x,y)`; an empty cell does
      not pretend to have a Tile or offer a link target.
- [x] The inspector offers compatible object-layer spawns as link targets,
      showing type, object ID and whether they provide artwork or restrictions.
      Linking and unlinking are surgical, undoable through map discard and
      persisted on save; no unrelated object properties or unknown XML change.
      A Tile may link multiple objects and a River object may span an irregular
      set of painted cells on **one** layer without being cloned.
- [x] A linked object retains its object ID on edit/re-import. Existing links
      are visible on re-open. Removing the last link retains the independent
      spawn and makes that occupancy change explicit; deleting an object uses
      the existing spawn-delete flow. Moving a linked object's anchor keeps
      absolute target cells stable by adjusting relative offsets, or refuses
      before writing if that cannot be represented.
- [x] Erasing/replacing a painted Tile updates its authored links so the saved
      map is importable. Removing a Tile clears only links to that layer/cell;
      other linked cells and unrelated objects survive. A no-op stroke or link
      request writes nothing and does not dirty the session.
- [x] Refuse invalid target cells, duplicate object IDs, unresolved layer IDs,
      incompatible entity types/visuals, missing required components and
      malformed footprints before changing the working TMX. Report failures on
      the relevant Tile or spawn in MAP as well as in the edit response. A
      saved positive fixture imports through `tilemap.LoadMap`, preserving
      shared identity, draw placement, movement and sight behavior.
- [x] `make test`, tagged builds and both lint tag sets pass. `make e2e`
      verifies real user actions and accessibility. No collision polygon
      authoring. TILES template editing remains Stories 5–8.

## Playwright steps — browser-only evidence, before code

1. Select two stacked painted Tiles at one coordinate via their distinct
   `data-testid` targets. Assert the URL/inspector follows the chosen layer ID
   and coordinate, the TSX template class is shown, and selection survives an
   unrelated stream patch. Check an empty cell exposes no attach action.
2. Place a River object via the MAP spawn palette, select a painted Tile, link
   the River, then select an irregular second cell and link the **same** object
   ID. Assert exactly one POST per action; the inspector lists the link on
   both cells and the TMX session becomes dirty. Save, inspect actual TMX
   metadata, and run the fixture's real map importer to confirm one River ID,
   two Tile references and the movement/sight and art effects.
3. Unlink one cell and confirm the other still links the same object. Discard
   restores the saved links; reloading after save restores the authored links.
   Erase a linked cell and assert the server updates the footprint without a
   second, silently broken map edit.
4. Try a duplicate object ID, an empty cell and a bad layer. Assert visible
   actionable refusal and **no request that changes the map** (or no dirty
   session after a rejected request). Confirm keyboard access to selection,
   link and unlink controls and accessible names for each.

Break at least one link handler deliberately during development and verify
that the browser spec fails on its missing network/working-map effect.

## Verification

`make test` (including the race checks and tagged vet), both builds, both lint
tag sets and **310 Playwright checks** pass. `internal/forge/tilelinks` has
82.5% statement coverage; `internal/forge/mapvalidation` has 96.9%. The real
schema/TMX/TSX/SQLite integration test confirms one shared River, distinct
per-cell artwork, live movement and sight rules, stable re-import identity,
object moves and linked-cell erasure. Inspect supports keyboard arrows and
Enter, including a visible cell cursor. Unlinking the last shared-art provider
is refused before it can leave a map the importer cannot load. Spawn edits
preflight linked map candidates as well. Inspect navigation preserves browser
zoom, layer visibility and the selected tool. The browser suite deliberately failed
when the link handler was removed, then passed after restoration.
