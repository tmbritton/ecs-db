# Story 2 Implementation Plan: Machine Parser and StateNode Tree

**Epic:** 3 — Agents (behavior-as-data) runtime  
**Goal:** Create `internal/agent/machine.go` with the `MachineDefinition` and `StateNode` types, and implement `ParseMachine(data []byte) (*MachineDefinition, error)`.

---

## Assessment

| Requirement | Current State | Gap |
|---|---|---|
| `internal/agent/` package | ❌ Does not exist | Create package and initial file |
| `MachineDefinition`, `StateNode` types | ❌ | Define in `machine.go` |
| `ParseMachine` function | ❌ | JSON parsing with polymorphic cond/actions |
| Polymorphic `cond` (string / object) | ❌ | Parse helpers |
| Polymorphic `actions` (string / object / array) | ❌ | Parse helpers |
| `invoke` rejection at any nesting level | ❌ | Check at each recursion level |
| Parent pointer linking | ❌ | Recursive linking during tree build |
| History node support | ❌ | Infer from `type` and `history` fields |
| `after` keys preserved as strings | ❌ | Store raw duration strings as map keys |
| `wandering_goblin` round-trip | ❌ | Integration test |

---

## Architecture

### Types — `internal/agent/machine.go`

Exported types from the approved Epic 3 spec, with `History`/`Target` fields for history nodes:

```go
type StateType string

const (
    StateTypeAtomic   StateType = "atomic"
    StateTypeCompound StateType = "compound"
    StateTypeParallel StateType = "parallel"
    StateTypeFinal    StateType = "final"
    StateTypeHistory  StateType = "history"
)

type ActionSpec struct {
    Type   string
    Params map[string]any
}

type CondSpec struct {
    Type   string
    Params map[string]any
}

type Transition struct {
    Target  string
    Cond    *CondSpec    // nil = unconditional
    Actions []ActionSpec
}

type StateNode struct {
    ID       string
    Type     StateType
    Parent   *StateNode            // nil for top-level states
    Children map[string]*StateNode
    Initial  string
    On       map[string][]Transition
    Entry    []ActionSpec
    Exit     []ActionSpec
    After    map[string][]Transition // key = raw duration string ("500", "1000ms")
    History  string                  // "shallow" or "deep"; history nodes only
    Target   string                  // default history target; history nodes only
}

type MachineDefinition struct {
    ID      string
    Initial string
    Context map[string]any
    States  map[string]*StateNode   // top-level states only
}
```

### Raw parsing structs (unexported)

Parallel structs with `json.RawMessage` for polymorphic fields. Go's default decoder ignores unknown fields, so Stately extras (`description`, `meta`, `tags`) are silently dropped:

```go
type rawMachine struct {
    ID      string                     `json:"id"`
    Initial string                     `json:"initial"`
    Context map[string]any             `json:"context"`
    States  map[string]json.RawMessage `json:"states"`
    Invoke  json.RawMessage            `json:"invoke"`
    ...
}

type rawStateNode struct {
    ID      string                     `json:"id"`
    Type    string                     `json:"type"`
    Initial string                     `json:"initial"`
    History string                     `json:"history"`
    Target  string                     `json:"target"`
    On      map[string]json.RawMessage `json:"on"`
    Entry   json.RawMessage            `json:"entry"`
    Exit    json.RawMessage            `json:"exit"`
    After   map[string]json.RawMessage `json:"after"`
    States  map[string]json.RawMessage `json:"states"`
    Invoke  json.RawMessage            `json:"invoke"`
}
```

### State type inference

```
raw.Type == "parallel"             → StateTypeParallel
raw.Type == "final"                → StateTypeFinal
raw.Type == "history" || "deep"    → StateTypeHistory (Stately may export type "deep")
raw.History != ""                  → StateTypeHistory (history:"deep" without explicit type)
len(raw.States) > 0                → StateTypeCompound
otherwise                          → StateTypeAtomic
```

### `ParseMachine(data []byte) (*MachineDefinition, error)`

1. `json.Unmarshal` into `rawMachine`
2. If `rm.Invoke` non-nil → `"machine '<id>': invoke is not supported"`
3. For each key/value in `rm.States`, call `parseStateNode(machineID, name, rawJSON, nil)`
4. Return `MachineDefinition{ID, Initial, Context, States}`

### `parseStateNode(machineID, name string, data json.RawMessage, parent *StateNode) (*StateNode, error)`

1. `json.Unmarshal` into `rawStateNode`
2. Resolve ID: if `raw.ID != ""` use it; else `machineID + "." + name`
3. If `raw.Invoke` non-nil → `"machine '<machineID>': state '<name>': invoke is not supported"`
4. Infer `StateType`
5. Parse `entry`, `exit` via `parseActionSpecs`
6. Parse `on`, `after` via `parseTransitionMap`
7. For history nodes: store `raw.History` and `raw.Target`
8. Recurse into `raw.States`, passing current node as `parent`

### Polymorphic parsing helpers (all unexported)

- **`parseActionSpecs`** — absent/nil → nil; string → single; object → single; array → slice
- **`parseActionSpec`** — string → `ActionSpec{Type}`; object → `ActionSpec{Type, Params}`
- **`parseCondSpec`** — nil → nil; string → `&CondSpec{Type}`; object → `&CondSpec{Type, Params}`
- **`parseTransitions`** — string → single unconditional; object → single; array → recurse each element
- **`parseTransitionMap`** — iterate map, call `parseTransitions` per value

---

## Tasks

### Task 1: Type definitions and constants — `internal/agent/machine.go`

New file. All exported types, `StateType` constants, raw structs.

### Task 2: `ParseMachine` skeleton

Top-level unmarshal, invoke check, state iteration.

**Tests** (`internal/agent/machine_test.go`):
- `TestParseMachine_MalformedJSON`
- `TestParseMachine_MinimalMachine`
- `TestParseMachine_RootInvokeRejected`

### Task 3: `parseStateNode` — type inference, parent pointers, recursion

**Tests**:
- `TestParseMachine_AtomicState`
- `TestParseMachine_CompoundState`
- `TestParseMachine_ParallelState`
- `TestParseMachine_FinalState`
- `TestParseMachine_HistoryState_Shallow`
- `TestParseMachine_HistoryState_Deep`
- `TestParseMachine_HistoryState_Target`
- `TestParseMachine_ParentPointers`
- `TestParseMachine_StateInvokeRejected`
- `TestParseMachine_ExplicitStateID`

### Task 4: Polymorphic action parsing

**Tests**:
- `TestParseMachine_EntryActionString`
- `TestParseMachine_EntryActionObject`
- `TestParseMachine_EntryActionArray_Mixed`
- `TestParseMachine_ExitActions`

### Task 5: Polymorphic transition parsing

**Tests**:
- `TestParseMachine_OnTransitionStringTarget`
- `TestParseMachine_OnTransitionObject`
- `TestParseMachine_OnTransitionCondObject`
- `TestParseMachine_OnTransitionArray_Mixed`
- `TestParseMachine_OnTransitionWithActions`
- `TestParseMachine_AfterKeysPreserved`

### Task 6: Unknown field tolerance and round-trip

**Tests**:
- `TestParseMachine_UnknownFieldsIgnored`
- `TestParseMachine_WanderingGoblinRoundTrip` — verifies ID, Initial, state count, `attacking.After["500"]`, `idle.On["TICK"]` conditional, `idle.On["PLAYER_NEARBY"]` unconditional, context null values

---

## Files

| File | Action | Est. lines |
|------|--------|------------|
| `internal/agent/machine.go` | **Create** | ~210 |
| `internal/agent/machine_test.go` | **Create** | ~290 |

**Total: ~500 new lines. No changes to any existing files.**

---

## Acceptance criteria → test mapping

| Criterion | Tests |
|---|---|
| `ParseMachine` parses valid XState v4 JSON | `TestParseMachine_MinimalMachine`, `_WanderingGoblinRoundTrip` |
| Atomic state | `TestParseMachine_AtomicState` |
| Compound state | `TestParseMachine_CompoundState` |
| Parallel state | `TestParseMachine_ParallelState` |
| Final state | `TestParseMachine_FinalState` |
| History state (shallow + deep) | `_HistoryState_Shallow`, `_Deep`, `_Target` |
| `on`, `entry`, `exit`, `after`, `cond`, `target`, `actions`, `context`, `initial`, `id` | Tasks 3–6 |
| `cond` string shorthand | `TestParseMachine_OnTransitionObject` |
| `cond` object form | `TestParseMachine_OnTransitionCondObject` |
| Action string shorthand | `TestParseMachine_EntryActionString` |
| Action object form | `TestParseMachine_EntryActionObject` |
| `invoke` rejected at root | `TestParseMachine_RootInvokeRejected` |
| `invoke` rejected in nested state | `TestParseMachine_StateInvokeRejected` |
| Unknown fields silently ignored | `TestParseMachine_UnknownFieldsIgnored` |
| Malformed JSON → error | `TestParseMachine_MalformedJSON` |
| Parent pointers linked | `TestParseMachine_ParentPointers` |
| `after` keys preserved | `TestParseMachine_AfterKeysPreserved` |
| History `history` field and `target` | `_HistoryState_Deep`, `_Target` |
| `wandering_goblin` round-trip | `TestParseMachine_WanderingGoblinRoundTrip` |

---

## Risks

| Risk | Mitigation |
|---|---|
| `on` value can be string, object, or array — missed case silently drops transitions | Explicit test for each form; `parseTransitions` handles all three |
| History state: `type:"history"` vs `type:"deep"` (Stately quirk) | Handle both in inference; test both explicitly |
| Nested `invoke` detection — only root caught | `rawStateNode.Invoke` field checked inside `parseStateNode` at every recursion level |
| `After` key `"500"` vs `"500ms"` | Parser preserves keys verbatim; validator/scheduler normalizes |
| `context` with null values (`"target_x": null`) | Go `map[string]any` decodes JSON null as `nil` — correct |
