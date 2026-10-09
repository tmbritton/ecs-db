# Story 5: Project tileset editing session

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** ✅ Complete
**Priority:** High — TILES must own the TSX bytes it saves

## Contract

External tilesets referenced by any map open in MAP are one project-wide
authoring resource. A `.tsx` named by two maps opens **once**, at one stable
path with one working document and one dirty/save/conflict state. The session
uses Story 1's lossless `tiled.TilesetDocument`, never re-encodes the lossy
`tiled.Tileset` reading model. MAP can keep its own unsaved TMX edits; a map
reference edited in that working value still determines what TILES discovers.

This story supplies the TILES file list, selection and session lifecycle.
The sheet/collection grid, enlarged tile and metadata editor are Stories 6–8.
Do not present a stub for unsupported collision polygons, terrain or tile
animation as if they affect the game.

## Acceptance criteria

- [x] Discover external `.tsx` and `.tsj` references from all open project
      maps, including their unsaved working values. Resolve paths against the
      *map's* directory, deduplicate the same TSX across maps, and present
      unresolved references with their map and useful reason. Embedded
      tilesets are not external files. No arbitrary path parameter opens a
      file or crosses the project root, including through symlinks.
- [x] Hold one `editable.File[*tiled.TilesetDocument]` per writable TSX.
      Unchanged bytes remain exact, duplicate references share edits, `Dirty`
      compares bytes, and no-op Save does not rewrite the file. `.tsj` is
      listed as readable but explicitly read-only; embedded tilesets are
      described as belonging to their map, not as editable external TSX.
- [x] Save is atomic and refuses an external change with Reload and
      SaveOverwriting choices. Discard restores the session's last saved
      snapshot; Reload takes disk state even when the working value is dirty.
      A deleted TSX retains any unsaved working value and reports what Save
      would recreate rather than losing work when map discovery refreshes.
- [x] TILES can select an external tileset by project-held path; the selected
      file's path, maps referencing it, writable/read-only state and dirty
      state are honest. The footer's Save/Discard/Reload and conflict behavior
      belong to that selected file. Unsaved TSX work is visible in the footer
      even when switching to another mode, once Story 7 exposes an actual
      metadata edit that can make it dirty.
- [x] Editing a shared working TSX is observed by every affected MAP preview
      and its validation, without waiting for a save or requiring a server
      restart. A saved TSX is **not** hot-reloaded into a running game; the
      mode says the next `ecs-db run` picks it up.
- [x] `make test`, both builds/lint tag sets and `make e2e` pass. Story 5's
      browser suite asserts on real navigation, shared-file identity,
      read-only controls and accessibility. Session tests prove dirty bytes,
      conflict, atomic saves and cross-map working previews. Story 7 adds the
      browser dirty/edit/save/discard assertions with its actual TILES editor.

## Playwright steps — browser-only evidence, before code

1. Open TILES and select a TSX referenced by two maps. Assert the URL chooses
   one held file, both referencing maps are named, and a second click on its
   row does not create a second dirty entry. A `.tsj` appears read-only with
   no Save action; an embedded tileset is not offered as an external file.
2. Navigate from a shared TSX to MAP and back; assert the selected file and
   referring maps remain correct. Verify the TILES page says a saved file takes
   effect on the next `ecs-db run`, and that unsaved edits are not claimed when
   no edit control exists yet. Story 7's browser spec proves the working edit,
   dirty footer, cross-map preview, Save and Discard effects.
3. Change a TSX on disk after opening it and use Reload to take the disk
   version. Assert its file status changes without navigating elsewhere. The
   session's conflict and SaveOverwriting branches are proved in Go here; the
   Story 7 browser spec exercises them once a TSX edit control is available.
4. Try a guessed path or project-external symlink. Assert the route does not
   expose its bytes and does not open/edit that target. Include an explicit
   `accessibility` block for file selection, disabled read-only controls,
   save/conflict status and keyboard navigation.

Deliberately break one session action and prove the browser spec fails on its
missing effect before trusting it.

## Verification

- `make test` (including Forge race tests and tagged vet), both builds,
  untagged and `ebitengine` lint: pass. The installed mise linter was invoked
  directly for both tag sets because nested `mise exec` in `make lint` selected
  a different Go compiler than the 1.26.7 standard library.
- `make e2e`: **315 passed**. Replacing Reload with Discard deliberately made
  the Reload browser spec fail on unchanged TSX metadata; the real handler was
  restored and the full suite passed.
- Statement coverage: `internal/forge/tilesets` **86.4%**,
  `internal/forge/maps` **87.1%**, `internal/forge/server` **86.7%**.
