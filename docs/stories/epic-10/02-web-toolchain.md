# Story 2: Web toolchain — templ, Datastar, embedded assets

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** ✅ Complete  
**Priority:** High — every later Forge story renders through this

**Depends on:** Story 1 (tag-free `ecs-db` root command)

## Context

The repo has no web stack at all: no `net/http` import anywhere, no template engine, no `go:embed`, and no codegen step in the Makefile. This story stands the whole thing up and gets one server-rendered page on screen.

The stack is Go + [templ](https://templ.guide) + [Datastar](https://data-star.dev), matching the pattern proven in `fancykaraoke-go`. templ gives type-safe server-rendered components with a `templ generate` step; Datastar gives declarative client interactivity and SSE-driven updates through `data-*` attributes, with no frontend build step. templ is pulled in as a go.mod `tool` dependency so `go run github.com/a-h/templ/cmd/templ generate` works from a clean checkout without anyone installing anything.

Datastar has two halves and they version separately: the **Go SDK** (`github.com/starfederation/datastar-go`, currently v1.2.2) writes the SSE wire format server-side, and the **client bundle** (`datastar.js`, currently v1.0.2) reads it in the browser. Vendor the client bundle rather than loading it from a CDN — Forge is a local authoring tool and must work with no network. The same reasoning applies to the two Google fonts.

Everything static is served from `go:embed`, so a built `ecs-db` binary is self-contained.

## Acceptance Criteria

- [x] `go.mod` gains `github.com/a-h/templ` (latest — v0.3.1020 or newer) plus a
      `tool github.com/a-h/templ/cmd/templ` directive
- [ ] ~~`go.mod` gains `github.com/starfederation/datastar-go`~~ — **deferred to
      Story 5.** Nothing imports the SDK until there is an SSE endpoint, so
      `go mod tidy` strips it. Adding a blank import purely to pin it would be
      dishonest about what the code uses. The *client* bundle is vendored here
      and its wire compatibility with SDK v1.2.2 is verified (see Notes)
- [x] `Makefile`:
  - `generate:` runs `go run github.com/a-h/templ/cmd/templ generate`
  - `build` and `build-headless` both depend on `generate`
  - `fmt` also runs `templ fmt .`
  - `clean` also deletes `*_templ.go`
- [x] Generated `*_templ.go` files are **committed** (`.golangci.yml` already sets `exclusions.generated: lax`)
- [x] `internal/forge/web/static/` embedded via `go:embed`, containing:
  - `js/vendor/datastar.js` — pinned client bundle, version recorded in a sibling `VERSION` file
  - `fonts/` — self-hosted Chakra Petch (400/500/600) and JetBrains Mono (400/500) woff2
  - `css/` — empty placeholder; Story 3 fills it
- [x] `internal/forge/server` — `net/http` with:
  - `Server` struct constructed from an explicit config value (no package-level state)
  - `GET /` serving a templ-rendered page
  - `GET /static/*` serving the embedded FS with long-lived cache headers
  - `GET /healthz` returning 200
  - Graceful shutdown on `SIGINT`/`SIGTERM` with a bounded drain timeout
- [x] `internal/config` gains a `[forge]` section: `addr` (default `127.0.0.1:7777`)
- [x] `cmd/ecs-db/forge.go` — tag-free `forge` subcommand that starts the server and prints its URL
- [x] No external network requests from the served page (verifiable: load with devtools offline)
- [x] `go test ./...` passes, including a handler test per route

## As Implemented

- Deps: `templ v0.3.1020` plus the `tool github.com/a-h/templ/cmd/templ`
  directive. **`datastar-go` is deliberately not yet in `go.mod`** — nothing
  imports it until Story 5's SSE endpoint, and `go mod tidy` removes unused
  requirements. The client bundle is vendored now because the *page* needs it.
- **Wire format verified before building on it.** Grepped the pinned v1.0.2
  client bundle: it understands `datastar-patch-elements`, `datastar-patch-signals`
  and every dataline key the Go SDK emits (`elements`, `signals`, `selector`,
  `mode`, `useViewTransition`, `onlyIfMissing`). The two halves version
  separately, so this was worth confirming rather than assuming.
- **Deviation — fonts:** the Google Fonts API returns 18 woff2 URLs for these two
  families, one per unicode subset. Only the 5 latin faces are vendored (~92K
  total). Forge is a local dev tool, not a multilingual site.
- **Deviation — `templates.Component`:** a type alias for `templ.Component`, so
  `internal/forge/server` renders pages without importing templ directly.
- `web.Static` is a package-level `var` (see this directory's README on the
  AGENTS.md divergence): immutable embedded data, not mutable state. `mustSub`
  panics only on a bad `//go:embed` edit, which is a compile-time constant.
- `Load` gained `applyDefaults`, because a config file omitting `[forge]` would
  otherwise leave `Addr` empty and bind the editor to every interface.
- `make test` now depends on `generate`, so a stale `*_templ.go` cannot pass.

### Changed in review

- **`Start()` split into `Listen()` + `Serve()`.** The old shape announced
  "⚒ Forge listening on ..." *before* binding, so a bind failure printed a
  cheerful lie and then the error. It also printed the *configured* address, so
  `addr = "127.0.0.1:0"` advertised a port nobody could connect to. `Addr()` now
  reports what was actually bound.
- **Nil-context guard in `runForge`.** Cobra populates `cmd.Context()` in
  `ExecuteC`, but leaves it nil when `RunE` is called directly — as a test does —
  and `signal.NotifyContext(nil, …)` panics.
- **`internal/forge/web` gained tests.** Handler tests inject a `fstest.MapFS`,
  which is right for them but meant nothing ever opened the real embedded FS.
  Now a table test stats every required asset, and `fonts.css`'s `url()` values
  are parsed and checked against the embedded files so the two cannot drift —
  a missing woff2 otherwise degrades silently to a system font.
- **Static directory listings are 404.** `http.FileServerFS` was serving an
  autoindex of the asset tree with a one-year immutable cache header. The first
  guard missed `/static/` itself, because `StripPrefix` leaves that as `""`
  rather than a trailing slash — caught by the test.
- **`Defaults()` now routes through `applyDefaults`** rather than repeating the
  constant, so the loopback default has one source of truth, and `server.New`
  defaults an empty `Addr` too — the boundary that actually binds should not
  depend on config having been loaded correctly.
- `make clean` scopes its `*_templ.go` deletion to `./internal ./cmd`; it was
  walking the whole tree.

Coverage: `internal/config` 100%, `internal/forge/server` 93.6%,
`cmd/ecs-db` 87.9%.

Verified in a real browser (headless chromium): zero external requests, zero
console errors, Datastar's module initialises, and both fonts load *and apply*
from the embedded FS — checked with `document.fonts.check()` rather than trusting
that a 200 on the woff2 meant the `@font-face` was right.

## Playwright steps

`e2e/specs/02-toolchain.spec.js`. This story's claim — that Forge is
self-contained and needs no network — is a claim about what the browser fetches,
so it can only be checked in a browser.

- [x] Every request the page makes is same-origin; a CDN reference would work on
      the developer's machine and fail on a plane
- [x] All four assets are actually requested: `fonts.css`, `tokens.css`,
      `forge.css`, `datastar.js`
- [x] Datastar **executed**, not merely 200'd — inject a `data-text` element at
      runtime and confirm the framework processes it. A bundle that failed to
      parse leaves every attribute inert and reports nothing
- [x] Both webfonts report `status === "loaded"`, so a silent substitution to a
      system sans cannot pass a screenshot review
- [x] Static assets carry `immutable`; `/static/`, `/static/css/` and
      `/static/js/vendor/` are not browsable

## Notes

- Client bundle source: `https://cdn.jsdelivr.net/gh/starfederation/datastar@v1.0.2/bundles/datastar.js`. Download once, commit it, record the version. Do **not** reference the CDN at runtime.
- Datastar is loaded as `<script type="module" src="/static/js/vendor/datastar.js">`. It self-initialises; there is no bootstrap call.
- Go SDK API surface worth knowing before Story 6 (verified against v1.2.2):
  - `sse := datastar.NewSSE(w, r)` — sets the SSE headers and flushes
  - `sse.PatchElements(html, datastar.WithSelector("#id"), datastar.WithModeInner())`
  - `sse.MarshalAndPatchSignals(struct{...}{...})`
  - `sse.Context()` / `sse.IsClosed()` for lifecycle
  - `datastar.ReadSignals(r, &signals)` to read the client's signal state on a request
- Fonts: fetch the woff2 files from the Google Fonts CSS API with a modern user-agent so you get woff2 rather than ttf, then write a local `@font-face` block. Subsetting is not worth it for a local tool.
- The server must not import anything Ebitengine-tagged. `internal/renderer/anim_loader.go` and `tick.go` are untagged and safe; `game.go`, `image_cache.go` and `tilemap_renderer.go` are not.
- Bind to loopback by default. Forge reads and writes project files; it is not a service to expose.
