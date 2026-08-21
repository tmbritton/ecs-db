# Epic 10 Story 2: Web toolchain — Implementation Plan

**Goal:** `ecs-db forge` serves a templ-rendered page with Datastar and both fonts loading from an embedded filesystem, with no network access required.

**Architecture:** `internal/forge/server` owns HTTP and nothing else — no domain logic, no file authoring. It takes a `Config` value and an `fs.FS` of static assets, both injected, so handler tests run against a real `httptest.Server` with no globals to reset. templ output is committed, so `go build` works without the templ binary; `make generate` regenerates it.

**Depends on:** Story 1.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Modify | `go.mod` | templ + templ tool directive, datastar-go |
| Create | `internal/forge/web/embed.go` | `//go:embed static` → exported `Static fs.FS` |
| Create | `internal/forge/web/static/js/vendor/datastar.js` | Pinned client bundle |
| Create | `internal/forge/web/static/js/vendor/VERSION` | Records the pinned version |
| Create | `internal/forge/web/static/fonts/*.woff2` | Chakra Petch 400/500/600, JetBrains Mono 400/500 |
| Create | `internal/forge/web/static/css/forge.css` | Placeholder; Story 3 fills it |
| Create | `internal/forge/templates/layout.templ` | Base HTML document |
| Create | `internal/forge/server/server.go` | `Server`, routes, graceful shutdown |
| Create | `internal/forge/server/server_test.go` | Route tests |
| Modify | `internal/config/config.go` | `ForgeConfig` + `Forge` field + default |
| Modify | `internal/config/config_test.go` | Cover the new field and default |
| Create | `cmd/ecs-db/forge.go` | `forge` subcommand (no build tag) |
| Modify | `Makefile` | `generate`, `fmt`, `clean` |
| Modify | `game.toml` | `[forge]` section |

---

## Task 1: Dependencies and Makefile

```bash
go get github.com/a-h/templ@latest
go get github.com/starfederation/datastar-go@latest
go get -tool github.com/a-h/templ/cmd/templ
go mod tidy
```

`go.mod` should end up with a `tool github.com/a-h/templ/cmd/templ` line, matching the pattern in `fancykaraoke-go`.

Makefile additions:

```make
.PHONY: build build-headless run generate fmt clean test

generate:
	go run github.com/a-h/templ/cmd/templ generate

build: generate
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	go build -tags ebitengine -o bin/ecs-db ./cmd/ecs-db

build-headless: generate
	CGO_ENABLED=0 go build -o bin/ecs-db-headless ./cmd/ecs-db

fmt:
	gofumpt -w .
	go run github.com/a-h/templ/cmd/templ fmt .

clean:
	rm -rf bin/
	find . -name '*_templ.go' -delete
```

---

## Task 2: Vendor the client assets

```bash
mkdir -p internal/forge/web/static/js/vendor internal/forge/web/static/fonts internal/forge/web/static/css

curl -fsSL -o internal/forge/web/static/js/vendor/datastar.js \
  https://cdn.jsdelivr.net/gh/starfederation/datastar@v1.0.2/bundles/datastar.js
echo "v1.0.2" > internal/forge/web/static/js/vendor/VERSION
```

Fonts: request the Google Fonts stylesheet with a woff2-capable user-agent, extract the `src` URLs, download each to `static/fonts/`, and hand-write the `@font-face` rules into `static/css/fonts.css`. Five files: `chakra-petch-{400,500,600}.woff2`, `jetbrains-mono-{400,500}.woff2`.

Sanity check that nothing external survived:

```bash
grep -rn 'https\?://' internal/forge/web/static/css/ && echo "FAIL: external reference" || echo OK
```

### `internal/forge/web/embed.go`

```go
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var embedded embed.FS

// Static is the served asset tree, rooted so that "static/css/forge.css"
// is addressable as "css/forge.css".
var Static fs.FS = mustSub(embedded, "static")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err) // embed path is a compile-time constant; this cannot fail at runtime
	}
	return sub
}
```

---

## Task 3: Config

### `internal/config/config.go`

```go
type ForgeConfig struct {
	Addr string `toml:"addr"`
}
```

Add `Forge ForgeConfig \`toml:"forge"\`` to `Config`, and to `Defaults()`:

```go
Forge: ForgeConfig{Addr: "127.0.0.1:7777"},
```

`Load` must also apply the default when the file omits `[forge]`, otherwise `Addr` is empty and the server binds to every interface — the opposite of what is wanted. Add a small `applyDefaults(*Config)` called at the end of `Load`, and a test for the omitted-section case.

`game.toml`:

```toml
[forge]
addr = "127.0.0.1:7777"
```

---

## Task 4: Layout template

### `internal/forge/templates/layout.templ`

```templ
package templates

templ Layout(title string) {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="utf-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1"/>
			<title>{ title }</title>
			<link rel="stylesheet" href="/static/css/fonts.css"/>
			<link rel="stylesheet" href="/static/css/forge.css"/>
			<script type="module" src="/static/js/vendor/datastar.js"></script>
		</head>
		<body>
			{ children... }
		</body>
	</html>
}
```

---

## Task 5: Server

### `internal/forge/server/server.go`

```go
package server

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"time"
)

type Config struct {
	Addr string
}

type Server struct {
	cfg    Config
	static fs.FS
	http   *http.Server
}

func New(cfg Config, static fs.FS) *Server {
	s := &Server{cfg: cfg, static: static}
	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", s.staticHandler()))
	mux.HandleFunc("GET /", s.handleIndex)
	return mux
}
```

`staticHandler` wraps `http.FileServerFS(s.static)` and sets `Cache-Control: public, max-age=31536000, immutable` — safe because a rebuild changes the binary, and Story 5 can add a content hash to the query string if cache-busting during development becomes annoying.

`handleIndex` must 404 on anything but exactly `/` (Go's `"GET /"` pattern is a catch-all), then renders the layout:

```go
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Layout("Forge").Render(r.Context(), w); err != nil {
		// response is already partially written; log and drop the connection
		slog.Error("render index", "err", err)
	}
}
```

`Start`/`Shutdown`:

```go
func (s *Server) Start() error {
	err := s.http.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

// Addr reports the bound address; useful once tests bind to :0.
func (s *Server) Addr() string { return s.cfg.Addr }
```

### `internal/forge/server/server_test.go`

Table-driven over the routes, using `httptest.NewServer(New(Config{}, testFS).routes())`:

| name | path | want status | want header/body |
|---|---|---|---|
| `index` | `/` | 200 | `Content-Type: text/html…`, body contains `datastar.js` |
| `index_404_on_subpath` | `/nope` | 404 | — |
| `healthz` | `/healthz` | 200 | — |
| `static_asset` | `/static/js/vendor/datastar.js` | 200 | `Cache-Control` set |
| `static_missing` | `/static/nope.js` | 404 | — |

Use a `fstest.MapFS` for `testFS` so the test does not depend on the real vendored bundle.

---

## Task 6: The `forge` subcommand

### `cmd/ecs-db/forge.go` (no build tag)

```go
package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/forge/server"
	"github.com/tmbritton/ecs-db/internal/forge/web"
)

var forgeCmd = &cobra.Command{
	Use:   "forge",
	Short: "Run the Forge content editor",
	RunE:  runForge,
}

func init() { rootCmd.AddCommand(forgeCmd) }

func runForge(cmd *cobra.Command, args []string) error {
	cfg := config.Get()
	srv := server.New(server.Config{Addr: cfg.Forge.Addr}, web.Static)

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	fmt.Printf("⚒ Forge listening on http://%s\n", cfg.Forge.Addr)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
```

`cmd.Context()` is non-nil because `rootCmd.Execute()` supplies one.

---

## Task 7: Mark story complete

Tick `docs/stories/epic-10/02-web-toolchain.md`, add `## As Implemented`, tick **Web toolchain** in `docs/plan.md`.

---

## Verification

```bash
make generate && go test ./...
make build-headless

./bin/ecs-db-headless forge &
curl -sf localhost:7777/healthz
curl -s localhost:7777/ | grep -q 'datastar.js' && echo "datastar linked"
curl -sI localhost:7777/static/js/vendor/datastar.js | grep -i cache-control
kill %1
```

Then load `http://127.0.0.1:7777` in a browser with devtools **offline mode on**: the page must render with both fonts applied and zero failed requests. Confirm `window.location` shows no console errors from Datastar's module init.

Before committing, launch a fresh-context code-review subagent over the staged diff, per `AGENTS.md`.
