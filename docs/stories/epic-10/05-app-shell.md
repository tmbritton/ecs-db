# Story 5: App shell — menu bar, mode rail, routed modes

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** ✅ Complete  
**Priority:** High — the frame every mode is delivered into

**Depends on:** Story 4 (primitives)

## Context

Forge is a single window, full-viewport, laid out as a column: a ~34px menu bar across the top, then a body that is a 62px mode rail on the left plus the active mode's content filling the rest. Every mode page renders that same frame around different content.

The mode rail is the app's primary navigation — six buttons, each an icon over a mono caption, with a settings cog pinned to the bottom. The active button inverts: `--ink` fill with amber border and amber glyph, against the rail's `--raised` background. The menu bar carries the `⚒ FORGE` wordmark in amber, then File · Edit · View · Map · Engine · Help, with `Engine` itself accented amber because it owns the engine-connection actions.

Each mode is a URL — `/forge/map`, `/forge/schema`, and so on — and switching modes is an ordinary page load. That is the shape Datastar asks for: the server renders a whole page, the page subscribes to an SSE stream, and everything that changes afterwards arrives as an HTML patch pushed down that stream. Interactivity lives inside a mode, not between modes. Bookmarks, deep links and the back button then work because they are real navigation, not because we reimplemented them on top of a router.

This story delivers the frame with six empty mode stubs, plus the one SSE subscription each page opens — `/forge/{mode}/events` — which later stories and epics push their updates through. The modes themselves arrive in Epics 12–20; what has to be right here is the chrome, the routing, and that single stream.

## Acceptance Criteria

- [x] Menu bar (~34px, `--raised`, 2px bottom border `--border`):
  - `⚒ FORGE` wordmark in amber
  - File · Edit · View · Map · Engine · Help, with `Engine` amber
  - Right-aligned slot reserved for the engine-status readout (Story 6 fills it)
- [x] Mode rail (62px, `--raised`), six 46px buttons, glyph over 10px mono caption:

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
- [x] `GET /forge/{mode}` renders the full shell with that mode active; unknown modes 404
- [x] Rail buttons are plain `<a href="/forge/{slug}">` — a mode switch is a full page load
- [x] Deep-linking to `/forge/agents` works, and browser back/forward moves between modes
- [x] `/` redirects to `/forge/map`
- [x] Each of the six modes renders a stub identifying itself
- [x] Each mode page opens exactly one SSE subscription to `GET /forge/{mode}/events` (stubbed here; Story 6 gives it its first payload)
- [x] Table-driven route tests: every mode 200s and marks the right rail button active; an unknown mode 404s
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/05-app-shell.spec.js`. What is worth testing in a browser here is not
the markup — Go covers that — but the things only a browser knows: that a link
actually navigates, that history works, and that the page really opens its
subscription.

- [x] `/` lands in MAP with its rail button active
- [x] The frame and the correct page title render on all six modes
- [x] Exactly one rail button is active, and it is this mode's
- [x] Each mode renders a stub naming itself and the epic that fills it
- [x] Clicking each rail button navigates and moves the active state
- [x] Back and forward move between modes; a mode can be deep-linked
- [x] An unknown mode is a 404
- [x] Each page opens **exactly one** SSE subscription, to its own mode
- [x] The stream is **still open** afterwards — not merely answered with
      `text/event-stream`. The SDK flushes those headers before the handler body
      runs, so the header check alone passes against a server that hangs up
      instantly, and Datastar does not reconnect after a clean end-of-stream
- [x] The shell fills the viewport and the page never scrolls in either axis
- [x] Chrome dimensions are **measured**: menu bar ~34px, rail 62px, rail
      buttons 46px wide and no taller than 48px, cog below the last mode button
- [x] Menu items are disabled; the cog is disabled and navigates nowhere
- [x] Every link the shell renders resolves to a 200
- [x] Accessibility, stated explicitly: the rail is a labelled `navigation`
      landmark; the active mode carries `aria-current="page"`; rail buttons
      announce the mode's **full name** rather than its abbreviation; the rail
      is operable by keyboard

## Notes

- Model the rail as a typed `Mode` value with a slice of definitions (glyph, caption, slug), not six copy-pasted blocks. The same slice drives the rail, the route table and the tests.
- **Mode switching is a full page load, not a swap.** Datastar's model is: the server renders the whole page, the page subscribes to SSE, and everything after that arrives as HTML patches pushed down that stream. It has no notion of an endpoint returning a fragment representation of a resource — so there is nothing to content-negotiate and no `pushState` to write. Rail buttons are anchors. The URL, the back button, and deep links then work because they are ordinary navigation, not because we reimplemented them.
- The shell is small and its assets are cached and local, so a full load is not the cost it would be over a network. Resist the urge to optimise it into a swap; the fragment-vs-document branch that buys is exactly the complexity this model exists to avoid.
- Keep the shell free of mode-specific knowledge. It renders a `templ.Component` for the content region and nothing more; modes register themselves through the mode table.
- The menu bar items are non-functional in this story — the dialogs they open are Epic 17. Render them as disabled-looking affordances rather than dead links that appear clickable.
- Full viewport, no page scroll: the shell is `height: 100dvh` with `overflow: hidden`, and scrolling happens inside panels. Getting this wrong is easy to miss until a mode with a long list arrives.

## As Implemented

`internal/forge/mode` holds one table of six `Mode` values; it drives the rail, the routes, the page titles and every test in this story. `templates.Shell(active, content)` renders the frame and nothing else — it takes a `Component` for the content region and has no knowledge of any mode. `templates/modes` holds the six stubs behind a `Registry` map, checked against `mode.All` in both directions so a mode cannot exist in the rail without content, or the reverse.

Coverage: `mode` 100%, `server` 92.6%, `templates` 67.8%, `modes` 79.1% (the last two held down by templ's generated `if err != nil { return }` after every write, which a `bytes.Buffer` cannot reach).

### Divergences from the plan

- **The subscription attribute is `data-init`, not `data-on-load`.** The plan specified `data-on-load`, which does not exist. See below — this is the important finding of the story.
- **The settings cog is a disabled button, not a link.** The plan gave it `href="/forge/settings"`, but `settings` is not in the mode table, so that route 404s. Preferences is Epic 17; until then the cog is an affordance that plainly is not ready rather than a 404 sitting in the primary navigation. `TestShell_CogIsNotADeadLink` pins it, and `TestShellLinks_AllResolve` fetches every link the shell emits and fails on any non-200 — which would have caught it even if nobody had thought to look.
- **Menu items are `<button disabled>`, not styled spans.** They open Epic 17's dialogs. A disabled button reports its state to a screen reader and cannot be tabbed to and pressed to no effect; a span styled to look clickable does neither. Full `role="menubar"` semantics are deliberately *not* used yet — they would promise arrow-key navigation that is not implemented. That arrives with the dialogs.
- **`Server` gained a base context that `Shutdown` cancels.** An SSE handler blocks for the life of its connection, and `http.Server.Shutdown` waits for in-flight requests — so without this, Ctrl-C on `ecs-db forge` would hang for as long as one browser tab held a stream open. `TestShutdown_DoesNotWaitForOpenEventStreams` proves a stream is genuinely open (via an `openStreams` counter) before shutting down, so it cannot pass by the request never arriving.
- **The bundle-scanning Datastar guard moved to `internal/forge/web/dstest`.** Story 4 left it inside the `components` package, where the shell templates could not reach it. It is now shared, and its plugin regex accepts hyphens — `on-intersect`, `on-interval`, `on-signal-patch` and `json-signals` are real plugin names that the old `[a-zA-Z]+` pattern would have rejected as unregistered.

### The finding: `data-on-load` does not exist

The implementation plan called for `<body data-on-load="@get('/forge/{mode}/events')">`. That attribute is wrong twice over. Datastar v1.0.2 registers no `on-load` plugin at all — the plugin list is `attr, bind, class, computed, effect, indicator, init, json-signals, on, on-intersect, on-interval, on-signal-patch, peek, ref, setAll, show, signals, style, text, toggleAll` — and it is written in the dash form that Story 4 established is parsed as a plugin name rather than an event key. Either mistake alone is silent: an attribute naming an unregistered plugin is skipped with no error and no console warning.

The correct attribute is **`data-init`**, whose plugin runs its expression once when the element is set up.

Had this shipped, every mode page would have rendered perfectly and never connected to its stream — and Story 6, whose entire job is to push the engine-status readout down that stream, would have looked broken for a reason that had nothing to do with Story 6.

Three independent guards now catch it, all verified to fail against the `data-on-load` form before being trusted: `TestShell_DatastarAttributesResolve` (checks the attribute against the vendored bundle), `TestShell_SubscribesWithInitNotOnLoad` (names the specific mistake), and `TestShell_OpensOneEventStreamForItsOwnMode`.

### Driving the page

The shell is driven in a real browser by `e2e/specs/05-app-shell.spec.js` — see the **Playwright steps** section above. It began as a standalone `scripts/verify-shell.js`; that script has been folded into the Playwright suite added alongside this story, since two ways of checking the same thing is how one of them goes stale.

Three things it caught that nothing else could:

- The rail rendered correctly but its buttons were 46×52px against the design's 46×46. The glyph had been enlarged to 14px for legibility; at the design's 10px the rail reads as the column of squares it is meant to be. Measured in the browser, not eyeballed.
- A false negative in the checker itself: Datastar appends its signal state to the URL, so a stream request arrives as `/forge/map/events?datastar=%7B%7D`. Matching on `endsWith("/events")` found nothing — indistinguishable from a page that never subscribed. Worth remembering: a verification script is code too, and "the check found nothing" and "there is nothing to find" look identical from the outside.
- The rail's accessible names. The visible captions are abbreviations, so the app's primary navigation announced "SPRT" for Sprites and "ENTS" for Entity Types. `mode.Mode` already carried the spelled-out `Title`; the rail was the one place it went unused.

While confirming the diagnosis, the vendored `datastar.js` was checked directly for the fetch actions. They are registered through a factory (`Te("get","GET",false)`) rather than a literal, so a grep for the literal form finds nothing and suggests the bundle is missing `@get` entirely — it is not. `POST /dev/noop` was re-checked end to end from `/dev/tokens`: two clicks, two POSTs on the wire. Story 4's actions do reach the server.
