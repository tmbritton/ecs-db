# Story 6: Delayed Transitions (`after`) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Wire `after`-transition scheduling/cancellation end-to-end: duration parsing in the agent package, corrected after-event routing in the interpreter, load-time duration validation, and a real SQL-backed `MachineWriter` in `internal/storage`.

**Architecture:** Five focused changes. (1) New `scheduler.go` owns duration math. (2) `agent.go` gains `TickDurationMs` and a corrected `parseDurationTicks`. (3) `interpreter.go` fixes the latent after-event routing bug. (4) `validator.go` rejects bad duration strings at load time. (5) `internal/storage/machine_writer.go` implements all four `agent.MachineWriter` methods against a real `*sql.Tx`.

**Event type format:** XState v4 — `xstate.after(<duration>).<state-id>` — already established by Story 5. The Story 6 spec's `after:<state-id>:<duration>` notation is superseded by the existing implementation.

**Tech Stack:** Go stdlib (`fmt`, `math`, `strconv`, `strings`, `time`). SQLite via `modernc.org/sqlite`. Module: `github.com/tmbritton/ecs-db`.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/agent/scheduler.go` | Create | `ParseDurationMs`, `DurationToTicks`, updated `parseDurationTicks` |
| `internal/agent/scheduler_test.go` | Create | Unit tests for duration parsing and tick conversion |
| `internal/agent/agent.go` | Modify | Add `TickDurationMs` to `Agent`; update `NewAgent`; update `parseDurationTicks` callers |
| `internal/agent/agent_test.go` | Modify | Update `NewAgent` call sites to pass tick duration |
| `internal/agent/interpreter.go` | Modify | Fix `selectEligibleTransitions` after-event routing |
| `internal/agent/interpreter_test.go` | Modify | Update `NewAgent` call sites; add after-event routing test |
| `internal/agent/validator.go` | Modify | Validate `After` duration keys in `validateStateNode` |
| `internal/agent/validator_test.go` | Modify | Tests for invalid after-duration rejection |
| `internal/storage/machine_writer.go` | Create | `sqliteMachineWriter` implementing `agent.MachineWriter` |
| `internal/storage/machine_writer_test.go` | Create | Integration tests against in-memory SQLite |

---

## Context

Story 5 implemented the full SCXML microstep algorithm and called `mw.ScheduleAfterEvent` / `mw.CancelAfterEvents` through a mock. Three gaps remain:

1. **Duration parsing** — `parseDurationTicks` only handles bare integers and `"500ms"`. Strings like `"1s"`, `"1.5s"`, `"2m"` return 0.
2. **After-event routing bug** — `selectEligibleTransitions` does `cur.After[event.Type]` where `After` keys are raw duration strings like `"500"` but delivered event types are `"xstate.after(500).m.idle"`. The lookup always misses, so after-events are silently swallowed.
3. **No storage implementation** — `agent.MachineWriter` has no concrete SQL-backed type; the real `event_queue` table is never written.

---

## Task 1: scheduler.go — Duration parsing

**Files:**
- Create: `internal/agent/scheduler.go`
- Create: `internal/agent/scheduler_test.go`

- [x] **Step 1: Write failing scheduler_test.go**

```go
package agent

import (
	"testing"
)

func TestParseDurationMs_BareInt(t *testing.T) {
	ms, err := ParseDurationMs("500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms != 500 {
		t.Errorf("got %d, want 500", ms)
	}
}

func TestParseDurationMs_MillisecondSuffix(t *testing.T) {
	ms, err := ParseDurationMs("500ms")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms != 500 {
		t.Errorf("got %d, want 500", ms)
	}
}

func TestParseDurationMs_SecondSuffix(t *testing.T) {
	ms, err := ParseDurationMs("1s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms != 1000 {
		t.Errorf("got %d, want 1000", ms)
	}
}

func TestParseDurationMs_FractionalSecond(t *testing.T) {
	ms, err := ParseDurationMs("1.5s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms != 1500 {
		t.Errorf("got %d, want 1500", ms)
	}
}

func TestParseDurationMs_MinuteSuffix(t *testing.T) {
	ms, err := ParseDurationMs("2m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ms != 120000 {
		t.Errorf("got %d, want 120000", ms)
	}
}

func TestParseDurationMs_Invalid(t *testing.T) {
	cases := []string{"bad", "1x", "", "abc123"}
	for _, s := range cases {
		_, err := ParseDurationMs(s)
		if err == nil {
			t.Errorf("ParseDurationMs(%q): expected error, got nil", s)
		}
	}
}

func TestDurationToTicks_ExactDivision(t *testing.T) {
	ticks := DurationToTicks(500, 50)
	if ticks != 10 {
		t.Errorf("got %d, want 10", ticks)
	}
}

func TestDurationToTicks_CeilRemainder(t *testing.T) {
	// 100ms / 50ms per tick = 2 ticks — no ceiling needed
	if got := DurationToTicks(100, 50); got != 2 {
		t.Errorf("100ms/50ms = %d, want 2", got)
	}
	// 75ms / 50ms = 1.5 → ceil = 2
	if got := DurationToTicks(75, 50); got != 2 {
		t.Errorf("75ms/50ms = %d, want 2", got)
	}
}

func TestDurationToTicks_DefaultTickDuration(t *testing.T) {
	// 1000ms at 50ms/tick = 20 ticks
	if got := DurationToTicks(1000, 50); got != 20 {
		t.Errorf("1000ms/50ms = %d, want 20", got)
	}
}

func TestDurationToTicks_ZeroTickDuration(t *testing.T) {
	// Zero tick duration falls back to 1ms/tick; 500ms = 500 ticks
	if got := DurationToTicks(500, 0); got != 500 {
		t.Errorf("DurationToTicks(500,0) = %d, want 500", got)
	}
}
```

- [x] **Step 2: Run to confirm compile failure**

```
go test ./internal/agent/... -run "TestParseDurationMs|TestDurationToTicks" -v 2>&1 | head -10
```

Expected: `undefined: ParseDurationMs`, `undefined: DurationToTicks`.

- [x] **Step 3: Create scheduler.go**

```go
package agent

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseDurationMs converts an after-duration string to milliseconds.
// Accepts bare integer strings (XState ms shorthand) and time.Duration strings.
func ParseDurationMs(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}
	// Bare integer → treat as milliseconds.
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	// Delegate to time.ParseDuration for "500ms", "1s", "1.5s", "2m", etc.
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid after duration %q: %w", s, err)
	}
	return d.Milliseconds(), nil
}

// DurationToTicks converts a millisecond count to ticks using ceiling division.
// tickDurationMs of 0 falls back to 1 (1ms per tick) to avoid division by zero.
func DurationToTicks(ms, tickDurationMs int64) int64 {
	if tickDurationMs <= 0 {
		tickDurationMs = 1
	}
	return (ms + tickDurationMs - 1) / tickDurationMs
}

// parseDurationTicks converts an after-duration string to a tick count.
func parseDurationTicks(duration string, tickDurationMs int64) int64 {
	ms, err := ParseDurationMs(duration)
	if err != nil {
		return 0
	}
	return DurationToTicks(ms, tickDurationMs)
}

// afterCandidates returns the After transitions for cur whose computed event type
// matches eventType. After keys are raw duration strings; the event type is
// "xstate.after(<duration>).<state-id>", so we compute and compare.
func afterCandidates(cur *StateNode, eventType string) []Transition {
	for duration, ts := range cur.After {
		if eventType == afterEventType(duration, cur.ID) {
			return ts
		}
	}
	return nil
}
```

Note: `afterEventType` and `parseDurationTicks` are moved here from `agent.go`; the old definitions in `agent.go` must be deleted in Task 2.

- [x] **Step 4: Run tests**

```
go test ./internal/agent/... -run "TestParseDurationMs|TestDurationToTicks" -v
```

Expected: all pass.

- [x] **Step 5: Full regression check**

```
go test ./...
```

Expected: all pass (old `afterEventType` still in agent.go — no conflict yet since they're identical).

- [x] **Step 6: Commit**

```bash
git add internal/agent/scheduler.go internal/agent/scheduler_test.go
git commit -m "feat(epic-3/story-6): add scheduler duration parsing"
```

---

## Task 2: Update agent.go — TickDurationMs and wiring

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_test.go`

- [x] **Step 1: Edit agent.go**

**(a) Add `TickDurationMs` to `Agent` struct:**

```go
type Agent struct {
	Definition           *MachineDefinition
	Configuration        []*StateNode
	EntityID             int64
	History              map[string][]*StateNode
	ActivatedByComponent string
	TickDurationMs       int64 // ms per tick; defaults to 50 (20 Hz)
}
```

**(b) Update `NewAgent` — add `tickDurationMs int64` parameter (0 → default 50):**

```go
func NewAgent(def *MachineDefinition, entityID int64, activatedByComponent string, tickDurationMs int64) *Agent {
	if tickDurationMs <= 0 {
		tickDurationMs = 50
	}
	return &Agent{
		Definition:           def,
		EntityID:             entityID,
		History:              make(map[string][]*StateNode),
		ActivatedByComponent: activatedByComponent,
		TickDurationMs:       tickDurationMs,
	}
}
```

**(c) Update the `parseDurationTicks` call in `StartAgent`** — find the after-scheduling loop and add `agent.TickDurationMs` as the second argument:

```go
for duration := range state.After {
    targetTick := tick + parseDurationTicks(duration, agent.TickDurationMs)
    evType := afterEventType(duration, state.ID)
    ...
}
```

**(d) Delete** the old `afterEventType` and `parseDurationTicks` function bodies from `agent.go` (they now live in `scheduler.go`).

**(e) Remove `strings` and `strconv` imports** from `agent.go` — only `"fmt"` remains.

- [x] **Step 2: Update agent_test.go — fix NewAgent call sites**

Every `NewAgent(def, id, comp)` gains a fourth argument `0`:

- `TestNewAgent_Fields`: `NewAgent(def, 42, "StatusEffect", 0)`
- `TestNewAgent_EmptyActivatedBy`: `NewAgent(def, 1, "", 0)`
- `TestStartAgent_SeedsContextComponents`: `NewAgent(def, 1, "", 0)`
- `TestStartAgent_SkipsExistingComponents`: `NewAgent(def, 1, "", 0)`
- `TestStartAgent_SetsInitialConfiguration`: `NewAgent(def, 1, "", 0)`
- `TestStartAgent_PersistsMachineState`: `NewAgent(def, 7, "", 0)`
- `TestStartAgent_RunsEntryActions`: `NewAgent(def, 1, "", 0)`

Also add two new tests at the end of agent_test.go:

```go
func TestNewAgent_TickDurationMs_Default(t *testing.T) {
	def := &MachineDefinition{ID: "m", Initial: "a", States: map[string]*StateNode{}}
	a := NewAgent(def, 1, "", 0)
	if a.TickDurationMs != 50 {
		t.Errorf("TickDurationMs = %d, want 50 (default)", a.TickDurationMs)
	}
}

func TestNewAgent_TickDurationMs_Custom(t *testing.T) {
	def := &MachineDefinition{ID: "m", Initial: "a", States: map[string]*StateNode{}}
	a := NewAgent(def, 1, "", 100)
	if a.TickDurationMs != 100 {
		t.Errorf("TickDurationMs = %d, want 100", a.TickDurationMs)
	}
}
```

- [x] **Step 3: Run agent tests**

```
go test ./internal/agent/... -run "TestNewAgent|TestStartAgent" -v
```

Expected: all pass.

- [x] **Step 4: Full regression check**

```
go test ./...
```

interpreter_test.go also calls `NewAgent` — those will fail here. That's expected; fix them in Task 3.

- [x] **Step 5: Commit (after Task 3 fixes interpreter_test.go)**

Hold this commit until Task 3 is complete so the repo compiles cleanly.

---

## Task 3: Update interpreter.go — Fix after-event routing + call sites

**Files:**
- Modify: `internal/agent/interpreter.go`
- Modify: `internal/agent/interpreter_test.go`

**The bug:** `selectEligibleTransitions` checks `cur.After[event.Type]` where `After` keys are raw duration strings (`"500"`) but delivered event types are `"xstate.after(500).m.idle"`. The lookup always misses.

- [x] **Step 1: Fix selectEligibleTransitions in interpreter.go**

Find the candidates lookup block inside `selectEligibleTransitions`:

```go
var candidates []Transition
if ts, ok := cur.On[event.Type]; ok {
    candidates = ts
} else if ts, ok := cur.After[event.Type]; ok {
    candidates = ts
}
```

Replace it with:

```go
var candidates []Transition
if ts, ok := cur.On[event.Type]; ok {
    candidates = ts
} else {
    candidates = afterCandidates(cur, event.Type)
}
```

(`afterCandidates` is defined in `scheduler.go`, same package.)

- [x] **Step 2: Update the parseDurationTicks call in SendEvent**

Find the after-scheduling loop inside `SendEvent`'s entry block:

```go
for duration := range state.After {
    targetTick := tick + parseDurationTicks(duration)
```

Change it to:

```go
for duration := range state.After {
    targetTick := tick + parseDurationTicks(duration, agent.TickDurationMs)
```

- [x] **Step 3: Update interpreter_test.go — fix NewAgent call sites**

Every `NewAgent(def, id, comp)` call gains `0` as the fourth argument. These appear in:
- The `startedAgent` helper: `a := NewAgent(def, entityID, "", 0)`
- `TestSendEvent_EntryExitActionsOrder`: `a := NewAgent(def, 1, "", 0)`
- `TestSendEvent_TransitionActions`: `a := NewAgent(def, 1, "", 0)`
- `TestSendEvent_SelfTransition`: `a := NewAgent(def, 1, "", 0)`
- `TestSendEvent_DepthPreemption`: `a := NewAgent(def, 1, "", 0)`
- `TestSendEvent_ParallelRegions_BothTransition`: `a := NewAgent(def, 1, "", 0)`
- `TestSendEvent_HistoryShallow_RestoresRecordedState`: `a := NewAgent(def, 1, "", 0)`
- `TestSendEvent_HistoryShallow_DefaultTargetWhenNoHistory`: `a := NewAgent(def, 1, "", 0)`
- `TestSendEvent_FinalState_DetachesActivatingComponent`: `a := NewAgent(def, 1, "StatusBuff", 0)`
- `TestSendEvent_FinalState_NoPrimaryDetach`: `a := NewAgent(def, 1, "", 0)`

- [x] **Step 4: Add after-event routing test to interpreter_test.go**

```go
func TestSendEvent_AfterEventDelivery_Routed(t *testing.T) {
	// Verifies that a synthetic after-event (xstate.after(500).m.idle) is
	// routed to the correct After transition, not silently dropped.
	reached := false
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "onTimeout"}, actionFunc(func(ActionContext) error {
		reached = true
		return nil
	}))

	def := mustParse(t, `{
		"id":"m","initial":"idle",
		"states":{
			"idle":{"after":{"500":[{"actions":["onTimeout"]}]}}
		}
	}`)
	def.ContextManifest = map[string]string{}
	a := NewAgent(def, 1, "", 50)
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	afterEv := Event{Type: afterEventType("500", "m.idle")}
	if err := SendEvent(a, afterEv, 10, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if !reached {
		t.Error("after-transition action not reached — event routing is broken")
	}
}
```

- [x] **Step 5: Run all agent tests**

```
go test ./internal/agent/... -v
```

Expected: all pass including `TestSendEvent_AfterEventDelivery_Routed`.

- [x] **Step 6: Full regression check**

```
go test ./...
```

Expected: all pass.

- [x] **Step 7: Commit Tasks 2 and 3 together**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go \
        internal/agent/interpreter.go internal/agent/interpreter_test.go
git commit -m "feat(epic-3/story-6): TickDurationMs config; fix after-event routing"
```

---

## Task 4: Update validator.go — Validate after-duration keys

**Files:**
- Modify: `internal/agent/validator.go`
- Modify: `internal/agent/validator_test.go`

- [x] **Step 1: Add after-duration validation to validateStateNode**

In `validateStateNode`, find the existing `for _, transitions := range node.After` loop:

```go
for _, transitions := range node.After {
    for _, t := range transitions {
        errs = append(errs, validateTransition(machineID, node.ID, t, registry, knownStates)...)
    }
}
```

Change it to also validate the duration key:

```go
for duration, transitions := range node.After {
    if _, err := ParseDurationMs(duration); err != nil {
        errs = append(errs, ValidationError{
            MachineID: machineID, StateID: node.ID, Field: duration,
            Message: fmt.Sprintf("after duration %q is invalid: %v", duration, err),
        })
    }
    for _, t := range transitions {
        errs = append(errs, validateTransition(machineID, node.ID, t, registry, knownStates)...)
    }
}
```

- [x] **Step 2: Add tests to validator_test.go**

Look at `validator_test.go` for the existing `testSchema()` and `mustParse()` helpers, then append:

```go
func TestValidateMachine_AfterDuration_Valid(t *testing.T) {
	s := testSchema()
	r := NewRegistry()
	for _, dur := range []string{"500", "500ms", "1s", "1.5s", "2m"} {
		raw := `{"id":"m","initial":"idle","states":{"idle":{"after":{"` + dur + `":[{"target":"idle"}]}}}}`
		def := mustParse(t, raw)
		errs := ValidateMachine(def, r, s)
		for _, e := range errs {
			if e.Field == dur {
				t.Errorf("duration %q rejected unexpectedly: %s", dur, e.Message)
			}
		}
	}
}

func TestValidateMachine_AfterDuration_Invalid(t *testing.T) {
	s := testSchema()
	r := NewRegistry()
	raw := `{"id":"m","initial":"idle","states":{"idle":{"after":{"bad":[{"target":"idle"}]}}}}`
	def := mustParse(t, raw)
	errs := ValidateMachine(def, r, s)
	found := false
	for _, e := range errs {
		if e.Field == "bad" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for duration 'bad', got none")
	}
}
```

- [x] **Step 3: Run tests**

```
go test ./internal/agent/... -run "TestValidateMachine" -v
```

Expected: all pass.

- [x] **Step 4: Full regression check**

```
go test ./...
```

Expected: all pass.

- [x] **Step 5: Commit**

```bash
git add internal/agent/validator.go internal/agent/validator_test.go
git commit -m "feat(epic-3/story-6): validate after-duration keys at load time"
```

---

## Task 5: storage/machine_writer.go — SQL implementation

**Files:**
- Create: `internal/storage/machine_writer.go`
- Create: `internal/storage/machine_writer_test.go`

The `event_queue`, `behavior_components`, and `transitions` tables already exist — created by `EnsureInterpreterTables`.

**Cancellation LIKE pattern:** `xstate.after(%).<stateID>` — safe because the literal `).` anchors the boundary between duration and state ID. See the Context section for the proof that this pattern cannot spuriously match sibling states.

- [x] **Step 1: Write failing machine_writer_test.go**

```go
package storage_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/storage"
	_ "modernc.org/sqlite"
)

func setupMachineWriterDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	stmts := []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL DEFAULT 0)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	if err := storage.EnsureInterpreterTables(db); err != nil {
		t.Fatalf("EnsureInterpreterTables: %v", err)
	}
	return db
}

func insertTestEntity(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('test', 0)")
	if err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func beginWriterTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

// ── SetMachineState ───────────────────────────────────────────────────────────

func TestSetMachineState_InsertsRow(t *testing.T) {
	db := setupMachineWriterDB(t)
	entityID := insertTestEntity(t, db)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	if err := mw.SetMachineState(entityID, "m", []string{"m.idle"}, 5); err != nil {
		t.Fatalf("SetMachineState: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var states string
	var updatedAt int64
	err := db.QueryRow("SELECT current_states, updated_at FROM behavior_components WHERE entity_id=? AND machine_id=?", entityID, "m").Scan(&states, &updatedAt)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !strings.Contains(states, "m.idle") {
		t.Errorf("current_states = %q, want to contain m.idle", states)
	}
	if updatedAt != 5 {
		t.Errorf("updated_at = %d, want 5", updatedAt)
	}
}

func TestSetMachineState_UpdatesExistingRow(t *testing.T) {
	db := setupMachineWriterDB(t)
	entityID := insertTestEntity(t, db)

	tx1, _ := db.BeginTx(context.Background(), nil)
	_ = storage.NewMachineWriter(tx1).SetMachineState(entityID, "m", []string{"m.idle"}, 1)
	_ = tx1.Commit()

	tx2, _ := db.BeginTx(context.Background(), nil)
	_ = storage.NewMachineWriter(tx2).SetMachineState(entityID, "m", []string{"m.active"}, 2)
	_ = tx2.Commit()

	var states string
	_ = db.QueryRow("SELECT current_states FROM behavior_components WHERE entity_id=?", entityID).Scan(&states)
	if !strings.Contains(states, "m.active") {
		t.Errorf("current_states = %q, expected m.active after update", states)
	}
}

// ── AppendTransition ──────────────────────────────────────────────────────────

func TestAppendTransition_InsertsRow(t *testing.T) {
	db := setupMachineWriterDB(t)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	tr := true
	rec := agent.TransitionRecord{
		Tick: 10, WallMs: 999, EntityID: 1, MachineID: "m",
		FromStates: []string{"m.a"}, ToStates: []string{"m.b"},
		Event: "GO", CondResult: &tr, ActionsRun: []string{"doWork"},
	}
	if err := mw.AppendTransition(rec); err != nil {
		t.Fatalf("AppendTransition: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var tick int64
	var event string
	var condResult sql.NullInt64
	err := db.QueryRow("SELECT tick, event, cond_result FROM transitions ORDER BY id DESC LIMIT 1").Scan(&tick, &event, &condResult)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if tick != 10 {
		t.Errorf("tick = %d, want 10", tick)
	}
	if event != "GO" {
		t.Errorf("event = %q, want GO", event)
	}
	if !condResult.Valid || condResult.Int64 != 1 {
		t.Errorf("cond_result = %v, want 1", condResult)
	}
}

func TestAppendTransition_NilCondResult(t *testing.T) {
	db := setupMachineWriterDB(t)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	rec := agent.TransitionRecord{
		Tick: 1, WallMs: 0, EntityID: 1, MachineID: "m",
		FromStates: []string{"m.a"}, ToStates: []string{"m.b"},
		Event: "E", CondResult: nil, ActionsRun: nil,
	}
	_ = mw.AppendTransition(rec)
	_ = tx.Commit()

	var condResult sql.NullInt64
	_ = db.QueryRow("SELECT cond_result FROM transitions ORDER BY id DESC LIMIT 1").Scan(&condResult)
	if condResult.Valid {
		t.Errorf("cond_result = %v, want NULL for unconditional transition", condResult)
	}
}

// ── ScheduleAfterEvent ────────────────────────────────────────────────────────

func TestScheduleAfterEvent_InsertsRow(t *testing.T) {
	db := setupMachineWriterDB(t)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	if err := mw.ScheduleAfterEvent(1, "m", "xstate.after(500).m.idle", 15); err != nil {
		t.Fatalf("ScheduleAfterEvent: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var evType string
	var targetTick int64
	err := db.QueryRow("SELECT event_type, target_tick FROM event_queue WHERE entity_id=1 AND machine_id='m'").Scan(&evType, &targetTick)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if evType != "xstate.after(500).m.idle" {
		t.Errorf("event_type = %q, want xstate.after(500).m.idle", evType)
	}
	if targetTick != 15 {
		t.Errorf("target_tick = %d, want 15", targetTick)
	}
}

func TestScheduleAfterEvent_MultipleEntries(t *testing.T) {
	db := setupMachineWriterDB(t)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	_ = mw.ScheduleAfterEvent(1, "m", "xstate.after(500).m.idle", 10)
	_ = mw.ScheduleAfterEvent(1, "m", "xstate.after(1000).m.idle", 20)
	_ = tx.Commit()

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM event_queue WHERE entity_id=1 AND machine_id='m'").Scan(&count)
	if count != 2 {
		t.Errorf("event_queue rows = %d, want 2", count)
	}
}

// ── CancelAfterEvents ─────────────────────────────────────────────────────────

func TestCancelAfterEvents_DeletesMatchingRows(t *testing.T) {
	db := setupMachineWriterDB(t)

	tx1, _ := db.BeginTx(context.Background(), nil)
	mw1 := storage.NewMachineWriter(tx1)
	_ = mw1.ScheduleAfterEvent(1, "m", "xstate.after(500).m.idle", 10)
	_ = mw1.ScheduleAfterEvent(1, "m", "xstate.after(200).m.active", 5)
	_ = tx1.Commit()

	tx2, _ := db.BeginTx(context.Background(), nil)
	mw2 := storage.NewMachineWriter(tx2)
	if err := mw2.CancelAfterEvents(1, "m", []string{"m.idle"}); err != nil {
		t.Fatalf("CancelAfterEvents: %v", err)
	}
	_ = tx2.Commit()

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM event_queue WHERE entity_id=1 AND machine_id='m'").Scan(&count)
	if count != 1 {
		t.Errorf("event_queue rows = %d, want 1 (m.active survives)", count)
	}
}

func TestCancelAfterEvents_NoopWhenEmpty(t *testing.T) {
	db := setupMachineWriterDB(t)
	tx := beginWriterTx(t, db)
	mw := storage.NewMachineWriter(tx)

	if err := mw.CancelAfterEvents(1, "m", []string{"m.idle"}); err != nil {
		t.Fatalf("CancelAfterEvents on empty queue: %v", err)
	}
}

func TestCancelAfterEvents_DoesNotMatchSiblingState(t *testing.T) {
	// m.idle and m.outer.idle are different states — cancel m.idle must not touch m.outer.idle.
	db := setupMachineWriterDB(t)

	tx1, _ := db.BeginTx(context.Background(), nil)
	mw1 := storage.NewMachineWriter(tx1)
	_ = mw1.ScheduleAfterEvent(1, "m", "xstate.after(500).m.idle", 10)
	_ = mw1.ScheduleAfterEvent(1, "m", "xstate.after(500).m.outer.idle", 10)
	_ = tx1.Commit()

	tx2, _ := db.BeginTx(context.Background(), nil)
	mw2 := storage.NewMachineWriter(tx2)
	_ = mw2.CancelAfterEvents(1, "m", []string{"m.idle"})
	_ = tx2.Commit()

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM event_queue WHERE entity_id=1").Scan(&count)
	if count != 1 {
		t.Errorf("event_queue rows = %d, want 1 (m.outer.idle survives)", count)
	}
}

// ── Full flow: enter → row; exit → row deleted; re-enter → updated tick ───────

func TestAfterEventFlow_EnterExitReenter(t *testing.T) {
	db := setupMachineWriterDB(t)
	entityID := insertTestEntity(t, db)

	// Enter state with after-transition.
	tx1, _ := db.BeginTx(context.Background(), nil)
	mw := storage.NewMachineWriter(tx1)
	_ = mw.SetMachineState(entityID, "m", []string{"m.idle"}, 0)
	_ = mw.ScheduleAfterEvent(entityID, "m", "xstate.after(500).m.idle", 10)
	_ = tx1.Commit()

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM event_queue WHERE entity_id=?", entityID).Scan(&count)
	if count != 1 {
		t.Fatalf("after entry: event_queue rows = %d, want 1", count)
	}

	// Exit state — cancel pending events.
	tx2, _ := db.BeginTx(context.Background(), nil)
	mw2 := storage.NewMachineWriter(tx2)
	_ = mw2.CancelAfterEvents(entityID, "m", []string{"m.idle"})
	_ = mw2.SetMachineState(entityID, "m", []string{"m.active"}, 1)
	_ = tx2.Commit()

	_ = db.QueryRow("SELECT COUNT(*) FROM event_queue WHERE entity_id=?", entityID).Scan(&count)
	if count != 0 {
		t.Fatalf("after exit: event_queue rows = %d, want 0", count)
	}

	// Re-enter — new row with updated target tick.
	tx3, _ := db.BeginTx(context.Background(), nil)
	mw3 := storage.NewMachineWriter(tx3)
	_ = mw3.ScheduleAfterEvent(entityID, "m", "xstate.after(500).m.idle", 25)
	_ = tx3.Commit()

	var targetTick int64
	_ = db.QueryRow("SELECT target_tick FROM event_queue WHERE entity_id=?", entityID).Scan(&targetTick)
	if targetTick != 25 {
		t.Errorf("re-entry target_tick = %d, want 25", targetTick)
	}
}
```

- [x] **Step 2: Run to confirm compile failure**

```
go test ./internal/storage/... -run "TestSetMachineState|TestScheduleAfterEvent|TestCancelAfterEvents|TestAppendTransition|TestAfterEventFlow" -v 2>&1 | head -15
```

Expected: `undefined: storage.NewMachineWriter`.

- [x] **Step 3: Create machine_writer.go**

```go
package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// sqliteMachineWriter implements agent.MachineWriter against a live *sql.Tx.
// All operations run inside the caller's transaction for atomicity with
// the microstep that caused the entry/exit.
type sqliteMachineWriter struct {
	tx *sql.Tx
}

// NewMachineWriter wraps a *sql.Tx to produce an agent.MachineWriter.
func NewMachineWriter(tx *sql.Tx) agent.MachineWriter {
	return &sqliteMachineWriter{tx: tx}
}

func (w *sqliteMachineWriter) SetMachineState(entityID int64, machineID string, states []string, tick int64) error {
	data, err := json.Marshal(states)
	if err != nil {
		return fmt.Errorf("SetMachineState: marshal states: %w", err)
	}
	_, err = w.tx.Exec(
		`INSERT INTO behavior_components (entity_id, machine_id, current_states, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(entity_id, machine_id) DO UPDATE SET
		   current_states = excluded.current_states,
		   updated_at     = excluded.updated_at`,
		entityID, machineID, string(data), tick,
	)
	if err != nil {
		return fmt.Errorf("SetMachineState: %w", err)
	}
	return nil
}

func (w *sqliteMachineWriter) AppendTransition(rec agent.TransitionRecord) error {
	fromJSON, err := json.Marshal(rec.FromStates)
	if err != nil {
		return fmt.Errorf("AppendTransition: marshal from_states: %w", err)
	}
	toJSON, err := json.Marshal(rec.ToStates)
	if err != nil {
		return fmt.Errorf("AppendTransition: marshal to_states: %w", err)
	}
	actionsJSON, err := json.Marshal(rec.ActionsRun)
	if err != nil {
		return fmt.Errorf("AppendTransition: marshal actions_run: %w", err)
	}
	var condResult interface{}
	if rec.CondResult != nil {
		if *rec.CondResult {
			condResult = 1
		} else {
			condResult = 0
		}
	}
	_, err = w.tx.Exec(
		`INSERT INTO transitions
		   (tick, wall_ms, entity_id, machine_id, from_states, to_states, event, cond_result, actions_run)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.Tick, rec.WallMs, rec.EntityID, rec.MachineID,
		string(fromJSON), string(toJSON), rec.Event, condResult, string(actionsJSON),
	)
	if err != nil {
		return fmt.Errorf("AppendTransition: %w", err)
	}
	return nil
}

func (w *sqliteMachineWriter) ScheduleAfterEvent(entityID int64, machineID, eventType string, targetTick int64) error {
	_, err := w.tx.Exec(
		`INSERT INTO event_queue (entity_id, machine_id, event_type, target_tick) VALUES (?, ?, ?, ?)`,
		entityID, machineID, eventType, targetTick,
	)
	if err != nil {
		return fmt.Errorf("ScheduleAfterEvent: %w", err)
	}
	return nil
}

func (w *sqliteMachineWriter) CancelAfterEvents(entityID int64, machineID string, stateIDs []string) error {
	for _, stateID := range stateIDs {
		// Pattern: xstate.after(<any duration>).<stateID>
		// The literal ")." anchors the boundary so no sibling state is matched.
		pattern := "xstate.after(%)." + escapeForLIKE(stateID)
		if _, err := w.tx.Exec(
			`DELETE FROM event_queue WHERE entity_id = ? AND machine_id = ? AND event_type LIKE ? ESCAPE '\'`,
			entityID, machineID, pattern,
		); err != nil {
			return fmt.Errorf("CancelAfterEvents %q: %w", stateID, err)
		}
	}
	return nil
}

// escapeForLIKE escapes SQLite LIKE wildcards in s.
// State IDs are dot-path identifiers and should never contain wildcards,
// but this guard prevents silent mismatches if that assumption is violated.
func escapeForLIKE(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// Compile-time interface check.
var _ agent.MachineWriter = (*sqliteMachineWriter)(nil)
```

- [x] **Step 4: Run tests**

```
go test ./internal/storage/... -run "TestSetMachineState|TestScheduleAfterEvent|TestCancelAfterEvents|TestAppendTransition|TestAfterEventFlow" -v
```

Expected: all pass.

- [x] **Step 5: Full regression check and coverage**

```
go test ./... && \
go test ./internal/agent/... -coverprofile=agent_cov.out && \
go tool cover -func=agent_cov.out | grep -E "scheduler\.go|agent\.go|interpreter\.go|validator\.go" && \
go test ./internal/storage/... -coverprofile=storage_cov.out && \
go tool cover -func=storage_cov.out | grep "machine_writer\.go"
```

Expected: all pass; `machine_writer.go` ≥ 90%; `scheduler.go` ≥ 90%.

- [x] **Step 6: Commit**

```bash
git add internal/storage/machine_writer.go internal/storage/machine_writer_test.go
git commit -m "feat(epic-3/story-6): implement MachineWriter SQL backend for event_queue"
```

---

## Verification

```bash
# Full suite
go test ./...

# Coverage
go test ./internal/agent/... -coverprofile=agent_cov.out
go tool cover -func=agent_cov.out | grep -E "scheduler|agent\.go|interpreter|validator"

go test ./internal/storage/... -coverprofile=storage_cov.out
go tool cover -func=storage_cov.out | grep machine_writer

# Dependency guard — agent must not import storage or database/sql
grep -r '"database/sql"\|"internal/storage"' internal/agent/
# Expected: no output
```

---

## Acceptance Criteria Traceability

| Criterion | File | Location |
|-----------|------|----------|
| Bare integer `"500"` → 500ms | `scheduler.go` | `ParseDurationMs` |
| Duration strings `"1s"`, `"1.5s"`, `"2m"` | `scheduler.go` | `ParseDurationMs` via `time.ParseDuration` |
| Invalid string → validation error at load time | `validator.go` | `validateStateNode` after-loop |
| Schedule `event_queue` row on state entry | `storage/machine_writer.go` | `ScheduleAfterEvent` |
| Cancel rows on state exit | `storage/machine_writer.go` | `CancelAfterEvents` LIKE query |
| Same transaction as microstep | `machine_writer.go` | `*sql.Tx` passed by caller |
| Tick duration configurable (default 50ms) | `agent.go` | `Agent.TickDurationMs` / `NewAgent` |
| Multiple `after` entries all scheduled | `machine_writer_test.go` | `TestScheduleAfterEvent_MultipleEntries` |
| Enter → row; exit → deleted; re-enter → new row | `machine_writer_test.go` | `TestAfterEventFlow_EnterExitReenter` |
| After-event delivered via correct transition | `interpreter.go` | `afterCandidates` + `TestSendEvent_AfterEventDelivery_Routed` |
| ≥ 90% coverage | `scheduler_test.go`, `machine_writer_test.go` | all test functions |
| No raw SQL / no storage import in agent | — | dependency guard above |
