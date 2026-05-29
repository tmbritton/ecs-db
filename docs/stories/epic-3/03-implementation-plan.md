# Registry and Context Types Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the action/guard registry and domain context types to `internal/agent` — the dispatch layer between machine JSON (which names actions/guards as strings) and registered Go handler implementations.

**Architecture:** Two new files in `internal/agent/`: `context.go` defines the domain interfaces (`WorldWriter`, `WorldReader`) and value types (`Event`, `ActionContext`, `GuardContext`); `registry.go` defines the `ActionHandler`/`GuardHandler` interfaces, metadata types (`ActionMeta`, `GuardMeta`, `ParamSchema`), and the `Registry` type. The `internal/agent` package must never import `internal/storage/` — the storage package implements these interfaces, not the other way around.

**Tech Stack:** Go standard library only (`fmt`, `sort`). No new dependencies.

---

## File Map

| File | Status | Responsibility |
|------|--------|----------------|
| `internal/agent/context.go` | Create | `Event`, `WorldReader`, `WorldWriter`, `ActionContext`, `GuardContext` |
| `internal/agent/registry.go` | Create | `ParamSchema`, `ActionMeta`, `GuardMeta`, `ActionHandler`, `GuardHandler`, `Registry` |
| `internal/agent/registry_test.go` | Create | 100% coverage of registry logic; compile-time interface checks |

`internal/agent/machine.go` and `machine_test.go` are unchanged.

---

### Task 1: context.go — domain interfaces and context value types

**Files:**
- Create: `internal/agent/context.go`
- Create: `internal/agent/registry_test.go` (compile-time interface check only in this task)

- [ ] **Step 1: Write a failing compile-check test**

Create `internal/agent/registry_test.go`:

```go
package agent

import "testing"

// Compile-time interface satisfaction checks. These lines fail to compile if
// any method signature is wrong — catching drift before runtime.
var _ ActionHandler = (*testActionHandler)(nil)
var _ GuardHandler = (*testGuardHandler)(nil)
var _ WorldWriter = (*testWorldWriter)(nil)
var _ WorldReader = (*testWorldReader)(nil)

// testActionHandler is a test double for ActionHandler.
type testActionHandler struct{ runErr error }

func (h *testActionHandler) Run(ActionContext) error { return h.runErr }

// testGuardHandler is a test double for GuardHandler.
type testGuardHandler struct{ result bool }

func (h *testGuardHandler) Evaluate(GuardContext) bool { return h.result }

// testWorldWriter is a test double for WorldWriter.
type testWorldWriter struct{}

func (w *testWorldWriter) SpawnEntity(entityType string) (int64, error) { return 1, nil }
func (w *testWorldWriter) AttachComponent(entityID int64, compName string, values map[string]any) error {
	return nil
}
func (w *testWorldWriter) DetachComponent(entityID int64, compName string) error { return nil }
func (w *testWorldWriter) SetComponentValue(entityID int64, compName, field string, value any) error {
	return nil
}

// testWorldReader is a test double for WorldReader.
type testWorldReader struct{}

func (r *testWorldReader) GetComponentValue(entityID int64, compName, field string) (any, error) {
	return nil, nil
}
func (r *testWorldReader) HasComponent(entityID int64, compName string) (bool, error) {
	return false, nil
}

func TestContextTypes_Compile(t *testing.T) {
	ac := ActionContext{
		EntityID: 1,
		Tick:     10,
		World:    &testWorldWriter{},
		Params:   map[string]any{"k": "v"},
		Event:    Event{Type: "TEST", Payload: map[string]any{"x": 1}},
	}
	gc := GuardContext{
		EntityID: 1,
		Tick:     10,
		World:    &testWorldReader{},
		Params:   map[string]any{},
		Event:    Event{Type: "TEST"},
	}
	_ = ac
	_ = gc
}
```

- [ ] **Step 2: Run test to confirm it fails**

```
go test ./internal/agent/... -run TestContextTypes_Compile -v
```
Expected: compile error — `ActionContext`, `GuardContext`, `Event`, `WorldWriter`, `WorldReader`, `ActionHandler`, `GuardHandler` undefined.

- [ ] **Step 3: Implement context.go**

Create `internal/agent/context.go`:

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
	// SetComponentValue writes a single field on an existing component row.
	// Actions that need to update multiple fields call it once per field.
	SetComponentValue(entityID int64, compName, field string, value any) error
}

// WorldReader is the read-side interface that guards use to inspect world state.
// The concrete implementation (backed by *sql.DB) lives in internal/storage.
type WorldReader interface {
	GetComponentValue(entityID int64, compName, field string) (any, error)
	HasComponent(entityID int64, compName string) (bool, error)
}

// ActionContext is passed to ActionHandler.Run. World is write-capable because
// actions are side-effecting by definition.
type ActionContext struct {
	EntityID int64
	Tick     int64
	World    WorldWriter
	Params   map[string]any // static params from the machine JSON action spec
	Event    Event
}

// GuardContext is passed to GuardHandler.Evaluate. World is read-only because
// guards must not produce side effects.
type GuardContext struct {
	EntityID int64
	Tick     int64
	World    WorldReader
	Params   map[string]any // static params from the machine JSON cond spec
	Event    Event
}
```

- [ ] **Step 4: Run test to confirm it passes**

```
go test ./internal/agent/... -run TestContextTypes_Compile -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/context.go internal/agent/registry_test.go
git commit -m "feat(epic-3/story-3): add domain interfaces and context types"
```

---

### Task 2: registry.go — metadata types, handler interfaces, Registry

**Files:**
- Create: `internal/agent/registry.go`
- Modify: `internal/agent/registry_test.go` (append all registry tests)

- [ ] **Step 1: Append failing registry tests to registry_test.go**

Append to `internal/agent/registry_test.go` (after the existing `TestContextTypes_Compile` function):

```go
func TestNewRegistry_Empty(t *testing.T) {
	r := NewRegistry()
	if got := r.Actions(); len(got) != 0 {
		t.Errorf("Actions() = %v, want empty slice", got)
	}
	if got := r.Guards(); len(got) != 0 {
		t.Errorf("Guards() = %v, want empty slice", got)
	}
}

func TestRegistry_RegisterAndGetAction(t *testing.T) {
	r := NewRegistry()
	meta := ActionMeta{
		Name:        "dealDamage",
		Description: "Deal damage to a target entity",
		Params: []ParamSchema{
			{Name: "amount", Type: "number", Required: true},
			{Name: "target", Type: "string", Required: false, Default: "$self"},
		},
	}
	handler := &testActionHandler{}
	r.RegisterAction(meta, handler)

	got, ok := r.GetAction("dealDamage")
	if !ok {
		t.Fatal("GetAction: expected ok=true, got false")
	}
	if got != handler {
		t.Errorf("GetAction returned wrong handler")
	}
}

func TestRegistry_GetAction_Miss(t *testing.T) {
	r := NewRegistry()
	_, ok := r.GetAction("notRegistered")
	if ok {
		t.Error("GetAction: expected ok=false for unknown name, got true")
	}
}

func TestRegistry_RegisterAndGetGuard(t *testing.T) {
	r := NewRegistry()
	meta := GuardMeta{
		Name:        "inRange",
		Description: "True when entity is within distance of target",
		Params: []ParamSchema{
			{Name: "distance", Type: "number", Required: true},
		},
	}
	handler := &testGuardHandler{result: true}
	r.RegisterGuard(meta, handler)

	got, ok := r.GetGuard("inRange")
	if !ok {
		t.Fatal("GetGuard: expected ok=true, got false")
	}
	if got != handler {
		t.Errorf("GetGuard returned wrong handler")
	}
}

func TestRegistry_GetGuard_Miss(t *testing.T) {
	r := NewRegistry()
	_, ok := r.GetGuard("notRegistered")
	if ok {
		t.Error("GetGuard: expected ok=false for unknown name, got true")
	}
}

func TestRegistry_Actions_ReturnsSortedMetas(t *testing.T) {
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "zzz"}, &testActionHandler{})
	r.RegisterAction(ActionMeta{Name: "aaa"}, &testActionHandler{})
	r.RegisterAction(ActionMeta{Name: "mmm"}, &testActionHandler{})

	metas := r.Actions()
	if len(metas) != 3 {
		t.Fatalf("Actions() len = %d, want 3", len(metas))
	}
	want := []string{"aaa", "mmm", "zzz"}
	for i, m := range metas {
		if m.Name != want[i] {
			t.Errorf("Actions()[%d].Name = %q, want %q", i, m.Name, want[i])
		}
	}
}

func TestRegistry_Guards_ReturnsSortedMetas(t *testing.T) {
	r := NewRegistry()
	r.RegisterGuard(GuardMeta{Name: "zzz"}, &testGuardHandler{})
	r.RegisterGuard(GuardMeta{Name: "aaa"}, &testGuardHandler{})

	metas := r.Guards()
	if len(metas) != 2 {
		t.Fatalf("Guards() len = %d, want 2", len(metas))
	}
	if metas[0].Name != "aaa" {
		t.Errorf("Guards()[0].Name = %q, want %q", metas[0].Name, "aaa")
	}
	if metas[1].Name != "zzz" {
		t.Errorf("Guards()[1].Name = %q, want %q", metas[1].Name, "zzz")
	}
}

func TestRegistry_DuplicateAction_Panics(t *testing.T) {
	r := NewRegistry()
	r.RegisterAction(ActionMeta{Name: "doThing"}, &testActionHandler{})
	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected panic on duplicate RegisterAction, got none")
		}
	}()
	r.RegisterAction(ActionMeta{Name: "doThing"}, &testActionHandler{})
}

func TestRegistry_DuplicateGuard_Panics(t *testing.T) {
	r := NewRegistry()
	r.RegisterGuard(GuardMeta{Name: "isReady"}, &testGuardHandler{})
	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected panic on duplicate RegisterGuard, got none")
		}
	}()
	r.RegisterGuard(GuardMeta{Name: "isReady"}, &testGuardHandler{})
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/agent/... -v
```
Expected: compile errors — `NewRegistry`, `Registry`, `ActionMeta`, `GuardMeta`, `ParamSchema` undefined.

- [ ] **Step 3: Implement registry.go**

Create `internal/agent/registry.go`:

```go
package agent

import (
	"fmt"
	"sort"
)

// ParamSchema describes a single parameter that an action or guard accepts.
type ParamSchema struct {
	Name     string
	Type     string // "string", "number", "boolean", etc.
	Required bool
	Default  any // used when Required is false and the caller omits the param
}

// ActionMeta is the metadata stored alongside an ActionHandler in the registry.
// Exposed via Registry.Actions() for tooling such as a visual machine editor.
type ActionMeta struct {
	Name        string
	Description string
	Params      []ParamSchema
}

// GuardMeta is the metadata stored alongside a GuardHandler in the registry.
// Exposed via Registry.Guards() for tooling such as a visual machine editor.
type GuardMeta struct {
	Name        string
	Description string
	Params      []ParamSchema
}

// ActionHandler executes a side-effecting action during a state transition.
// Defined as an interface (not a bare func) so future Lua handlers can satisfy
// it without any changes to the interpreter.
type ActionHandler interface {
	Run(ActionContext) error
}

// GuardHandler evaluates a boolean condition used during transition selection.
// Defined as an interface for the same Lua-readiness reason as ActionHandler.
type GuardHandler interface {
	Evaluate(GuardContext) bool
}

type actionEntry struct {
	meta    ActionMeta
	handler ActionHandler
}

type guardEntry struct {
	meta    GuardMeta
	handler GuardHandler
}

// Registry maps action and guard names to their handlers and metadata.
// Duplicate registration panics — intended to be called from init() so
// misconfiguration is caught at startup, not at dispatch time.
type Registry struct {
	actions map[string]actionEntry
	guards  map[string]guardEntry
}

// NewRegistry returns an empty Registry ready for registration.
func NewRegistry() *Registry {
	return &Registry{
		actions: make(map[string]actionEntry),
		guards:  make(map[string]guardEntry),
	}
}

// RegisterAction adds an action to the registry.
// Panics if an action with the same name is already registered.
func (r *Registry) RegisterAction(meta ActionMeta, handler ActionHandler) {
	if _, exists := r.actions[meta.Name]; exists {
		panic(fmt.Sprintf("agent registry: action %q already registered", meta.Name))
	}
	r.actions[meta.Name] = actionEntry{meta: meta, handler: handler}
}

// RegisterGuard adds a guard to the registry.
// Panics if a guard with the same name is already registered.
func (r *Registry) RegisterGuard(meta GuardMeta, handler GuardHandler) {
	if _, exists := r.guards[meta.Name]; exists {
		panic(fmt.Sprintf("agent registry: guard %q already registered", meta.Name))
	}
	r.guards[meta.Name] = guardEntry{meta: meta, handler: handler}
}

// GetAction returns the handler for the named action, or (nil, false) if absent.
func (r *Registry) GetAction(name string) (ActionHandler, bool) {
	e, ok := r.actions[name]
	if !ok {
		return nil, false
	}
	return e.handler, true
}

// GetGuard returns the handler for the named guard, or (nil, false) if absent.
func (r *Registry) GetGuard(name string) (GuardHandler, bool) {
	e, ok := r.guards[name]
	if !ok {
		return nil, false
	}
	return e.handler, true
}

// Actions returns all registered action metadata sorted by name.
// Intended for tooling (visual editor, validator) that needs to enumerate
// available actions.
func (r *Registry) Actions() []ActionMeta {
	metas := make([]ActionMeta, 0, len(r.actions))
	for _, e := range r.actions {
		metas = append(metas, e.meta)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Name < metas[j].Name })
	return metas
}

// Guards returns all registered guard metadata sorted by name.
func (r *Registry) Guards() []GuardMeta {
	metas := make([]GuardMeta, 0, len(r.guards))
	for _, e := range r.guards {
		metas = append(metas, e.meta)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Name < metas[j].Name })
	return metas
}
```

- [ ] **Step 4: Run tests and check coverage**

```
go test ./internal/agent/... -v -coverprofile=coverage.out
go tool cover -func=coverage.out | grep registry
```
Expected: all tests PASS. Every function in `registry.go` shows 100%.

- [ ] **Step 5: Confirm no regressions across the full suite**

```
go test ./...
```
Expected: all tests PASS.

- [ ] **Step 6: Confirm the dependency rule holds**

```
grep -r "internal/storage" internal/agent/
```
Expected: no output. The `internal/agent` package must not import `internal/storage`.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/registry.go internal/agent/registry_test.go
git commit -m "feat(epic-3/story-3): add action/guard registry and metadata types"
```

---

## Verification

Run these after both tasks are complete:

```bash
# Full suite green
go test ./...

# Registry at 100% coverage
go test ./internal/agent/... -coverprofile=coverage.out
go tool cover -func=coverage.out | grep -E "registry|context"

# Dependency rule: agent must not import storage
grep -r "internal/storage" internal/agent/
# Expected: no output
```

Acceptance criteria traceability:

| Criterion | File | Location |
|-----------|------|----------|
| `ActionHandler` with `Run(ActionContext) error` | `registry.go` | `ActionHandler` interface |
| `GuardHandler` with `Evaluate(GuardContext) bool` | `registry.go` | `GuardHandler` interface |
| `Registry.RegisterAction` / `RegisterGuard` | `registry.go` | `Registry` methods |
| `Registry.Actions()` / `Registry.Guards()` | `registry.go` | `Registry` methods |
| `Registry.GetAction` / `Registry.GetGuard` | `registry.go` | `Registry` methods |
| `ActionMeta` / `GuardMeta` with Name, Description, Params | `registry.go` | struct declarations |
| `ParamSchema` with Name, Type, Required, Default | `registry.go` | struct declaration |
| `WorldWriter` with 4 methods | `context.go` | `WorldWriter` interface |
| `WorldReader` with 2 methods | `context.go` | `WorldReader` interface |
| `ActionContext` fields | `context.go` | `ActionContext` struct |
| `GuardContext` fields | `context.go` | `GuardContext` struct |
| `Event` with Type, Payload | `context.go` | `Event` struct |
| Duplicate registration panics | `registry.go` | `RegisterAction` / `RegisterGuard` |
| 100% test coverage on registry logic | `registry_test.go` | all `TestRegistry_*` functions |
