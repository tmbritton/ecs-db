# Story 1: Config File and Mod Directory Structure

**Epic:** 4 — Behavior hot reload  
**Status:** ✅ Complete  
**Priority:** High — prerequisite for hot reload (Epic 4) and the game binary (Epic 5)

**Depends on:** Epic 3 (agent Loader exists)

## Context

All file paths are currently hardcoded in `cmd/cli/main.go`: `./schema.json`, `./ecs.db`. There is no support for loading behavior machine files from disk at startup. The project is designed for moddability — a core game plus optional mod directories — so paths must be configurable and multiple behavior directories must be supported.

A TOML config file (`game.toml`) is the right format: it supports comments (essential for a user-edited file), and its `[[mods]]` array-of-tables syntax cleanly represents ordered mod loading without workarounds.

## Acceptance Criteria

- [ ] `internal/config` package with `Config`, `DatabaseConfig`, `SchemaConfig`, `ModConfig` structs
- [ ] `config.Load(path string) (*Config, error)` — reads and parses TOML
- [ ] `config.Defaults() *Config` — returns sensible defaults (`./ecs.db`, `./schema.json`, no mods)
- [ ] `ModConfig` fields: `name`, `behaviors`, `actions`, `guards`, `assets`
  - `behaviors` — directory of XState v4 machine JSON files
  - `actions` — directory of Lua custom action scripts (reserved for future Lua epic)
  - `guards` — directory of Lua custom guard scripts (reserved for future Lua epic)
  - `assets` — directory of sprites/sounds (reserved for future rendering epic)
- [ ] `agent.Loader.ScanDir(dir, modName string) (int, error)` — loads all `*.json` in dir; last mod wins on duplicate machine ID (with logged warning); returns error if dir missing
- [ ] `cmd/cli/main.go` reads `-config` flag (default `./game.toml`), falls back to `Defaults()` if file absent
- [ ] `game.toml` committed to the project root as the default config
- [ ] All new code has tests; `go test ./...` passes

## Notes

- `internal/config/config.go` and `internal/config/config_test.go`
- `internal/agent/loader.go` — add `sources map[string]string` field and `ScanDir` method
- `internal/agent/loader_test.go` — new file
- `cmd/cli/main.go` — rewritten with flag parsing and config-driven paths
- TOML library: `github.com/BurntSushi/toml`
- `actions`, `guards`, and `assets` fields are declared in config and `game.toml` now but the CLI does not act on them yet — they will be wired in their respective epics
