# Story 3: Design tokens — palette, type, and shape language

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** 🔲 Not started  
**Priority:** High — Story 4's primitives are meaningless without it

**Depends on:** Story 2 (embedded static CSS + fonts)

## Context

The handoff calls the visual language "high fidelity" and means it: the exact palette, type treatment and shape rules are specified, and the "technical instrument" look is described as load-bearing for the product's identity. `Forge Design System.dc.html` is the authoritative reference — it names every colour and states what each hue is allowed to mean. It is not in this repo; see [`README.md`](README.md) in this directory for where to find it.

The rule that matters most is the semantic one: **each hue means exactly one thing**, and nothing outside the set may be introduced. Amber is authoring, action and selection — the tool itself. Green is entities and validity. Cyan is the live source and the active statechart node. Violet is engine-computed values and the replay source. Red is danger. If something needs emphasis, it gets weight, a border, or the active-row fill, never a new colour.

Typography carries a second signal: Chakra Petch for anything the user writes, JetBrains Mono for anything the engine owns — paths, ticks, IDs, SQL, coordinates, section headings. That split is how a reader tells authored data from engine data at a glance, so it is a rule, not a preference.

This story lands the tokens as CSS custom properties and a `/dev/tokens` reference page that renders them, giving every later story something to check itself against.

## Acceptance Criteria

- [ ] `internal/forge/web/static/css/tokens.css` defines, on `:root`:
  - **Surfaces:** `--void #1a1714` (app backdrop), `--ink #201d1a` (canvas, inset wells), `--panel #252220`, `--raised #282420` (bars, rail), `--active #332e28` (selected row), `--hover #2a2622`, `--canvas #242019`
  - **Borders:** `--border #3d372f` (default 2px stroke), `--border-dashed #4a4337`, `--border-hi #5a5142` (hover/focus)
  - **Text:** `--text-hi #f2ead9`, `--text #d8d2c8`, `--text-dim #9a8f7d`, `--text-faint #6e675c`, plus `--text-secondary #b8ae9e`
  - **Accents:** `--amber #ffb454` / `--amber-hi #ffc678`, `--green #9ece6a`, `--cyan #56c5d0`, `--violet #c8a4ff`, `--red #e06c60`
  - **Type:** `--font-ui` (Chakra Petch stack), `--font-mono` (JetBrains Mono stack)
- [ ] `forge.css` sets the shape language globally:
  - `border-radius: 0` everywhere except graph ports and the low-hp halo
  - 2px solid borders; no gradients anywhere
  - `--shadow-node: 4px 4px 0 rgba(0,0,0,.25)`, `--shadow-modal: 8px 8px 0 rgba(0,0,0,.4)`
  - Body 13px `--font-ui`; section headings 10px mono, uppercase, `letter-spacing: 1.5px`, amber
  - Row rhythm: 4–5px vertical padding; panel gutters 12–14px; icon buttons 26px square
- [ ] Keyframes defined: `fpulse` (low-hp halo), `fblink`, `fdash` (marching ants)
- [ ] `GET /dev/tokens` renders the palette, both type scales, and the keyframes — the in-repo counterpart to the design-system HTML
- [ ] A test asserts no colour literal outside the token set appears in any stylesheet
- [ ] `go test ./...` passes

## Notes

- Define colours **only** as custom properties on `:root`. Every other rule references `var(--…)`. That is what makes the "no hue outside the set" test enforceable by grep.
- The `/dev/tokens` route is a development aid, not part of the app shell. Keep it behind the same server but out of the mode rail; it costs nothing and it is the fastest way to review a rendering regression.
- The prototype uses inline styles throughout because it is a single-file design reference. Do not carry that across — the whole point of the token layer is that Story 4's primitives never hard-code a colour.
- Forge is a dark-only tool. There is no light theme and no `prefers-color-scheme` handling; the palette is the product.
- Iconography in the prototype is Unicode glyphs (`⚒ ♟ ⛃ ◉ 🗺 ▦ 🧍 ⚙ 👁 ⟳ ◆ ⏸ ⑂`). Keep them for now — they need no assets and they render consistently. Swapping for a real icon set is a later, separable change.
