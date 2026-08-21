# Story 4: Templ primitives — the shared component library

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** ✅ Complete  
**Priority:** High — Epics 12–20 all render through these

**Depends on:** Story 3 (tokens)

## Context

Forge is six modes and nine dialogs built from a small, repeating vocabulary: bordered panels with mono section headings, dense selectable list rows, component chips with lock and remove affordances, segmented controls, dropdowns, checkboxes, context menus, modal shells and a save footer. The prototype re-implements each of these inline, dozens of times, because it is a single-file design reference. Building them once as typed templ components is what keeps the later modes small and consistent.

Every primitive takes a props struct and reads its colours from tokens. None of them hard-codes a hex value, and none accepts a raw style string — if a caller needs a variant, it becomes an enumerated field on the props struct so the set of possible appearances stays finite and reviewable.

The two rows that carry meaning beyond layout are worth naming up front. A **chip** shows `🔒` when the component is required by the entity type and `✕` when it is optional and detachable — the lock is not decoration, it is the schema contract. A **layer row** is used both for real map layers and for SQL-backed debug overlays, which is why query layers can be toggled like any other layer.

## Acceptance Criteria

- [x] `internal/forge/templates/components/` contains, each with a props struct and no inline colour:
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
- [x] Variants are enumerated Go types (e.g. `ButtonVariant`, `LensSource`), never free-form strings
- [x] `/dev/tokens` gains a components section rendering every primitive in each of its states (default / hover / active / disabled, and required / optional for chips)
- [x] Table-driven render tests per primitive asserting the state-carrying output — active row emits the active class, required chip emits `🔒` and not `✕`, unchecked checkbox emits no `✓`, non-dirty save footer disables both buttons
- [x] `go test ./...` passes

## Notes

- Test templ components by rendering to a `bytes.Buffer` via `Component.Render(ctx, &buf)` and asserting on the output. Assert on the *meaningful* signal — a class name, a glyph, a `disabled` attribute — not on whole-HTML equality, which turns every future style tweak into a test failure.
- Where a primitive needs child content, use templ's `{ children... }` rather than passing HTML strings, so callers cannot inject unescaped markup.
- A "Datastar action URL" is not a REST endpoint returning a resource. `@post('/forge/schema/field')` sends the page's signals and the server answers with a stream of `datastar-patch-elements` / `datastar-patch-signals` events — HTML and state pushed back, never JSON for the client to render. Primitives therefore carry a URL and nothing else; what comes back is the server's business.
- Hover and focus states belong in `forge.css` keyed off the primitive's class, not in Go. The props struct describes *what the thing is*, CSS describes *how it responds*.
- `SaveFooter` is shared across SCHEMA, ENTS, AGENTS and MAP. It needs to be dumb — it takes `Dirty bool` and two Datastar action URLs, and knows nothing about what is being saved.
- Density is specified and easy to lose: rows 4–5px vertical padding, panel gutters 12–14px, panels 210–290px wide, icon buttons 26px. Check against the design system page rather than eyeballing.

## As Implemented

Eleven primitives in `internal/forge/templates/components/`, one props struct each, no colour and no raw style or class string in any of them. `types.go` holds the enumerated variants (`LensSource`, `Accent`, `ChipKind`, `Tone`, `ButtonVariant`) and the helpers the templates share: `classes`, `when`, `pxWidth`, `boolAttr`, `listRowClass`.

Coverage: `components` 74.3%, `templates` 66.9% (both held down by templ's generated `if err != nil { return }` branch after every write, which a `bytes.Buffer` cannot reach). `internal/forge/server` 93.9% and `web` 75.0% unchanged.

### Divergences from the plan

- **`Tone` replaces the plan's `Muted bool`.** Muted and danger are mutually exclusive content colours, and two parallel booleans let a caller ask for both at once — exactly the open-ended appearance the story is trying to close. `Active` stays a bool because selection is genuinely orthogonal to tone.
- **`SaveFooter` gained a `File string`.** The story says it takes only `Dirty` plus two actions, but the prototype's hint is `● unsaved · overworld.tmx` — the filename is the whole point of the line. It is a display label; the footer still knows nothing about what saving means.
- **`PanelProps` has no `Class` field**, though the plan specified one. It is precisely the escape hatch this package's stated rule exists to close, and it would let one primitive inject another's state modifier.
- **`Component` is aliased inside the package.** `ModalShellProps.Footer` needs a component-valued field, and importing templ in a `.templ` file collides with the import templ's own codegen adds.

### Design decisions worth knowing later

- **Attribute syntax is `data-on:click`, colon-separated** — see below; it is the single most important thing this story established, and every later epic depends on getting it right.
- **Controls bind to signals, not form names.** `Dropdown` and `Checkbox` take a `Signal` and emit `data-bind`. An action posts the page's *signals*; a plain HTML `name` attribute contributes nothing outside a form submission, which this architecture does not have. A control with an action but no binding fires a request the server cannot interpret.
- **`SegmentedControl`'s action moved onto `SegmentOption`.** One group-level expression could not say which segment was pressed, for the same reason: `value=` is not a signal, so N buttons sharing one expression are indistinguishable server-side.
- **A `ListRow` with an `Action` renders as a `<button>`**, not a div with a click handler, so it is keyboard-reachable and activates on Enter and Space with no handler of our own. Selection is announced with `aria-current`, which is valid standalone; a mode that wraps a list in `role="listbox"` can upgrade to `role="option"` + `aria-selected`.
- **`ModalShell` dismisses via an explicit `evt.target === el` test on the backdrop.** Neither a bare backdrop handler nor the `__outside` modifier expresses "clicked the scrim" — see the review findings below.
- **`Checkbox` renders its `✓` server-side** and drives the label colour from the same flag, so both halves have one source of truth. The real input is clipped, not `display: none`, so it keeps its place in the focus order.
- **A disabled `IconButton` drops its `data-on-click`,** and an `IconButton` with no `Title` emits neither `title` nor `aria-label` — an empty `aria-label` is skipped by accname, leaving the button announced as its geometric glyph.
- **The modal scrim is `rgba(0, 0, 0, 0.62)`,** not the prototype's `rgba(16, 14, 12, 0.62)`. Over `--void` the two are indistinguishable, and pure black keeps every colour in `forge.css` inside the palette the guard enforces.
- **`btn--*` are shared modifiers**, not `icon-btn` internals — the same five variants will dress the text buttons the modes need (`SAVE → assets/`, `+ ADD COMPONENT`) without a second vocabulary.

### What the gallery caught

`ButtonPrimary` was the zero value of `ButtonVariant`, so every call site that did not name a variant got the amber commit fill — the most emphatic control in the design language, handed out by default. It was visible the moment the icon-button row rendered: the button labelled "ghost" came out amber. `ButtonGhost` is now the zero value, with `TestButtonVariant_DefaultsToGhost` covering `0`, the named constant, and two out-of-range values; the guard was verified to fail against the old ordering.

Two smaller fixes came from reading the rendered page rather than the code: `.section-heading__source` was inheriting the heading's `text-transform: uppercase`, so a live-data suffix rendered `◂ WORLD.SQLITE` — a path is engine-owned text and must not be case-mangled. And the `&#32;` entities used for spacing became real elements CSS can space.

### The bug that mattered: every handler was inert

Driving the rendered page with Playwright — clicking things, not just screenshotting them — showed that **no Datastar action fired anywhere**. The engine was alive (`data-text` worked), but every *keyed* attribute was silently ignored.

Datastar v1.0.2 separates a plugin from its key with a **colon**. Reading the vendored bundle's key parser settles it:

```js
hn = e => { let [t, ...n] = e.split("__"), [r, s] = t.split(/:(.+)/); … }
```

So it is `data-on:click`, not `data-on-click` — the latter parses as a plugin named `on-click`, which is not registered, and is skipped with no error, no console warning and no visual difference. Every `data-on-click` and `data-on-change` in this package was dead markup, and would have stayed dead through Story 5 until someone wondered why nothing responded.

`datastar_test.go` is the guard: it renders every primitive, extracts the `data-*` attributes, parses each the way the bundle does, and asserts the plugin name is one the **vendored bundle actually registers** — the plugin list is scraped from `datastar.js` rather than hardcoded, so a version bump that renames or drops a plugin fails here instead of shipping mute UI. It also asserts the scan found handlers at all, so it cannot pass by having nothing to check. Verified to fail when a single attribute is reverted to the dash form.

The gallery is now wired with real actions to a `POST /dev/noop` sink returning 204, and a browser pass confirms each primitive fires: list row, chip remove, icon button, segment, menu item and scrim all post; clicking the page background and clicking inside the dialog post nothing.

### What the code review caught

- **The modal closed on every click inside it.** Datastar compiles `data-on:*` to a plain `addEventListener` with no target filtering — verified against the pinned bundle, which only checks the target under the `outside` modifier. A dismiss handler on the backdrop fired for every click bubbling out of the dialog, and double-fired alongside the ✕ button. The first fix, `__outside` on the card, was wrong in the other direction: that modifier binds at the *document* and asks "was this click outside the card", so it fired for clicks anywhere else on the page — and the gallery renders a permanently-present dialog, so every click on `/dev/tokens` would have posted to it. It is now `evt.target === el && (…)` on the backdrop, which is an exact scrim test. `el` and `evt` are both in expression scope: the runtime compiles `Function("el", "$", "__action", "evt", …)`.
- **A developer comment was shipping to the browser.** templ emits HTML comments verbatim, so an (incorrect) internal note about event scoping would have been in every dialog's wire bytes. There is now a test that the shell renders no `<!--`.
- **The `SaveFooter` "clean is not wired" test was vacuous.** The clean case passed no actions, so the `props.Dirty &&` half of the template guard was never exercised — the reviewer proved it by rewriting both guards to drop the dirty check and watching the suite stay green. The case now passes real actions, and the same mutation was re-run to confirm it fails.
- **`Checkbox` had two sources of truth.** The box followed the server; a `:checked` CSS rule made the label follow the browser. They desynchronised on the first click — visibly broken on `/dev/tokens` itself, where nothing is wired.
- **Hover erased the selection fill.** `.list-row:hover` outranked `.list-row--active`, so pointing at the selected row swapped `--active` for `--hover`. Same defect for `.segment`. Both now use `:not(...)`.
- **The section-heading source suffix was never right-aligned** — an acceptance criterion with no rule behind it. `.section-heading` is now a flex row.
- Accessible-name fixes: the chip's remove button was announced as "✕" rather than "detach component" (text content wins over `title`), `.chip__lock`'s `title` on a non-interactive span was not announced at all, and `role="menu"` had an unnamed, non-permitted `<div>` child.
- `--row-pad` had gone dead — `.list-row` hardcoded its padding — and `--amber-hi` had no hover to attach to, since `.btn--primary` and `.btn--outline` were the only two variants with no `:hover` rule.
- Several `TestDevTokens_RendersEveryPrimitive` markers were satisfiable by unrelated elements (`✕` is drawn by the chip, an icon button *and* every modal close). Tightened to markers unique to each primitive.

`TestDevTokens_RendersEveryPrimitive` renders the whole reference page and asserts every primitive and meaning-carrying state appears on it, so a primitive cannot quietly drop out of the page a visual change is reviewed against. Verified to fail when `@devComponents()` is removed.
