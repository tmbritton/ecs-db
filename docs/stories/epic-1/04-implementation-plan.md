# Story 4 Implementation Plan: Entity Creation with Type Validation

**Epic:** 1 — Schema-driven data foundation  
**Goal:** Implement entity creation with full type contract validation, transactional persistence, and integration tests.

## Assessment

The schema foundation (Stories 1-3) is complete — `schema.json` loads, validates, and generates SQLite DDL. What's missing is the ability to **create entities** against that schema. The architecture doc specifies entity types with `requiredComponents`, `optionalComponents`, `allowExtraComponents`, and `validationLevel` — all of which must be enforced at creation time.

| Story Requirement | Current State | Gap |
|---|---|---|
| Entity creation API | ❌ No code exists | Need domain port + implementation |
| Entity type existence check | ❌ Not implemented | Need validation in domain layer |
| `requiredComponents` enforcement | ❌ Not implemented | Schema types exist (`EntityType`), but no consumer |
| `allowExtraComponents` enforcement | ❌ Not implemented | `IsComponentAllowed()` exists on `EntityType` |
| `validationLevel` (strict/warning) | ❌ Not implemented | `ValidationLevel` type exists, no consumer |
| Transactional entity + component insert | ❌ Not implemented | Storage layer has no entity/component insertion |
| `created_tick` from `world` table | ❌ Not implemented | `world` table exists but is empty |
| Integration tests | ❌ Not implemented | Need end-to-end creation tests |

---

## Architecture

Per `AGENTS.md` hexagonal rules:

```
internal/
  world/              # Domain: entity creation, validation, lifecycle
    port.go           # EntityStore interface (domain port)
    service.go        # EntityService orchestrating creation
    validate.go       # Pure validation logic (no IO)
    validate_test.go  # Unit tests for validation
    service_test.go   # Tests with mock EntityStore
  storage/
    entity.go         # SQLite adapter: CreateEntity, InsertComponent
    entity_test.go    # Integration tests with :memory: SQLite
```

**Domain packages (`world/`) import nothing from `storage/`.** The `EntityStore` port is an interface in `world/port.go`. `storage/entity.go` implements it.

---

## Tasks

### Task 1: Define domain port — `internal/world/port.go`

Define the `EntityStore` interface that the entity service uses:

```go
type EntityStore interface {
    BeginTx(ctx context.Context) (EntityTx, error)
    GetCurrentTick(ctx context.Context) (int64, error)
}

type EntityTx interface {
    CreateEntity(entityType string, createdTick int64) (int64, error)
    InsertComponent(entityID int64, compName string, values map[string]interface{}) error
    Commit() error
    Rollback() error
}
```

**Why separate `EntityTx`:** Keeps transaction boundaries explicit. The entity service calls `BeginTx()`, does all inserts on the returned `EntityTx`, then commits or rolls back. No leaked `*sql.Tx` — the domain only knows the interface.

### Task 2: Implement pure validation logic — `internal/world/validate.go`

Create `ValidateEntityCreation(schema, entityTypeName, providedComponentNames) error` that performs:

1. **Entity type lookup** — Reject if `entityTypeName` not in `schema.EntityTypes`
2. **Required component check** — Every `RequiredComponents` must be in `providedComponentNames`
3. **Allowed component check** — Every `providedComponentNames` must be `IsComponentAllowed()` when `allowExtraComponents=false`
4. **Declaration check** — Every `providedComponentNames` must exist in `schema.Components` (unknown component = always error, regardless of warning mode)

**Warning vs strict:** When `validationLevel="warning"` and a check fails in steps 2–3, log the warning (via a `log` parameter or callback) but return `nil` error. Step 4 always errors because an undeclared component has no table to insert into.

**Return type:** `ValidationError` struct with `ValidationErrors []string` and `Warnings []string` so the caller gets structured diagnostics.

### Task 3: Implement entity service — `internal/world/service.go`

`EntityService` struct:
```go
type EntityService struct {
    store  EntityStore
    schema schema.DatabaseSchema
    logger *log.Logger  // for warnings
}
```

`CreateEntity(ctx, entityTypeName, components []EntityComponent) (*Entity, error)`:

1. Extract component names from `components` slice
2. Call `ValidateEntityCreation` — return error if validation fails (in strict mode), log warnings (in warning mode)
3. `BeginTx`
4. `GetCurrentTick` for `created_tick`
5. `tx.CreateEntity(entityTypeName, tick)` → `entityID`
6. For each component, `tx.InsertComponent(entityID, compName, compValues)`
7. `tx.Commit()` → return `&Entity{ID: entityID, ...}`
8. On any error → `tx.Rollback()` → return error

### Task 4: Implement SQLite adapter — `internal/storage/entity.go`

Methods on `SQLiteStore`:

- `BeginTx(ctx) → (*sql.Tx, error)` — Returns a `*sql.Tx` wrapped as `EntityTx`
- `GetCurrentTick(ctx) → int64` — `SELECT CAST(value AS INTEGER) FROM world WHERE key='current_tick'`, returns 0 if not found
- `CreateEntity(entityType, createdTick) → int64` — `INSERT INTO entities (entity_type, created_tick) VALUES (?, ?)` then `LastInsertId()`
- `InsertComponent(entityID, compName, values) → error` — Dynamic INSERT into `comp_<lowercase(compName)>`. Column names and values come from the `values` map. Must handle:
  - **Object components:** `INSERT INTO comp_<name> (entity_id, col1, col2, ...) VALUES (?, val1, val2, ...)`
  - **Scalar components:** `INSERT INTO comp_<name> (entity_id, value) VALUES (?, val)`
  - **Entity-ref components:** `INSERT INTO comp_<name> (entity_id, target_entity_id) VALUES (?, val)`
  - **Array components:** `INSERT INTO comp_<name> (entity_id, value) VALUES (?, jsonString)` where `jsonString` is `json.Marshal(values["value"])`

### Task 5: Define `EntityComponent` domain type — `internal/world/entity.go`

```go
type EntityComponent struct {
    Name   string                 // Matches key in schema.Components
    Values map[string]interface{} // Property→value map
}
```

Simple data carrier. No methods needed — just a struct.

### Task 6: Unit tests for validation — `internal/world/validate_test.go`

Table-driven test for `ValidateEntityCreation`:

| Test Case | Schema Setup | Provided Components | Expected Result |
|---|---|---|---|
| Valid entity, all required | Goblin: req=[Position,Health], opt=[Velocity] | Position, Health, Velocity | No error |
| Missing required component | Goblin: req=[Position,Health] | Position only | Error: missing "Health" |
| Extra component, strict, allowExtra=false | Goblin: req=[Position], allowExtra=false | Position, Velocity | Error: "Velocity" not allowed |
| Extra component, allowExtra=true | Goblin: req=[Position], allowExtra=true | Position, Velocity | No error |
| Warning mode, missing required | Goblin: req=[Position,Health], level=warning | Position only | No error, has warning |
| Warning mode, extra component | Goblin: req=[Position], allowExtra=false, level=warning | Position, Velocity | No error, has warning |
| Unknown entity type | Empty entity types | Any | Error: unknown type |
| Undeclared component | Goblin: req=[Position] | Position, FakeComponent | Error: "FakeComponent" not declared |
| Empty components, none required | Goblin: req=[], allowExtra=true | (none) | No error |

### Task 7: Service tests with mock store — `internal/world/service_test.go`

Mock `EntityStore` and `EntityTx` implementations (hand-written):

| Test Case | Mock Behavior | Expected |
|---|---|---|
| CreateEntity success | BeginTx→OK, CreateEntity→id=1, InsertComponent→OK, Commit→OK | Returns Entity{ID:1} |
| Validation failure | No store calls | Returns validation error |
| InsertComponent failure | Rollback called | Returns error, no commit |
| BeginTx failure | No entity created | Returns error |
| GetCurrentTick returns 0 | created_tick=0 | Entity created with tick=0 |

### Task 8: Integration tests — `internal/storage/entity_test.go`

Integration tests using `:memory:` SQLite with real `NewSQLiteStore`:

| Test Case | Setup | Verify |
|---|---|---|
| Create entity with object component | Schema: Goblin(Position, Health) | Row in `entities`, row in `comp_position`, row in `comp_health` |
| Create entity with scalar component | Schema: NPC(Name:string) | Row in `entities`, row in `comp_name` |
| Create entity with entity-ref | Schema: Weapon(Wielder:entity-ref) | Row in `comp_wielder` with correct target |
| Create entity with array component | Schema: NPC(Inventory:array) | Row in `comp_inventory` with JSON value |
| Missing required → no partial data | Goblin(req=Position,Health), only insert Position | No rows in `entities` or `comp_position` |
| created_tick reflects world tick | world table has current_tick=42 | entities.created_tick=42 |
| Entity ID auto-increments | Create 3 entities | IDs: 1, 2, 3 |
| Cascade delete after creation | Create entity with Health, delete it | comp_health row gone |

### Task 9: Create story implementation plan file

This document lives at `docs/stories/epic-1/04-implementation-plan.md` (i.e., this file).

### Task 10: Update story checkbox status

After implementation, update `docs/stories/epic-1/04-entity-creation-validation.md` to reflect completion.

---

## Files to Create/Modify

| File | Action | Lines | Reason |
|------|--------|-------|--------|
| `internal/world/port.go` | **Create** | ~30 | `EntityStore` and `EntityTx` domain ports |
| `internal/world/entity.go` | **Create** | ~15 | `Entity` and `EntityComponent` domain types |
| `internal/world/validate.go` | **Create** | ~60 | `ValidateEntityCreation` pure function |
| `internal/world/validate_test.go` | **Create** | ~100 | Table-driven unit tests for validation |
| `internal/world/service.go` | **Create** | ~80 | `EntityService` with `CreateEntity` method |
| `internal/world/service_test.go` | **Create** | ~120 | Service tests with mock store |
| `internal/storage/entity.go` | **Create** | ~120 | SQLite adapter for entity creation |
| `internal/storage/entity_test.go` | **Create** | ~180 | Integration tests |
| `docs/stories/epic-1/04-*-validation.md` | **Modify** | ~20 | Update checkboxes to done |

**Total scope:** ~725 lines across 8 new files + 1 modified story file.

---

## Design Decisions

### Why `EntityComponent.Values` is `map[string]interface{}`

The domain layer doesn't know about SQL column types. Component values come from the caller (ultimately the interpreter tick loop) as a map of property names to values. The storage adapter's `InsertComponent` maps these to SQL columns using the schema's component definition. This is the **right** seam because:

- Domain code validates component *names* (are they declared? are they allowed?) without inspecting values
- Storage code maps values to typed columns (REAL, INTEGER, TEXT) using `propertySQLType`
- The interpreter layer (which provides values) stays out of the domain/storage boundary

### Why `validationLevel="warning"` doesn't skip undeclared-component check

An entity type may opt into `validationLevel: warning` to allow flexible component sets. But if a component name isn't declared in `schema.Components` at all, there's no `comp_*` table to insert into. This is always a hard error — the schema loader should have caught it, but if it slips through, we must fail loudly rather than silently skip.

### Why `GetCurrentTick` exists as a separate port method

The tick is part of world state, not entity state. In Epic 5 (tick loop), the interpreter will advance `world.current_tick`. The entity creation service reads this value at creation time so `entities.created_tick` reflects the game's logical time, not wall clock. This method exists now so it's ready when Epic 5 lands.

---

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Dynamic INSERT column ordering | Medium | Build column list deterministically (sort by property name or use schema order) |
| `map[string]interface{}` value type mismatches | Medium | Validation in `InsertComponent` checks value types match schema property types |
| Transaction rollback not cleaning up | Low | SQLite guarantees rollback atomicity — test confirms |
| Entity-ref circular references | Low | FK constraint allows self-references; runtime logic handles cycles |

---

## Acceptance Criteria Checklist

- [x] `CreateEntity` with valid type + all required components → rows in `entities` + all `comp_*` tables
- [x] `CreateEntity` missing required component → error, no rows inserted (strict mode)
- [x] `CreateEntity` with extra component on strict type → error, no rows inserted
- [x] `CreateEntity` with extra component when `allowExtraComponents=true` → success
- [x] `CreateEntity` with unknown entity type → error
- [x] `created_tick` read from `world` table and stored correctly
- [x] Validation failure → transaction rollback, no partial data
- [x] At least 8 integration tests covering entity type validation, missing required, extra rejected, extra allowed, unknown type, and rollback
- [x] Domain packages import nothing from `storage/`
- [x] All tests pass with `go test ./...`
- [x] Coverage ≥90% on `internal/world/`
