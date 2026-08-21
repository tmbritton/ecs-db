# Story 5: App shell — menu bar, mode rail, routed modes

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** 🔲 Not started  
**Priority:** High — the frame every mode is delivered into

**Depends on:** Story 4 (primitives)

## Context

Forge is a single window, full-viewport, laid out as a column: a ~34px menu bar across the top, then a body that is a 62px mode rail on the left plus the active mode's content filling the rest. Every mode page renders that same frame around different content.

The mode rail is the app's primary navigation — six buttons, each an icon over a mono caption, with a settings cog pinned to the bottom. The active button inverts: `--ink` fill with amber border and amber glyph, against the rail's `--raised` background. The menu bar carries the `⚒ FORGE` wordmark in amber, then File · Edit · View · Map · Engine · Help, with `Engine` itself accented amber because it owns the engine-connection actions.

Each mode is a URL — `/forge/map`, `/forge/schema`, and so on — and switching modes is an ordinary page load. That is the shape Datastar asks for: the server renders a whole page, the page subscribes to an SSE stream, and everything that changes afterwards arrives as an HTML patch pushed down that stream. Interactivity lives inside a mode, not between modes. Bookmarks, deep links and the back button then work because they are real navigation, not because we reimplemented them on top of a router.

This story delivers the frame with six empty mode stubs, plus the one SSE subscription each page opens — `/forge/{mode}/events` — which later stories and epics push their updates through. The modes themselves arrive in Epics 12–20; what has to be right here is the chrome, the routing, and that single stream.

## Acceptance Criteria

- [ ] Menu bar (~34px, `--raised`, 2px bottom border `--border`):
  - `⚒ FORGE` wordmark in amber
  - File · Edit · View · Map · Engine · Help, with `Engine` amber
  - Right-aligned slot reserved for the engine-status readout (Story 6 fills it)
- [ ] Mode rail (62px, `--raised`), six 46px buttons, glyph over 10px mono caption:

  | Glyph | Caption | Route |
  |---|---|---|
  | `🗺` | MAP | `/forge/map` |
  | `▦` | TILES | `/forge/tiles` |
  | `♟` | ENTS | `/forge/ents` |
  | `⛃` | SCHEMA | `/forge/schema` |
  | `◉→◉` | AGENTS | `/forge/agents` |
  | `🧍` | SPRT | `/forge/sprites` |

  - Active: `--ink` fill, amber border and glyph. Inactive: `--border` border, `--text-dim` glyph
  - `⚙` settings cog pinned to the bottom (`margin-top: auto`)
- [ ] `GET /forge/{mode}` renders the full shell with that mode active; unknown modes 404
- [ ] Rail buttons are plain `<a href="/forge/{slug}">` — a mode switch is a full page load
- [ ] Deep-linking to `/forge/agents` works, and browser back/forward moves between modes
- [ ] `/` redirects to `/forge/map`
- [ ] Each of the six modes renders a stub identifying itself
- [ ] Each mode page opens exactly one SSE subscription to `GET /forge/{mode}/events` (stubbed here; Story 6 gives it its first payload)
- [ ] Table-driven route tests: every mode 200s and marks the right rail button active; an unknown mode 404s
- [ ] `go test ./...` passes

## Notes

- Model the rail as a typed `Mode` value with a slice of definitions (glyph, caption, slug), not six copy-pasted blocks. The same slice drives the rail, the route table and the tests.
- **Mode switching is a full page load, not a swap.** Datastar's model is: the server renders the whole page, the page subscribes to SSE, and everything after that arrives as HTML patches pushed down that stream. It has no notion of an endpoint returning a fragment representation of a resource — so there is nothing to content-negotiate and no `pushState` to write. Rail buttons are anchors. The URL, the back button, and deep links then work because they are ordinary navigation, not because we reimplemented them.
- The shell is small and its assets are cached and local, so a full load is not the cost it would be over a network. Resist the urge to optimise it into a swap; the fragment-vs-document branch that buys is exactly the complexity this model exists to avoid.
- Keep the shell free of mode-specific knowledge. It renders a `templ.Component` for the content region and nothing more; modes register themselves through the mode table.
- The menu bar items are non-functional in this story — the dialogs they open are Epic 17. Render them as disabled-looking affordances rather than dead links that appear clickable.
- Full viewport, no page scroll: the shell is `height: 100dvh` with `overflow: hidden`, and scrolling happens inside panels. Getting this wrong is easy to miss until a mode with a long list arrives.
