# Story 2: Ebitengine Wiring + Tick Loop

**Epic:** 5 — Interpreter tick loop & Ebitengine monolith  
**Status:** ✅ Complete  
**Priority:** High — foundation for all rendering and input stories

**Depends on:** Story 1 (schema), Epic 4 (Loader, watcher, config)

## Context

The engine currently has no entry point beyond the CLI debug tool (`cmd/cli/main.go`). This story creates the game binary: `cmd/game/main.go`. It wires Ebitengine into the interpreter, establishing the 60 TPS / 20 Hz split that the rest of Epic 5 builds on.

Ebitengine calls `Update()` at a fixed TPS (set to 60). The interpreter fires on every third call (~20 Hz game logic), while input is sampled every frame (60 Hz). `Draw()` renders the current world state after each `Update()`.

At the end of this story, a window opens showing a black screen. The interpreter tick runs at ~20 Hz, logging each tick. No sprites, no input handling yet.

## Acceptance Criteria

- [x] `go.mod` updated with `github.com/hajimehoshi/ebiten/v2` dependency
- [x] `cmd/game/main.go` — entry point that:
  - Loads config from `-config` flag (default `./game.toml`)
  - Opens SQLite database (via existing `SQLiteStore`)
  - Ensures interpreter tables exist (`EnsureInterpreterTables`)
  - Scans behavior directories from config mods
  - Starts filesystem watcher (from Epic 4)
  - Calls `ebiten.RunGame(&Game{...})`
- [x] `internal/renderer/game.go` — `Game` struct implementing `ebiten.Game` (requires `-tags ebitengine` + X11 headers to build):
  - Fields: `ticker *Ticker`, `frameCount int`, `logicalW/H int`
  - `const TicksPerSecond = 20` (in `tick.go`)
  - `Update() error`: increments `frameCount`; if `frameCount % (60/TicksPerSecond) == 0`, calls `ticker.RunTick()`
  - `Draw(*ebiten.Image)`: clears screen to black (stub)
  - `Layout(outsideW, outsideH int) (int, int)`: returns fixed logical resolution from config
- [x] Interpreter tick sequence inside `Ticker.RunTick()` (in `internal/renderer/tick.go`):
  1. Drain unconsumed `input_events` rows → no-op stub (not yet wired)
  2. Drain due `event_queue` rows (`target_tick ≤ current_tick`)
  3. Deliver `TICK` event to every entity with an active `behavior_components` row
  4. Advance `world.current_tick` and bump `world.world_version` — all in one SQLite transaction
- [x] `game.toml` updated with `[window]` section: `title`, `width`, `height` (logical pixels), `tileSize` (pixels per tile)
- [x] `go test ./...` passes (Ebitengine files gated behind `//go:build ebitengine`; window smoke test requires X11 dev headers)

## Notes

- `ebiten.SetTPS(60)` in `main()` before `ebiten.RunGame`.
- The interpreter tick transaction wraps steps 1–4 as a single `BEGIN` / `COMMIT`. On error, it returns the error from `Update()` which Ebitengine treats as a fatal signal.
- `frameCount` overflows eventually (int wraps at 2^63 on 64-bit). Use `frameCount % (60/TicksPerSecond) == 1` rather than `== 0` to avoid firing on the very first frame before any state is loaded. Or just let it start immediately — both are fine for the prototype.
- The `scheduler` is whatever existing mechanism delivers `TICK` events to all active machines. Look at `internal/agent/scheduler.go` to understand the current API before designing the call site.
- `internal/renderer/` is a new package. It must not import `internal/agent` directly if `agent` already imports anything that would create a cycle — check the import graph before adding dependencies.
- Ebitengine's built-in accumulator guarantees `Update()` is called at 60 TPS even when `Draw()` runs slower; multiple `Update()` calls may precede a single `Draw()` during catch-up. This is correct behavior — the `% 3` check handles it naturally.
