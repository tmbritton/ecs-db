# Story 2: Web toolchain — templ, Datastar, embedded assets

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** 🔲 Not started  
**Priority:** High — every later Forge story renders through this

**Depends on:** Story 1 (tag-free `ecs-db` root command)

## Context

The repo has no web stack at all: no `net/http` import anywhere, no template engine, no `go:embed`, and no codegen step in the Makefile. This story stands the whole thing up and gets one server-rendered page on screen.

The stack is Go + [templ](https://templ.guide) + [Datastar](https://data-star.dev), matching the pattern proven in `fancykaraoke-go`. templ gives type-safe server-rendered components with a `templ generate` step; Datastar gives declarative client interactivity and SSE-driven updates through `data-*` attributes, with no frontend build step. templ is pulled in as a go.mod `tool` dependency so `go run github.com/a-h/templ/cmd/templ generate` works from a clean checkout without anyone installing anything.

Datastar has two halves and they version separately: the **Go SDK** (`github.com/starfederation/datastar-go`, currently v1.2.2) writes the SSE wire format server-side, and the **client bundle** (`datastar.js`, currently v1.0.2) reads it in the browser. Vendor the client bundle rather than loading it from a CDN — Forge is a local authoring tool and must work with no network. The same reasoning applies to the two Google fonts.

Everything static is served from `go:embed`, so a built `ecs-db` binary is self-contained.

## Acceptance Criteria

- [ ] `go.mod` gains:
  - `github.com/a-h/templ` (latest — v0.3.1020 or newer) plus a `tool github.com/a-h/templ/cmd/templ` directive
  - `github.com/starfederation/datastar-go` v1.2.2 or newer
- [ ] `Makefile`:
  - `generate:` runs `go run github.com/a-h/templ/cmd/templ generate`
  - `build` and `build-headless` both depend on `generate`
  - `fmt` also runs `templ fmt .`
  - `clean` also deletes `*_templ.go`
- [ ] Generated `*_templ.go` files are **committed** (`.golangci.yml` already sets `exclusions.generated: lax`)
- [ ] `internal/forge/web/static/` embedded via `go:embed`, containing:
  - `js/vendor/datastar.js` — pinned client bundle, version recorded in a sibling `VERSION` file
  - `fonts/` — self-hosted Chakra Petch (400/500/600) and JetBrains Mono (400/500) woff2
  - `css/` — empty placeholder; Story 3 fills it
- [ ] `internal/forge/server` — `net/http` with:
  - `Server` struct constructed from an explicit config value (no package-level state)
  - `GET /` serving a templ-rendered page
  - `GET /static/*` serving the embedded FS with long-lived cache headers
  - `GET /healthz` returning 200
  - Graceful shutdown on `SIGINT`/`SIGTERM` with a bounded drain timeout
- [ ] `internal/config` gains a `[forge]` section: `addr` (default `127.0.0.1:7777`)
- [ ] `cmd/ecs-db/forge.go` — tag-free `forge` subcommand that starts the server and prints its URL
- [ ] No external network requests from the served page (verifiable: load with devtools offline)
- [ ] `go test ./...` passes, including a handler test per route

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
