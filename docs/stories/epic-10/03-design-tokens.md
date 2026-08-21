# Story 3: Design tokens — palette, type, and shape language

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** ✅ Complete  
**Priority:** High — Story 4's primitives are meaningless without it

**Depends on:** Story 2 (embedded static CSS + fonts)

## Context

The handoff calls the visual language "high fidelity" and means it: the exact palette, type treatment and shape rules are specified, and the "technical instrument" look is described as load-bearing for the product's identity. `Forge Design System.dc.html` is the authoritative reference — it names every colour and states what each hue is allowed to mean. It is not in this repo; see [`README.md`](README.md) in this directory for where to find it.

The rule that matters most is the semantic one: **each hue means exactly one thing**, and nothing outside the set may be introduced. Amber is authoring, action and selection — the tool itself. Green is entities and validity. Cyan is the live source and the active statechart node. Violet is engine-computed values and the replay source. Red is danger. If something needs emphasis, it gets weight, a border, or the active-row fill, never a new colour.

Typography carries a second signal: Chakra Petch for anything the user writes, JetBrains Mono for anything the engine owns — paths, ticks, IDs, SQL, coordinates, section headings. That split is how a reader tells authored data from engine data at a glance, so it is a rule, not a preference.

This story lands the tokens as CSS custom properties and a `/dev/tokens` reference page that renders them, giving every later story something to check itself against.

## Acceptance Criteria

- [x] `internal/forge/web/static/css/tokens.css` defines, on `:root`:
  - **Surfaces:** `--void #1a1714` (app backdrop), `--ink #201d1a` (canvas, inset wells), `--panel #252220`, `--raised #282420` (bars, rail), `--active #332e28` (selected row), `--hover #2a2622`, `--canvas #242019`
  - **Borders:** `--border #3d372f` (default 2px stroke), `--border-dashed #4a4337`, `--border-hi #5a5142` (hover/focus)
  - **Text:** `--text-hi #f2ead9`, `--text #d8d2c8`, `--text-dim #9a8f7d`, `--text-faint #6e675c`, plus `--text-secondary #b8ae9e`
  - **Accents:** `--amber #ffb454` / `--amber-hi #ffc678`, `--green #9ece6a`, `--cyan #56c5d0`, `--violet #c8a4ff`, `--red #e06c60`
  - **Type:** `--font-ui` (Chakra Petch stack), `--font-mono` (JetBrains Mono stack)
- [x] `forge.css` sets the shape language globally:
  - `border-radius: 0` everywhere except graph ports and the low-hp halo
  - 2px solid borders; no gradients anywhere
  - `--shadow-node: 4px 4px 0 rgba(0,0,0,.25)`, `--shadow-modal: 8px 8px 0 rgba(0,0,0,.4)`
  - Body 13px `--font-ui`; section headings 10px mono, uppercase, `letter-spacing: 1.5px`, amber
  - Row rhythm: 4–5px vertical padding; panel gutters 12–14px; icon buttons 26px square
- [x] Keyframes defined: `fpulse` (low-hp halo), `fblink`, `fdash` (marching ants)
- [x] `GET /dev/tokens` renders the palette, both type scales, and the keyframes — the in-repo counterpart to the design-system HTML
- [x] A test asserts no colour literal outside the token set appears in any stylesheet
- [x] `go test ./...` passes

## As Implemented

- `tokens.css` holds declarations only — no rules. That separation is what makes
  the containment test enforceable by parsing, rather than by discipline.
- `forge.css` carries the reset, element defaults, shape language, the three
  keyframes, and the `/dev/tokens` page styles. Every colour is a `var(--token)`.
- **Palette containment is tested — and the first version of that test failed
  open.** It is now the guard for the whole design language, so it has its own
  20-case table test. Each case is a leak an earlier version let through:
  - `stripComments` had no string-literal awareness, so a `/*` inside
    `content: "…"` opened a comment that never closed and **silently discarded
    the rest of the file**. `a{content:"/*";color:#ff00ff}` passed clean.
  - `isAllowed` checked only the alpha channel, never the RGB triple, so
    `rgba(255,0,255,.5)` — magenta — was allowed. Alpha was also compared as a
    string, so `1.00` read as translucent.
  - Named colours, `hsl()` and gradients were invisible: `color: red` passed, and
    so did `linear-gradient(#ffb454,#9ece6a)` despite "no gradients" being an AC.
  - `#id` selectors were scanned as colours; CSS Color 4 space syntax
    (`rgb(0 0 0 / 50%)`) and 8-digit hex were rejected though both are valid.
  - The scan was non-recursive over `css/` only, with no assertion that any file
    was found — so a broken embed would have passed vacuously.

  It now strips comments quote-aware, scans declaration *values* rather than
  whole lines, walks every `.css` in the embedded tree, and requires translucent
  `rgba()` to carry a palette hue (or pure black/white) with a parsed alpha < 1.
- The inverse is tested too: every entry in `Palette` must be declared in
  `tokens.css`, so the allow-list cannot rot into colours nobody uses.
- **Deviation:** `swatchStyle` returns `templ.SafeCSS` — the one place a colour
  literal legitimately reaches an inline style, since a swatch has to paint the
  hex it documents.
- **Deviation:** the accents render in a dedicated 5-column row
  (`swatchGroupWide`). The first cut used the same 4-column grid as every group
  and orphaned `--red` onto its own line; the design reads the five hues as one
  statement, so they belong on one row. Caught by screenshotting `/dev/tokens`
  beside the prototype rather than by reading the CSS.
- `prefers-reduced-motion` disables every animation. The first cut selected
  `[class*='fdash']` — but `fdash` is a *keyframes* name, not a class, so it
  matched nothing and the marching ants kept running. Two tests now guard both
  directions: every selector in that block must name a class that exists, and
  every class that sets an `animation` must appear in the block.
- `--amber-hi` was in `tokens.css` and the palette but never rendered on
  `/dev/tokens`. The hexes had drifted across four copies, so `Palette` moved
  into production code (`web/palette.go`) as the single source, and a test now
  asserts the page's swatch groups cover it exactly — no missing tokens, no
  extras, no duplicates.
- `swatchStyle` validates its input against `^#[0-9a-fA-F]{6}$` and returns
  empty otherwise. `templ.SafeCSS` bypasses sanitisation by design; the guard
  belongs at the boundary rather than depending on every caller passing a
  constant.

- `--inset #1c1916` is an addition to the AC's surface list, for the
  generated-DDL block in Epic 12.

Verified by rendering `/dev/tokens` in headless chromium and comparing against
the design-system prototype side by side.

## Notes

- Define colours **only** as custom properties on `:root`. Every other rule references `var(--…)`. That is what makes the "no hue outside the set" test enforceable by grep.
- The `/dev/tokens` route is a development aid, not part of the app shell. Keep it behind the same server but out of the mode rail; it costs nothing and it is the fastest way to review a rendering regression.
- The prototype uses inline styles throughout because it is a single-file design reference. Do not carry that across — the whole point of the token layer is that Story 4's primitives never hard-code a colour.
- Forge is a dark-only tool. There is no light theme and no `prefers-color-scheme` handling; the palette is the product.
- Iconography in the prototype is Unicode glyphs (`⚒ ♟ ⛃ ◉ 🗺 ▦ 🧍 ⚙ 👁 ⟳ ◆ ⏸ ⑂`). Keep them for now — they need no assets and they render consistently. Swapping for a real icon set is a later, separable change.
