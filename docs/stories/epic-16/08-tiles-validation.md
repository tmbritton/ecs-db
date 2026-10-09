# Story 8: TILES validation and honest metadata boundaries

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** ✅ Complete
**Depends on:** Stories 5–7

## Contract

TILES must show why a referenced TSX/TSJ, image, tile or metadata value will
not work in the game. A broken authoring file remains discoverable and names
its failure; a usable TSX with one bad tile stays inspectable and fixable.
Validation must distinguish problems that prevent the game's import from
preserved Tiled metadata the game does not consume. A Save must not erase
unknown XML or pretend Tiled animations/polygons/wangsets control the game.

## Acceptance criteria

- [x] A referenced unreadable TSX/TSJ and unsupported extension show the
      failing path, referring maps and parser/refusal reason. Broken files are
      never offered a Save control. A valid selected tileset still renders its
      usable tiles if another referenced file fails.
- [x] Sheet/collection image absence, unsafe or unsupported format, impossible
      sheet source rectangle and absent collection image are shown at the
      relevant tileset/tile with a visible fallback. No out-of-project image
      is served through the asset route. Paged grids diagnose their shown
      page and selected tile without allocating the entire sheet.
- [x] Invalid/unknown `entityType` metadata, deprecated artwork `passable`,
      invalid typed property values and ambiguous tile class/type are
      attributed to the authoring source. Save stays available so the file
      can be repaired. Errors for refused Story 7 edits appear by the input
      as well as in the mode's summary, and a successful edit clears them.
- [x] Existing collision polygon/object-group, `<animation>`, wangset and
      terrain definitions survive edits but are identified as **Tiled-only
      metadata** in Forge; no fake gameplay polygon/terrain/animation controls
      or implicit movement toggle. Sprite animation belongs to SPRT; movement
      and sight belong to referenced entities in MAP.
- [x] Browser steps below, Go-side pinned test IDs, `make test`, both builds
      and lint tag sets, and `make e2e` pass. Deliberately break an important
      validation mark and confirm its Playwright check fails.

## Playwright steps — browser-only evidence, before code

1. Add a project-local TSX with an image that cannot be served and another
   with a declared image rectangle too small for one tile. Open TILES, click
   the affected IDs and assert tile-specific warnings, missing-art fallbacks
   and a 404 for a project-external symlink; unrelated sheet tiles still draw.
2. Add a hand-authored tile with unknown `entityType`, deprecated `passable`
   and a malformed typed value. Assert reasons attach to that tile and clear
   after a working metadata edit/reload, while Save remains available.
3. Load TSX containing Tiled collision object groups, `<animation>`,
   wangsets/terrains and unknown XML; inspect the page's explicit "preserved,
   not simulated" note, edit class, Save and confirm those bytes survive.
4. Select a malformed TSX/TSJ or an unsupported extension. Assert its
   map-attributed reason and no edit controls. Include an explicit
   `accessibility` block for text warnings, error association and keyboard
   selection rather than relying on warning colour alone.

## Verification

- `make test` including race tests and tagged vet, tagged/headless builds,
  both direct linter tag sets and `make e2e` pass (**332 browser checks**).
  The nested mise call in `make lint` selects a mismatched compiler; both
  linter invocations ran directly with pinned Go 1.26.7.
- Deliberately disabling the tile-grid warning mark made the missing-art
  Playwright check fail. The mark was restored before the full suite.
- Statement coverage: `internal/forge/tilesetvalidation` **91.8%**,
  `internal/forge/tilesurface` **95.4%**, `internal/tiled` **94.3%**,
  `internal/tilemap` **87.7%** and `internal/forge/server` **87.0%**.
