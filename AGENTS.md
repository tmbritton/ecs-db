# AGENTS.md — ecs-db

## Project Overview

**ecs-db** is a process-separated, database-as-contract game engine. Game state lives in a SQLite database whose structure is defined by a declarative `schema.json` file. Game behavior is defined by JSON state machines (XState-compatible) attached per-entity. Each component of the engine — interpreter, renderer, debugger — is a separate OS process communicating only through the database.

**Key principles:**
- The `schema.json` is the API and the source of truth for components and entity types
- The SQLite schema is the IPC contract — all cross-process communication is a SQL query
- Strict validation by default; modder mistakes are loud, not silent
- One writer per table to avoid SQLite write contention
- Local-first, moddable with a text editor, language-polyglot by design

## Milestone Order

1. **Monolithic prototype** — Schema-driven SQL generation, one entity running, hot-reload behavior
2. **Extract debugger** — Read-only HTTP server, validates database-as-contract
3. **Extract renderer** — Input flow becomes async, three processes, one game
4. **Add effects** — Transitions-as-effect-source flow
5. **Add second renderer** — Prove multi-renderer thesis
6. **Schema versioning & migrations** — Once the pain shape is known

See `docs/game-engine-arch.md` for the full architecture and `docs/plan.md` for the implementation roadmap.

## Current State

We are working through **Epic 1: Schema-driven data foundation**. The `schema.json` format and Go types are being aligned to match the architecture document (see `docs/stories/epic-1/01-implementation-plan.md`).

**Language:** Go  
**Database:** SQLite (WAL mode)  
**Test framework:** `testing` + `testify` (stdlib-first, testify for assertions if needed)

---

## Code Style & Architecture Guidelines

### Hexagonal Architecture (Ports & Adapters)

Every domain use case is isolated behind a **port** (Go interface). Adapters implement ports for specific infrastructure concerns. This makes the interpreter testable without a real database, the schema loader testable without the filesystem, etc.

```
internal/
  schema/          # Domain: schema types, loading, validation
  world/           # Domain: entities, components, lifecycle
  agent/           # Domain: state machine execution
  ports.go         # All domain ports (interfaces)
  adapters/        # Infrastructure adapters
    sqlite/        # SQLite-backed implementations
    file/          # Filesystem implementations
    mock/          # Test doubles
```

**Rules:**
- Domain packages import nothing from `adapters/`
- Adapters depend on domain interfaces, never leak SQLite types upward
- `main.go` and `cmd/` are the composition root — the only place that wires concrete implementations to ports
- No `init()` functions; explicit construction via constructors
- No package-level `var` singletons; everything is constructed and wired in `main()`

### Dependency Injection (Wire)

**Google Wire** is used for compile-time dependency injection. It generates explicit construction code — no reflection, no runtime DI container.

**Rules:**
- Define providers as functions that return concrete types or interfaces
- Wire sets live in `cmd/*/wire.go` — they declare which providers assemble the object graph for each binary
- Run `wire gen ./cmd/...` after changing any provider or set; commit the generated `wire_gen.go`
- Providers should be in the package that owns the type they construct (e.g., `schema.NewLoader()` lives in `internal/schema/`, not in `cmd/`)
- Interfaces in `ports.go` are bound via provider functions that return the interface type
- No `init()` functions; no package-level `var` singletons; everything is wired by Wire
- When a provider needs configuration (flags, env vars), pass it in as an explicit parameter

```go
// internal/schema/provider.go
func NewLoader(config *Config) *schema.Loader { ... }

// cmd/server/wire.go
var ServerSet = wire.NewSet(
    NewConfigFromFlags,
    schema.NewLoader,
    world.NewService,
)

// Wire generates cmd/server/wire_gen.go
```

The domain never imports Wire. Wire-only imports belong exclusively in `wire.go` files under `cmd/`.

### CLI (Cobra)

All command-line binaries use [Cobra](https://github.com/spf13/cobra) for command structure, flag parsing, and help text.

**Rules:**
- One root command per binary in `cmd/<binary>/`
- Subcommands for distinct actions (e.g., `ecs-db migrate up`, `ecs-db schema validate`)
- Flags defined on cmds, parsed via Cobra; converted to a `Config` struct via a Wire provider
- No flag parsing in domain or adapter packages — Cobra stays in `cmd/`
- Commands should be thin: parse flags, call the domain service via Wire-wired interfaces, exit

```go
// cmd/ecs-db/cmd/root.go
var rootCmd = &cobra.Command{
    Use:   "ecs-db",
    Short: "ECS game engine backed by SQLite",
}

func init() {
    rootCmd.Flags().StringP("schema", "s", "schema.json", "Path to schema.json")
    rootCmd.Flags().StringP("db", "d", "world.sqlite", "Path to SQLite database")
}
```

### Code Composition Summary

```
cmd/<binary>/
  cmd/          # Cobra command tree
  wire.go       # Wire provider sets (imports domain + adapters)
  wire_gen.go   # Generated by wire gen — committed to repo
  main.go       # Cobra .Execute() + error handling (thin)

internal/
  schema/       # Domain: schema types, loading, validation
  world/        # Domain: entities, components, lifecycle
  agent/        # Domain: state machine execution
  ports.go      # All domain ports (interfaces)
  adapters/     # Infrastructure adapters
    sqlite/     # SQLite-backed implementations
    file/       # Filesystem implementations
    mock/       # Test doubles
```

### Test-Driven Development

**Write tests first, always.** The workflow:

1. Write the test (failing — red)
2. Write the minimum code to pass (green)
3. Refactor (refactor)
4. Commit

**No production code without a test.** If you're adding a function, adding a validation, or changing behavior — the test comes first.

### 100% Test Coverage Target

- Every exported function, every branch, every validation path must be tested
- Use `go test -cover` to measure — aim for 100% on domain packages
- Edge cases and error paths are not optional: test them too
- Integration tests in `/adapters/` use a fresh in-memory SQLite per test

---

## Testing Conventions

**Table-driven tests** for all function testing:
```go
func TestValidateFoo(t *testing.T) {
    tests := []struct {
        name    string
        input   SomeInput
        want    ExpectedOutput
        wantErr bool
    }{{...}}
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) { ... })
    }
}
```

**Test naming:** `Test{FunctionName}_{Scenario}` — e.g., `TestLoadSchema_InvalidVersion`, `TestCreateEntity_MissingRequiredComponent`

**Mocking:** Use hand-written mocks or `go-mock` for interfaces in `ports.go`. No test-specific behavior in production code.

**SQLite adapter tests:** Use `file::memory:?cache=shared` or `:memory:` — no filesystem dependencies.

**Integration boundaries:** One `*_integration_test.go` per adapter for end-to-end adapter verification. Everything else is unit tests.

---

## Git Conventions

- Commits follow Conventional Commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`
- One commit per logical change — no "fix" commits on top of "feat" commits for the same concern
- Update stories as tasks are completed: mark checkboxes in `docs/stories/epic-1/*.md`
- Implementation lives on `main`; feature branches only for parallel work
