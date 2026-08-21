# Story 1: CLI restructure — split Cobra out of the ebitengine build tag

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** ✅ Complete  
**Priority:** High — nothing else in Epic 10 can build until this lands

**Depends on:** nothing

## Context

Every file in `cmd/game/` carries `//go:build ebitengine`, and `stub.go` supplies an empty `main()` for the untagged build. That was fine while the binary only ever ran a game window, but Forge is a webapp: it needs no OpenGL, no X11, and no cgo. Today a Forge subcommand would either drag the entire Ebitengine toolchain into every build or be unreachable without it.

This story splits the Cobra tree in two. The root command, the `--config` flag, and everything that only touches `internal/config` and `internal/schema` become tag-free. Only `run` — the composition root that opens a window — stays behind `//go:build ebitengine`, with a `!ebitengine` counterpart that registers the same subcommand and exits with a clear explanation instead of silently doing nothing.

Two shape changes come with it. The binary is renamed `ecs-db` (the name `AGENTS.md` has documented all along), and running the game becomes an explicit `ecs-db run` rather than the bare root command. `game schema validate` becomes `ecs-db schema validate` and starts working in a headless build, which it never could before.

## Acceptance Criteria

- [x] `cmd/game/` renamed to `cmd/ecs-db/`; Makefile builds `bin/ecs-db`
- [x] `cmd/ecs-db/main.go` — **no build tag**:
  ```go
  var rootCmd = &cobra.Command{
      Use:   "ecs-db",
      Short: "ECS-in-SQLite game engine and content tools",
      PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
          return config.Init(cfgPath)
      },
  }
  ```
  - Root has no `RunE` — bare `ecs-db` prints help
  - `--config/-c` persistent flag, default `./game.toml`, unchanged
- [x] `cmd/ecs-db/run.go` (`//go:build ebitengine`) — the current `runGame` composition root moved verbatim, registering a `run` subcommand
- [x] `cmd/ecs-db/run_stub.go` (`//go:build !ebitengine`) — registers `run`, returns an error naming the missing tag:
  ```
  this binary was built without the "ebitengine" build tag; rebuild with `make build` to run the game
  ```
- [x] `cmd/ecs-db/schema.go` — build tag removed; `ecs-db schema validate` works headless
- [x] `cmd/game/stub.go` deleted — the untagged build now has a real `main()`
- [x] `Makefile`:
  - `build` keeps `-tags ebitengine` and the CGO flags, outputs `bin/ecs-db`
  - `run` becomes `./bin/ecs-db run`
  - new `build-headless` target: `CGO_ENABLED=0 go build -o bin/ecs-db-headless ./cmd/ecs-db`
- [x] `CGO_ENABLED=0 go build ./cmd/ecs-db` succeeds with no build tags
- [x] `make build && ./bin/ecs-db run` — player and wandering goblin behave exactly as before
- [x] `go test ./...` passes

## As Implemented

- `cmd/game/` → `cmd/ecs-db/`; `schema_cmd.go` → `schema.go` (build tag removed);
  `stub.go` deleted. Binary is `bin/ecs-db`.
- `main.go` is tag-free and holds only `cfgPath`, `rootCmd`, `init`, `main` — no
  shared imports with `run.go`, so the cut is clean: `main.go` needs `os`, `cobra`
  and `internal/config`; everything else (`context`, `sha256`, `sql`, `hex`,
  `errors`, `filepath`, `ebiten`, all `internal/*`) went to `run.go`.
- **Deviation:** usage output is silenced from inside `PersistentPreRunE`
  (`cmd.SilenceUsage = true`), not via the struct field. Without any silencing,
  Cobra buries the headless `run` error behind a full usage block. But setting
  the struct field silences *flag-parse* errors too — cobra gates every error
  path on it — so `ecs-db --nonsuch` printed the error and nothing else, leaving
  a flag typo with no recovery hint. Cobra parses flags before it runs
  `PersistentPreRunE`, so setting it there keeps usage for flag errors and drops
  it for runtime ones. Caught in review after the first attempt shipped the
  struct field.
- `tickDurationMs` moved to `run.go`; it is used only by `ensureGoblinBehavior`.
- **Deviation:** four tests rather than one, driven through `rootCmd.Execute()`
  via an `execute` helper so they cover real wiring (including `PersistentPreRunE`
  loading config) rather than calling `RunE` directly.
  `TestRunCmd_RegisteredInHeadlessBuild` pins that `run` still *exists* headless;
  `TestRootCmd_HasNoRunE` pins that bare `ecs-db` prints help rather than
  launching a window; `TestSchemaValidate` is table-driven over valid, malformed
  and reserved-name schemas.
- `runSchemaValidate` now writes to `cmd.OutOrStdout()` rather than `fmt.Printf`,
  so its output is capturable. Untagging `schema.go` is what makes
  `ecs-db schema validate` work headless, so it needed a test rather than the
  0% coverage it had. Package coverage 38.5% → 85.7%.
- `make test` now also runs `go vet -tags ebitengine ./...`. `run.go` is 260
  lines of composition root that untagged `go test ./...` never compiles, so a
  refactor could break the tagged build with every local signal green. Verified
  the vet step actually fails on a deliberate type error in `run.go`.
- The Makefile already derived `BREW_PREFIX`/`XORGPROTO` rather than hardcoding
  them, so only the output path and the new `build-headless` target changed.

Verified: `bin/ecs-db-headless` is 4.5M against the tagged binary's 19M, confirming
Ebitengine is genuinely excluded. Ran the pre-change `bin/game` and the new
`./bin/ecs-db run` back to back — identical startup output and tick progression,
so the rename is behaviour-preserving.

## Notes

- The `ebitengine` build tag is the project's own, not Ebitengine's — `internal/renderer/{game,image_cache,tilemap_renderer}.go` carry it too and are untouched by this story. `internal/renderer/{anim_loader,anim_state,tick}.go` are already untagged and stay importable from the headless binary.
- `config.Init` runs in `PersistentPreRunE` on the root, so it still fires for every subcommand including `forge` (Story 2).
- Cobra's `init()` flag registration is the one sanctioned exception to the project's no-`init()` rule (`AGENTS.md`). Keep it; do not introduce package-level state beyond what is already there.
- Renaming the directory is a `git mv` — keep it as one commit so the history follows the files.
- `make build` needs `mesa`, `libxinerama` and `libxxf86vm` from Homebrew for `GL/glx.h` and the GLFW X11 extension headers. `build-headless` needs none of them; that is the point of this story.
