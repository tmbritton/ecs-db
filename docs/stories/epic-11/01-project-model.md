# Story 1: Project model — resolve a project and its mods

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** 🔲 Not started  
**Priority:** High — every later story in this epic loads through it

**Depends on:** Epic 10 (complete)

## Context

Everything Forge edits lives in a project: a `game.toml` naming a schema, a database, and an ordered list of mods, each contributing behavior files. Today `cmd/ecs-db` resolves that by hand at startup and the pieces are scattered — config parsing here, `agent.Loader.ScanDir` there, mod order implied by iteration.

Forge needs one value that answers: what schema is this, which mods are loaded, in what order, which machines exist, and which file did each come from. The last question is the one nothing can answer today. `agent.Loader` records `machineID → "modName:filepath"` in a private `sources` map and prints a line to stdout when a later mod shadows an earlier one — but exposes no accessor, so the AGENTS-mode machine list has nothing to call and the `override` tag has no data behind it.

This story builds `internal/forge/project` and adds the two missing accessors to `agent.Loader`. It reads only; writing arrives in Stories 2–5.

Load order is the engine's, not a new one: mods are listed in `game.toml`, scanned in order, and a later mod's machine of the same ID replaces an earlier one. Forge must resolve it identically — a project model that disagrees with the engine about which file wins would make the editor show one machine while the game runs another.

## Acceptance Criteria

- [ ] `internal/forge/project` exposes a `Project` resolved from a `game.toml` path:
  - schema (loaded and validated) plus its path
  - database path
  - ordered mods, each with name and behaviors directory
  - machines, each with ID, source file, owning mod, and whether it overrides an earlier mod's machine of the same ID
- [ ] `agent.Loader` gains `List()` and `Sources()` accessors, both returning copies — the internal maps must not escape, and both are read under the existing `RWMutex`
- [ ] Load order matches the engine's: later mods win, and the overridden machine is reported rather than silently dropped
- [ ] A mod directory that does not exist is a warning, not a failure — a project with an empty `mods/my-mod/behaviors/` is normal
- [ ] A machine that fails to parse or validate does not abort the load; it is collected and reported with its file path, so Forge can show the error instead of refusing to open the project
- [ ] A schema that fails to load **is** fatal to opening the project, and says which file and why
- [ ] `Project` is a value, not a singleton: constructed from a path, injected where needed, no package-level state
- [ ] Table-driven tests over fixture projects in `t.TempDir()`, covering: single mod, two mods with an override, missing mod directory, broken machine file, broken schema
- [ ] `go test ./...` passes

## Playwright steps

None yet. This story adds no browser surface — the project model is loaded by
`ecs-db forge` at startup and nothing renders it until Epic 12's SCHEMA and ENTS
modes. Its correctness is covered by Go tests against fixture projects.

The one browser-visible consequence is indirect and already covered: the
engine-status readout names the first mod (`mods/e2e-core`), and
`e2e/specs/06-engine-status.spec.js` asserts it.

## Notes

- **Do not reimplement `ScanDir`'s rules.** Resolve through `agent.Loader` so there is exactly one implementation of "which file wins". Two implementations of load order is precisely the bug this story exists to prevent.
- `ScanDir` currently `fmt.Printf`s its override warning to stdout. Forge needs that as data, not a log line. Returning it through the new accessors is the minimum; changing `ScanDir`'s logging is out of scope.
- The registry that `NewLoader` needs is built exactly as `cmd/ecs-db/run.go` builds it (`builtins.NewRegistry` plus `RegisterPathfinding`/`RegisterLineOfSight`). Those two need a non-nil `*tilemap.TileGrid` for their closures; pass a throwaway `NewTileGrid(0, 0)` when only metadata is wanted. Forge never ticks a machine.
- Keep `project` free of HTTP and templ, like `status`. It is a domain package.
- The database is **not** opened here. `internal/forge/status` owns that, read-only, and this story only records the path.
