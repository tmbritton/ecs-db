# Story 7: Built-In Actions and Guards — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Register 9 actions and 5 guards into a concrete `*agent.Registry` via `RegisterBuiltins`, all implemented against the existing `agent.WorldWriter` / `agent.WorldReader` interfaces backed by a real SQLite transaction.

**Architecture:** Four sequential changes. (1) Extend `ActionContext` / `GuardContext` with a `Reader WorldReader` field and a `ContextManifest map[string]string` field; extend `WorldReader` with `FindEntityByType`. (2) Implement SQL-backed `agent.WorldWriter` and `agent.WorldReader` adapters in `internal/storage/world_adapter.go`. (3) Implement 9 action handlers and 5 guard handlers in `internal/agent/builtins/`. (4) Expose `RegisterBuiltins(*agent.Registry)`. Actions access component values through `ActionContext.Reader` (read) and `ActionContext.World` (write); guards use `GuardContext.World` for both. `ContextManifest` maps context key names to their owning component names, letting built-ins look up e.g. `ContextManifest["target_x"] → "GoblinBehavior"` without hard-coding component names.

**Tech Stack:** Go stdlib (`fmt`, `math`, `math/rand`, `strings`). SQLite via `modernc.org/sqlite`. Module: `github.com/tmbritton/ecs-db`.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/agent/context.go` | Modify | Add `Reader WorldReader`, `ContextManifest map[string]string` to `ActionContext`; add `ContextManifest` to `GuardContext`; add `FindEntityByType` to `WorldReader` |
| `internal/agent/registry_test.go` | Modify | Add `FindEntityByType` stub to `testWorldReader` |
| `internal/agent/agent_test.go` | Modify | Add `FindEntityByType` stub to `alwaysHasComponent` |
| `internal/agent/interpreter.go` | Modify | Populate `Reader` + `ContextManifest` in all three `ActionContext` literals; pass `contextManifest` into `evaluateTransition` |
| `internal/agent/agent.go` | Modify | Populate `Reader` + `ContextManifest` in `ActionContext` inside `StartAgent` |
| `internal/storage/world_adapter.go` | Create | `txWorldWriter` (agent.WorldWriter on `*sql.Tx`) and `txWorldReader` (agent.WorldReader on `*sql.Tx`) |
| `internal/storage/world_adapter_test.go` | Create | Integration tests for the SQL adapter |
| `internal/agent/builtins/actions.go` | Create | 9 action handler structs + `toFloat` / `resolveTarget` helpers |
| `internal/agent/builtins/guards.go` | Create | 5 guard handler structs |
| `internal/agent/builtins/register.go` | Create | `RegisterBuiltins(*agent.Registry)` |
| `internal/agent/builtins/builtins_test.go` | Create | Integration tests — all 14 handlers against in-memory SQLite |

---

## Context

`ActionContext.World` is a `WorldWriter` — write-only. Actions like `moveTowardTarget` must also read the entity's current position. Adding `Reader WorldReader` to `ActionContext` gives actions read access without collapsing the read/write separation.

`ContextManifest` maps context key names (e.g. `"speed"`, `"target_x"`) to their component names (e.g. `"GoblinBehavior"`). Builtins use it to call `SetComponentValue(entityID, manifest["target_x"], "target_x", val)` without hard-coding the component. If a key is absent from the manifest, the builtin logs a warning and returns nil.

`FindEntityByType` on `WorldReader` is the lookup mechanism for the `"$player"` sentinel used by `dealDamage`, `setPursueTarget`, and `inRange`.

The storage world adapter is intentionally schema-agnostic: it builds SQL from the component/field names passed by the caller, using `"comp_" + strings.ToLower(compName)` as the table name and `strings.ToLower(field)` as the column name. No schema reference needed.

---

## Task 1: Extend interfaces and update call sites

**Files:**
- Modify: `internal/agent/context.go`
- Modify: `internal/agent/registry_test.go`
- Modify: `internal/agent/agent_test.go`
- Modify: `internal/agent/interpreter.go`
- Modify: `internal/agent/agent.go`

- [x] **Step 1: Update `context.go`**

Replace the entire file contents with:

```go
package agent

// Event is the event that triggered a transition. Payload carries
// arbitrary JSON-decoded data from the event source.
type Event struct {
	Type    string
	Payload map[string]any
}

// WorldWriter is the write-side interface that actions use to mutate world state.
// The concrete implementation (backed by *sql.Tx) lives in internal/storage.
// Agent code never imports storage directly.
type WorldWriter interface {
	SpawnEntity(entityType string) (int64, error)
	AttachComponent(entityID int64, compName string, values map[string]any) error
	DetachComponent(entityID int64, compName string) error
	SetComponentValue(entityID int64, compName, field string, value any) error
}

// WorldReader is the read-side interface that guards (and read-capable actions) use
// to inspect world state. The concrete implementation lives in internal/storage.
type WorldReader interface {
	GetComponentValue(entityID int64, compName, field string) (any, error)
	HasComponent(entityID int64, compName string) (bool, error)
	// FindEntityByType returns the ID of the first entity of the given type.
	// Used to resolve the "$player" sentinel in dealDamage, inRange, setPursueTarget.
	FindEntityByType(entityType string) (int64, error)
}

// ActionHandler is implemented by Go code that executes a named XState action.
type ActionHandler interface {
	Run(ActionContext) error
}

// GuardHandler is implemented by Go code that evaluates a named XState guard condition.
type GuardHandler interface {
	Evaluate(GuardContext) bool
}

// ActionContext is passed to ActionHandler.Run.
type ActionContext struct {
	EntityID        int64
	Tick            int64
	World           WorldWriter
	Reader          WorldReader       // read access for actions that need current component values
	Params          map[string]any    // static params from the machine JSON action spec
	Event           Event
	ContextManifest map[string]string // context key → component name; from MachineDefinition
}

// GuardContext is passed to GuardHandler.Evaluate.
type GuardContext struct {
	EntityID        int64
	Tick            int64
	World           WorldReader
	Params          map[string]any    // static params from the machine JSON cond spec
	Event           Event
	ContextManifest map[string]string // context key → component name; from MachineDefinition
}

// TransitionRecord is the data written to the transitions table after each microstep.
type TransitionRecord struct {
	Tick       int64
	WallMs     int64
	EntityID   int64
	MachineID  string
	FromStates []string
	ToStates   []string
	Event      string
	CondResult *bool // nil = unconditional; true = guard passed; false = guard failed
	ActionsRun []string
}

// MachineWriter is the write-side interface for interpreter-owned tables
// (behavior_components, transitions, event_queue).
// The concrete implementation (backed by *sql.Tx) lives in internal/storage.
// Agent code never imports storage directly.
type MachineWriter interface {
	SetMachineState(entityID int64, machineID string, states []string, tick int64) error
	AppendTransition(rec TransitionRecord) error
	ScheduleAfterEvent(entityID int64, machineID, eventType string, targetTick int64) error
	CancelAfterEvents(entityID int64, machineID string, stateIDs []string) error
}
```

- [x] **Step 2: Run to confirm compile failure**

```
go build ./internal/agent/... 2>&1 | head -20
```

Expected: errors that `testWorldReader` and `alwaysHasComponent` do not implement `WorldReader` (missing `FindEntityByType`).

- [x] **Step 3: Add `FindEntityByType` stub to `testWorldReader` in `registry_test.go`**

Find the `testWorldReader` type at the bottom of the existing methods and add:

```go
func (r *testWorldReader) FindEntityByType(entityType string) (int64, error) {
	return 0, fmt.Errorf("testWorldReader: no entities")
}
```

Also add `"fmt"` to the import block of `registry_test.go` if it is not already present.

- [x] **Step 4: Add `FindEntityByType` stub to `alwaysHasComponent` in `agent_test.go`**

Find the `alwaysHasComponent` type and add:

```go
func (r *alwaysHasComponent) FindEntityByType(entityType string) (int64, error) {
	return 0, fmt.Errorf("alwaysHasComponent: no entities")
}
```

- [x] **Step 5: Update the three `ActionContext` literals in `interpreter.go`**

There are three calls to `runActionList` in `SendEvent`. Each constructs an `ActionContext`. Update them all to include `Reader` and `ContextManifest`:

Exit actions (around line 39):
```go
ran, err := runActionList(state.Exit, ActionContext{
    EntityID: agent.EntityID, Tick: tick, World: world, Reader: reader,
    Event: event, ContextManifest: agent.Definition.ContextManifest,
}, registry)
```

Transition actions (around line 55):
```go
ran, err := runActionList(sel.Transition.Actions, ActionContext{
    EntityID: agent.EntityID, Tick: tick, World: world, Reader: reader,
    Event: event, ContextManifest: agent.Definition.ContextManifest,
}, registry)
```

Entry actions (around line 71):
```go
ran, err := runActionList(state.Entry, ActionContext{
    EntityID: agent.EntityID, Tick: tick, World: world, Reader: reader,
    Event: event, ContextManifest: agent.Definition.ContextManifest,
}, registry)
```

- [x] **Step 6: Thread `ContextManifest` into `evaluateTransition`**

Change the signature of `evaluateTransition` to accept `contextManifest`:

```go
func evaluateTransition(t Transition, entityID, tick int64, event Event, registry *Registry, reader WorldReader, contextManifest map[string]string) (eligible bool, condResult *bool, err error) {
	if t.Cond == nil {
		return true, nil, nil
	}
	handler, ok := registry.GetGuard(t.Cond.Type)
	if !ok {
		return false, nil, nil
	}
	result := handler.Evaluate(GuardContext{
		EntityID: entityID, Tick: tick, World: reader,
		Params: t.Cond.Params, Event: event,
		ContextManifest: contextManifest,
	})
	b := result
	return result, &b, nil
}
```

Update the call site inside `selectEligibleTransitions`:

```go
eligible, condResult, err := evaluateTransition(t, agent.EntityID, tick, event, registry, reader, agent.Definition.ContextManifest)
```

- [x] **Step 7: Update `ActionContext` in `StartAgent` in `agent.go`**

Find the `runActionList` call inside the entry loop in `StartAgent` and add the two new fields:

```go
if _, err := runActionList(state.Entry, ActionContext{
    EntityID: agent.EntityID, Tick: tick, World: world, Reader: reader,
    Event: initEvent, ContextManifest: def.ContextManifest,
}, registry); err != nil {
```

- [x] **Step 8: Run all agent tests**

```
go test ./internal/agent/... -v 2>&1 | tail -20
```

Expected: all pass.

- [x] **Step 9: Full regression check**

```
go test ./...
```

Expected: all pass.

- [x] **Step 10: Commit**

```bash
git add internal/agent/context.go internal/agent/registry_test.go \
        internal/agent/agent_test.go internal/agent/interpreter.go \
        internal/agent/agent.go
git commit -m "feat(epic-3/story-7): extend ActionContext/GuardContext with Reader and ContextManifest"
```

---

## Task 2: Storage world adapter

**Files:**
- Create: `internal/storage/world_adapter.go`
- Create: `internal/storage/world_adapter_test.go`

The adapter is schema-agnostic: table names are `comp_` + lowercase component name; column names are lowercase field names. No schema reference needed.

- [x] **Step 1: Write failing `world_adapter_test.go`**

```go
package storage_test

import (
	"database/sql"
	"testing"

	"github.com/tmbritton/ecs-db/internal/storage"
	_ "modernc.org/sqlite"
)

func setupAdapterDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	stmts := []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL NOT NULL DEFAULT 0, y REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_health   (entity_id INTEGER PRIMARY KEY, hp REAL NOT NULL DEFAULT 100)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return db
}

func beginAdapterTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

// ── txWorldWriter ─────────────────────────────────────────────────────────────

func TestTxWorldWriter_SpawnEntity(t *testing.T) {
	db := setupAdapterDB(t)
	tx := beginAdapterTx(t, db)
	w := storage.NewTxWorldWriter(tx)

	id, err := w.SpawnEntity("Goblin")
	if err != nil {
		t.Fatalf("SpawnEntity: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var entityType string
	_ = db.QueryRow("SELECT entity_type FROM entities WHERE id = ?", id).Scan(&entityType)
	if entityType != "Goblin" {
		t.Errorf("entity_type = %q, want Goblin", entityType)
	}
}

func TestTxWorldWriter_AttachComponent(t *testing.T) {
	db := setupAdapterDB(t)
	tx := beginAdapterTx(t, db)
	w := storage.NewTxWorldWriter(tx)

	_, _ = tx.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Goblin', 0)")
	if err := w.AttachComponent(1, "Position", map[string]any{"x": 3.0, "y": 4.0}); err != nil {
		t.Fatalf("AttachComponent: %v", err)
	}
	_ = tx.Commit()

	var x, y float64
	_ = db.QueryRow("SELECT x, y FROM comp_position WHERE entity_id = 1").Scan(&x, &y)
	if x != 3.0 || y != 4.0 {
		t.Errorf("position = (%v, %v), want (3, 4)", x, y)
	}
}

func TestTxWorldWriter_DetachComponent(t *testing.T) {
	db := setupAdapterDB(t)
	_, _ = db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Goblin', 0)")
	_, _ = db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (1, 0, 0)")

	tx := beginAdapterTx(t, db)
	w := storage.NewTxWorldWriter(tx)
	if err := w.DetachComponent(1, "Position"); err != nil {
		t.Fatalf("DetachComponent: %v", err)
	}
	_ = tx.Commit()

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM comp_position WHERE entity_id = 1").Scan(&count)
	if count != 0 {
		t.Errorf("comp_position rows = %d after detach, want 0", count)
	}
}

func TestTxWorldWriter_SetComponentValue(t *testing.T) {
	db := setupAdapterDB(t)
	_, _ = db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Goblin', 0)")
	_, _ = db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (1, 100)")

	tx := beginAdapterTx(t, db)
	w := storage.NewTxWorldWriter(tx)
	if err := w.SetComponentValue(1, "Health", "hp", 75.0); err != nil {
		t.Fatalf("SetComponentValue: %v", err)
	}
	_ = tx.Commit()

	var hp float64
	_ = db.QueryRow("SELECT hp FROM comp_health WHERE entity_id = 1").Scan(&hp)
	if hp != 75.0 {
		t.Errorf("hp = %v, want 75", hp)
	}
}

// ── txWorldReader ─────────────────────────────────────────────────────────────

func TestTxWorldReader_GetComponentValue(t *testing.T) {
	db := setupAdapterDB(t)
	_, _ = db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (1, 42)")

	tx := beginAdapterTx(t, db)
	r := storage.NewTxWorldReader(tx)
	val, err := r.GetComponentValue(1, "Health", "hp")
	if err != nil {
		t.Fatalf("GetComponentValue: %v", err)
	}
	if val == nil {
		t.Fatal("GetComponentValue returned nil")
	}
	switch v := val.(type) {
	case float64:
		if v != 42 {
			t.Errorf("hp = %v, want 42", v)
		}
	case int64:
		if v != 42 {
			t.Errorf("hp = %v, want 42", v)
		}
	default:
		t.Errorf("unexpected type %T", val)
	}
}

func TestTxWorldReader_GetComponentValue_Missing(t *testing.T) {
	db := setupAdapterDB(t)
	tx := beginAdapterTx(t, db)
	r := storage.NewTxWorldReader(tx)
	val, err := r.GetComponentValue(99, "Health", "hp")
	if err != nil {
		t.Fatalf("GetComponentValue on missing row: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil for missing entity, got %v", val)
	}
}

func TestTxWorldReader_HasComponent(t *testing.T) {
	db := setupAdapterDB(t)
	_, _ = db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (1, 100)")

	tx := beginAdapterTx(t, db)
	r := storage.NewTxWorldReader(tx)

	has, err := r.HasComponent(1, "Health")
	if err != nil {
		t.Fatalf("HasComponent: %v", err)
	}
	if !has {
		t.Error("HasComponent = false, want true")
	}

	has2, _ := r.HasComponent(99, "Health")
	if has2 {
		t.Error("HasComponent(missing) = true, want false")
	}
}

func TestTxWorldReader_FindEntityByType(t *testing.T) {
	db := setupAdapterDB(t)
	res, _ := db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Player', 0)")
	wantID, _ := res.LastInsertId()

	tx := beginAdapterTx(t, db)
	r := storage.NewTxWorldReader(tx)

	id, err := r.FindEntityByType("Player")
	if err != nil {
		t.Fatalf("FindEntityByType: %v", err)
	}
	if id != wantID {
		t.Errorf("id = %d, want %d", id, wantID)
	}
}

func TestTxWorldReader_FindEntityByType_Missing(t *testing.T) {
	db := setupAdapterDB(t)
	tx := beginAdapterTx(t, db)
	r := storage.NewTxWorldReader(tx)

	_, err := r.FindEntityByType("Dragon")
	if err == nil {
		t.Error("expected error for missing entity type, got nil")
	}
}
```

- [x] **Step 2: Run to confirm compile failure**

```
go test ./internal/storage/... -run "TestTxWorldWriter|TestTxWorldReader" -v 2>&1 | head -10
```

Expected: `undefined: storage.NewTxWorldWriter`, `undefined: storage.NewTxWorldReader`.

- [x] **Step 3: Create `world_adapter.go`**

```go
package storage

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// txWorldWriter implements agent.WorldWriter using a live *sql.Tx.
// Table names are "comp_" + lowercase(compName); column names are lowercase(field).
// No schema reference is required — callers supply valid component and field names.
type txWorldWriter struct{ tx *sql.Tx }

// NewTxWorldWriter wraps tx to produce an agent.WorldWriter.
func NewTxWorldWriter(tx *sql.Tx) agent.WorldWriter { return &txWorldWriter{tx: tx} }

func (w *txWorldWriter) SpawnEntity(entityType string) (int64, error) {
	res, err := w.tx.Exec(
		"INSERT INTO entities (entity_type, created_tick) VALUES (?, 0)", entityType,
	)
	if err != nil {
		return 0, fmt.Errorf("SpawnEntity: %w", err)
	}
	return res.LastInsertId()
}

func (w *txWorldWriter) AttachComponent(entityID int64, compName string, values map[string]any) error {
	table := "comp_" + strings.ToLower(compName)
	cols := []string{"entity_id"}
	args := []any{entityID}
	for col, val := range values {
		cols = append(cols, strings.ToLower(col))
		args = append(args, val)
	}
	ph := make([]string, len(cols))
	for i := range ph {
		ph[i] = "?"
	}
	_, err := w.tx.Exec(
		fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ","), strings.Join(ph, ",")),
		args...,
	)
	if err != nil {
		return fmt.Errorf("AttachComponent %q: %w", compName, err)
	}
	return nil
}

func (w *txWorldWriter) DetachComponent(entityID int64, compName string) error {
	table := "comp_" + strings.ToLower(compName)
	_, err := w.tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE entity_id = ?", table), entityID)
	if err != nil {
		return fmt.Errorf("DetachComponent %q: %w", compName, err)
	}
	return nil
}

func (w *txWorldWriter) SetComponentValue(entityID int64, compName, field string, value any) error {
	table := "comp_" + strings.ToLower(compName)
	col := strings.ToLower(field)
	_, err := w.tx.Exec(
		fmt.Sprintf("UPDATE %s SET %s = ? WHERE entity_id = ?", table, col),
		value, entityID,
	)
	if err != nil {
		return fmt.Errorf("SetComponentValue %q.%q: %w", compName, field, err)
	}
	return nil
}

// txWorldReader implements agent.WorldReader using a live *sql.Tx.
// Reads within the same transaction see uncommitted writes from the same tx.
type txWorldReader struct{ tx *sql.Tx }

// NewTxWorldReader wraps tx to produce an agent.WorldReader.
func NewTxWorldReader(tx *sql.Tx) agent.WorldReader { return &txWorldReader{tx: tx} }

func (r *txWorldReader) GetComponentValue(entityID int64, compName, field string) (any, error) {
	table := "comp_" + strings.ToLower(compName)
	col := strings.ToLower(field)
	var val any
	err := r.tx.QueryRow(
		fmt.Sprintf("SELECT %s FROM %s WHERE entity_id = ?", col, table), entityID,
	).Scan(&val)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetComponentValue %q.%q: %w", compName, field, err)
	}
	return val, nil
}

func (r *txWorldReader) HasComponent(entityID int64, compName string) (bool, error) {
	table := "comp_" + strings.ToLower(compName)
	var n int
	err := r.tx.QueryRow(
		fmt.Sprintf("SELECT 1 FROM %s WHERE entity_id = ? LIMIT 1", table), entityID,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("HasComponent %q: %w", compName, err)
	}
	return true, nil
}

func (r *txWorldReader) FindEntityByType(entityType string) (int64, error) {
	var id int64
	err := r.tx.QueryRow(
		"SELECT id FROM entities WHERE entity_type = ? LIMIT 1", entityType,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("no entity of type %q", entityType)
	}
	if err != nil {
		return 0, fmt.Errorf("FindEntityByType %q: %w", entityType, err)
	}
	return id, nil
}

// Compile-time interface checks.
var (
	_ agent.WorldWriter = (*txWorldWriter)(nil)
	_ agent.WorldReader = (*txWorldReader)(nil)
)
```

- [x] **Step 4: Run tests**

```
go test ./internal/storage/... -run "TestTxWorldWriter|TestTxWorldReader" -v
```

Expected: all pass.

- [x] **Step 5: Full regression check**

```
go test ./...
```

Expected: all pass.

- [x] **Step 6: Commit**

```bash
git add internal/storage/world_adapter.go internal/storage/world_adapter_test.go
git commit -m "feat(epic-3/story-7): add SQL-backed WorldWriter/WorldReader adapter"
```

---

## Task 3: Built-in actions

**Files:**
- Create: `internal/agent/builtins/builtins_test.go` (action tests only for now)
- Create: `internal/agent/builtins/actions.go`

- [x] **Step 1: Write failing `builtins_test.go` — setup helpers and action tests**

```go
package builtins_test

import (
	"context"
	"database/sql"
	"math"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/agent/builtins"
	"github.com/tmbritton/ecs-db/internal/storage"
	_ "modernc.org/sqlite"
)

// ── Test DB setup ─────────────────────────────────────────────────────────────

func setupBuiltinsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_position    (entity_id INTEGER PRIMARY KEY, x REAL NOT NULL DEFAULT 0, y REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_health      (entity_id INTEGER PRIMARY KEY, hp REAL NOT NULL DEFAULT 100, maxhp REAL NOT NULL DEFAULT 100)`,
		`CREATE TABLE comp_goblinstats (entity_id INTEGER PRIMARY KEY, speed REAL NOT NULL DEFAULT 2, aggrorange REAL NOT NULL DEFAULT 80, target_x REAL NOT NULL DEFAULT 0, target_y REAL NOT NULL DEFAULT 0, patience REAL NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return db
}

func insertEntity(t *testing.T, db *sql.DB, entityType string) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES (?, 0)", entityType)
	if err != nil {
		t.Fatalf("insertEntity: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// goblinManifest maps the wandering_goblin context keys to GoblinStats component.
var goblinManifest = map[string]string{
	"speed":      "GoblinStats",
	"aggrorange": "GoblinStats",
	"target_x":   "GoblinStats",
	"target_y":   "GoblinStats",
	"patience":   "GoblinStats",
}

// runAction creates a tx, runs fn(writer, reader), commits, then runs check(db).
func runAction(t *testing.T, db *sql.DB, fn func(w agent.WorldWriter, r agent.WorldReader)) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	fn(storage.NewTxWorldWriter(tx), storage.NewTxWorldReader(tx))
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func actx(entityID int64, w agent.WorldWriter, r agent.WorldReader, params map[string]any) agent.ActionContext {
	return agent.ActionContext{
		EntityID:        entityID,
		Tick:            1,
		World:           w,
		Reader:          r,
		Params:          params,
		Event:           agent.Event{Type: "TICK"},
		ContextManifest: goblinManifest,
	}
}

// ── Action tests ──────────────────────────────────────────────────────────────

func TestAction_setTimer(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_goblinstats (entity_id) VALUES (?)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"key": "patience", "ticks": float64(40)})
		handler, _ := r.GetAction("setTimer")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("setTimer: %v", err)
		}
	})

	var patience float64
	db.QueryRow("SELECT patience FROM comp_goblinstats WHERE entity_id = ?", entityID).Scan(&patience)
	if patience != 40 {
		t.Errorf("patience = %v, want 40", patience)
	}
}

func TestAction_moveTowardTarget_MovesStep(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)                              VALUES (?, 0, 0)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, speed, target_x, target_y)         VALUES (?, 2, 10, 0)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, nil)
		handler, _ := r.GetAction("moveTowardTarget")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("moveTowardTarget: %v", err)
		}
	})

	var x, y float64
	db.QueryRow("SELECT x, y FROM comp_position WHERE entity_id = ?", entityID).Scan(&x, &y)
	// direction is (10,0), speed=2 → new position (2, 0)
	if math.Abs(x-2.0) > 0.001 || math.Abs(y) > 0.001 {
		t.Errorf("position = (%v, %v), want (2, 0)", x, y)
	}
}

func TestAction_moveTowardTarget_SpeedMult(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)                              VALUES (?, 0, 0)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, speed, target_x, target_y)         VALUES (?, 2, 10, 0)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"speed_mult": float64(1.5)})
		handler, _ := r.GetAction("moveTowardTarget")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("moveTowardTarget: %v", err)
		}
	})

	var x float64
	db.QueryRow("SELECT x FROM comp_position WHERE entity_id = ?", entityID).Scan(&x)
	// speed=2 * mult=1.5 = 3; dist=10 > 3 → x=3
	if math.Abs(x-3.0) > 0.001 {
		t.Errorf("x = %v, want 3.0 (speed_mult=1.5)", x)
	}
}

func TestAction_moveTowardTarget_AlreadyAtTarget(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)                              VALUES (?, 5, 5)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, speed, target_x, target_y)         VALUES (?, 2, 5, 5)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, nil)
		handler, _ := r.GetAction("moveTowardTarget")
		_ = handler.Run(ctx)
	})

	var x, y float64
	db.QueryRow("SELECT x, y FROM comp_position WHERE entity_id = ?", entityID).Scan(&x, &y)
	// position unchanged when dist < epsilon
	if math.Abs(x-5.0) > 0.001 || math.Abs(y-5.0) > 0.001 {
		t.Errorf("position = (%v, %v), expected no movement at target", x, y)
	}
}

func TestAction_pickRandomTarget_WithinRadius(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)                              VALUES (?, 50, 50)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, target_x, target_y)                VALUES (?, 0, 0)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"radius": float64(100)})
		handler, _ := r.GetAction("pickRandomTarget")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("pickRandomTarget: %v", err)
		}
	})

	var tx, ty float64
	db.QueryRow("SELECT target_x, target_y FROM comp_goblinstats WHERE entity_id = ?", entityID).Scan(&tx, &ty)
	dx, dy := tx-50, ty-50
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist > 100+0.001 {
		t.Errorf("target at distance %v > radius 100", dist)
	}
}

func TestAction_setPursueTarget(t *testing.T) {
	db := setupBuiltinsDB(t)
	playerID := insertEntity(t, db, "Player")
	goblinID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position    (entity_id, x, y) VALUES (?, 77, 88)", playerID)
	db.Exec("INSERT INTO comp_position    (entity_id, x, y) VALUES (?, 0, 0)", goblinID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, target_x, target_y) VALUES (?, 0, 0)", goblinID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(goblinID, w, rd, nil)
		handler, _ := r.GetAction("setPursueTarget")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("setPursueTarget: %v", err)
		}
	})

	var tx, ty float64
	db.QueryRow("SELECT target_x, target_y FROM comp_goblinstats WHERE entity_id = ?", goblinID).Scan(&tx, &ty)
	if math.Abs(tx-77) > 0.001 || math.Abs(ty-88) > 0.001 {
		t.Errorf("target = (%v, %v), want (77, 88)", tx, ty)
	}
}

func TestAction_dealDamage_DirectEntityID(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	targetID := insertEntity(t, db, "Player")
	db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (?, 100)", targetID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"amount": float64(15), "target": float64(targetID)})
		handler, _ := r.GetAction("dealDamage")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("dealDamage: %v", err)
		}
	})

	var hp float64
	db.QueryRow("SELECT hp FROM comp_health WHERE entity_id = ?", targetID).Scan(&hp)
	if math.Abs(hp-85) > 0.001 {
		t.Errorf("hp = %v, want 85", hp)
	}
}

func TestAction_dealDamage_PlayerSentinel(t *testing.T) {
	db := setupBuiltinsDB(t)
	goblinID := insertEntity(t, db, "Goblin")
	playerID := insertEntity(t, db, "Player")
	db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (?, 100)", playerID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(goblinID, w, rd, map[string]any{"amount": float64(5), "target": "$player"})
		handler, _ := r.GetAction("dealDamage")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("dealDamage $player: %v", err)
		}
	})

	var hp float64
	db.QueryRow("SELECT hp FROM comp_health WHERE entity_id = ?", playerID).Scan(&hp)
	if math.Abs(hp-95) > 0.001 {
		t.Errorf("hp = %v, want 95", hp)
	}
}

func TestAction_spawnEntity(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"entity_type": "Player"})
		handler, _ := r.GetAction("spawnEntity")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("spawnEntity: %v", err)
		}
	})

	var count int
	db.QueryRow("SELECT COUNT(*) FROM entities WHERE entity_type = 'Player'").Scan(&count)
	if count != 1 {
		t.Errorf("Player entities = %d, want 1", count)
	}
}

func TestAction_attachComponent(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{
			"component": "Position",
			"data":      map[string]any{"x": 1.0, "y": 2.0},
		})
		handler, _ := r.GetAction("attachComponent")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("attachComponent: %v", err)
		}
	})

	var x float64
	db.QueryRow("SELECT x FROM comp_position WHERE entity_id = ?", entityID).Scan(&x)
	if math.Abs(x-1.0) > 0.001 {
		t.Errorf("x = %v, want 1.0", x)
	}
}

func TestAction_detachComponent(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 0, 0)", entityID)

	r := builtins.NewRegistry()
	runAction(t, db, func(w agent.WorldWriter, rd agent.WorldReader) {
		ctx := actx(entityID, w, rd, map[string]any{"component": "Position"})
		handler, _ := r.GetAction("detachComponent")
		if err := handler.Run(ctx); err != nil {
			t.Fatalf("detachComponent: %v", err)
		}
	})

	var count int
	db.QueryRow("SELECT COUNT(*) FROM comp_position WHERE entity_id = ?", entityID).Scan(&count)
	if count != 0 {
		t.Errorf("comp_position rows = %d after detach, want 0", count)
	}
}

func TestAction_log(t *testing.T) {
	r := builtins.NewRegistry()
	handler, ok := r.GetAction("log")
	if !ok {
		t.Fatal("log action not registered")
	}
	ctx := agent.ActionContext{Params: map[string]any{"message": "hello world"}}
	if err := handler.Run(ctx); err != nil {
		t.Errorf("log: %v", err)
	}
}
```

- [x] **Step 2: Run to confirm compile failure**

```
go test ./internal/agent/builtins/... -v 2>&1 | head -10
```

Expected: `cannot find package "github.com/tmbritton/ecs-db/internal/agent/builtins"`.

- [x] **Step 3: Create `internal/agent/builtins/actions.go`**

```go
package builtins

import (
	"fmt"
	"math"
	"math/rand"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// toFloat coerces any SQLite-returned or JSON-decoded numeric value to float64.
func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

// resolveTargetID resolves a "$player" sentinel or numeric entity ID param.
// Returns (id, true) on success; (0, false) on failure (logs nothing — caller decides).
func resolveTargetID(reader agent.WorldReader, targetParam any) (int64, bool) {
	switch v := targetParam.(type) {
	case string:
		if v == "$player" {
			id, err := reader.FindEntityByType("Player")
			if err != nil {
				return 0, false
			}
			return id, true
		}
	case float64:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}

// manifestComp returns the component name for a context key, or "" if absent.
func manifestComp(ctx agent.ActionContext, key string) string {
	if ctx.ContextManifest == nil {
		return ""
	}
	return ctx.ContextManifest[key]
}

// ── setTimer ──────────────────────────────────────────────────────────────────

type setTimerAction struct{}

func (a *setTimerAction) Run(ctx agent.ActionContext) error {
	key, _ := ctx.Params["key"].(string)
	if key == "" {
		return nil
	}
	comp := manifestComp(ctx, key)
	if comp == "" {
		fmt.Printf("[agent] setTimer: key %q not in ContextManifest\n", key)
		return nil
	}
	ticks := int64(toFloat(ctx.Params["ticks"]))
	return ctx.World.SetComponentValue(ctx.EntityID, comp, key, ticks)
}

// ── moveTowardTarget ──────────────────────────────────────────────────────────

type moveTowardTargetAction struct{}

func (a *moveTowardTargetAction) Run(ctx agent.ActionContext) error {
	if ctx.Reader == nil {
		return nil
	}
	px, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Position", "x")
	py, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Position", "y")

	txComp := manifestComp(ctx, "target_x")
	tyComp := manifestComp(ctx, "target_y")
	speedComp := manifestComp(ctx, "speed")
	if txComp == "" || tyComp == "" || speedComp == "" {
		return nil
	}

	tx, _ := ctx.Reader.GetComponentValue(ctx.EntityID, txComp, "target_x")
	ty, _ := ctx.Reader.GetComponentValue(ctx.EntityID, tyComp, "target_y")
	speedVal, _ := ctx.Reader.GetComponentValue(ctx.EntityID, speedComp, "speed")

	speed := toFloat(speedVal)
	if mult, ok := ctx.Params["speed_mult"]; ok {
		speed *= toFloat(mult)
	}

	dx := toFloat(tx) - toFloat(px)
	dy := toFloat(ty) - toFloat(py)
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist < 0.001 {
		return nil
	}
	step := math.Min(speed, dist)
	newX := toFloat(px) + (dx/dist)*step
	newY := toFloat(py) + (dy/dist)*step

	if err := ctx.World.SetComponentValue(ctx.EntityID, "Position", "x", newX); err != nil {
		return err
	}
	return ctx.World.SetComponentValue(ctx.EntityID, "Position", "y", newY)
}

// ── pickRandomTarget ──────────────────────────────────────────────────────────

type pickRandomTargetAction struct{}

func (a *pickRandomTargetAction) Run(ctx agent.ActionContext) error {
	if ctx.Reader == nil {
		return nil
	}
	txComp := manifestComp(ctx, "target_x")
	tyComp := manifestComp(ctx, "target_y")
	if txComp == "" || tyComp == "" {
		return nil
	}

	px, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Position", "x")
	py, _ := ctx.Reader.GetComponentValue(ctx.EntityID, "Position", "y")
	radius := toFloat(ctx.Params["radius"])

	angle := rand.Float64() * 2 * math.Pi
	dist := rand.Float64() * radius
	newTX := toFloat(px) + dist*math.Cos(angle)
	newTY := toFloat(py) + dist*math.Sin(angle)

	if err := ctx.World.SetComponentValue(ctx.EntityID, txComp, "target_x", newTX); err != nil {
		return err
	}
	return ctx.World.SetComponentValue(ctx.EntityID, tyComp, "target_y", newTY)
}

// ── setPursueTarget ───────────────────────────────────────────────────────────

type setPursueTargetAction struct{}

func (a *setPursueTargetAction) Run(ctx agent.ActionContext) error {
	if ctx.Reader == nil {
		return nil
	}
	txComp := manifestComp(ctx, "target_x")
	tyComp := manifestComp(ctx, "target_y")
	if txComp == "" || tyComp == "" {
		return nil
	}

	playerID, ok := resolveTargetID(ctx.Reader, "$player")
	if !ok {
		return nil
	}
	playerX, _ := ctx.Reader.GetComponentValue(playerID, "Position", "x")
	playerY, _ := ctx.Reader.GetComponentValue(playerID, "Position", "y")

	if err := ctx.World.SetComponentValue(ctx.EntityID, txComp, "target_x", toFloat(playerX)); err != nil {
		return err
	}
	return ctx.World.SetComponentValue(ctx.EntityID, tyComp, "target_y", toFloat(playerY))
}

// ── dealDamage ────────────────────────────────────────────────────────────────

type dealDamageAction struct{}

func (a *dealDamageAction) Run(ctx agent.ActionContext) error {
	if ctx.Reader == nil {
		return nil
	}
	amount := toFloat(ctx.Params["amount"])
	targetID, ok := resolveTargetID(ctx.Reader, ctx.Params["target"])
	if !ok {
		return nil
	}
	hp, _ := ctx.Reader.GetComponentValue(targetID, "Health", "hp")
	return ctx.World.SetComponentValue(targetID, "Health", "hp", toFloat(hp)-amount)
}

// ── spawnEntity ───────────────────────────────────────────────────────────────

type spawnEntityAction struct{}

func (a *spawnEntityAction) Run(ctx agent.ActionContext) error {
	entityType, _ := ctx.Params["entity_type"].(string)
	if entityType == "" {
		return nil
	}
	id, err := ctx.World.SpawnEntity(entityType)
	if err != nil {
		return err
	}
	fmt.Printf("[agent] spawnEntity: created entity %d of type %q\n", id, entityType)
	return nil
}

// ── attachComponent ───────────────────────────────────────────────────────────

type attachComponentAction struct{}

func (a *attachComponentAction) Run(ctx agent.ActionContext) error {
	compName, _ := ctx.Params["component"].(string)
	if compName == "" {
		return nil
	}
	var data map[string]any
	if d, ok := ctx.Params["data"]; ok {
		data, _ = d.(map[string]any)
	}
	if data == nil {
		data = map[string]any{}
	}
	return ctx.World.AttachComponent(ctx.EntityID, compName, data)
}

// ── detachComponent ───────────────────────────────────────────────────────────

type detachComponentAction struct{}

func (a *detachComponentAction) Run(ctx agent.ActionContext) error {
	compName, _ := ctx.Params["component"].(string)
	if compName == "" {
		return nil
	}
	return ctx.World.DetachComponent(ctx.EntityID, compName)
}

// ── log ───────────────────────────────────────────────────────────────────────

type logAction struct{}

func (a *logAction) Run(ctx agent.ActionContext) error {
	msg := fmt.Sprintf("%v", ctx.Params["message"])
	fmt.Printf("[agent log] %s\n", msg)
	return nil
}
```

Also create `internal/agent/builtins/register.go` with just enough to satisfy the test's `builtins.NewRegistry()`:

```go
package builtins

import "github.com/tmbritton/ecs-db/internal/agent"

// NewRegistry returns a registry pre-populated with all built-in actions and guards.
// Call this instead of agent.NewRegistry() when you want the full standard library.
func NewRegistry() *agent.Registry {
	r := agent.NewRegistry()
	RegisterBuiltins(r)
	return r
}

// RegisterBuiltins registers all built-in actions and guards into r.
// Call once at interpreter startup before loading machine definitions.
func RegisterBuiltins(r *agent.Registry) {
	registerActions(r)
	registerGuards(r)
}

func registerActions(r *agent.Registry) {
	r.RegisterAction(agent.ActionMeta{
		Name:        "setTimer",
		Description: "Write a tick count to a named timer field in the entity's context component.",
		Params: []agent.ParamSchema{
			{Name: "key", Type: "string", Required: true},
			{Name: "ticks", Type: "number", Required: true},
		},
	}, &setTimerAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "moveTowardTarget",
		Description: "Move entity one step toward target_x/target_y at speed (× speed_mult if provided).",
		Params: []agent.ParamSchema{
			{Name: "speed_mult", Type: "number", Required: false, Default: 1.0},
		},
	}, &moveTowardTargetAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "pickRandomTarget",
		Description: "Pick a random position within radius of entity's position and write to target_x/target_y.",
		Params: []agent.ParamSchema{
			{Name: "radius", Type: "number", Required: true},
		},
	}, &pickRandomTargetAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "setPursueTarget",
		Description: "Copy the Player entity's position into entity's target_x/target_y.",
	}, &setPursueTargetAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "dealDamage",
		Description: "Decrement Health.hp on target entity by amount. Target is an entity ID or \"$player\".",
		Params: []agent.ParamSchema{
			{Name: "amount", Type: "number", Required: true},
			{Name: "target", Type: "string", Required: false, Default: "$player"},
		},
	}, &dealDamageAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "spawnEntity",
		Description: "Create a new entity of the given type; logs the new entity ID.",
		Params: []agent.ParamSchema{
			{Name: "entity_type", Type: "string", Required: true},
		},
	}, &spawnEntityAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "attachComponent",
		Description: "Attach a component to the current entity with optional initial field values.",
		Params: []agent.ParamSchema{
			{Name: "component", Type: "string", Required: true},
			{Name: "data", Type: "object", Required: false},
		},
	}, &attachComponentAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "detachComponent",
		Description: "Detach a component from the current entity.",
		Params: []agent.ParamSchema{
			{Name: "component", Type: "string", Required: true},
		},
	}, &detachComponentAction{})

	r.RegisterAction(agent.ActionMeta{
		Name:        "log",
		Description: "Print params.message to the interpreter log. No database effect.",
		Params: []agent.ParamSchema{
			{Name: "message", Type: "string", Required: true},
		},
	}, &logAction{})
}

func registerGuards(r *agent.Registry) {
	// Guards are registered in Task 5 once guards.go is implemented.
}
```

- [x] **Step 4: Run action tests**

```
go test ./internal/agent/builtins/... -run "TestAction_" -v
```

Expected: all 11 action tests pass.

- [x] **Step 5: Full regression check**

```
go test ./...
```

Expected: all pass.

- [x] **Step 6: Commit**

```bash
git add internal/agent/builtins/actions.go internal/agent/builtins/register.go \
        internal/agent/builtins/builtins_test.go
git commit -m "feat(epic-3/story-7): implement built-in action handlers"
```

---

## Task 4: Built-in guards

**Files:**
- Modify: `internal/agent/builtins/builtins_test.go` (add guard tests)
- Create: `internal/agent/builtins/guards.go`

- [x] **Step 1: Add guard tests to `builtins_test.go`**

Append the following after the action tests (at the end of the file):

```go
// ── Guard helpers ─────────────────────────────────────────────────────────────

func gctx(entityID int64, reader agent.WorldReader, params map[string]any) agent.GuardContext {
	return agent.GuardContext{
		EntityID:        entityID,
		Tick:            1,
		World:           reader,
		Params:          params,
		Event:           agent.Event{Type: "TICK"},
		ContextManifest: goblinManifest,
	}
}

func readGuard(t *testing.T, db *sql.DB, fn func(r agent.WorldReader) bool) bool {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()
	return fn(storage.NewTxWorldReader(tx))
}

// ── Guard tests ───────────────────────────────────────────────────────────────

func TestGuard_timerExpired_True(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_goblinstats (entity_id, patience) VALUES (?, 0)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("timerExpired")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"key": "patience"}))
	})
	if !got {
		t.Error("timerExpired(patience=0) = false, want true")
	}
}

func TestGuard_timerExpired_False(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_goblinstats (entity_id, patience) VALUES (?, 10)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("timerExpired")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"key": "patience"}))
	})
	if got {
		t.Error("timerExpired(patience=10) = true, want false")
	}
}

func TestGuard_atTarget_True(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	// Position (5, 5), target (5.5, 5) → distance = 0.5 ≤ 1.0
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)            VALUES (?, 5, 5)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, target_x, target_y) VALUES (?, 5.5, 5)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("atTarget")
		return handler.Evaluate(gctx(entityID, rd, nil))
	})
	if !got {
		t.Error("atTarget at distance 0.5 = false, want true")
	}
}

func TestGuard_atTarget_False(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	// Position (0, 0), target (10, 0) → distance = 10 > 1.0
	db.Exec("INSERT INTO comp_position    (entity_id, x, y)            VALUES (?, 0, 0)", entityID)
	db.Exec("INSERT INTO comp_goblinstats (entity_id, target_x, target_y) VALUES (?, 10, 0)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("atTarget")
		return handler.Evaluate(gctx(entityID, rd, nil))
	})
	if got {
		t.Error("atTarget at distance 10 = true, want false")
	}
}

func TestGuard_inRange_True(t *testing.T) {
	db := setupBuiltinsDB(t)
	goblinID := insertEntity(t, db, "Goblin")
	playerID := insertEntity(t, db, "Player")
	// Goblin at (0,0), Player at (5,0) → dist=5 ≤ 10
	db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 0, 0)", goblinID)
	db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 5, 0)", playerID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		ctx := agent.GuardContext{
			EntityID: goblinID, World: rd,
			Params:          map[string]any{"target": "$player", "distance": float64(10)},
			ContextManifest: goblinManifest,
		}
		handler, _ := r.GetGuard("inRange")
		return handler.Evaluate(ctx)
	})
	if !got {
		t.Error("inRange(dist=5, range=10) = false, want true")
	}
}

func TestGuard_inRange_False(t *testing.T) {
	db := setupBuiltinsDB(t)
	goblinID := insertEntity(t, db, "Goblin")
	playerID := insertEntity(t, db, "Player")
	// Goblin at (0,0), Player at (20,0) → dist=20 > 10
	db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 0, 0)", goblinID)
	db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (?, 20, 0)", playerID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		ctx := agent.GuardContext{
			EntityID: goblinID, World: rd,
			Params:          map[string]any{"target": "$player", "distance": float64(10)},
			ContextManifest: goblinManifest,
		}
		handler, _ := r.GetGuard("inRange")
		return handler.Evaluate(ctx)
	})
	if got {
		t.Error("inRange(dist=20, range=10) = true, want false")
	}
}

func TestGuard_hasComponent_True(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (?, 100)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("hasComponent")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"component": "Health"}))
	})
	if !got {
		t.Error("hasComponent(Health) = false, want true")
	}
}

func TestGuard_hasComponent_False(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("hasComponent")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"component": "Health"}))
	})
	if got {
		t.Error("hasComponent(Health) on entity without Health = true, want false")
	}
}

func TestGuard_healthAbove_True(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (?, 80)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("healthAbove")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"threshold": float64(50)}))
	})
	if !got {
		t.Error("healthAbove(hp=80, threshold=50) = false, want true")
	}
}

func TestGuard_healthAbove_False(t *testing.T) {
	db := setupBuiltinsDB(t)
	entityID := insertEntity(t, db, "Goblin")
	db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (?, 30)", entityID)

	r := builtins.NewRegistry()
	got := readGuard(t, db, func(rd agent.WorldReader) bool {
		handler, _ := r.GetGuard("healthAbove")
		return handler.Evaluate(gctx(entityID, rd, map[string]any{"threshold": float64(50)}))
	})
	if got {
		t.Error("healthAbove(hp=30, threshold=50) = true, want false")
	}
}
```

- [x] **Step 2: Run to confirm guard tests fail**

```
go test ./internal/agent/builtins/... -run "TestGuard_" -v 2>&1 | head -20
```

Expected: `GetGuard("timerExpired")` returns `ok=false` — guards not yet registered.

- [x] **Step 3: Create `internal/agent/builtins/guards.go`**

```go
package builtins

import (
	"math"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// ── timerExpired ──────────────────────────────────────────────────────────────

type timerExpiredGuard struct{}

func (g *timerExpiredGuard) Evaluate(ctx agent.GuardContext) bool {
	key, _ := ctx.Params["key"].(string)
	if key == "" || ctx.ContextManifest == nil {
		return false
	}
	comp := ctx.ContextManifest[key]
	if comp == "" {
		return false
	}
	val, _ := ctx.World.GetComponentValue(ctx.EntityID, comp, key)
	return toFloat(val) <= 0
}

// ── atTarget ──────────────────────────────────────────────────────────────────

type atTargetGuard struct{}

func (g *atTargetGuard) Evaluate(ctx agent.GuardContext) bool {
	if ctx.ContextManifest == nil {
		return false
	}
	txComp := ctx.ContextManifest["target_x"]
	tyComp := ctx.ContextManifest["target_y"]
	if txComp == "" || tyComp == "" {
		return false
	}

	px, _ := ctx.World.GetComponentValue(ctx.EntityID, "Position", "x")
	py, _ := ctx.World.GetComponentValue(ctx.EntityID, "Position", "y")
	tx, _ := ctx.World.GetComponentValue(ctx.EntityID, txComp, "target_x")
	ty, _ := ctx.World.GetComponentValue(ctx.EntityID, tyComp, "target_y")

	dx := toFloat(tx) - toFloat(px)
	dy := toFloat(ty) - toFloat(py)
	return math.Sqrt(dx*dx+dy*dy) <= 1.0
}

// ── inRange ───────────────────────────────────────────────────────────────────

type inRangeGuard struct{}

func (g *inRangeGuard) Evaluate(ctx agent.GuardContext) bool {
	maxDist := toFloat(ctx.Params["distance"])
	targetID, ok := resolveTargetID(ctx.World, ctx.Params["target"])
	if !ok {
		return false
	}

	px, _ := ctx.World.GetComponentValue(ctx.EntityID, "Position", "x")
	py, _ := ctx.World.GetComponentValue(ctx.EntityID, "Position", "y")
	tx, _ := ctx.World.GetComponentValue(targetID, "Position", "x")
	ty, _ := ctx.World.GetComponentValue(targetID, "Position", "y")

	dx := toFloat(tx) - toFloat(px)
	dy := toFloat(ty) - toFloat(py)
	return math.Sqrt(dx*dx+dy*dy) <= maxDist
}

// ── hasComponent ──────────────────────────────────────────────────────────────

type hasComponentGuard struct{}

func (g *hasComponentGuard) Evaluate(ctx agent.GuardContext) bool {
	compName, _ := ctx.Params["component"].(string)
	if compName == "" {
		return false
	}
	has, _ := ctx.World.HasComponent(ctx.EntityID, compName)
	return has
}

// ── healthAbove ───────────────────────────────────────────────────────────────

type healthAboveGuard struct{}

func (g *healthAboveGuard) Evaluate(ctx agent.GuardContext) bool {
	threshold := toFloat(ctx.Params["threshold"])
	hp, _ := ctx.World.GetComponentValue(ctx.EntityID, "Health", "hp")
	return toFloat(hp) > threshold
}
```

Note: `resolveTargetID` is defined in `actions.go` in the same package and is accessible here.

- [x] **Step 4: Update `registerGuards` in `register.go`**

Replace the stub `registerGuards` body:

```go
func registerGuards(r *agent.Registry) {
	r.RegisterGuard(agent.GuardMeta{
		Name:        "timerExpired",
		Description: "True when the named timer field is ≤ 0.",
		Params: []agent.ParamSchema{
			{Name: "key", Type: "string", Required: true},
		},
	}, &timerExpiredGuard{})

	r.RegisterGuard(agent.GuardMeta{
		Name:        "atTarget",
		Description: "True when entity's Position is within 1 unit of target_x/target_y.",
	}, &atTargetGuard{})

	r.RegisterGuard(agent.GuardMeta{
		Name:        "inRange",
		Description: "True when distance between this entity and target is ≤ distance param.",
		Params: []agent.ParamSchema{
			{Name: "target", Type: "string", Required: true},
			{Name: "distance", Type: "number", Required: true},
		},
	}, &inRangeGuard{})

	r.RegisterGuard(agent.GuardMeta{
		Name:        "hasComponent",
		Description: "True when the entity has the named component attached.",
		Params: []agent.ParamSchema{
			{Name: "component", Type: "string", Required: true},
		},
	}, &hasComponentGuard{})

	r.RegisterGuard(agent.GuardMeta{
		Name:        "healthAbove",
		Description: "True when Health.hp > threshold.",
		Params: []agent.ParamSchema{
			{Name: "threshold", Type: "number", Required: true},
		},
	}, &healthAboveGuard{})
}
```

- [x] **Step 5: Run all builtins tests**

```
go test ./internal/agent/builtins/... -v
```

Expected: all 21 tests pass (11 action + 10 guard).

- [x] **Step 6: Full regression check**

```
go test ./...
```

Expected: all pass.

- [x] **Step 7: Commit**

```bash
git add internal/agent/builtins/guards.go internal/agent/builtins/register.go \
        internal/agent/builtins/builtins_test.go
git commit -m "feat(epic-3/story-7): implement built-in guard handlers and complete registration"
```

---

## Task 5: Registration coverage test

**Files:**
- Modify: `internal/agent/builtins/builtins_test.go`

- [x] **Step 1: Add registration test**

Append to `builtins_test.go`:

```go
// ── Registration ──────────────────────────────────────────────────────────────

func TestRegisterBuiltins_AllPresent(t *testing.T) {
	r := builtins.NewRegistry()

	wantActions := []string{
		"attachComponent", "dealDamage", "detachComponent",
		"log", "moveTowardTarget", "pickRandomTarget",
		"setPursueTarget", "setTimer", "spawnEntity",
	}
	for _, name := range wantActions {
		if _, ok := r.GetAction(name); !ok {
			t.Errorf("action %q not registered", name)
		}
	}

	wantGuards := []string{"atTarget", "hasComponent", "healthAbove", "inRange", "timerExpired"}
	for _, name := range wantGuards {
		if _, ok := r.GetGuard(name); !ok {
			t.Errorf("guard %q not registered", name)
		}
	}
}
```

- [x] **Step 2: Run**

```
go test ./internal/agent/builtins/... -run "TestRegisterBuiltins_AllPresent" -v
```

Expected: PASS.

- [x] **Step 3: Full suite + coverage**

```
go test ./... && \
go test ./internal/agent/builtins/... -coverprofile=builtins_cov.out && \
go tool cover -func=builtins_cov.out | grep -E "actions\.go|guards\.go|register\.go"
```

Expected: all pass; actions.go and guards.go ≥ 80% (error branches for nil Reader/missing manifest are not covered by happy-path tests).

- [x] **Step 4: Commit**

```bash
git add internal/agent/builtins/builtins_test.go
git commit -m "feat(epic-3/story-7): add registration coverage test"
```

---

## Verification

```bash
# Full suite
go test ./...

# Coverage summary for builtins
go test ./internal/agent/builtins/... -coverprofile=builtins_cov.out
go tool cover -func=builtins_cov.out

# Dependency guard — builtins must not import storage or database/sql
grep -r '"database/sql"\|"internal/storage"' internal/agent/builtins/
# Expected: no output

# Agent package must not import storage or database/sql
grep -r '"database/sql"\|"internal/storage"' internal/agent/*.go
# Expected: no output
```

---

## Acceptance Criteria Traceability

| Criterion | File | Location |
|-----------|------|----------|
| `moveTowardTarget` moves toward target at speed | `actions.go` | `moveTowardTargetAction.Run` |
| `moveTowardTarget` respects `speed_mult` | `builtins_test.go` | `TestAction_moveTowardTarget_SpeedMult` |
| `dealDamage` decrements Health.hp | `actions.go` | `dealDamageAction.Run` |
| `dealDamage` resolves `"$player"` sentinel | `actions.go` | `resolveTargetID` |
| `spawnEntity` creates entity row | `actions.go` | `spawnEntityAction.Run` |
| `attachComponent` inserts component row | `actions.go` | `attachComponentAction.Run` |
| `detachComponent` removes component row | `actions.go` | `detachComponentAction.Run` |
| `setTimer` writes tick count to context field | `actions.go` | `setTimerAction.Run` |
| `log` writes message without panicking | `actions.go` | `logAction.Run` |
| `pickRandomTarget` writes target within radius | `actions.go` | `pickRandomTargetAction.Run` |
| `setPursueTarget` copies player position | `actions.go` | `setPursueTargetAction.Run` |
| `timerExpired` true when field ≤ 0 | `guards.go` | `timerExpiredGuard.Evaluate` |
| `atTarget` true within 1 unit | `guards.go` | `atTargetGuard.Evaluate` |
| `inRange` uses entity distance | `guards.go` | `inRangeGuard.Evaluate` |
| `hasComponent` reflects real DB state | `guards.go` | `hasComponentGuard.Evaluate` |
| `healthAbove` compares Health.hp | `guards.go` | `healthAboveGuard.Evaluate` |
| All 14 registered with metadata | `register.go` | `RegisterBuiltins` |
| Each has at least one integration test | `builtins_test.go` | all `TestAction_*` and `TestGuard_*` |
| Agent package has no SQL dependency | — | dependency guard |
