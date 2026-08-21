# Story 1: CLI restructure — split Cobra out of the ebitengine build tag

**Epic:** 10 — Forge: foundation, toolchain & design system  
**Status:** 🔲 Not started  
**Priority:** High — nothing else in Epic 10 can build until this lands

**Depends on:** nothing

## Context

Every file in `cmd/game/` carries `//go:build ebitengine`, and `stub.go` supplies an empty `main()` for the untagged build. That was fine while the binary only ever ran a game window, but Forge is a webapp: it needs no OpenGL, no X11, and no cgo. Today a Forge subcommand would either drag the entire Ebitengine toolchain into every build or be unreachable without it.

This story splits the Cobra tree in two. The root command, the `--config` flag, and everything that only touches `internal/config` and `internal/schema` become tag-free. Only `run` — the composition root that opens a window — stays behind `//go:build ebitengine`, with a `!ebitengine` counterpart that registers the same subcommand and exits with a clear explanation instead of silently doing nothing.

Two shape changes come with it. The binary is renamed `ecs-db` (the name `AGENTS.md` has documented all along), and running the game becomes an explicit `ecs-db run` rather than the bare root command. `game schema validate` becomes `ecs-db schema validate` and starts working in a headless build, which it never could before.

## Acceptance Criteria

- [ ] `cmd/game/` renamed to `cmd/ecs-db/`; Makefile builds `bin/ecs-db`
- [ ] `cmd/ecs-db/main.go` — **no build tag**:
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
- [ ] `cmd/ecs-db/run.go` (`//go:build ebitengine`) — the current `runGame` composition root moved verbatim, registering a `run` subcommand
- [ ] `cmd/ecs-db/run_stub.go` (`//go:build !ebitengine`) — registers `run`, returns an error naming the missing tag:
  ```
  this binary was built without the "ebitengine" build tag; rebuild with `make build` to run the game
  ```
- [ ] `cmd/ecs-db/schema.go` — build tag removed; `ecs-db schema validate` works headless
- [ ] `cmd/game/stub.go` deleted — the untagged build now has a real `main()`
- [ ] `Makefile`:
  - `build` keeps `-tags ebitengine` and the CGO flags, outputs `bin/ecs-db`
  - `run` becomes `./bin/ecs-db run`
  - new `build-headless` target: `CGO_ENABLED=0 go build -o bin/ecs-db-headless ./cmd/ecs-db`
- [ ] `CGO_ENABLED=0 go build ./cmd/ecs-db` succeeds with no build tags
- [ ] `make build && ./bin/ecs-db run` — player and wandering goblin behave exactly as before
- [ ] `go test ./...` passes

## Notes

- The `ebitengine` build tag is the project's own, not Ebitengine's — `internal/renderer/{game,image_cache,tilemap_renderer}.go` carry it too and are untouched by this story. `internal/renderer/{anim_loader,anim_state,tick}.go` are already untagged and stay importable from the headless binary.
- `config.Init` runs in `PersistentPreRunE` on the root, so it still fires for every subcommand including `forge` (Story 2).
- Cobra's `init()` flag registration is the one sanctioned exception to the project's no-`init()` rule (`AGENTS.md`). Keep it; do not introduce package-level state beyond what is already there.
- Renaming the directory is a `git mv` — keep it as one commit so the history follows the files.
- `make build` needs `mesa`, `libxinerama` and `libxxf86vm` from Homebrew for `GL/glx.h` and the GLFW X11 extension headers. `build-headless` needs none of them; that is the point of this story.
