# Epic 10 Story 3: Design tokens — Implementation Plan

**Goal:** every colour, font and shape rule in Forge resolves to a named token, and `/dev/tokens` proves it.

**Architecture:** Three stylesheets, loaded in order — `fonts.css` (from Story 2), `tokens.css` (declarations only, no rules), `forge.css` (element defaults and shared utility classes, referencing tokens exclusively). The layering is what makes the "no stray hue" check a one-line grep.

**Depends on:** Story 2.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/web/static/css/tokens.css` | Custom properties only |
| Modify | `internal/forge/web/static/css/forge.css` | Reset, element defaults, shape language, keyframes |
| Modify | `internal/forge/templates/layout.templ` | Link `tokens.css` before `forge.css` |
| Create | `internal/forge/templates/devtokens.templ` | The reference page |
| Modify | `internal/forge/server/server.go` | `GET /dev/tokens` route |
| Create | `internal/forge/web/tokens_test.go` | Palette-containment test |

---

## Task 1: `tokens.css`

Declarations only — no selectors other than `:root`.

```css
:root {
  /* surfaces */
  --void:   #1a1714;  /* app backdrop */
  --ink:    #201d1a;  /* canvas, inset wells */
  --inset:  #1c1916;  /* deep inset (DDL block) */
  --panel:  #252220;
  --raised: #282420;  /* bars, mode rail */
  --canvas: #242019;
  --active: #332e28;  /* selected row */
  --hover:  #2a2622;

  /* borders */
  --border:        #3d372f;
  --border-dashed: #4a4337;
  --border-hi:     #5a5142;

  /* text */
  --text-hi:        #f2ead9;
  --text:           #d8d2c8;
  --text-secondary: #b8ae9e;
  --text-dim:       #9a8f7d;
  --text-faint:     #6e675c;

  /* semantic accents — each hue means exactly one thing */
  --amber:    #ffb454;  /* authoring, action, selection, the tool itself */
  --amber-hi: #ffc678;
  --green:    #9ece6a;  /* entities, valid, on/true */
  --cyan:     #56c5d0;  /* live source, active statechart node */
  --violet:   #c8a4ff;  /* engine-computed values, replay source */
  --red:      #e06c60;  /* danger, collision, breakpoint hit */

  /* type */
  --font-ui:   'Chakra Petch', system-ui, sans-serif;
  --font-mono: 'JetBrains Mono', ui-monospace, monospace;

  /* shape */
  --border-w:     2px;
  --shadow-node:  4px 4px 0 rgba(0, 0, 0, .25);
  --shadow-modal: 8px 8px 0 rgba(0, 0, 0, .4);

  /* rhythm */
  --row-pad:    5px;
  --gutter:     13px;
  --icon-size: 26px;
}
```

---

## Task 2: `forge.css`

Reset, element defaults, the shape language, and the shared utility classes the primitives will lean on.

```css
*, *::before, *::after { box-sizing: border-box; border-radius: 0; }

html, body {
  margin: 0;
  background: var(--void);
  color: var(--text);
  font-family: var(--font-ui);
  font-size: 13px;
  line-height: 1.45;
}

.mono { font-family: var(--font-mono); }

/* Section heading: 10px mono caps, amber, wide tracking. */
.section-heading {
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 1.5px;
  text-transform: uppercase;
  color: var(--amber);
}

/* Engine-owned values read as mono regardless of context. */
.value { font-family: var(--font-mono); color: var(--text-hi); }

/* The ƒ badge: a value the engine computes at spawn. */
.badge-ctx {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--violet);
  background: rgb(200 164 255 / .12);
  padding: 1px 7px;
}

@keyframes fpulse { 0%,100% { opacity: 1 } 50% { opacity: .35 } }
@keyframes fblink { 0%,49% { opacity: 1 } 50%,100% { opacity: 0 } }
@keyframes fdash  { to { stroke-dashoffset: -12 } }
```

The two documented exceptions to `border-radius: 0` are opt-in classes, not overrides of the reset:

```css
.port { border-radius: 50%; }        /* statechart graph ports (Epic 13) */
.halo { border-radius: 50%; animation: fpulse 1.2s infinite; }  /* low-hp entity (Epic 18) */
```

Update `layout.templ` to link `tokens.css` before `forge.css`.

---

## Task 3: `/dev/tokens`

`devtokens.templ` renders, inside `Layout("Forge — tokens")`:

1. **Palette** — a swatch grid per group (surfaces, borders, text, accents), each swatch labelled with its token name, hex, and the one thing the hue is allowed to mean.
2. **Type** — Chakra Petch at 26/17/13/11px and JetBrains Mono at 14/12/11/10px, with a real example of each mono use (path, tick counter, `ƒ` badge, panel header).
3. **Shape** — a bordered card with `--shadow-node`, a modal-ish card with `--shadow-modal`, and the three keyframes running.

Route it in `server.routes()`:

```go
mux.HandleFunc("GET /dev/tokens", s.handleDevTokens)
```

Render swatches by driving a Go slice of `{Token, Hex, Meaning}` through the template rather than hand-writing 25 divs — the same slice then feeds Task 4's test.

---

## Task 4: Palette-containment test

The rule "never introduce a hue outside this set" only holds if something checks it.

### `internal/forge/web/tokens_test.go`

```go
package web

// Palette is the complete set of colour literals Forge is allowed to use.
// Adding to it is a design decision, not a code change — see
// the Forge Design System prototype (see docs/stories/epic-10/README.md).
var Palette = map[string]string{ /* token -> hex, mirroring tokens.css */ }

func TestStylesheets_NoColourOutsidePalette(t *testing.T) {
	// Walk every *.css in the embedded FS.
	// Extract #rgb/#rrggbb literals and rgb()/rgba() functional colours.
	// Allowed: any hex in Palette (case-insensitive); rgb()/rgba() only where
	// the alpha channel is < 1 (shadows and the ƒ badge tint).
	// Fail with the offending file, line and literal.
}
```

Two table-driven helpers make this readable and independently testable: `extractColours(css string) []colourRef` and `isAllowed(colourRef) bool`. Test those directly with a table (bare hex, uppercase hex, `rgb()` opaque, `rgba()` translucent, hex inside a comment, hex inside a `url()` data URI) before wiring the walk.

Also assert the inverse — every token in `Palette` actually appears in `tokens.css` — so the map cannot rot into a list of colours nobody uses.

---

## Task 5: Mark story complete

Tick `docs/stories/epic-10/03-design-tokens.md`, add `## As Implemented`, tick **Design tokens** in `docs/plan.md`.

---

## Verification

```bash
make generate && go test ./internal/forge/... -run Colour -v
go test ./...
make build-headless && ./bin/ecs-db-headless forge
```

Open `http://127.0.0.1:7777/dev/tokens` beside the `Forge Design System.dc.html` prototype (see [`README.md`](README.md)) and compare section by section: every swatch matches, both type scales match, no corner is rounded, no gradient appears, and the three keyframes animate.

Deliberately break it once to prove the test bites — add `color: #ff00ff` to `forge.css`, confirm `TestStylesheets_NoColourOutsidePalette` fails naming the file and line, then revert.

Before committing, launch a fresh-context code-review subagent over the staged diff, per `AGENTS.md`.
