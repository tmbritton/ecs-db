# Epic 10 Story 1: CLI restructure — Implementation Plan

**Goal:** `CGO_ENABLED=0 go build ./cmd/ecs-db` succeeds with no build tags, while `make build` still produces a working game binary.

**Architecture:** One Cobra root, tag-free. Subcommands opt into build tags individually. The `run` subcommand exists in both builds — real behind `ebitengine`, an error-returning stub without it — so the CLI surface is identical and the failure mode is a clear message rather than a missing command.

**Depends on:** nothing.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Move | `cmd/game/` → `cmd/ecs-db/` | Binary rename |
| Create | `cmd/ecs-db/main.go` | Tag-free root command + `Execute()` |
| Create | `cmd/ecs-db/run.go` | `//go:build ebitengine` — `run` subcommand (was `runGame`) |
| Create | `cmd/ecs-db/run_stub.go` | `//go:build !ebitengine` — `run` that errors |
| Modify | `cmd/ecs-db/schema.go` | Was `schema_cmd.go`; build tag removed |
| Delete | `cmd/ecs-db/stub.go` | Untagged build now has a real `main()` |
| Modify | `Makefile` | `bin/ecs-db`, `run` target, new `build-headless` |

---

## Task 1: Move the package

```bash
git mv cmd/game cmd/ecs-db
git mv cmd/ecs-db/schema_cmd.go cmd/ecs-db/schema.go
git rm cmd/ecs-db/stub.go
```

No content changes yet — `make build` must still pass after adjusting the Makefile output path, proving the move alone broke nothing.

---

## Task 2: Extract the tag-free root

Split the current `cmd/ecs-db/main.go` (tagged) into a tag-free `main.go` and a tagged `run.go`.

### `cmd/ecs-db/main.go` (new, no build tag)

```go
package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/tmbritton/ecs-db/internal/config"
)

var cfgPath string

var rootCmd = &cobra.Command{
	Use:   "ecs-db",
	Short: "ECS-in-SQLite game engine and content tools",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return config.Init(cfgPath)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "./game.toml", "path to TOML config file")
	rootCmd.AddCommand(schemaCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
```

Note the removed `RunE` — bare `ecs-db` now prints help. `runCmd` is registered by whichever of the two files below is compiled in.

### `cmd/ecs-db/run.go` (`//go:build ebitengine`)

Everything the old `main.go` had below `func main()` moves here unchanged: `tickDurationMs`, `runGame`, `ensurePlayerEntity`, `ensureGoblinEntity`, `ensureGoblinBehavior`, and the Ebitengine imports. Add:

```go
//go:build ebitengine

package main

import "github.com/spf13/cobra"

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the game",
	RunE:  runGame,
}

func init() { rootCmd.AddCommand(runCmd) }
```

### `cmd/ecs-db/run_stub.go` (`//go:build !ebitengine`)

```go
//go:build !ebitengine

package main

import (
	"errors"

	"github.com/spf13/cobra"
)

var errNoEbitengine = errors.New(
	`this binary was built without the "ebitengine" build tag; rebuild with ` + "`make build`" + ` to run the game`)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the game (unavailable in this build)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return errNoEbitengine
	},
}

func init() { rootCmd.AddCommand(runCmd) }
```

Both files declare `runCmd` and an `init()`; the build tags guarantee exactly one is compiled.

---

## Task 3: Untag the schema command

In `cmd/ecs-db/schema.go`, delete the `//go:build ebitengine` line and the blank line after it. Nothing else changes — it only imports `internal/config` and `internal/schema`, both untagged.

---

## Task 4: Makefile

```make
.PHONY: build build-headless run clean test

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	go build -tags ebitengine -o bin/ecs-db ./cmd/ecs-db

# Proves the Forge/CLI path builds with no graphics toolchain at all.
build-headless:
	CGO_ENABLED=0 go build -o bin/ecs-db-headless ./cmd/ecs-db

run: build
	./bin/ecs-db run
```

Keep the existing `CGO_CFLAGS`/`CGO_LDFLAGS` values and the `clean`/`test` targets. Add `bin/ecs-db-headless` coverage to `clean` via the existing `rm -rf bin/`.

---

## Task 5: Tests

There are no tests in `cmd/` today and this story adds no domain logic, so the meaningful verification is the build matrix plus a behavioural check that `run` still works. Add one table-driven test for the stub's error path so the `!ebitengine` branch is not untested:

### `cmd/ecs-db/run_stub_test.go` (`//go:build !ebitengine`)

```go
//go:build !ebitengine

package main

import (
	"errors"
	"testing"
)

func TestRunCmd_WithoutEbitengine_ReturnsError(t *testing.T) {
	err := runCmd.RunE(runCmd, nil)
	if !errors.Is(err, errNoEbitengine) {
		t.Fatalf("got %v, want errNoEbitengine", err)
	}
}
```

---

## Task 6: Mark story complete

Tick the boxes in `docs/stories/epic-10/01-cli-restructure.md`, add an `## As Implemented` section recording any deviation, and tick the **CLI restructure** story in `docs/plan.md` (Epic 10).

---

## Verification

```bash
# 1. Headless build — the point of the story. No tags, no cgo.
CGO_ENABLED=0 go build -o /tmp/ecs-db-headless ./cmd/ecs-db

# 2. Headless CLI surface
/tmp/ecs-db-headless --help                 # lists run, schema
/tmp/ecs-db-headless schema validate        # works — it never could before
/tmp/ecs-db-headless run                    # clear "built without ebitengine" error, exit 1

# 3. Tagged build unchanged
make build
./bin/ecs-db schema validate
./bin/ecs-db run                            # player moves, goblin wanders

# 4. Suite + lint
go test ./...
mise exec -- golangci-lint run
```

Before committing, launch a fresh-context code-review subagent over the staged diff, per `AGENTS.md`.
