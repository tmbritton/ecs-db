# Epic 10 Story 5: App shell — Implementation Plan

**Goal:** all six modes reachable, correctly chromed, each a real URL served as a complete page, with one SSE subscription per page for everything that updates live.

**Architecture:** A single `Mode` table is the source of truth for the rail, the routes and the tests. The shell template takes the active mode plus a content `templ.Component`; it knows nothing about what any mode contains. Every route renders one thing: a complete document. Datastar's job starts *after* the page lands — the page opens an SSE subscription and the server pushes HTML patches down it. There is no fragment representation of a mode and nothing to content-negotiate.

**Depends on:** Story 4.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/mode/mode.go` | `Mode` type + the six definitions |
| Create | `internal/forge/mode/mode_test.go` | Table integrity + lookup |
| Create | `internal/forge/templates/shell.templ` | Menu bar, rail, content region |
| Create | `internal/forge/templates/modes/*.templ` | Six stubs |
| Modify | `internal/forge/server/server.go` | `/forge/{mode}` routing, `/` redirect |
| Modify | `internal/forge/server/server_test.go` | Route table tests |
| Modify | `internal/forge/web/static/css/forge.css` | Shell layout, rail, menu bar |

---

## Task 1: The mode table

### `internal/forge/mode/mode.go`

```go
package mode

// Mode is one of Forge's six top-level editors. The zero value is invalid;
// use Lookup.
type Mode struct {
	Slug    string // URL segment and stable identifier
	Caption string // 10px mono caption under the glyph
	Glyph   string // Unicode icon shown in the rail
	Title   string // window/page title suffix
}

// All is the rail order, top to bottom. It drives the rail, the routes and
// the tests — there is no second list to keep in sync.
var All = []Mode{
	{Slug: "map", Caption: "MAP", Glyph: "🗺", Title: "Map"},
	{Slug: "tiles", Caption: "TILES", Glyph: "▦", Title: "Tiles"},
	{Slug: "ents", Caption: "ENTS", Glyph: "♟", Title: "Entity Types"},
	{Slug: "schema", Caption: "SCHEMA", Glyph: "⛃", Title: "Schema"},
	{Slug: "agents", Caption: "AGENTS", Glyph: "◉→◉", Title: "Agents"},
	{Slug: "sprites", Caption: "SPRT", Glyph: "🧍", Title: "Sprites"},
}

// Default is where "/" lands.
var Default = All[0]

func Lookup(slug string) (Mode, bool) {
	for _, m := range All {
		if m.Slug == slug {
			return m, true
		}
	}
	return Mode{}, false
}
```

Tests: `Lookup` hit and miss (table-driven), slugs are unique and non-empty, `Default` is a member of `All`.

---

## Task 2: Shell template

### `internal/forge/templates/shell.templ`

```templ
templ Shell(active mode.Mode, content templ.Component) {
	@Layout("Forge — " + active.Title) {
		<div class="shell">
			@menuBar()
			<div class="shell__body">
				@modeRail(active)
				<main id="mode-content" class="shell__content">
					@content
				</main>
			</div>
		</div>
	}
}
```

`modeRail` ranges over `mode.All`, marking `m.Slug == active.Slug`:

```templ
templ modeRail(active mode.Mode) {
	<nav class="rail">
		for _, m := range mode.All {
			<a
				href={ templ.SafeURL("/forge/" + m.Slug) }
				class={ "rail__btn", templ.KV("rail__btn--active", m.Slug == active.Slug) }
			>
				<span class="rail__glyph">{ m.Glyph }</span>
				<span class="rail__caption mono">{ m.Caption }</span>
			</a>
		}
		<a href="/forge/settings" class="rail__btn rail__cog"><span class="rail__glyph">⚙</span></a>
	</nav>
}
```

The cog sits after the loop and takes `margin-top: auto`. Anchors, not buttons — a mode switch is navigation. Give them `display: flex` so they lay out identically to the buttons the prototype draws.

`menuBar` renders the wordmark, the six menu labels with `Engine` carrying `menu__item--accent`, and an empty `<div id="engine-status">` placeholder that Story 6 targets.

---

## Task 3: Mode stubs

Six files under `internal/forge/templates/modes/`, each a placeholder naming itself and the epic that fills it — e.g. `schema.templ` renders "SCHEMA — components editor. Epic 12." That makes an unfinished build self-documenting rather than blank.

A small registry maps slug → stub component so the handler stays a lookup rather than a switch that has to grow:

```go
// internal/forge/templates/modes/registry.go
var Registry = map[string]func() templ.Component{
	"map": Map, "tiles": Tiles, "ents": Ents,
	"schema": Schema, "agents": Agents, "sprites": Sprites,
}
```

---

## Task 4: Routing

```go
mux.HandleFunc("GET /forge/{mode}", s.handleMode)
mux.HandleFunc("GET /", s.handleRoot) // redirects to /forge/map
```

```go
func (s *Server) handleMode(w http.ResponseWriter, r *http.Request) {
	m, ok := mode.Lookup(r.PathValue("mode"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Shell(m, modes.Registry[m.Slug]()).Render(r.Context(), w); err != nil {
		slog.Error("render shell", "mode", m.Slug, "err", err)
	}
}
```

That is the whole handler. One representation, always the full document.

It is worth being explicit about why, because the HTMX-shaped instinct is strong and this story is where it would take root. Datastar is **HTML over the wire via SSE**: the server renders a complete page, the page subscribes to a stream, and every subsequent change arrives as a `datastar-patch-elements` event carrying HTML. Actions (`@get`, `@post`) are not requests for a resource — their response *is* a patch stream. So there is no second representation of `/forge/schema` to negotiate and no `history.pushState` to write, because navigation is navigation. (The client does send a `Datastar-Request` header — verified in the v1.0.2 bundle — but it marks an action-initiated fetch, not a request for a fragment. Branching page rendering on it would be reinventing content negotiation Datastar does not ask for.)

### The page-level event stream

Each mode page opens exactly one subscription, in the shell:

```templ
<body data-on-load={ fmt.Sprintf("@get('/forge/%s/events')", active.Slug) }>
```

`GET /forge/{mode}/events` is the only SSE endpoint the shell needs, and everything that updates live on that page is pushed down it. In this story it carries just the engine-status patch (Story 6). Epic 18 adds `world_version`-gated entity patches to the same stream for MAP; Epic 12 will push DDL-preview patches to it for SCHEMA. One stream per page, not one per widget — browsers cap concurrent connections per origin, and a stream per component burns that budget for no benefit.

Stub the endpoint here so the wiring exists and Story 6 has somewhere to land:

```go
func (s *Server) handleModeEvents(w http.ResponseWriter, r *http.Request) {
	m, ok := mode.Lookup(r.PathValue("mode"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	sse := datastar.NewSSE(w, r)
	<-sse.Context().Done() // Story 6 pushes engine status; later epics push mode data
	_ = m
}
```

## Task 5: Shell CSS

```css
.shell { display: flex; flex-direction: column; height: 100dvh; overflow: hidden; }
.shell__body { display: flex; flex: 1; min-height: 0; }
.shell__content { flex: 1; min-width: 0; overflow: hidden; }

.menubar {
  height: 34px; background: var(--raised);
  border-bottom: var(--border-w) solid var(--border);
  display: flex; align-items: center; gap: 14px; padding: 0 12px;
}
.menubar__wordmark { color: var(--amber); font-weight: 600; letter-spacing: 1px; }
.menu__item--accent { color: var(--amber); }

.rail {
  width: 62px; background: var(--raised);
  border-right: var(--border-w) solid var(--border);
  display: flex; flex-direction: column; align-items: center;
  gap: 6px; padding: 8px 0;
}
.rail__btn {
  width: 46px; padding: 6px 0; background: none; cursor: pointer;
  border: var(--border-w) solid var(--border); color: var(--text-dim);
  display: flex; flex-direction: column; align-items: center; gap: 3px;
}
.rail__btn--active { background: var(--ink); border-color: var(--amber); color: var(--amber); }
.rail__caption { font-size: 10px; letter-spacing: .5px; }
.rail__cog { margin-top: auto; }
```

`min-height: 0` / `min-width: 0` on the flex children are load-bearing — without them a long list in a later mode pushes the whole shell past the viewport instead of scrolling inside its panel.

---

## Task 6: Route tests

Table-driven over `mode.All`:

```go
for _, m := range mode.All {
	t.Run(m.Slug, func(t *testing.T) {
		// GET /forge/<slug> → 200
		// body contains rail__btn--active adjacent to this mode's caption
		// body contains the stub's identifying text
	})
}
```

Plus: unknown slug → 404; `/` → 302 to `/forge/map`; and — the one that pins the model — **every** mode route returns a complete document, asserted by requiring `<!DOCTYPE` and the rail markup in the body. If a later change reintroduces a fragment representation, that test fails.

Also assert the rail renders anchors with the right `href`, not buttons; that is what makes back/forward work without any code of ours.

---

## Task 7: Mark story complete

Tick `docs/stories/epic-10/05-app-shell.md`, add `## As Implemented`, tick **App shell** in `docs/plan.md`.

---

## Verification

```bash
make generate && go test ./... && make build-headless
./bin/ecs-db-headless forge
```

In the browser at `http://127.0.0.1:7777`:
- lands on MAP with its rail button amber
- clicking each rail button navigates to that mode and the URL follows
- browser back/forward moves between modes with no code of ours involved
- devtools Network shows exactly one EventStream per page (`/forge/{mode}/events`), not one per widget
- reloading on `/forge/agents` renders AGENTS active directly
- `/forge/nope` is a 404
- the window does not scroll; the viewport is exactly filled

Compare the chrome against `Forge Editor.dc.html`: menu bar height and wordmark, rail width, button size, active-state inversion, cog pinned to the bottom.

Before committing, launch a fresh-context code-review subagent over the staged diff, per `AGENTS.md`.
