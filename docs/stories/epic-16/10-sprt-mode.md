# Story 10: SPRT animation and binding editor

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** ✅ Complete
**Depends on:** Story 9

## Contract

The SPRT editor works on the first assets mod's lossless `animations.toml`
session. The renderer reads column indices from `frames` and crops square
`window.tileSize` frames from the **first row** of a horizontal sprite strip.
Other rows, arbitrary rectangles and per-frame timing are not playable. An
animation's `sheet` and an `entity_asset` binding's `sheet` are distinct
authoring values; both must resolve to files in the selected mod's sprites
directory before artwork is presented as available. Editing is only a draft
until the existing file Save writes it; the running game watches that save.

## Acceptance criteria

- [x] Select an animation by URL (including a direct link and browser back),
      show the named sheet, full strip, ordered frame previews and playback
      using the actual `tileSize`, `fps` and `loop` values. Invalid or missing
      art, out-of-range frame columns, nonpositive fps and unsupported sheet
      dimensions show a specific problem, not a misleading playable preview.
      The authoring surface deliberately accepts one-row PNG strips even
      though the renderer can crop the first row of a taller PNG.
- [x] Create and edit `name`, `sheet`, ordered column indices, `fps` and `loop`
      through the Story 9 document. Support replacing a frame sequence without
      changing unrelated TOML; invalid inputs leave the held bytes untouched.
      Save/Discard/Reload/conflict use Story 9's footer and file scope. When
      art is available, frame edits that exceed its width are refused before
      they change the draft.
- [x] Show `entity_asset` bindings separately and edit/create their
      `entity_type` and `sheet` without confusing a binding with an animation.
      Report the first-mod, one-row frame and hot-reload boundaries clearly.
      Because there is no animation-to-entity declaration, show a conditional
      warning when a binding's sheet cannot play the selected animation's
      columns rather than claiming to know which entity uses that animation.
      Bound sheets use the renderer's row-zero crop rule for this warning;
      pre-existing taller sheets can still play that first row.
- [x] Server serves only selected-mod sprite images named by the held document,
      with a safe image media type. A later-mod file, symlink escape or guessed
      path cannot be served or edited through the active SPRT session.
- [x] `make test`, tagged/headless builds, both lint tag sets and `make e2e`
      pass. A deliberately broken browser edit action makes its spec fail.
      Fresh-context staged review, roadmap update, commit and push.

## Playwright steps — written before browser code

1. Open an animation via its row and direct URL, check that a real image
   request succeeds, the preview crops the named columns in authored order,
   and Back restores the prior selection. Verify a missing or out-of-range
   frame displays a named problem rather than an image that appears playable.
2. Change FPS, loop and frame order using the controls; assert a POST reaches
   the correct file and name, the draft preview changes, the footer turns
   dirty, and the TOML does **not** change until Save. After Save, verify the
   on-disk fields changed and unknown/comment bytes survived. Discard and
   conflict resolution must act on that file only.
3. Create an animation and a binding; edit their sheets independently. A
   guessed later-mod path or an out-of-scope sheet is refused. Include an
   explicit `accessibility` block for named controls, keyboard selection and
   non-color-only errors. Break the FPS or frames handler on purpose and
   confirm its browser check fails before restoring it.

## Verification

- `make test` (including race checks and tagged vet), both builds, both lint
  tag sets and `make e2e` passed: **344 browser checks**. Deliberately bypassing
  the frames edit made three browser checks fail (draft update, playback and
  invalid-frame explanation); restoring it passed.
- Statement coverage: `internal/forge/animations` **82.3%**,
  `internal/forge/server` **86.8%**, `internal/renderer` **76.1%**. The selected mod's sheet is validated
  before editing or serving, even when its image has not been created yet.
  Full PNG decoding refuses truncated pixel data that header-only dimensions
  would have advertised as playable. The game resolves assets-relative sheet
  `sprites/` names against the loaded animation file's directory before checking
  cwd, and resolves paths starting with the configured assets directory against
  game.toml's directory, even when the game starts elsewhere. Other existing
  working-directory-relative paths remain supported. The resolved path is
  cached until the TOML reload; an authored SPRT sheet is therefore opened by
  the renderer as well as by Forge.
