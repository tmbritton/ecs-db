# Epic 11 Story 1: Project model — Implementation Plan

**Goal:** one value that answers "what is this project", resolved the same way the engine resolves it.

**Architecture:** `internal/forge/project` is a domain package — a path in, a `Project` out, no HTTP and no templates. It delegates every load-order decision to `agent.Loader` rather than reimplementing them.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Modify | `internal/agent/loader.go` | `List()` and `Sources()` accessors |
| Modify | `internal/agent/loader_test.go` | Accessor tests, including the copy guarantee |
| Create | `internal/forge/project/project.go` | `Project`, `Machine`, `Mod`, `Open` |
| Create | `internal/forge/project/project_test.go` | Fixture-project table tests |
| Modify | `cmd/ecs-db/forge.go` | Open the project at startup |

---

## Task 1: The missing accessors

```go
// List returns the loaded machines keyed by ID. The map is a copy: the
// loader's own map is mutated by ReloadFile under lock, and handing it out
// would race with the watcher goroutine.
func (l *Loader) List() map[string]*MachineDefinition

// Sources returns machineID → "modName:filepath" for every loaded machine,
// also as a copy.
func (l *Loader) Sources() map[string]string
```

Both take `l.mu.RLock()`. The copy is shallow — the `*MachineDefinition` values
are shared, which is correct: `ReloadFile` *replaces* the pointer in the map
rather than mutating the definition in place, so a caller holding an old pointer
sees a consistent old value rather than a torn new one. Worth a comment; it is
the kind of thing a later reader "fixes" into a deep copy.

**Test the copy guarantee by mutation**: take a `List()`, delete a key from the
returned map, and assert the loader still has it.

## Task 2: The project model

```go
type Mod struct {
	Name      string
	Behaviors string // directory
}

type Machine struct {
	ID         string
	Path       string // file it was loaded from
	Mod        string // mod that contributed the winning file
	Overrides  bool   // shadows an earlier mod's machine of the same ID
	Definition *agent.MachineDefinition
}

type Project struct {
	ConfigPath string
	SchemaPath string
	DBPath     string
	Schema     schema.DatabaseSchema
	Mods       []Mod
	Machines   []Machine // sorted by ID, for stable rendering
	Problems   []Problem // non-fatal: a machine that would not load
}

type Problem struct {
	Path string
	Err  error
}

func Open(configPath string) (*Project, error)
```

`Open` fails only on a config or schema it cannot load. Everything else is a
`Problem`: Forge's job includes showing you the broken file so you can fix it,
and refusing to open the project would make that impossible.

**Override detection.** `ScanDir` overwrites `sources[id]` silently from the
caller's point of view. To know a machine *was* overridden, snapshot
`Sources()` between mod scans and compare — scan mod 1, snapshot, scan mod 2,
and any ID whose source changed was overridden. That keeps the rule inside
`agent` and derives the flag from observed behaviour rather than a second copy
of the precedence logic.

## Task 3: Wiring

`cmd/ecs-db/forge.go` opens the project and passes it to the server. Note that
`status.Config` already takes a `SchemaPath` and re-reads per poll (Story 6 of
Epic 10) — leave that as it is. The project model is a startup snapshot; the
readout is deliberately live. They answer different questions.

Opening must not be fatal to `ecs-db forge`: a project that will not resolve
should still start the editor with the problem visible, for the same reason a
broken machine is a `Problem`. Log it and serve; Epic 12 renders it.

---

## Verification

Fixture projects built in `t.TempDir()` by a helper that writes a `game.toml`, a
`schema.json` and N mod directories. Cases: single mod; two mods where the second
shadows a machine from the first (assert `Overrides` on exactly the winner, and
that its `Path` is the second mod's file); a mod directory that does not exist;
a machine file that fails validation; a schema that fails to load.

Before committing: fresh-context code review, `make test`, `make e2e`, linter.
