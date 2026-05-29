# Story 5: SCXML Microstep Interpreter — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the full SCXML microstep algorithm as `SendEvent` in `internal/agent/interpreter.go`, backed by an `Agent` type in `internal/agent/agent.go`, with ≥90% coverage.

**Architecture:** Two new files (`agent.go`, `interpreter.go`) plus targeted extensions to `context.go`, `machine.go`, and `validator.go`. `MachineWriter` is a new interface (separate from `WorldWriter`) for interpreter-owned table persistence. No `*sql.Tx` or `internal/storage` import in the agent package — all SQL stays in the storage layer. `Configuration` holds only atomic leaf states (not ancestor compounds), consistent with how XState v4 tracks active states.

**Tech Stack:** Go stdlib only (`fmt`, `strconv`, `strings`, `sort`, `time`). Module: `github.com/tmbritton/ecs-db`.

**Note on spec signature:** The spec writes `SendEvent(agent, event, tick, tx *sql.Tx)`, but taking a raw `*sql.Tx` forces `internal/agent` to write raw SQL and couples it to `database/sql`. Instead: `SendEvent` takes `MachineWriter` for persistence and `WorldWriter`/`WorldReader` for action/guard dispatch. This preserves the existing no-storage-import rule and enables stub-based unit tests.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/agent/context.go` | Modify | Add `MachineWriter` interface, `TransitionRecord` type |
| `internal/agent/machine.go` | Modify | Add `ContextManifest` field to `MachineDefinition` |
| `internal/agent/validator.go` | Modify | Populate `ContextManifest` after successful validation |
| `internal/agent/agent.go` | Create | `Agent` struct, `NewAgent`, `StartAgent`, shared helpers |
| `internal/agent/interpreter.go` | Create | `SendEvent` and all SCXML microstep helpers |
| `internal/agent/agent_test.go` | Create | Tests for Agent type and StartAgent |
| `internal/agent/interpreter_test.go` | Create | Tests for SendEvent edge cases |

`registry.go`, `loader.go`, `registry_test.go`, `loader_test.go`, `machine_test.go`, `validator_test.go` are unchanged.

---

## Task 1: Extend types and create Agent

**Files:**
- Modify: `internal/agent/context.go`
- Modify: `internal/agent/machine.go`
- Modify: `internal/agent/validator.go`
- Create: `internal/agent/agent_test.go`
- Create: `internal/agent/agent.go`

- [x] **Step 1: Write failing agent_test.go**

Create `internal/agent/agent_test.go`:

```go
package agent

import (
	"testing"
)

// Compile-time check.
var _ MachineWriter = (*testMachineWriter)(nil)

type testMachineWriter struct {
	savedStates     []string
	savedTransition *TransitionRecord
	scheduled       []scheduledAfter
	cancelled       []cancelledAfter
}

type scheduledAfter struct {
	entityID   int64
	machineID  string
	eventType  string
	targetTick int64
}

type cancelledAfter struct {
	entityID  int64
	machineID string
	stateIDs  []string
}

func (m *testMachineWriter) SetMachineState(entityID int64, machineID string, states []string, tick int64) error {
	m.savedStates = states
	return nil
}

func (m *testMachineWriter) AppendTransition(rec TransitionRecord) error {
	m.savedTransition = &rec
	return nil
}

func (m *testMachineWriter) ScheduleAfterEvent(entityID int64, machineID, eventType string, targetTick int64) error {
	m.scheduled = append(m.scheduled, scheduledAfter{entityID, machineID, eventType, targetTick})
	return nil
}

func (m *testMachineWriter) CancelAfterEvents(entityID int64, machineID string, stateIDs []string) error {
	m.cancelled = append(m.cancelled, cancelledAfter{entityID, machineID, stateIDs})
	return nil
}

// captureWorldWriter records AttachComponent and DetachComponent calls.
type captureWorldWriter struct {
	detached []string
	attached []attachedComp
}

type attachedComp struct {
	compName string
	values   map[string]any
}

func (w *captureWorldWriter) SpawnEntity(entityType string) (int64, error) { return 1, nil }
func (w *captureWorldWriter) DetachComponent(entityID int64, compName string) error {
	w.detached = append(w.detached, compName)
	return nil
}
func (w *captureWorldWriter) AttachComponent(entityID int64, compName string, values map[string]any) error {
	w.attached = append(w.attached, attachedComp{compName, values})
	return nil
}
func (w *captureWorldWriter) SetComponentValue(entityID int64, compName, field string, value any) error {
	return nil
}

// alwaysHasComponent is a WorldReader where HasComponent always returns true.
type alwaysHasComponent struct{}
func (r *alwaysHasComponent) GetComponentValue(int64, string, string) (any, error) { return nil, nil }
func (r *alwaysHasComponent) HasComponent(int64, string) (bool, error)              { return true, nil }

// actionFunc adapts a plain function to ActionHandler.
type actionFunc func(ActionContext) error
func (f actionFunc) Run(ctx ActionContext) error { return f(ctx) }

// ── Agent struct ──────────────────────────────────────────────────────────────

func TestNewAgent_Fields(t *testing.T) {
	def := &MachineDefinition{ID: "m", Initial: "a", States: map[string]*StateNode{}}
	a := NewAgent(def, 42, "StatusEffect")
	if a.Definition != def {
		t.Error("Definition not set")
	}
	if a.EntityID != 42 {
		t.Errorf("EntityID = %d, want 42", a.EntityID)
	}
	if a.ActivatedByComponent != "StatusEffect" {
		t.Errorf("ActivatedByComponent = %q, want StatusEffect", a.ActivatedByComponent)
	}
	if a.Configuration != nil {
		t.Error("Configuration should be nil before StartAgent")
	}
	if a.History == nil {
		t.Error("History map should be initialised")
	}
}

func TestNewAgent_EmptyActivatedBy(t *testing.T) {
	def := &MachineDefinition{ID: "m", Initial: "a", States: map[string]*StateNode{}}
	a := NewAgent(def, 1, "")
	if a.ActivatedByComponent != "" {
		t.Error("ActivatedByComponent should be empty for primary machine")
	}
}

// ── StartAgent ────────────────────────────────────────────────────────────────

func TestStartAgent_SeedsContextComponents(t *testing.T) {
	def := mustParse(t, `{
		"id":"m","initial":"idle",
		"context":{"x":0.0,"hp":100},
		"states":{"idle":{}}
	}`)
	def.ContextManifest = map[string]string{"x": "Position", "hp": "Health"}

	world := &captureWorldWriter{}
	a := NewAgent(def, 1, "")
	r := NewRegistry()
	if err := StartAgent(a, r, 0, world, &testWorldReader{}, &testMachineWriter{}); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	attached := make(map[string]bool)
	for _, att := range world.attached {
		attached[att.compName] = true
	}
	if !attached["Position"] {
		t.Error("Position not attached")
	}
	if !attached["Health"] {
		t.Error("Health not attached")
	}
}

func TestStartAgent_SkipsExistingComponents(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"idle","context":{"hp":100},"states":{"idle":{}}}`)
	def.ContextManifest = map[string]string{"hp": "Health"}

	world := &captureWorldWriter{}
	a := NewAgent(def, 1, "")
	r := NewRegistry()
	if err := StartAgent(a, r, 0, world, &alwaysHasComponent{}, &testMachineWriter{}); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	for _, att := range world.attached {
		if att.compName == "Health" {
			t.Error("Health should not be attached when entity already has it")
		}
	}
}

func TestStartAgent_SetsInitialConfiguration(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"idle","states":{"idle":{},"active":{}}}`)
	def.ContextManifest = map[string]string{}

	a := NewAgent(def, 1, "")
	r := NewRegistry()
	if err := StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, &testMachineWriter{}); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	if len(a.Configuration) != 1 {
		t.Fatalf("Configuration len = %d, want 1", len(a.Configuration))
	}
	if a.Configuration[0].ID != "m.idle" {
		t.Errorf("Configuration[0].ID = %q, want m.idle", a.Configuration[0].ID)
	}
}

func TestStartAgent_PersistsMachineState(t *testing.T) {
	def := mustParse(t, `{"id":"m","initial":"idle","states":{"idle":{}}}`)
	def.ContextManifest = map[string]string{}

	mw := &testMachineWriter{}
	a := NewAgent(def, 7, "")
	r := NewRegistry()
	if err := StartAgent(a, r, 5, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if len(mw.savedStates) == 0 {
		t.Error("SetMachineState not called")
	}
}

func TestStartAgent_RunsEntryActions(t *testing.T) {
	ran := false
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "onEnter"}, actionFunc(func(ActionContext) error {
		ran = true
		return nil
	}))

	def := mustParse(t, `{"id":"m","initial":"idle","states":{"idle":{"entry":["onEnter"]}}}`)
	def.ContextManifest = map[string]string{}

	a := NewAgent(def, 1, "")
	if err := StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, &testMachineWriter{}); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if !ran {
		t.Error("entry action not called by StartAgent")
	}
}
```

- [x] **Step 2: Run to confirm compile failure**

```
go test ./internal/agent/... -run TestAgent -v 2>&1 | head -20
```
Expected: undefined: `MachineWriter`, `TransitionRecord`, `NewAgent`, `StartAgent`.

- [x] **Step 3: Extend context.go — add MachineWriter and TransitionRecord**

Append to the end of `internal/agent/context.go`:

```go
// TransitionRecord is the data written to the transitions table after each microstep.
type TransitionRecord struct {
	Tick       int64
	WallMs     int64
	EntityID   int64
	MachineID  string
	FromStates []string
	ToStates   []string
	Event      string
	CondResult *bool    // nil = unconditional; true = guard passed; false = guard failed
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

- [x] **Step 4: Extend MachineDefinition in machine.go**

In `internal/agent/machine.go`, change `MachineDefinition` to:

```go
// MachineDefinition is the parsed in-memory representation of an XState v4 machine.
type MachineDefinition struct {
	ID              string
	Initial         string
	Context         map[string]any
	States          map[string]*StateNode
	ContextManifest map[string]string // field → component name; populated by ValidateMachine
}
```

- [x] **Step 5: Update ValidateMachine to populate ContextManifest**

In `internal/agent/validator.go`, in `ValidateMachine`, add these lines just before `return errs`:

```go
	manifest := make(map[string]string, len(def.Context))
	for key := range def.Context {
		if comps := fieldIndex[key]; len(comps) == 1 {
			manifest[key] = comps[0]
		}
	}
	def.ContextManifest = manifest
```

- [x] **Step 6: Create agent.go**

Create `internal/agent/agent.go`:

```go
package agent

import (
	"fmt"
	"strconv"
	"strings"
)

// Agent is a running instance of a MachineDefinition bound to a specific entity.
// Configuration holds the currently active atomic (leaf) states only — never
// ancestor compound or parallel nodes.
type Agent struct {
	Definition           *MachineDefinition
	Configuration        []*StateNode            // active atomic (leaf) states
	EntityID             int64
	History              map[string][]*StateNode // history node ID → recorded atomic snapshot
	ActivatedByComponent string                  // non-empty if activated via AttachComponent
}

// NewAgent returns an Agent with no active configuration.
// Call StartAgent once before delivering events via SendEvent.
func NewAgent(def *MachineDefinition, entityID int64, activatedByComponent string) *Agent {
	return &Agent{
		Definition:           def,
		EntityID:             entityID,
		History:              make(map[string][]*StateNode),
		ActivatedByComponent: activatedByComponent,
	}
}

// StartAgent performs machine startup:
//  1. Seeds each context-declared component missing from the entity.
//  2. Enters the initial state tree (root→leaf), running entry actions.
//  3. Schedules any after-transitions for entered states.
//  4. Persists the initial configuration to behavior_components.
func StartAgent(agent *Agent, registry *Registry, tick int64, world WorldWriter, reader WorldReader, mw MachineWriter) error {
	def := agent.Definition

	// Group context fields by component, then attach missing ones.
	compValues := make(map[string]map[string]any)
	for field, initVal := range def.Context {
		compName := def.ContextManifest[field]
		if compName == "" {
			continue
		}
		if compValues[compName] == nil {
			compValues[compName] = make(map[string]any)
		}
		compValues[compName][field] = initVal
	}
	for compName, values := range compValues {
		has, err := reader.HasComponent(agent.EntityID, compName)
		if err != nil {
			return fmt.Errorf("StartAgent: checking %q: %w", compName, err)
		}
		if !has {
			if err := world.AttachComponent(agent.EntityID, compName, values); err != nil {
				return fmt.Errorf("StartAgent: attaching %q: %w", compName, err)
			}
		}
	}

	// Enter initial state tree.
	entered := expandEntry(def.States[def.Initial])
	initEvent := Event{Type: "xstate.init"}
	for _, state := range entered {
		if state.Type == StateTypeHistory {
			continue
		}
		if _, err := runActionList(state.Entry, ActionContext{
			EntityID: agent.EntityID, Tick: tick, World: world, Event: initEvent,
		}, registry); err != nil {
			return fmt.Errorf("StartAgent: entry actions for %q: %w", state.ID, err)
		}
		for duration := range state.After {
			targetTick := tick + parseDurationTicks(duration)
			evType := afterEventType(duration, state.ID)
			if err := mw.ScheduleAfterEvent(agent.EntityID, def.ID, evType, targetTick); err != nil {
				return fmt.Errorf("StartAgent: scheduling after for %q: %w", state.ID, err)
			}
		}
	}

	agent.Configuration = atomicStates(entered)
	return mw.SetMachineState(agent.EntityID, def.ID, nodeIDs(agent.Configuration), tick)
}

// ── Helpers shared by agent.go and interpreter.go ────────────────────────────

// expandEntry returns states to enter (root→leaf) when targeting node.
// Compound: enters node then recurses into its initial child.
// Parallel: enters node then all children.
func expandEntry(node *StateNode) []*StateNode {
	if node == nil {
		return nil
	}
	result := []*StateNode{node}
	switch node.Type {
	case StateTypeCompound:
		if node.Initial != "" {
			result = append(result, expandEntry(node.Children[node.Initial])...)
		}
	case StateTypeParallel:
		for _, child := range node.Children {
			result = append(result, expandEntry(child)...)
		}
	}
	return result
}

// atomicStates filters to atomic and final nodes only.
func atomicStates(nodes []*StateNode) []*StateNode {
	var out []*StateNode
	for _, n := range nodes {
		if n.Type == StateTypeAtomic || n.Type == StateTypeFinal {
			out = append(out, n)
		}
	}
	return out
}

// nodeIDs returns the ID of each node.
func nodeIDs(nodes []*StateNode) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

// nodeDepth counts how many ancestors a node has (root = 0).
func nodeDepth(n *StateNode) int {
	depth := 0
	for p := n.Parent; p != nil; p = p.Parent {
		depth++
	}
	return depth
}

// isDescendant reports whether s is equal to ancestor or a descendant of it.
func isDescendant(s, ancestor *StateNode) bool {
	for cur := s; cur != nil; cur = cur.Parent {
		if cur == ancestor {
			return true
		}
	}
	return false
}

// runActionList dispatches each action through the registry.
// Returns names of actions that ran (for transitions.actions_run).
func runActionList(actions []ActionSpec, ctx ActionContext, registry *Registry) ([]string, error) {
	var ran []string
	for _, spec := range actions {
		handler, ok := registry.GetAction(spec.Type)
		if !ok {
			continue
		}
		ctx.Params = spec.Params
		if err := handler.Run(ctx); err != nil {
			return ran, fmt.Errorf("action %q: %w", spec.Type, err)
		}
		ran = append(ran, spec.Type)
	}
	return ran, nil
}

// afterEventType returns the synthetic event type for an after-transition.
// Format matches XState v4: xstate.after(N).STATE_ID
func afterEventType(duration, stateID string) string {
	return "xstate.after(" + duration + ")." + stateID
}

// parseDurationTicks converts an after-duration string to a tick count.
// Treats the value as integer milliseconds (1 ms = 1 tick for Story 5).
// Story 6 extends this with proper duration parsing.
func parseDurationTicks(duration string) int64 {
	s := strings.TrimSuffix(duration, "ms")
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
```

- [x] **Step 7: Run tests**

```
go test ./internal/agent/... -run "TestNewAgent|TestStartAgent" -v
```
Expected: all pass.

- [x] **Step 8: Full regression check**

```
go test ./...
```
Expected: all pass.

- [x] **Step 9: Commit**

```bash
git add internal/agent/context.go internal/agent/machine.go internal/agent/validator.go \
        internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(epic-3/story-5): add Agent type, MachineWriter interface, ContextManifest"
```

---

## Task 2: Implement SendEvent (interpreter.go)

**Files:**
- Create: `internal/agent/interpreter_test.go`
- Create: `internal/agent/interpreter.go`

- [x] **Step 1: Write failing interpreter_test.go**

Create `internal/agent/interpreter_test.go`:

```go
package agent

import (
	"testing"
)

// callCountingAction counts invocations.
type callCountingAction struct{ count int }
func (a *callCountingAction) Run(ActionContext) error { a.count++; return nil }

// recordingGuard returns a fixed result and records calls.
type recordingGuard struct{ result bool; calls int }
func (g *recordingGuard) Evaluate(GuardContext) bool { g.calls++; return g.result }

// interpreterRegistry has a standard set of test handlers.
func interpreterRegistry() *Registry {
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "onEnter"}, &callCountingAction{})
	r.RegisterAction(ActionMeta{Name: "onExit"}, &callCountingAction{})
	r.RegisterAction(ActionMeta{Name: "doWork"}, &callCountingAction{})
	r.RegisterGuard(GuardMeta{Name: "alwaysTrue"}, &recordingGuard{result: true})
	r.RegisterGuard(GuardMeta{Name: "alwaysFalse"}, &recordingGuard{result: false})
	return r
}

// startedAgent parses a machine JSON, calls StartAgent, and returns all handles.
func startedAgent(t *testing.T, json string, entityID int64) (*Agent, *Registry, *captureWorldWriter, *testMachineWriter) {
	t.Helper()
	def := mustParse(t, json)
	def.ContextManifest = map[string]string{}
	r := interpreterRegistry()
	world := &captureWorldWriter{}
	mw := &testMachineWriter{}
	a := NewAgent(def, entityID, "")
	if err := StartAgent(a, r, 0, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	return a, r, world, mw
}

// send is a helper that calls SendEvent and fails the test on error.
func send(t *testing.T, a *Agent, ev string, r *Registry, world WorldWriter, mw *testMachineWriter) {
	t.Helper()
	if err := SendEvent(a, Event{Type: ev}, 1, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent(%q): %v", ev, err)
	}
}

// ── Flat machine basics ───────────────────────────────────────────────────────

func TestSendEvent_UnconditionalTransition(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"GO":"b"}},"b":{}}
	}`, 1)
	mw.savedStates = nil
	send(t, a, "GO", r, world, mw)

	if len(a.Configuration) != 1 || a.Configuration[0].ID != "m.b" {
		t.Errorf("config = %v, want [m.b]", nodeIDs(a.Configuration))
	}
	if len(mw.savedStates) == 0 || mw.savedStates[0] != "m.b" {
		t.Errorf("savedStates = %v, want [m.b]", mw.savedStates)
	}
}

func TestSendEvent_UnknownEvent_NoTransition(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"GO":"b"}},"b":{}}
	}`, 1)
	mw.savedStates = nil
	send(t, a, "NOPE", r, world, mw)

	if a.Configuration[0].ID != "m.a" {
		t.Errorf("config = %v, want [m.a]", nodeIDs(a.Configuration))
	}
	if mw.savedStates != nil {
		t.Error("SetMachineState should not be called when no transition fires")
	}
}

func TestSendEvent_GuardTrue_Transitions(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"E":[{"target":"b","cond":"alwaysTrue"}]}},"b":{}}
	}`, 1)
	send(t, a, "E", r, world, mw)
	if a.Configuration[0].ID != "m.b" {
		t.Errorf("config = %v, want [m.b]", nodeIDs(a.Configuration))
	}
}

func TestSendEvent_GuardFalse_NoTransition(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"E":[{"target":"b","cond":"alwaysFalse"}]}},"b":{}}
	}`, 1)
	send(t, a, "E", r, world, mw)
	if a.Configuration[0].ID != "m.a" {
		t.Errorf("config = %v, want [m.a]", nodeIDs(a.Configuration))
	}
}

func TestSendEvent_EntryExitActionsOrder(t *testing.T) {
	order := []string{}
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "exitA"}, actionFunc(func(ActionContext) error {
		order = append(order, "exitA"); return nil
	}))
	r.RegisterAction(ActionMeta{Name: "enterB"}, actionFunc(func(ActionContext) error {
		order = append(order, "enterB"); return nil
	}))

	def := mustParse(t, `{
		"id":"m","initial":"a",
		"states":{
			"a":{"exit":["exitA"],"on":{"GO":"b"}},
			"b":{"entry":["enterB"]}
		}
	}`)
	def.ContextManifest = map[string]string{}
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "GO"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(order) != 2 || order[0] != "exitA" || order[1] != "enterB" {
		t.Errorf("order = %v, want [exitA, enterB]", order)
	}
}

func TestSendEvent_TransitionActions(t *testing.T) {
	ran := 0
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "doWork"}, actionFunc(func(ActionContext) error { ran++; return nil }))

	def := mustParse(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"E":[{"actions":["doWork"]}]}}}
	}`)
	def.ContextManifest = map[string]string{}
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "E"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if ran != 1 {
		t.Errorf("doWork ran %d times, want 1", ran)
	}
}

func TestSendEvent_SelfTransition(t *testing.T) {
	exitCount, enterCount := 0, 0
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "onExit"}, actionFunc(func(ActionContext) error { exitCount++; return nil }))
	r.RegisterAction(ActionMeta{Name: "onEnter"}, actionFunc(func(ActionContext) error { enterCount++; return nil }))

	def := mustParse(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"entry":["onEnter"],"exit":["onExit"],"on":{"LOOP":"a"}}}
	}`)
	def.ContextManifest = map[string]string{}
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)
	enterCount = 0 // reset; StartAgent fires entry once

	if err := SendEvent(a, Event{Type: "LOOP"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if exitCount != 1 || enterCount != 1 {
		t.Errorf("self-transition: exit=%d enter=%d, want 1 each", exitCount, enterCount)
	}
}

// ── Persistence ───────────────────────────────────────────────────────────────

func TestSendEvent_AppendTransition_Recorded(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{"id":"m","initial":"a","states":{"a":{"on":{"GO":"b"}},"b":{}}}`, 1)
	mw.savedTransition = nil
	if err := SendEvent(a, Event{Type: "GO"}, 5, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	rec := mw.savedTransition
	if rec == nil {
		t.Fatal("AppendTransition not called")
	}
	if rec.Tick != 5 {
		t.Errorf("Tick = %d, want 5", rec.Tick)
	}
	if rec.Event != "GO" {
		t.Errorf("Event = %q, want GO", rec.Event)
	}
	if len(rec.FromStates) == 0 || rec.FromStates[0] != "m.a" {
		t.Errorf("FromStates = %v, want [m.a]", rec.FromStates)
	}
	if len(rec.ToStates) == 0 || rec.ToStates[0] != "m.b" {
		t.Errorf("ToStates = %v, want [m.b]", rec.ToStates)
	}
	if rec.CondResult != nil {
		t.Error("unconditional transition: CondResult should be nil")
	}
}

func TestSendEvent_CondResult_GuardTrue(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"E":[{"target":"b","cond":"alwaysTrue"}]}},"b":{}}
	}`, 1)
	mw.savedTransition = nil
	_ = SendEvent(a, Event{Type: "E"}, 1, r, world, &testWorldReader{}, mw)
	if mw.savedTransition == nil || mw.savedTransition.CondResult == nil || !*mw.savedTransition.CondResult {
		t.Errorf("CondResult should be &true")
	}
}

// ── Compound state ────────────────────────────────────────────────────────────

func TestSendEvent_CompoundInitialResolution(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"outer",
		"states":{
			"outer":{"type":"compound","initial":"inner","states":{"inner":{"on":{"GO":"done"}}}},
			"done":{}
		}
	}`, 1)

	if len(a.Configuration) != 1 || a.Configuration[0].ID != "m.inner" {
		t.Fatalf("initial config = %v, want [m.inner]", nodeIDs(a.Configuration))
	}
	send(t, a, "GO", r, world, mw)
	if a.Configuration[0].ID != "m.done" {
		t.Errorf("after GO: config = %v, want [m.done]", nodeIDs(a.Configuration))
	}
}

// ── Depth preemption ──────────────────────────────────────────────────────────

func TestSendEvent_DepthPreemption(t *testing.T) {
	outerRan, innerRan := false, false
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "outerAction"}, actionFunc(func(ActionContext) error { outerRan = true; return nil }))
	r.RegisterAction(ActionMeta{Name: "innerAction"}, actionFunc(func(ActionContext) error { innerRan = true; return nil }))

	def := mustParse(t, `{
		"id":"m","initial":"outer",
		"states":{
			"outer":{
				"type":"compound","initial":"inner",
				"on":{"E":[{"actions":["outerAction"]}]},
				"states":{"inner":{"on":{"E":[{"actions":["innerAction"]}]}}}
			}
		}
	}`)
	def.ContextManifest = map[string]string{}
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "E"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if !innerRan {
		t.Error("inner action should have run")
	}
	if outerRan {
		t.Error("outer action should be preempted by inner")
	}
}

// ── Parallel regions ──────────────────────────────────────────────────────────

func TestSendEvent_ParallelRegions_BothTransition(t *testing.T) {
	leftRan, rightRan := 0, 0
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "leftAction"}, actionFunc(func(ActionContext) error { leftRan++; return nil }))
	r.RegisterAction(ActionMeta{Name: "rightAction"}, actionFunc(func(ActionContext) error { rightRan++; return nil }))

	def := mustParse(t, `{
		"id":"m","initial":"p",
		"states":{
			"p":{
				"type":"parallel",
				"states":{
					"left":{"on":{"E":[{"actions":["leftAction"]}]}},
					"right":{"on":{"E":[{"actions":["rightAction"]}]}}
				}
			}
		}
	}`)
	def.ContextManifest = map[string]string{}
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	if len(a.Configuration) != 2 {
		t.Fatalf("initial config len = %d, want 2", len(a.Configuration))
	}
	if err := SendEvent(a, Event{Type: "E"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if leftRan != 1 {
		t.Errorf("leftAction ran %d times, want 1", leftRan)
	}
	if rightRan != 1 {
		t.Errorf("rightAction ran %d times, want 1", rightRan)
	}
}

// ── History ───────────────────────────────────────────────────────────────────

func TestSendEvent_HistoryShallow_RestoresRecordedState(t *testing.T) {
	// c has children h (history), s1, s2. Navigate s1→s2, exit to done, re-enter via history.
	def := mustParse(t, `{
		"id":"m","initial":"c",
		"states":{
			"c":{
				"type":"compound","initial":"s1",
				"on":{"BACK":"c.h"},
				"states":{
					"h":{"type":"history"},
					"s1":{"on":{"NEXT":"s2"}},
					"s2":{"on":{"OUT":"done"}}
				}
			},
			"done":{"on":{"BACK":"c.h"}}
		}
	}`)
	def.ContextManifest = map[string]string{}
	r := interpreterRegistry()
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	doSend := func(ev string) {
		t.Helper()
		if err := SendEvent(a, Event{Type: ev}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
			t.Fatalf("SendEvent(%q): %v", ev, err)
		}
	}

	doSend("NEXT") // c.s1 → c.s2
	doSend("OUT")  // c.s2 → done; records s2 in history
	doSend("BACK") // done → c.h → restores c.s2

	if len(a.Configuration) == 0 || a.Configuration[0].ID != "m.s2" {
		t.Errorf("history restore: config = %v, want [m.s2]", nodeIDs(a.Configuration))
	}
}

func TestSendEvent_HistoryShallow_DefaultTargetWhenNoHistory(t *testing.T) {
	def := mustParse(t, `{
		"id":"m","initial":"done",
		"states":{
			"c":{
				"type":"compound","initial":"s1",
				"states":{
					"h":{"type":"history","target":"s1"},
					"s1":{}
				}
			},
			"done":{"on":{"BACK":"c.h"}}
		}
	}`)
	def.ContextManifest = map[string]string{}
	r := interpreterRegistry()
	a := NewAgent(def, 1, "")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, &captureWorldWriter{}, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "BACK"}, 1, r, &captureWorldWriter{}, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(a.Configuration) == 0 || a.Configuration[0].ID != "m.s1" {
		t.Errorf("default history: config = %v, want [m.s1]", nodeIDs(a.Configuration))
	}
}

// ── Final state lifecycle ─────────────────────────────────────────────────────

func TestSendEvent_FinalState_DetachesActivatingComponent(t *testing.T) {
	def := mustParse(t, `{
		"id":"m","initial":"active",
		"states":{"active":{"on":{"DONE":"finished"}},"finished":{"type":"final"}}
	}`)
	def.ContextManifest = map[string]string{}
	r := interpreterRegistry()
	world := &captureWorldWriter{}
	a := NewAgent(def, 1, "StatusBuff")
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, world, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "DONE"}, 1, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(world.detached) == 0 || world.detached[0] != "StatusBuff" {
		t.Errorf("detached = %v, want [StatusBuff]", world.detached)
	}
}

func TestSendEvent_FinalState_NoPrimaryDetach(t *testing.T) {
	def := mustParse(t, `{
		"id":"m","initial":"active",
		"states":{"active":{"on":{"DONE":"finished"}},"finished":{"type":"final"}}
	}`)
	def.ContextManifest = map[string]string{}
	r := interpreterRegistry()
	world := &captureWorldWriter{}
	a := NewAgent(def, 1, "") // primary machine — no component to detach
	mw := &testMachineWriter{}
	_ = StartAgent(a, r, 0, world, &testWorldReader{}, mw)

	if err := SendEvent(a, Event{Type: "DONE"}, 1, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(world.detached) != 0 {
		t.Errorf("primary machine should not detach, got %v", world.detached)
	}
}

// ── After scheduling / cancellation ──────────────────────────────────────────

func TestSendEvent_AfterEntry_Scheduled(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"on":{"GO":"b"}},"b":{"after":{"500":"a"}}}
	}`, 1)
	mw.scheduled = nil
	if err := SendEvent(a, Event{Type: "GO"}, 10, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(mw.scheduled) == 0 {
		t.Error("ScheduleAfterEvent not called on entry to b")
	}
	if mw.scheduled[0].targetTick != 510 {
		t.Errorf("targetTick = %d, want 510 (10+500)", mw.scheduled[0].targetTick)
	}
}

func TestSendEvent_AfterExit_Cancelled(t *testing.T) {
	a, r, world, mw := startedAgent(t, `{
		"id":"m","initial":"a",
		"states":{"a":{"after":{"200":"b"},"on":{"GO":"b"}},"b":{}}
	}`, 1)
	mw.cancelled = nil
	if err := SendEvent(a, Event{Type: "GO"}, 1, r, world, &testWorldReader{}, mw); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if len(mw.cancelled) == 0 {
		t.Error("CancelAfterEvents not called on exit from a")
	}
}
```

- [x] **Step 2: Run to confirm compile failure**

```
go test ./internal/agent/... -run TestSendEvent -v 2>&1 | head -10
```
Expected: undefined: `SendEvent`.

- [x] **Step 3: Create interpreter.go**

Create `internal/agent/interpreter.go`:

```go
package agent

import (
	"fmt"
	"sort"
	"time"
)

// selectedTransition pairs a source StateNode with the chosen transition.
type selectedTransition struct {
	Source     *StateNode
	Transition Transition
	CondResult *bool // nil = unconditional
}

// SendEvent runs the SCXML microstep algorithm for one event delivery.
// Returns nil immediately if no eligible transition exists.
// All state mutations (world and machine) happen through the provided interfaces;
// no SQL is written inside this function.
func SendEvent(agent *Agent, event Event, tick int64, registry *Registry, world WorldWriter, reader WorldReader, mw MachineWriter) error {
	fromStates := nodeIDs(agent.Configuration)

	// 1. Select eligible transitions.
	transitions, err := selectEligibleTransitions(agent, event, registry, reader, tick)
	if err != nil {
		return fmt.Errorf("SendEvent: %w", err)
	}
	if len(transitions) == 0 {
		return nil
	}

	// 2. Compute exit set (unordered).
	exitSet := computeExitSet(agent.Configuration, transitions)

	// 3. Record history before any state exits.
	recordHistoryNodes(agent, exitSet)

	// 4. Exit actions + cancel after events (leaf→root).
	actionsRun := []string{}
	for _, state := range sortByDepthDesc(exitSet) {
		if err := mw.CancelAfterEvents(agent.EntityID, agent.Definition.ID, []string{state.ID}); err != nil {
			return fmt.Errorf("SendEvent: cancel after for %q: %w", state.ID, err)
		}
		ran, err := runActionList(state.Exit, ActionContext{
			EntityID: agent.EntityID, Tick: tick, World: world, Event: event,
		}, registry)
		if err != nil {
			return fmt.Errorf("SendEvent: exit actions for %q: %w", state.ID, err)
		}
		actionsRun = append(actionsRun, ran...)
	}

	// 5. Transition actions + capture cond result.
	var condResult *bool
	for _, sel := range transitions {
		condResult = sel.CondResult
		ran, err := runActionList(sel.Transition.Actions, ActionContext{
			EntityID: agent.EntityID, Tick: tick, World: world, Event: event,
		}, registry)
		if err != nil {
			return fmt.Errorf("SendEvent: transition actions: %w", err)
		}
		actionsRun = append(actionsRun, ran...)
	}

	// 6. Compute entry set.
	entrySet := computeEntrySet(agent.Definition, transitions, agent.History)

	// 7. Entry actions + schedule after events (root→leaf).
	for _, state := range sortByDepthAsc(entrySet) {
		if state.Type == StateTypeHistory {
			continue
		}
		ran, err := runActionList(state.Entry, ActionContext{
			EntityID: agent.EntityID, Tick: tick, World: world, Event: event,
		}, registry)
		if err != nil {
			return fmt.Errorf("SendEvent: entry actions for %q: %w", state.ID, err)
		}
		actionsRun = append(actionsRun, ran...)
		for duration := range state.After {
			targetTick := tick + parseDurationTicks(duration)
			if err := mw.ScheduleAfterEvent(agent.EntityID, agent.Definition.ID, afterEventType(duration, state.ID), targetTick); err != nil {
				return fmt.Errorf("SendEvent: schedule after for %q: %w", state.ID, err)
			}
		}
	}

	// 8. Final-state lifecycle.
	for _, state := range entrySet {
		if state.Type == StateTypeFinal && agent.ActivatedByComponent != "" {
			if err := world.DetachComponent(agent.EntityID, agent.ActivatedByComponent); err != nil {
				return fmt.Errorf("SendEvent: final detach: %w", err)
			}
			break
		}
	}

	// 9. Update configuration.
	agent.Configuration = atomicStates(entrySet)

	// 10. Persist.
	toStates := nodeIDs(agent.Configuration)
	if err := mw.SetMachineState(agent.EntityID, agent.Definition.ID, toStates, tick); err != nil {
		return fmt.Errorf("SendEvent: SetMachineState: %w", err)
	}
	return mw.AppendTransition(TransitionRecord{
		Tick:       tick,
		WallMs:     time.Now().UnixMilli(),
		EntityID:   agent.EntityID,
		MachineID:  agent.Definition.ID,
		FromStates: fromStates,
		ToStates:   toStates,
		Event:      event.Type,
		CondResult: condResult,
		ActionsRun: actionsRun,
	})
}

// ── Transition selection ──────────────────────────────────────────────────────

func selectEligibleTransitions(agent *Agent, event Event, registry *Registry, reader WorldReader, tick int64) ([]selectedTransition, error) {
	var selected []selectedTransition
	handled := make(map[*StateNode]bool)

	for _, atom := range sortByDepthDesc(agent.Configuration) {
		if handled[atom] {
			continue
		}
		for cur := atom; cur != nil; cur = cur.Parent {
			if cur.Type == StateTypeParallel {
				break // each parallel region selects independently
			}
			// Check event transitions then after-event transitions.
			var candidates []Transition
			if ts, ok := cur.On[event.Type]; ok {
				candidates = ts
			} else if ts, ok := cur.After[event.Type]; ok {
				candidates = ts
			}
			found := false
			for _, t := range candidates {
				eligible, condResult, err := evaluateTransition(t, agent.EntityID, tick, event, registry, reader)
				if err != nil {
					return nil, err
				}
				if eligible {
					selected = append(selected, selectedTransition{Source: cur, Transition: t, CondResult: condResult})
					for mark := atom; mark != cur.Parent; mark = mark.Parent {
						handled[mark] = true
					}
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	return selected, nil
}

func evaluateTransition(t Transition, entityID, tick int64, event Event, registry *Registry, reader WorldReader) (eligible bool, condResult *bool, err error) {
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
	})
	b := result
	return result, &b, nil
}

// ── Exit set ──────────────────────────────────────────────────────────────────

func computeExitSet(config []*StateNode, transitions []selectedTransition) []*StateNode {
	exit := make(map[*StateNode]bool)
	for _, sel := range transitions {
		for _, active := range config {
			if isDescendant(active, sel.Source) {
				for cur := active; cur != sel.Source.Parent; cur = cur.Parent {
					exit[cur] = true
				}
			}
		}
		exit[sel.Source] = true
	}
	result := make([]*StateNode, 0, len(exit))
	for n := range exit {
		result = append(result, n)
	}
	return result
}

// ── History recording ─────────────────────────────────────────────────────────

func recordHistoryNodes(agent *Agent, exitSet []*StateNode) {
	exiting := make(map[*StateNode]bool, len(exitSet))
	for _, n := range exitSet {
		exiting[n] = true
	}
	for _, state := range exitSet {
		if state.Type != StateTypeCompound && state.Type != StateTypeParallel {
			continue
		}
		for _, child := range state.Children {
			if child.Type != StateTypeHistory {
				continue
			}
			var snapshot []*StateNode
			for _, active := range agent.Configuration {
				if child.History == "shallow" {
					if active.Parent == state {
						snapshot = append(snapshot, active)
					}
				} else {
					if isDescendant(active, state) {
						snapshot = append(snapshot, active)
					}
				}
			}
			agent.History[child.ID] = snapshot
		}
	}
}

// ── Entry set ─────────────────────────────────────────────────────────────────

func computeEntrySet(def *MachineDefinition, transitions []selectedTransition, history map[string][]*StateNode) []*StateNode {
	seen := make(map[*StateNode]bool)
	var result []*StateNode

	for _, sel := range transitions {
		target := resolveTarget(sel.Transition.Target, def)
		if target == nil {
			continue // internal transition
		}
		for _, n := range expandEntryWithHistory(target, history, def) {
			if !seen[n] {
				seen[n] = true
				result = append(result, n)
			}
		}
	}
	return result
}

func resolveTarget(target string, def *MachineDefinition) *StateNode {
	if target == "" {
		return nil
	}
	return findState(def.States, target)
}

func findState(states map[string]*StateNode, target string) *StateNode {
	for name, node := range states {
		if name == target || node.ID == target {
			return node
		}
		if found := findState(node.Children, target); found != nil {
			return found
		}
	}
	return nil
}

func expandEntryWithHistory(node *StateNode, history map[string][]*StateNode, def *MachineDefinition) []*StateNode {
	if node.Type == StateTypeHistory {
		if recorded, ok := history[node.ID]; ok && len(recorded) > 0 {
			var result []*StateNode
			for _, s := range recorded {
				result = append(result, expandEntryWithHistory(s, history, def)...)
			}
			return result
		}
		if node.Target != "" {
			if t := resolveTarget(node.Target, def); t != nil {
				return expandEntryWithHistory(t, history, def)
			}
		}
		return nil
	}
	result := []*StateNode{node}
	switch node.Type {
	case StateTypeCompound:
		if node.Initial != "" {
			if child := node.Children[node.Initial]; child != nil {
				result = append(result, expandEntryWithHistory(child, history, def)...)
			}
		}
	case StateTypeParallel:
		for _, child := range node.Children {
			result = append(result, expandEntryWithHistory(child, history, def)...)
		}
	}
	return result
}

// ── Sort helpers ──────────────────────────────────────────────────────────────

func sortByDepthDesc(nodes []*StateNode) []*StateNode {
	out := make([]*StateNode, len(nodes))
	copy(out, nodes)
	sort.Slice(out, func(i, j int) bool { return nodeDepth(out[i]) > nodeDepth(out[j]) })
	return out
}

func sortByDepthAsc(nodes []*StateNode) []*StateNode {
	out := make([]*StateNode, len(nodes))
	copy(out, nodes)
	sort.Slice(out, func(i, j int) bool { return nodeDepth(out[i]) < nodeDepth(out[j]) })
	return out
}
```

- [x] **Step 4: Run tests and check coverage**

```
go test ./internal/agent/... -run "TestSendEvent|TestNewAgent|TestStartAgent" -v \
    -coverprofile=coverage.out && \
go tool cover -func=coverage.out | grep -E "interpreter\.go|agent\.go"
```
Expected: all tests pass; both files ≥90% coverage.

- [x] **Step 5: Full regression check**

```
go test ./...
```
Expected: all pass.

- [x] **Step 6: Commit**

```bash
git add internal/agent/interpreter.go internal/agent/interpreter_test.go
git commit -m "feat(epic-3/story-5): implement SCXML microstep interpreter"
```

---

## Verification

```bash
# Full suite
go test ./...

# Coverage
go test ./internal/agent/... -coverprofile=coverage.out
go tool cover -func=coverage.out | grep -E "interpreter\.go|agent\.go"
# Both must show ≥90%

# Dependency guard
grep -r "internal/storage" internal/agent/
# Expected: no output
```

### Acceptance Criteria Traceability

| Criterion | File | Location |
|-----------|------|----------|
| `Agent` type with required fields | `agent.go` | `Agent` struct |
| `SendEvent` microstep | `interpreter.go` | `SendEvent` |
| Eligible transition selection deepest-first | `interpreter.go` | `selectEligibleTransitions` |
| Guard evaluation via WorldReader | `interpreter.go` | `evaluateTransition` |
| Depth preemption | `interpreter.go` | `handled` map + ancestor walk |
| Parallel regions independent | `interpreter.go` | parallel boundary break |
| Exit set leaf→root | `interpreter.go` | `computeExitSet` + `sortByDepthDesc` |
| Cancel after on exit | `interpreter.go` | `mw.CancelAfterEvents` in exit loop |
| Exit actions | `interpreter.go` | exit loop |
| Transition actions | `interpreter.go` | transition action loop |
| Entry set root→leaf | `interpreter.go` | `computeEntrySet` + `sortByDepthAsc` |
| Compound initial resolution | `agent.go` | `expandEntry` |
| Parallel all-children entry | `agent.go` | `expandEntry` |
| History restoration / default | `interpreter.go` | `expandEntryWithHistory` |
| History recording on exit | `interpreter.go` | `recordHistoryNodes` |
| Entry actions + after scheduling | `interpreter.go` | entry loop |
| Final state → component detach | `interpreter.go` | final-state lifecycle block |
| Machine startup — seed context | `agent.go` | `StartAgent` |
| Persist behavior_components | `interpreter.go` | `mw.SetMachineState` |
| Persist transitions row | `interpreter.go` | `mw.AppendTransition` |
| No raw SQL / no storage import | `interpreter.go` | no `sql` import anywhere |
| ≥90% coverage | `interpreter_test.go` | all test functions |
