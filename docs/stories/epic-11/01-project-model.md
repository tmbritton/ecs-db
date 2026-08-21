# Story 1: Project model — resolve a project and its mods

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** ✅ Complete  
**Priority:** High — every later story in this epic loads through it

**Depends on:** Epic 10 (complete)

## Context

Everything Forge edits lives in a project: a `game.toml` naming a schema, a database, and an ordered list of mods, each contributing behavior files. Today `cmd/ecs-db` resolves that by hand at startup and the pieces are scattered — config parsing here, `agent.Loader.ScanDir` there, mod order implied by iteration.

Forge needs one value that answers: what schema is this, which mods are loaded, in what order, which machines exist, and which file did each come from. The last question is the one nothing can answer today. `agent.Loader` records `machineID → "modName:filepath"` in a private `sources` map and prints a line to stdout when a later mod shadows an earlier one — but exposes no accessor, so the AGENTS-mode machine list has nothing to call and the `override` tag has no data behind it.

This story builds `internal/forge/project` and adds the two missing accessors to `agent.Loader`. It reads only; writing arrives in Stories 2–5.

Load order is the engine's, not a new one: mods are listed in `game.toml`, scanned in order, and a later mod's machine of the same ID replaces an earlier one. Forge must resolve it identically — a project model that disagrees with the engine about which file wins would make the editor show one machine while the game runs another.

## Acceptance Criteria

- [x] `internal/forge/project` exposes a `Project` resolved from a `game.toml` path:
  - schema (loaded and validated) plus its path
  - database path
  - ordered mods, each with name and behaviors directory
  - machines, each with ID, source file, owning mod, and whether it overrides an earlier mod's machine of the same ID
- [x] `agent.Loader` gains `List()` and `Sources()` accessors, both returning copies — the internal maps must not escape, and both are read under the existing `RWMutex`
- [x] Load order matches the engine's: later mods win, and the overridden machine is reported rather than silently dropped
- [x] A mod directory that does not exist is a warning, not a failure — a project with an empty `mods/my-mod/behaviors/` is normal
- [x] A machine that fails to parse or validate does not abort the load; it is collected and reported with its file path, so Forge can show the error instead of refusing to open the project
- [x] A schema that fails to load **is** fatal to opening the project, and says which file and why
- [x] `Project` is a value, not a singleton: constructed from a path, injected where needed, no package-level state
- [x] Table-driven tests over fixture projects in `t.TempDir()`, covering: single mod, two mods with an override, missing mod directory, broken machine file, broken schema
- [x] `go test ./...` passes

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

## As Implemented

`internal/forge/project` resolves a `game.toml` into a `Project` value: schema, paths, ordered mods, machines with their source file and owning mod, and a list of non-fatal `Problem`s. `agent.Loader` gained `List()` and `Sources()`, both returning copies.

### Divergences from the plan

- **Per-file error attribution needed a second pass.** `ScanDir` loads what it can and returns its failures aggregated into one error naming the *directory*, so a `Problem` built from it could not point at the file — which the AC requires. On the error path only, each `.json` in the directory is retried individually through `LoadMachine` to attribute the failure. Retrying a file that already loaded is harmless, and this keeps `ScanDir` as the single implementation of the precedence rule, which was the point.
- **Paths resolve relative to `game.toml`, not the working directory.** `config.Load` returns them verbatim, and Forge can be started from anywhere. Getting this wrong would have made the editor open a different project than the one the config names, depending on where it was launched.

### Design decisions worth knowing later

- **Override detection is derived, not computed.** Rather than reimplementing "later mod wins", the loader's `Sources()` is snapshotted between mod scans and the flag comes from what actually changed. Two implementations of precedence is exactly the bug that would let the editor show one machine while the game runs another. Verified by mutation: scanning mods in reverse fails the override test.
- **`List()` and `Sources()` return shallow copies, deliberately.** `ReloadFile` replaces the pointer in the map rather than mutating a definition in place, so a caller holding an older pointer sees a consistent previous version rather than a torn new one. A deep copy would be the obvious "fix" and would be wrong.
- **A broken machine is a `Problem`; a broken schema is fatal.** The schema describes everything else, so there is nothing to show without it. A broken machine file is precisely what someone opens Forge to fix.

### What the review caught

Two confirmed correctness bugs, both of the exact kind this package exists to prevent — the editor and the game disagreeing about what is loaded:

- **Every machine reported the wrong mod.** The ownership map was reassigned for the loader's *entire* source map after each mod scan, so the last mod with a readable behaviors directory owned every machine in the project. The panel would have shown "idle — from artpack" beside a file in `core/`. `Path` was right and `Mod` was wrong, so the two contradicted each other. The fix deletes the separate tracking entirely: the owning mod is already the first half of the loader's source string, and `splitSource` was returning it and discarding it.
- **A mod declaring no behaviors directory scanned the project root.** `run.go` skips those — an assets-only artpack is a first-class case — but `abs("")` resolves to the project root, `os.Stat` succeeds, and every stray `.json` there gets parsed as a behavior machine. Reproduced: `schema.json` and a `tileset.json` were both loaded as nameless machines.

Both had passed the original tests. `TestOpen_LaterModOverridesEarlier` checked `Overrides` but never `Mod`, and the one case where `Mod` was checked happened to have the right answer by accident, because that mod was also the last one scanned.

Also fixed from review: a schema fixture that was rejected by `LoadSchema` before `ValidateSchema` was ever reached, leaving that branch untested; the RWMutex acceptance criterion, which had no coverage at all until a test ran the accessors alongside `ReloadFile` under `-race`; `cmd/ecs-db/forge.go` resolving the same two paths two different ways in one function, so the status readout and the project model looked for `schema.json` in different places; duplicate problems and spurious override flags when two mods share a behaviors directory; and a machine id declared by two files in one mod, which silently loaded one and reported nothing.

**A deliberate divergence from the story's own note.** It said the registry is built "exactly as `run.go` builds it". It was not: `run.go` registers the pathfinding and line-of-sight builtins only when a map is configured, and Forge registered them unconditionally — so a machine using `computePath` would validate clean in Forge and then fail to load in the engine. Forge now mirrors the condition. That the engine's action vocabulary depends on map configuration at all is arguably an engine defect, but Forge's job is to describe the engine accurately, not to improve on it silently.

### The validation gap this uncovered

Building the "broken machine" fixture surfaced an engine-side gap: **`ValidateMachine` does not check that a machine's `initial` names a state that exists.** A transition to a missing state is rejected; `initial` is not. The fixture had to be rewritten to use an unregistered action instead.

It is recorded in the epic README with the full matrix, and has since been fixed in `internal/agent/validator.go` — see that README. The fixture here deliberately keeps using an unregistered action rather than switching to a bad `initial`, so it still tests what it says it tests if the validation rules change again.
