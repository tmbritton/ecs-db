# Story 4: Templ primitives — the shared component library

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** 🔲 Not started  
**Priority:** High — Epics 12–20 all render through these

**Depends on:** Story 3 (tokens)

## Context

Forge is six modes and nine dialogs built from a small, repeating vocabulary: bordered panels with mono section headings, dense selectable list rows, component chips with lock and remove affordances, segmented controls, dropdowns, checkboxes, context menus, modal shells and a save footer. The prototype re-implements each of these inline, dozens of times, because it is a single-file design reference. Building them once as typed templ components is what keeps the later modes small and consistent.

Every primitive takes a props struct and reads its colours from tokens. None of them hard-codes a hex value, and none accepts a raw style string — if a caller needs a variant, it becomes an enumerated field on the props struct so the set of possible appearances stays finite and reviewable.

The two rows that carry meaning beyond layout are worth naming up front. A **chip** shows `🔒` when the component is required by the entity type and `✕` when it is optional and detachable — the lock is not decoration, it is the schema contract. A **layer row** is used both for real map layers and for SQL-backed debug overlays, which is why query layers can be toggled like any other layer.

## Acceptance Criteria

- [ ] `internal/forge/templates/components/` contains, each with a props struct and no inline colour:
  - `Panel` — bordered container with optional mono `SectionHeading` and a fixed width
  - `SectionHeading` — 10px mono caps, amber, with an optional right-aligned source suffix (e.g. `◂ world.sqlite` in cyan)
  - `ListRow` — selectable dense row; `Active` gives amber left-border + `--active` fill; optional leading glyph and trailing value
  - `Chip` — component chip; `Required` renders `🔒`, otherwise `✕`; optional `ƒ ctx` badge
  - `SegmentedControl` — n-way selector; used for the AUTHORED/LIVE/REPLAY source lens and the TILES tabs
  - `Dropdown` — label + `▾`, options with a selected value
  - `Checkbox` — `✓` in green when on, empty bordered box when off
  - `IconButton` — 26px square, bordered, glyph only, with a `Title` for the tooltip
  - `ContextMenu` — mono caps title, rows with right-aligned shortcut hints, 2px divider, danger rows in red
  - `ModalShell` — centred card, header with mono uppercase title + `✕`, scrollable body, footer slot
  - `SaveFooter` — `● unsaved` (amber) vs `✓ saved`, with Save and Discard enabled only when dirty
- [ ] Variants are enumerated Go types (e.g. `ButtonVariant`, `LensSource`), never free-form strings
- [ ] `/dev/tokens` gains a components section rendering every primitive in each of its states (default / hover / active / disabled, and required / optional for chips)
- [ ] Table-driven render tests per primitive asserting the state-carrying output — active row emits the active class, required chip emits `🔒` and not `✕`, unchecked checkbox emits no `✓`, non-dirty save footer disables both buttons
- [ ] `go test ./...` passes

## Notes

- Test templ components by rendering to a `bytes.Buffer` via `Component.Render(ctx, &buf)` and asserting on the output. Assert on the *meaningful* signal — a class name, a glyph, a `disabled` attribute — not on whole-HTML equality, which turns every future style tweak into a test failure.
- Where a primitive needs child content, use templ's `{ children... }` rather than passing HTML strings, so callers cannot inject unescaped markup.
- A "Datastar action URL" is not a REST endpoint returning a resource. `@post('/forge/schema/field')` sends the page's signals and the server answers with a stream of `datastar-patch-elements` / `datastar-patch-signals` events — HTML and state pushed back, never JSON for the client to render. Primitives therefore carry a URL and nothing else; what comes back is the server's business.
- Hover and focus states belong in `forge.css` keyed off the primitive's class, not in Go. The props struct describes *what the thing is*, CSS describes *how it responds*.
- `SaveFooter` is shared across SCHEMA, ENTS, AGENTS and MAP. It needs to be dumb — it takes `Dirty bool` and two Datastar action URLs, and knows nothing about what is being saved.
- Density is specified and easy to lose: rows 4–5px vertical padding, panel gutters 12–14px, panels 210–290px wide, icon buttons 26px. Check against the design system page rather than eyeballing.
