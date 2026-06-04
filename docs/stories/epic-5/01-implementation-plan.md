# Epic 5, Story 1: Schema Updates — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update `schema.json` to v2 (evolve `Sprite`, add `Tile`/`Path`/`Speed` components and `Tile` entity type). Introduce Cobra to the CLI so subcommands can grow over time. Add a `schema validate` subcommand as the canonical way to validate `schema.json` content.

**Architecture:** `cmd/cli/main.go` gains a Cobra root command. The existing startup behaviour becomes the root `RunE` for now. `schema validate` is the first subcommand — reads config, loads and validates `schema.json`, prints result, exits. `schema.json` is content, not code — tests never read it; the CLI command is the validation tool.

**Tech Stack:** Go, `github.com/spf13/cobra`, existing `internal/schema` (LoadSchema, ValidateSchema, InitSchema).

**Save this plan to:** `docs/stories/epic-5/01-implementation-plan.md` as the first step of implementation.

---

## Context

`schema.json` currently has `schemaVersion: 1` with `Position`, `Health`, `Sprite` (imageId string, frame integer). Entity types: `Goblin`, `Player`.

Epic 5 needs:
- `Sprite` reshaped: drop `imageId`/`frame`, add `sheet` string + `animation` string + `flip_x` boolean
- New components: `Tile` (x int, y int, passable bool, tile_type string), `Path` (waypoints string, current_index int), `Speed` (value number)
- New entity type: `Tile` (requiredComponents: ["Tile"])
- `Goblin` gains `Path` + `Speed` as optional

`schema.json` is content, not code. Tests use inline `schema.DatabaseSchema` structs or `testdata/` files — never `schema.json` directly. The `schema validate` CLI command is the right tool for confirming the file is well-formed after editing.

Existing schema functions to reuse (all in `internal/schema/`):
- `LoadSchema(data []byte) (DatabaseSchema, error)` — parses JSON
- `ValidateSchema(s DatabaseSchema) error` — semantic validation
- `InitSchema(path string) (DatabaseSchema, error)` — reads disk → parse → validate in one call

The current CLI (`cmd/cli/main.go`) is a single `flag`-based command. This story restructures it with Cobra, moving the current behaviour into the root `RunE` and adding `schema validate` as the first subcommand.

---

## Task 1: Save this plan

**Files:**
- Create: `docs/stories/epic-5/01-implementation-plan.md`

- [ ] **Step 1: Copy plan to epic stories folder**

```bash
cp /var/home/tom/.claude/plans/toasty-crafting-toast.md \
   docs/stories/epic-5/01-implementation-plan.md
```

- [ ] **Step 2: Commit**

```bash
git add docs/stories/epic-5/01-implementation-plan.md
git commit -m "docs(epic-5/story-1): add implementation plan"
```

---

## Task 2: Add Cobra and restructure cmd/cli

**Files:**
- Modify: `cmd/cli/main.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add Cobra dependency**

```bash
go get github.com/spf13/cobra@latest
```

- [ ] **Step 2: Rewrite cmd/cli/main.go with Cobra**

The root command keeps the existing startup behaviour. The `-config` flag moves to a persistent flag on the root so all subcommands inherit it.

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tombrittany/ecs-db/internal/config"
	"github.com/tombrittany/ecs-db/internal/schema"
	"github.com/tombrittany/ecs-db/internal/storage"
	// ... other existing imports
)

var cfgPath string

var rootCmd = &cobra.Command{
	Use:   "ecs-db",
	Short: "ECS-in-SQLite game engine",
	RunE:  runGame,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "./game.toml", "path to game.toml")
	rootCmd.AddCommand(schemaCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runGame(cmd *cobra.Command, args []string) error {
	// existing startup logic moved here verbatim
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	// ... rest of current main() body
	return nil
}
```

Note: the existing import path for the module is whatever is declared in `go.mod` — check it before writing the import path above.

- [ ] **Step 3: Run tests to confirm no regressions**

```bash
go test ./...
```

Expected: all pass (no behaviour changed — only the CLI wiring moved to Cobra).

- [ ] **Step 4: Commit**

```bash
git add cmd/cli/main.go go.mod go.sum
git commit -m "feat(cli): introduce Cobra; existing startup becomes root RunE"
```

---

## Task 3: Add `schema validate` subcommand

**Files:**
- Create: `cmd/cli/schema_cmd.go`

- [ ] **Step 1: Create schema_cmd.go**

```go
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tombrittany/ecs-db/internal/config"
	"github.com/tombrittany/ecs-db/internal/schema"
)

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Schema utilities",
}

var schemaValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate schema.json against the loaded config",
	RunE:  runSchemaValidate,
}

func init() {
	schemaCmd.AddCommand(schemaValidateCmd)
}

func runSchemaValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	s, err := schema.InitSchema(cfg.Schema.Path)
	if err != nil {
		return fmt.Errorf("schema invalid: %w", err)
	}
	fmt.Printf("schema.json OK (version %d, %d components, %d entity types)\n",
		s.SchemaVersion, len(s.Components), len(s.EntityTypes))
	return nil
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 3: Smoke-test the subcommand against the current schema.json**

```bash
go run ./cmd/cli schema validate
```

Expected output (v1 schema):
```
schema.json OK (version 1, 3 components, 2 entity types)
```

- [ ] **Step 4: Commit**

```bash
git add cmd/cli/schema_cmd.go
git commit -m "feat(cli): add 'schema validate' subcommand"
```

---

## Task 4: Update schema.json to v2

**Files:**
- Modify: `schema.json`

- [ ] **Step 1: Replace schema.json**

```json
{
  "schemaVersion": 2,
  "components": {
    "Position": {
      "type": "object",
      "properties": {
        "x": { "type": "number" },
        "y": { "type": "number" }
      }
    },
    "Health": {
      "type": "object",
      "properties": {
        "hp":    { "type": "integer" },
        "maxHp": { "type": "integer" }
      }
    },
    "Sprite": {
      "type": "object",
      "properties": {
        "sheet":     { "type": "string" },
        "animation": { "type": "string" },
        "flip_x":    { "type": "boolean" }
      }
    },
    "Tile": {
      "type": "object",
      "properties": {
        "x":         { "type": "integer" },
        "y":         { "type": "integer" },
        "passable":  { "type": "boolean" },
        "tile_type": { "type": "string" }
      }
    },
    "Path": {
      "type": "object",
      "properties": {
        "waypoints":     { "type": "string" },
        "current_index": { "type": "integer" }
      }
    },
    "Speed": {
      "type": "object",
      "properties": {
        "value": { "type": "number" }
      }
    }
  },
  "entityTypes": {
    "Goblin": {
      "requiredComponents": ["Position", "Health", "Sprite"],
      "optionalComponents": ["Path", "Speed"],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    },
    "Player": {
      "requiredComponents": ["Position", "Health", "Sprite"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    },
    "Tile": {
      "requiredComponents": ["Tile"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    }
  }
}
```

- [ ] **Step 2: Validate with the new CLI command**

```bash
go run ./cmd/cli schema validate
```

Expected:
```
schema.json OK (version 2, 6 components, 3 entity types)
```

- [ ] **Step 3: Verify the migration runs cleanly against a fresh DB**

```bash
rm -f world.sqlite
go run ./cmd/cli
sqlite3 world.sqlite ".tables"
sqlite3 world.sqlite "PRAGMA table_info(comp_sprite);"
sqlite3 world.sqlite "PRAGMA table_info(comp_tile);"
sqlite3 world.sqlite "SELECT key, value FROM meta;"
```

Expected `comp_sprite` columns: `entity_id`, `sheet`, `animation`, `flip_x` (no `imageId`, no `frame`).
Expected new tables present: `comp_tile`, `comp_path`, `comp_speed`.
Expected meta: `schema_version = 2`.

- [ ] **Step 4: Commit**

```bash
git add schema.json
git commit -m "feat(epic-5/story-1): update schema.json to v2 — evolve Sprite, add Tile/Path/Speed"
```

---

## Task 5: Mark story complete

- [ ] **Step 1: Check off acceptance criteria in story file**

Open `docs/stories/epic-5/01-schema-updates.md` and tick all acceptance criteria.

- [ ] **Step 2: Commit**

```bash
git add docs/stories/epic-5/01-schema-updates.md
git commit -m "docs(epic-5/story-1): mark story 1 complete"
```

---

## Verification Checklist

- [ ] `go run ./cmd/cli schema validate` prints `schema.json OK (version 2, 6 components, 3 entity types)`
- [ ] `comp_sprite` has `sheet`, `animation`, `flip_x` (not `imageId`, `frame`)
- [ ] `comp_tile`, `comp_path`, `comp_speed` tables exist with correct columns
- [ ] `Tile` entity type declared; `Goblin` has `optionalComponents: ["Path", "Speed"]`
- [ ] `go test ./...` passes with no failures
- [ ] Story acceptance criteria checked off
