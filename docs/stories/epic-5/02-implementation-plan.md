# Epic 5, Story 2: Ebitengine Wiring + Tick Loop — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the `cmd/game` binary. Wire Ebitengine into the interpreter tick loop at 60 TPS / ~20 Hz. The window opens to a black screen; the interpreter fires every 3rd frame and logs each tick.

**Architecture:** New `internal/renderer` package owns the `ebiten.Game` implementation. `cmd/game/main.go` is a thin Cobra binary that bootstraps config → DB → behaviors → watcher → `ebiten.RunGame`. All interpreter state lives in SQLite; each tick runs in a single `BEGIN IMMEDIATE` / `COMMIT` transaction. A schema conflict between the legacy bootstrap DDL and the interpreter tables must be fixed before the two can coexist.

**Tech Stack:** Go, `github.com/hajimehoshi/ebiten/v2`, existing `internal/agent`, `internal/storage`, `internal/config`.

**Save plan to:** `docs/stories/epic-5/02-implementation-plan.md` as the first implementation step.

---

## Context

The engine has a CLI debug binary (`cmd/cli`) but no game window. This story adds `cmd/game`, which drives Ebitengine at 60 TPS and fires the interpreter tick every 3 frames (~20 Hz).

**Schema conflict (prerequisite fix):** `bootstrapDatabase` in `internal/storage/sqlite.go` creates `event_queue` (columns: `tick, target_entity, kind, payload`) and `transitions` (columns: `from_state, to_state, guard_result, actions_run`). `EnsureInterpreterTables` in `internal/storage/tables.go` creates the same two tables with incompatible schemas (`event_queue` needs `entity_id, machine_id, event_type, target_tick`; `transitions` needs `from_states, to_states, cond_result, actions_run`). Because bootstrap runs first, `EnsureInterpreterTables`'s `IF NOT EXISTS` clauses are no-ops, and any machine writer call fails at runtime. The fix is to replace the legacy DDL in `bootstrapDatabase` with the interpreter's schemas before the game binary is wired up.

**`LoadAgent`:** The tick loop must reconstruct an `*agent.Agent` from a `behavior_components` DB row (entity_id + machine_id + JSON state IDs). A new exported `agent.LoadAgent(def, entityID, stateIDs, tickDurationMs)` function encapsulates this.

**Tick sequence (one SQLite transaction per tick):**
1. Read `world.current_tick`
2. Drain due `event_queue` rows (`target_tick ≤ current_tick`), read into memory, then delete
3. For each due event: reconstruct Agent → `agent.SendEvent`
4. For each `behavior_components` row: reconstruct Agent → `agent.SendEvent` with `TICK`
5. `UPSERT world (current_tick, current_tick+1)` and `(world_version, world_version+1)`
6. `COMMIT`

**Import graph:** `internal/renderer` → `internal/agent`, `internal/storage`. No cycles.

---

## Task 1: Save this plan

- [ ] **Step 1: Copy plan to epic stories folder**

```bash
cp /var/home/tom/.claude/plans/toasty-crafting-toast.md \
   docs/stories/epic-5/02-implementation-plan.md
```

- [ ] **Step 2: Commit**

```bash
git add docs/stories/epic-5/02-implementation-plan.md
git commit -m "docs(epic-5/story-2): add implementation plan"
```

---

## Task 2: Add WindowConfig to config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `game.toml`

- [ ] **Step 1: Add WindowConfig struct and field to Config**

In `internal/config/config.go`, add after `ModConfig`:

```go
type WindowConfig struct {
	Title    string `toml:"title"`
	Width    int    `toml:"width"`
	Height   int    `toml:"height"`
	TileSize int    `toml:"tileSize"`
}
```

Add `Window WindowConfig` field to the `Config` struct:

```go
type Config struct {
	Database DatabaseConfig `toml:"database"`
	Schema   SchemaConfig   `toml:"schema"`
	Mods     []ModConfig    `toml:"mods"`
	Window   WindowConfig   `toml:"window"`
}
```

Update `Defaults()`:

```go
func Defaults() *Config {
	return &Config{
		Database: DatabaseConfig{Path: "./ecs.db"},
		Schema:   SchemaConfig{Path: "./schema.json"},
		Window: WindowConfig{
			Title:    "ECS Demo",
			Width:    640,
			Height:   480,
			TileSize: 16,
		},
	}
}
```

- [ ] **Step 2: Update game.toml**

Append to `game.toml`:

```toml
[window]
title    = "ECS Demo"
width    = 640
height   = 480
tileSize = 16
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/config/...
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go game.toml
git commit -m "feat(config): add WindowConfig for Ebitengine window dimensions"
```

---

## Task 3: Fix bootstrap/interpreter table schema conflict

**Files:**
- Modify: `internal/storage/sqlite.go` — replace legacy `event_queue` and `transitions` DDL
- Modify: `internal/storage/bootstrap_test.go` — update column assertions if any

The legacy `event_queue` and `transitions` DDL in `bootstrapDatabase` must be replaced with the interpreter schemas so `EnsureInterpreterTables` correctly finds them and both coexist.

- [ ] **Step 1: Replace legacy DDL in bootstrapDatabase**

In `internal/storage/sqlite.go`, in the `fixed` string inside `bootstrapDatabase`, replace:

```sql
CREATE TABLE event_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tick INTEGER NOT NULL,
    target_entity INTEGER,
    kind TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}'
);
```

with:

```sql
CREATE TABLE event_queue (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id   INTEGER NOT NULL,
    machine_id  TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    payload     TEXT,
    target_tick INTEGER NOT NULL
);
```

Replace the index on `event_queue`:
- Remove: `CREATE INDEX idx_event_queue_tick ON event_queue(tick);`
- Add: `CREATE INDEX idx_event_queue_target_tick ON event_queue(target_tick);`

Replace the legacy `transitions` DDL:

```sql
CREATE TABLE transitions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tick INTEGER NOT NULL,
    wall_ms INTEGER NOT NULL,
    entity_id INTEGER NOT NULL,
    machine_id TEXT NOT NULL,
    from_state TEXT NOT NULL,
    to_state TEXT NOT NULL,
    event TEXT NOT NULL,
    guard_result TEXT,
    actions_run TEXT
);
```

with:

```sql
CREATE TABLE transitions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    tick        INTEGER NOT NULL,
    wall_ms     INTEGER NOT NULL,
    entity_id   INTEGER NOT NULL,
    machine_id  TEXT NOT NULL,
    from_states TEXT NOT NULL,
    to_states   TEXT NOT NULL,
    event       TEXT NOT NULL,
    cond_result INTEGER,
    actions_run TEXT NOT NULL
);
```

- [ ] **Step 2: Run full test suite**

```bash
go test ./...
```

Expected: all pass. If `bootstrap_test.go` asserts specific column names on these tables, update those assertions to match the new schemas.

- [ ] **Step 3: Commit**

```bash
git add internal/storage/sqlite.go internal/storage/bootstrap_test.go
git commit -m "fix(storage): align bootstrap DDL with interpreter table schemas"
```

---

## Task 4: Add LoadAgent to agent package

**Files:**
- Modify: `internal/agent/agent.go`

`LoadAgent` reconstructs an `*Agent` from persisted state for use in the tick loop. The `findState` function (already in `interpreter.go`) is reused to map state ID strings back to `*StateNode` pointers.

- [ ] **Step 1: Add LoadAgent to internal/agent/agent.go**

After `StartAgent`, add:

```go
// LoadAgent reconstructs an Agent from persisted state for use in the tick loop.
// stateIDs are the currently active leaf state IDs stored in behavior_components.current_states.
// History is initialized empty — the reconciler handles invalid states on hot-reload.
func LoadAgent(def *MachineDefinition, entityID int64, stateIDs []string, tickDurationMs int64) *Agent {
	a := NewAgent(def, entityID, "", tickDurationMs)
	for _, id := range stateIDs {
		if node := findState(def.States, id); node != nil {
			a.Configuration = append(a.Configuration, node)
		}
	}
	return a
}
```

- [ ] **Step 2: Write a test**

Add to `internal/agent/agent_test.go` (or create it if it only exists as a file not imported):

```go
func TestLoadAgent_RebuildsConfiguration(t *testing.T) {
	def := &MachineDefinition{
		ID:      "test",
		Initial: "idle",
		States: map[string]*StateNode{
			"idle": {ID: "idle", Type: StateTypeAtomic},
			"run":  {ID: "run", Type: StateTypeAtomic},
		},
	}
	a := LoadAgent(def, 42, []string{"idle"}, 50)
	if len(a.Configuration) != 1 {
		t.Fatalf("want 1 state, got %d", len(a.Configuration))
	}
	if a.Configuration[0].ID != "idle" {
		t.Errorf("want state id=idle, got %q", a.Configuration[0].ID)
	}
	if a.EntityID != 42 {
		t.Errorf("want EntityID=42, got %d", a.EntityID)
	}
}

func TestLoadAgent_UnknownStateIDsSkipped(t *testing.T) {
	def := &MachineDefinition{
		ID:      "test",
		Initial: "idle",
		States:  map[string]*StateNode{"idle": {ID: "idle", Type: StateTypeAtomic}},
	}
	a := LoadAgent(def, 1, []string{"idle", "nonexistent"}, 50)
	if len(a.Configuration) != 1 {
		t.Errorf("want 1 state, got %d", len(a.Configuration))
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/agent/...
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): add LoadAgent to reconstruct Agent from persisted state"
```

---

## Task 5: Add Ebitengine dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add dependency**

```bash
go get github.com/hajimehoshi/ebiten/v2@latest
go mod tidy
```

- [ ] **Step 2: Verify build**

```bash
go build ./...
```

Expected: succeeds (no game binary yet, but packages compile).

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "feat(deps): add github.com/hajimehoshi/ebiten/v2"
```

---

## Task 6: Create internal/renderer/game.go

**Files:**
- Create: `internal/renderer/game.go`
- Create: `internal/renderer/game_test.go`

`Game` implements `ebiten.Game`. The `runTick` method executes the full interpreter sequence in a single SQLite transaction.

- [ ] **Step 1: Create internal/renderer/game.go**

```go
package renderer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"image/color"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/storage"
)

const TicksPerSecond = 20
const tickDurationMs = int64(1000 / TicksPerSecond)

// Game implements ebiten.Game, driving the interpreter at ~20 Hz.
type Game struct {
	db         *sql.DB
	loader     *agent.Loader
	registry   *agent.Registry
	frameCount int
	logicalW   int
	logicalH   int
}

func NewGame(db *sql.DB, loader *agent.Loader, registry *agent.Registry, w, h int) *Game {
	return &Game{db: db, loader: loader, registry: registry, logicalW: w, logicalH: h}
}

func (g *Game) Update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		return ebiten.Termination
	}
	g.frameCount++
	if g.frameCount%(60/TicksPerSecond) == 0 {
		if err := g.runTick(); err != nil {
			return fmt.Errorf("tick: %w", err)
		}
	}
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.Black)
}

func (g *Game) Layout(outsideW, outsideH int) (int, int) {
	return g.logicalW, g.logicalH
}

func (g *Game) runTick() error {
	tx, err := g.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Read current tick.
	var currentTick int64
	err = tx.QueryRow(`SELECT CAST(value AS INTEGER) FROM world WHERE key = 'current_tick'`).Scan(&currentTick)
	if err == sql.ErrNoRows {
		currentTick = 0
	} else if err != nil {
		return fmt.Errorf("reading current_tick: %w", err)
	}

	world := storage.NewTxWorldWriter(tx)
	reader := storage.NewTxWorldReader(tx)
	mw := storage.NewMachineWriter(tx)

	// 2. Drain due event_queue rows.
	type dueEvent struct {
		entityID  int64
		machineID string
		eventType string
	}
	dueRows, err := tx.Query(
		`SELECT entity_id, machine_id, event_type FROM event_queue WHERE target_tick <= ?`,
		currentTick,
	)
	if err != nil {
		return fmt.Errorf("querying event_queue: %w", err)
	}
	var due []dueEvent
	for dueRows.Next() {
		var e dueEvent
		if err := dueRows.Scan(&e.entityID, &e.machineID, &e.eventType); err != nil {
			dueRows.Close()
			return fmt.Errorf("scanning event_queue: %w", err)
		}
		due = append(due, e)
	}
	dueRows.Close()
	if err := dueRows.Err(); err != nil {
		return fmt.Errorf("iterating event_queue: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM event_queue WHERE target_tick <= ?`, currentTick); err != nil {
		return fmt.Errorf("deleting due events: %w", err)
	}

	// 3. Deliver due events.
	for _, e := range due {
		a, err := g.loadAgentFromDB(tx, e.entityID, e.machineID)
		if err != nil || a == nil {
			continue
		}
		if err := agent.SendEvent(a, agent.Event{Type: e.eventType}, currentTick, g.registry, world, reader, mw); err != nil {
			log.Printf("tick: deliver %q to entity %d: %v", e.eventType, e.entityID, err)
		}
	}

	// 4. Deliver TICK to all active behavior_components.
	bcRows, err := tx.Query(`SELECT entity_id, machine_id, current_states FROM behavior_components`)
	if err != nil {
		return fmt.Errorf("querying behavior_components: %w", err)
	}
	type bcRow struct {
		entityID      int64
		machineID     string
		currentStates string
	}
	var bcs []bcRow
	for bcRows.Next() {
		var r bcRow
		if err := bcRows.Scan(&r.entityID, &r.machineID, &r.currentStates); err != nil {
			bcRows.Close()
			return fmt.Errorf("scanning behavior_components: %w", err)
		}
		bcs = append(bcs, r)
	}
	bcRows.Close()
	if err := bcRows.Err(); err != nil {
		return fmt.Errorf("iterating behavior_components: %w", err)
	}

	for _, r := range bcs {
		var stateIDs []string
		if err := json.Unmarshal([]byte(r.currentStates), &stateIDs); err != nil {
			log.Printf("tick: parse states for entity %d: %v", r.entityID, err)
			continue
		}
		def, ok := g.loader.Get(r.machineID)
		if !ok {
			continue
		}
		a := agent.LoadAgent(def, r.entityID, stateIDs, tickDurationMs)
		if err := agent.SendEvent(a, agent.Event{Type: "TICK"}, currentTick, g.registry, world, reader, mw); err != nil {
			log.Printf("tick: TICK for entity %d: %v", r.entityID, err)
		}
	}

	// 5. Advance world state.
	if _, err := tx.Exec(
		`INSERT INTO world (key, value) VALUES ('current_tick', ?)
		 ON CONFLICT(key) DO UPDATE SET value = ?`,
		currentTick+1, currentTick+1,
	); err != nil {
		return fmt.Errorf("advancing current_tick: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO world (key, value) VALUES ('world_version', '1')
		 ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + 1`,
	); err != nil {
		return fmt.Errorf("advancing world_version: %w", err)
	}

	log.Printf("[tick %d] done", currentTick+1)
	return tx.Commit()
}

// loadAgentFromDB looks up current_states from behavior_components and reconstructs an Agent.
// Returns nil, nil if the machine definition is not loaded or the row doesn't exist.
func (g *Game) loadAgentFromDB(tx *sql.Tx, entityID int64, machineID string) (*agent.Agent, error) {
	def, ok := g.loader.Get(machineID)
	if !ok {
		return nil, nil
	}
	var raw string
	err := tx.QueryRow(
		`SELECT current_states FROM behavior_components WHERE entity_id = ? AND machine_id = ?`,
		entityID, machineID,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loadAgentFromDB: %w", err)
	}
	var stateIDs []string
	if err := json.Unmarshal([]byte(raw), &stateIDs); err != nil {
		return nil, fmt.Errorf("loadAgentFromDB: parse states: %w", err)
	}
	return agent.LoadAgent(def, entityID, stateIDs, tickDurationMs), nil
}
```

- [ ] **Step 2: Write a tick test**

Create `internal/renderer/game_test.go`:

```go
package renderer

import (
	"database/sql"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbSchema := schema.DatabaseSchema{SchemaVersion: 1}
	store, err := storage.NewSQLiteStore(":memory:", dbSchema, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	return store.DB()
}

func TestRunTick_AdvancesCurrentTick(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	g := NewGame(db, loader, registry, 640, 480)

	if err := g.runTick(); err != nil {
		t.Fatalf("runTick: %v", err)
	}

	var tick int64
	if err := db.QueryRow(`SELECT CAST(value AS INTEGER) FROM world WHERE key = 'current_tick'`).Scan(&tick); err != nil {
		t.Fatalf("reading current_tick: %v", err)
	}
	if tick != 1 {
		t.Errorf("current_tick = %d, want 1", tick)
	}
}

func TestRunTick_AdvancesWorldVersion(t *testing.T) {
	db := newTestDB(t)
	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, schema.DatabaseSchema{})
	g := NewGame(db, loader, registry, 640, 480)

	for i := 0; i < 3; i++ {
		if err := g.runTick(); err != nil {
			t.Fatalf("runTick %d: %v", i, err)
		}
	}

	var version int64
	if err := db.QueryRow(`SELECT CAST(value AS INTEGER) FROM world WHERE key = 'world_version'`).Scan(&version); err != nil {
		t.Fatalf("reading world_version: %v", err)
	}
	if version != 3 {
		t.Errorf("world_version = %d, want 3", version)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/renderer/...
```

Expected: both pass.

- [ ] **Step 4: Commit**

```bash
git add internal/renderer/game.go internal/renderer/game_test.go
git commit -m "feat(renderer): add Game struct implementing ebiten.Game with 20 Hz tick loop"
```

---

## Task 7: Create cmd/game/main.go

**Files:**
- Create: `cmd/game/main.go`

- [ ] **Step 1: Create cmd/game/main.go**

```go
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/spf13/cobra"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/config"
	"github.com/tmbritton/ecs-db/internal/renderer"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

var cfgPath string

var rootCmd = &cobra.Command{
	Use:   "game",
	Short: "ECS-in-SQLite game",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return config.Init(cfgPath)
	},
	RunE: runGame,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "./game.toml", "path to TOML config file")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runGame(cmd *cobra.Command, args []string) error {
	cfg := config.Get()

	schemaBytes, err := os.ReadFile(cfg.Schema.Path)
	if err != nil {
		return fmt.Errorf("loading schema: %w", err)
	}
	dbSchema, err := schema.LoadSchema(schemaBytes)
	if err != nil {
		return fmt.Errorf("parsing schema: %w", err)
	}
	if err := schema.ValidateSchema(dbSchema); err != nil {
		return fmt.Errorf("validating schema: %w", err)
	}

	hash := sha256.Sum256(schemaBytes)
	schemaHash := hex.EncodeToString(hash[:])

	store, err := storage.NewSQLiteStore(cfg.Database.Path, dbSchema, schemaHash)
	if err != nil {
		return fmt.Errorf("initializing database: %w", err)
	}
	defer func() { _ = store.Close() }()

	if err := storage.EnsureInterpreterTables(store.DB()); err != nil {
		return fmt.Errorf("ensuring interpreter tables: %w", err)
	}

	registry := builtins.NewRegistry()
	loader := agent.NewLoader(registry, dbSchema)

	var watchedDirs []agent.WatchedDir
	for _, mod := range cfg.Mods {
		if mod.Behaviors == "" {
			continue
		}
		n, err := loader.ScanDir(mod.Behaviors, mod.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: loading behaviors for mod %q: %v\n", mod.Name, err)
		}
		if n > 0 {
			fmt.Printf("Loaded %d behavior(s) from mod %q\n", n, mod.Name)
		}
		watchedDirs = append(watchedDirs, agent.WatchedDir{Path: mod.Behaviors, ModName: mod.Name})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher := agent.NewWatcher(loader, watchedDirs, 0)
	go func() {
		if err := watcher.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "watcher: %v\n", err)
		}
	}()

	ebiten.SetTPS(60)
	ebiten.SetWindowSize(cfg.Window.Width, cfg.Window.Height)
	ebiten.SetWindowTitle(cfg.Window.Title)

	game := renderer.NewGame(store.DB(), loader, registry, cfg.Window.Width, cfg.Window.Height)
	return ebiten.RunGame(game)
}
```

- [ ] **Step 2: Run full test suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 3: Build the game binary**

```bash
go build ./cmd/game
```

Expected: produces `./game` binary with no errors.

- [ ] **Step 4: Smoke-test the window**

```bash
./game
```

Expected: window opens with black screen, terminal logs `[tick N] done` at ~20 Hz, Escape or window-close quits cleanly.

- [ ] **Step 5: Commit**

```bash
git add cmd/game/main.go
git commit -m "feat(epic-5/story-2): add cmd/game entry point with Ebitengine tick loop"
```

---

## Task 8: Mark story complete

- [ ] **Step 1: Check off acceptance criteria in story file**

Open `docs/stories/epic-5/02-ebitengine-tick-loop.md` and tick all acceptance criteria checkboxes.

- [ ] **Step 2: Commit**

```bash
git add docs/stories/epic-5/02-ebitengine-tick-loop.md
git commit -m "docs(epic-5/story-2): mark story 2 complete"
```

---

## Verification Checklist

- [ ] `go test ./...` passes with no failures
- [ ] `go build ./cmd/game` succeeds
- [ ] Window opens showing a black screen
- [ ] Terminal logs `[tick N] done` at ~20 Hz
- [ ] Escape key or window close exits cleanly
- [ ] `current_tick` and `world_version` increment in the DB after running
