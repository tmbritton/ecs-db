# Epic 3: State Machine Interpreter — Design Spec

## Context

Epic 3 introduces the behavior runtime for ecs-db. Entities get behavior through XState v4 JSON
state machines ("agents") loaded from `mods/behaviors/*.json`. The original scope was a flat-state
subset interpreter; this spec expands that to **full XState v4 support minus `invoke`**, because
hierarchical and parallel states are required for realistic game AI, and building the correct SCXML
algorithm once avoids rework.

---

## Key Decisions

- **Go implementation** — XState v4 source (`StateNode.ts`, `interpreter.ts`) as semantic reference
- **`cond` terminology** (v4, not v5 `guard`) — Stately v4 exports work with zero manual editing
- **Full XState v4 except `invoke`** — hierarchical, parallel, final, history states all supported
- **Reject `invoke` loudly** — async services are incompatible with the synchronous tick model
- **Reject all other unsupported features loudly** at load time
- **Lua-ready registry** — actions/guards registered as interfaces, not func types; future Lua
  handlers implement the same interface without touching the interpreter
- **`WorldWriter`/`WorldReader` abstraction** — built-ins never call raw SQL; they write through
  domain-level interfaces that will become the Lua-accessible API
- **Multiple machines per entity** — primary machine + zero or more behavior-component machines
  run concurrently; machine state is keyed by `(entity_id, machine_id)`
- **`Behavior` is a reserved engine primitive** — not in schema.json; interpreter creates and
  owns its table at startup; schema validation rejects any user component named "Behavior"
- **Component-machine binding is explicit** — a component in schema.json may declare an optional
  `"behavior"` field naming a machine file; components without it are pure data; attaching a
  behavior-bearing component activates its machine; machine reaching final state detaches the
  component; lifecycle is bidirectional
- **Context = this machine's component manifest** — the `context` block declares only the
  components this machine manages; keys matched to component fields by name across schema.json;
  ambiguous or missing keys are validation errors; interpreter attaches any missing components at
  startup seeded with initial values; cross-entity writes go through WorldWriter with explicit IDs

---

## Design Goals

1. Stately v4 direct export: drop JSON in `mods/behaviors/`, it runs — no manual editing
2. Full XState v4 minus `invoke`: hierarchical, parallel, final, history states
3. Sandboxed by construction: agents only call registered actions/guards
4. Lua-ready: registry interfaces support future `LuaActionHandler` without interpreter changes
5. Transactional: one event delivery = one SQLite transaction
6. Visual-editor-ready: `Registry.Actions()` / `Registry.Guards()` introspection for future editor
7. Multiple machines per entity: primary + behavior-component machines run concurrently
8. Context as manifest: context block declares this machine's component ownership, scoped and explicit

---

## Package Structure

```
internal/agent/
├── machine.go        — MachineDefinition, StateNode types, JSON parsing
├── interpreter.go    — SCXML microstep execution algorithm
├── registry.go       — Action and guard registries with metadata
├── context.go        — ActionContext, GuardContext, WorldWriter, WorldReader interfaces
├── validator.go      — Load-time validation
├── scheduler.go      — Delayed transition scheduling (after → event_queue)
├── agent.go          — Agent: entity ↔ machine binding + runtime configuration
└── builtins/
    ├── actions.go    — Built-in actions
    └── guards.go     — Built-in guards
```

---

## Key Types

### Machine definition

```go
type StateType string  // atomic | compound | parallel | final | history

type MachineDefinition struct {
    ID      string
    Initial string
    Context map[string]any   // initial component field values; written once at machine startup
    States  map[string]*StateNode
}

type StateNode struct {
    ID       string
    Type     StateType
    Parent   *StateNode
    Children map[string]*StateNode
    Initial  string
    On       map[string][]Transition
    Entry    []ActionSpec
    Exit     []ActionSpec
    After    map[string]TransitionSpec  // "500ms" → target
}

type Transition struct {
    Target  string
    Cond    *CondSpec       // nil = unconditional
    Actions []ActionSpec
}
```

### Registry

```go
type ActionHandler interface {
    Run(ActionContext) error
}

type GuardHandler interface {
    Evaluate(GuardContext) bool
}

type ActionMeta struct {
    Name        string
    Description string
    Params      []ParamSchema
}

type Registry struct { /* ... */ }

func (r *Registry) RegisterAction(meta ActionMeta, h ActionHandler)
func (r *Registry) RegisterGuard(meta GuardMeta, h GuardHandler)
func (r *Registry) Actions() []ActionMeta   // introspection for future editor
func (r *Registry) Guards() []GuardMeta
```

### Contexts and world interfaces

```go
// WorldWriter — domain-level writes backed by *sql.Tx; becomes the Lua API surface.
type WorldWriter interface {
    AttachComponent(entityID int64, component string, data map[string]any) error
    DetachComponent(entityID int64, component string) error
    SetComponentValue(entityID int64, component, field string, value any) error
    SpawnEntity(entityType string) (int64, error)
}

// WorldReader — read-only, backed by *sql.DB.
type WorldReader interface {
    GetComponentValue(entityID int64, component, field string) (any, error)
    HasComponent(entityID int64, component string) (bool, error)
}

// No MachineContext field — context lives in components, accessed via World.
type ActionContext struct {
    EntityID int64
    Tick     int64
    World    WorldWriter
    Params   map[string]any   // static params from machine JSON
    Event    Event
}

type GuardContext struct {
    EntityID int64
    Tick     int64
    World    WorldReader
    Params   map[string]any
    Event    Event
}
```

### Agent runtime state

```go
// No Context field — working state lives in entity components.
type Agent struct {
    Definition    *MachineDefinition
    Configuration []*StateNode              // active states; >1 for parallel regions
    EntityID      int64
    History       map[string][]*StateNode   // history recordings keyed by state ID
}
```

---

## Execution Model: SCXML Microstep Algorithm

Each event delivery is one atomic SQLite transaction:

```
send(agent, event, tick, tx):
  1. Find eligible transitions
     — Walk configuration deepest-first
     — Check On[event] in document order per state
     — Evaluate cond via WorldReader (read-only); first true transition wins per state
     — Deeper states preempt ancestors; parallel regions contribute independently

  2. Compute exit set                — ordered leaf→root

  3. Run exit actions                — write via WorldWriter(tx)
     — Cancel after event_queue rows for exited states

  4. Run transition actions          — write via WorldWriter(tx)

  5. Compute entry set               — ordered root→leaf
     — Resolve compound initials, parallel regions, history targets

  6. Run entry actions               — write via WorldWriter(tx)
     — Insert after event_queue rows for entered states

  7. Write Behavior component        — update current_states + updated_at via WorldWriter

  8. Write transitions audit row     — entity_id, machine_id, from_states, to_states,
                                       event, cond_result, actions_run, tick
```

Reference: `packages/core/src/StateNode.ts` and `packages/core/src/interpreter.ts` in XState v4.

---

## Load-time Validation

On file load (startup or hot reload):

1. Parse JSON — reject if malformed
2. Reject `invoke` at any level: `"machine 'X': invoke is not supported"`
3. Verify all `cond.type` names exist in guard registry
4. Verify all action `type` names exist in action registry
5. Verify all transition `target` values resolve to defined states
6. Verify all `after` targets resolve
7. Verify history state default targets resolve
8. Verify all `context` keys resolve to exactly one component field in schema.json
   - Zero matches → reject: `"context key 'foo' does not match any component field"`
   - Multiple matches → reject: `"context key 'speed' is ambiguous: found in Movement and Goblin"`

On first run for an entity: attach missing components seeded from context initial values;
existing components are left unchanged.

**On failure:** log warning, skip file, retain previous in-memory version. Game keeps running.

**Unknown top-level fields** (Stately adds `description`, `meta`, `tags`): silently ignored.

---

## Schema Extensions

### Component-machine binding

A component in schema.json may declare an optional `"behavior"` field:

```json
{
  "name": "Burning",
  "behavior": "burning",
  "fields": [
    { "name": "burn_damage",     "type": "integer", "nullable": false },
    { "name": "ticks_remaining", "type": "integer", "nullable": false }
  ]
}
```

- Components without `"behavior"` are pure data
- If `"behavior"` is declared, `mods/behaviors/<value>.json` must exist at startup
- Attaching a behavior-bearing component activates its machine for that entity
- Machine reaching a final state triggers component detach (bidirectional lifecycle)

### Reserved component name

Schema validation rejects any user component named `"Behavior"`:
`"'Behavior' is a reserved component name"`

---

## Interpreter-Managed Tables

Created at startup (not via schema DDL):

```sql
-- Multiple rows per entity for concurrent machines
CREATE TABLE IF NOT EXISTS behavior_components (
    entity_id      INTEGER NOT NULL,
    machine_id     TEXT NOT NULL,
    current_states TEXT NOT NULL,   -- JSON array of active state IDs
    updated_at     INTEGER NOT NULL,
    PRIMARY KEY (entity_id, machine_id)
);

CREATE TABLE IF NOT EXISTS transitions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id   INTEGER NOT NULL,
    machine_id  TEXT NOT NULL,
    from_states TEXT NOT NULL,
    to_states   TEXT NOT NULL,
    event       TEXT NOT NULL,
    cond_result INTEGER,         -- 1/0/NULL
    actions_run TEXT NOT NULL,   -- JSON array of type names
    tick        INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS event_queue (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id   INTEGER NOT NULL,
    machine_id  TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    payload     TEXT,
    target_tick INTEGER NOT NULL
);
```

---

## Built-in Actions and Guards

Actions (write via WorldWriter, never raw SQL):
`moveTowardTarget`, `dealDamage`, `spawnEntity`, `attachComponent`, `detachComponent`,
`setTimer`, `log`, `pickRandomTarget`, `setPursueTarget`

Guards (read via WorldReader):
`timerExpired`, `atTarget`, `inRange`, `hasComponent`, `healthAbove`

---

## Testing Strategy

- **Parser**: valid v4 JSON round-trips; `invoke` rejected; unknown fields tolerated
- **SCXML algorithm**: port XState v4's edge-case tests — transition conflicts, parallel region
  sync, history restoration, compound initial resolution
- **Registry**: registration and metadata introspection
- **Integration**: `wandering_goblin` fixture — load → send events → assert `behavior_components`
  and `transitions` table contents
- **Component lifecycle**: attach behavior-bearing component → machine activates; machine
  reaches final state → component detaches
- **Stately round-trip**: real Stately v4 export in `testdata/`, parsed and validated in CI

---

## Story Sequencing

1. **Interpreter-managed tables + schema extensions** — `behavior_components`, `transitions`,
   `event_queue`; `"behavior"` field in schema.json; reserved name validation
2. **Machine parser + StateNode tree** — all node types; reject `invoke`; tolerate unknown fields
3. **Registry + context types** — interfaces, metadata, introspection, WorldWriter/WorldReader
4. **Load-time validator** — registries, targets, context key resolution, hot-reload semantics
5. **SCXML microstep interpreter** — full algorithm; component seeding; lifecycle hooks;
   behavior_components + transitions writes
6. **Delayed transitions** — event_queue scheduling and cancellation
7. **Built-in actions + guards** — all via WorldWriter/WorldReader
8. **Integration tests + doc updates** — wandering_goblin fixture; Stately testdata;
   update game-engine-arch.md and plan.md

---

## Future Epics

- **Lua actions/guards**: `LuaActionHandler` implements `ActionHandler`; WorldWriter/WorldReader
  become the Lua table API — no interpreter changes
- **Visual state machine editor**: web UI; uses `Registry.Actions()` / `Registry.Guards()` +
  schema.json for pickers; reads/writes `mods/behaviors/*.json`
