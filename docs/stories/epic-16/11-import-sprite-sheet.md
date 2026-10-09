# Story 11: Import a project-local sprite sheet in SPRT

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** ✅ Complete
**Depends on:** Stories 9–10

## Contract

SPRT offers a project-local PNG picker and a scoped import dialog. The chosen
source must be a regular file within the project; neither an absolute guessed
path outside the project nor a symlink into another directory qualifies. A
successful import copies that source into the selected first assets mod's
`sprites/` directory without replacing existing art. It decodes the complete
image and accepts only one row of `window.tileSize`-square columns. This
duplicates the actual authoring restriction from Story 10, not arbitrary
cropping. A `[[animation]]` and optional `[[entity_asset]]` reference use the
copied image's assets-relative `sprites/<name>.png` path. Creating those
references is a **draft** under Story 9's Save/Discard/Reload/conflict model;
the image copy itself happens immediately and is named as such in the UI.

Epic 17's generic asset dialog must reuse this import workflow instead of
offering a second way to import sprite strips.

## Acceptance criteria

- [x] A dialog in SPRT lists project-local PNG files, picks one, names a new
      animation and optionally an entity type, and explains the immediate
      image copy versus unsaved TOML draft. Keyboard selection and dismissal
      work; no dead or misleading upload control.
- [x] Decode the complete source PNG. Require height exactly `tileSize`, width
      a positive multiple of `tileSize`, and a bounded, nonempty number of
      columns. Reject malformed/truncated images, wrong dimensions, a missing
      or unsafe path and duplicate animation or binding names before copying.
- [x] Copy without overwriting an existing sprite or exposing half-written PNG
      bytes to the game watcher. A failed copy leaves both the target and the
      working TOML unchanged; a successful copy creates `frames = [0,1,…]`,
      a positive FPS and an optional entity binding in the working document.
- [x] The new animation's art is served by the scoped SPRT image route and
      becomes playable after Save in the running game with the same sheet
      path. The existing file's unknown TOML and unrelated tables survive.
- [x] `make test`, tagged/headless builds, both lint sets and `make e2e`
      pass, including a deliberately broken browser import action making the
      browser spec fail. Update roadmap and coverage; obtain a fresh-context
      staged review, commit and push. Then pause for user planning of the
      gameplay vertical slice.

## Playwright steps — before browser code

1. Open SPRT's Import dialog by keyboard. Inspect the offered project PNGs,
   pick one, enter animation and optional entity type, submit and assert the
   request copies a real PNG under the selected mod. Check the new animation
   and binding appear in the draft with ordered frame columns, the image route
   returns the copied art, and the footer becomes dirty while TOML on disk is
   unchanged until Save.
2. Save, reload the page and verify the authored animation/binding still resolve
   and unrelated comments remain. Try a duplicate destination, malformed or
   multi-row source and an unlisted/escaping path; assert no target file or
   working document changes and a visible refusal names the reason.
3. Carry an explicit `accessibility` block: dialog heading, labels, keyboard
   selection/dismissal, and error text. Break the import POST handler on
   purpose, confirm the browser test fails, restore it.

## Verification

- `make test` including race/tagged vet, both builds, both lint tag sets and
  `make e2e` passed: **349 browser checks**. Sending an empty source through
  the import handler made the success and invalid-dimension browser checks fail;
  restoring the real source passed.
- Statement coverage: `internal/forge/animations` **81.3%**,
  `internal/forge/server` **86.8%**. A no-replace hard link inside a pinned
  destination-directory handle rooted in the project publishes only complete `.png` bytes from a
  `.tmp` copy; an existing destination is never
  overwritten. Uppercase `.PNG` sources are copied under lowercase `.png` so
  the game's sprite watcher sees future updates. The TOML remains a draft until
  Save, even after the image has been copied. The source is opened through a
  project-root-bound directory handle and checked against the regular file
  listed before and after open, so a swapped file or parent-directory symlink
  cannot substitute an outside PNG. Swapping the destination directory's
  pathname cannot redirect the copy outside the selected assets mod either;
  the selected assets directory is itself opened relative to the pinned
  project handle.
