# Story 9: Animation-file editing session

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** ✅ Complete
**Depends on:** Stories 5–8

## Contract

The renderer loads only the **first** game.toml mod with an `assets` directory:
its `animations.toml` and `sprites/` directory. SPRT edits exactly that file;
later mods are not represented as game-loaded assets. The editing document
keeps TOML comments, ordering, whitespace, unknown keys/tables and unrelated
animation/entity bindings byte-for-byte. Use the renderer's TOML reader to
validate the file the game will actually load, rather than writing its lossy
runtime structs back out as TOML.

## Acceptance criteria

- [x] Discover the first configured assets mod in engine order, regardless
      of whether its `animations.toml` is missing or invalid. Record which
      mod supplies the game-loaded file and sprites directory. Other assets
      mods remain visible only as non-loaded context, never editable through
      the active session or claimed as live.
- [x] Hold one editable working document with exact unchanged bytes. Surgical
      edits to `[[animation]]` name/sheet/frames/fps/loop and
      `[[entity_asset]]` entity_type/sheet preserve unknown TOML and every
      other entry. Duplicate names or malformed TOML are explicit; no rename
      silently retargets another entry. A missing file is a discoverable
      problem with an explicit create path.
- [x] Dirty, no-op Save, atomic Save, external-change conflict with Reload/
      SaveOverwriting, Discard and reload of changed bytes follow the existing
      `editable.File` contract. Changes in Forge's working value are *not*
      yet claimed as live until Save; a saved `animations.toml` is watched by
      the engine for hot reload (unlike TSX). Source sprite images are confined
      to the selected assets root; a symlink escape is refused.
- [x] SPRT mode has a truthful source/binding file status and scoped save
      footer even before Story 10 adds its sheet editor. All modes' footers
      report unsaved animation work. Browser checks navigate to the selected
      source, confirm one active mod and read-only status for later mods,
      Reload an external edit and check accessibility. Story 10 adds browser
      edit/save/conflict checks with real animation controls.
- [x] `make test`, tagged/headless builds, both lint tag sets and `make e2e`
      pass. Break a browser-tested lifecycle action on purpose and confirm the
      spec fails. Fresh-context staged review before commit.

## Playwright steps — browser-only evidence, before code

1. Open SPRT and inspect the active first-assets-mod file, its sprite root and
   any later mod labelled "not loaded". Follow a file link and return; only
   the first file has a Save/Reload footer, with no surprise edits to the file.
2. Modify that TOML file on disk after opening SPRT. Click Reload and assert
   its visible animation count/name changes without navigating to another
   mode, a POST reaches the server, and the footer remains clean.
3. Inspect a missing/malformed animation file and a guessed path: show a
   useful path-specific reason, no unrestricted file access and no misleading
   successful status. Include an explicit `accessibility` block for file
   selection, disabled/unavailable actions and keyboard navigation.

## Verification

- `make test` including race/tagged vet, tagged and headless builds, direct
  lint in both tag sets, and `make e2e` pass (**336 browser checks**). The
  nested mise call in `make lint` selects a mismatched Go compiler, so both
  linter commands ran directly against pinned Go 1.26.7.
- Replacing SPRT Reload with Discard deliberately caused its browser spec to
  fail on unchanged animation data; restoring the route passed. A renderer
  integration test proves two successive atomic saves still hot-reload and
  watched `entity_asset` edits update existing `comp_sprite.sheet` rows;
  removing a binding clears the sheet it owned. Overlapping watch callbacks
  serialize reload and database sync, keeping the latest saved binding live.
  Each sheet sync is a transaction: a later failure cannot leave an earlier
  entity type half-updated. The previous successfully applied binding set is
  retained so the next save can retry a removed binding; removal clears only
  sheets still matching that binding, preserving independently changed sheets.
- Statement coverage: `internal/forge/animations` **82.7%**,
  `internal/renderer` **75.2%**, `internal/forge/server` **86.9%**.
