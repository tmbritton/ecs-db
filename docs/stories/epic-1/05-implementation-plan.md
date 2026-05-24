# Story 5 Implementation Plan: Component Attach/Detach with Type Validation

**Epic:** 1 — Schema-driven data foundation  
**Goal:** Enable entities to gain and lose components post-creation with the same type validation rules that govern creation.

## Assessment

Story 4 established entity creation with full validation and transactional persistence. The `world.Tx` interface has `InsertComponent`, and `EntityService.CreateEntity` orchestrates the full lifecycle. What's missing is **post-creation component mutation**: entities need to acquire new components (attach) and shed old ones (detach), both subject to the entity type contract.

| Story Requirement | Current State | Gap |
|---|---|---|
| Attach component to existing entity | ❌ No code exists | New Tx method + service method + validation |
| Validate component is declared | ❌ Not implemented | Reuse schema.Components lookup |
| Validate component allowed on entity type | ❌ Not implemented | Reuse `EntityType.IsComponentAllowed()` |
| Prevent duplicate attachment | ❌ Not implemented | Check existing component before insert |
| Detach component from entity | ❌ No code exists | New Tx method + service method + validation |
| Prevent detaching required components | ❌ Not implemented | Check `EntityType.IsComponentRequired()` |
| Honor `validationLevel` on attach | ❌ Not implemented | Reuse warning-mode pattern from creation |
| Execute in transactions | ❌ Not implemented | Each attach/detach uses its own Tx |
| Look up entity type by ID | ❌ No Tx method | Need `GetEntityType` on Tx |
| Check entity exists | ❌ No Tx method | Need `EntityExists` on Tx |

---

## Architecture

Extend the existing hexagonal boundary. Domain ports gain new methods; the SQLite adapter implements them. No new packages.

```
internal/
  world/              # Domain — no storage imports
    port.go           # ← Extend Tx and EntityStore interfaces
    validate_attach.go  # NEW: ValidateAttachComponent pure function
    validate_detach.go  # NEW: ValidateDetachComponent pure function
    service.go        # ← AttachComponent + DetachComponent methods
    entity.go         # ← unchanged (EntityComponent carries values)
  storage/
    entity.go         # ← Extend sqliteTx with new Tx methods
```

**Key principle:** Domain packages (`world/`) import nothing from `storage/`. The `Tx` interface is extended with attach/detach methods. The existing `sqliteTx` implements them.

---

## Tasks

### Task 1: Extend domain ports — `internal/world/port.go`

Add methods to the existing `Tx` interface:

```go
type Tx interface {
    // ... existing methods ...

    // AttachComponent inserts a component row for the given entity.
    // Returns ErrAlreadyAttached if the component is already present.
    AttachComponent(ctx context.Context, entityID int64, compName string, values map[string]interface{}) error
    // DetachComponent deletes the component row for the given entity.
    DetachComponent(ctx context.Context, entityID int64, compName string) error
}

type EntityStore interface {
    // ... existing methods ...

    // GetEntityType returns the entity type for the given entity ID.
    // Returns an error if the entity does not exist.
    GetEntityType(ctx context.Context, entityID int64) (string, error)
    // HasComponent returns true if the entity has the named component attached.
    HasComponent(ctx context.Context, entityID int64, compName string) (bool, error)
}
```

**Why `HasComponent` on `EntityStore` (not `Tx`):** The service needs this _before_ starting a transaction to perform the duplicate-attach check. It's a read-only lookup, so it belongs on the store alongside `GetEntityType`.

**Why the same `AttachComponent` signature as `InsertComponent`:** They do the same SQL operation (INSERT into `comp_*`). The domain layer validates _whether_ the attach is legal; the adapter _performs_ the insert. Same semantics, different entry points.

### Task 2: Implement attach validation — `internal/world/validate_attach.go`

`ValidateAttachComponent(schema, entityTypeName, componentName, isAlreadyAttached) ValidationResult`:

1. **Entity type must exist** — Return error if `entityTypeName` not in `schema.EntityTypes`
2. **Component must be declared** — Return error if `componentName` not in `schema.Components` (always hard error)
3. **Component must be allowed on entity type** — `entityType.IsComponentAllowed(componentName)`; error if `false` and `allowExtraComponents=false` (becomes warning in warning mode)
4. **Component must not already be attached** — `isAlreadyAttached=true` always errors (duplicate attach is never valid, even in warning mode — no upsert semantics)

Returns a `ValidationResult` (reuse the existing struct from `validate.go`).

### Task 3: Implement detach validation — `internal/world/validate_detach.go`

`ValidateDetachComponent(schema, entityTypeName, componentName) ValidationResult`:

1. **Entity type must exist** — Return error if `entityTypeName` not in `schema.EntityTypes`
2. **Component must be declared** — Return error if `componentName` not in `schema.Components`
3. **Component must not be required** — `entityType.IsComponentRequired(componentName)` always errors (required components cannot be detached, even in warning mode — removing required data has no safe "proceed anyway" path)

### Task 4: Extend `EntityService` — `internal/world/service.go`

Two new methods on the existing `EntityService`:

**`AttachComponent(ctx, entityID int64, compName string, values map[string]interface{}) error`:**

1. `store.GetEntityType(ctx, entityID)` → `entityTypeName` (errors if entity doesn't exist)
2. `store.HasComponent(ctx, entityID, compName)` → `isAlreadyAttached`
3. `ValidateAttachComponent(schema, entityTypeName, compName, isAlreadyAttached)` → `vr`
   - If `!vr.Valid()` → return `ValidationError`
   - If `len(vr.Warnings) > 0` → store warnings, proceed
4. `BeginTx` → `tx.AttachComponent(ctx, entityID, compName, values)` → `Commit`
5. On any error → `Rollback` → return error

**`DetachComponent(ctx context.Context, entityID int64, compName string) error`:**

1. `store.GetEntityType(ctx, entityID)` → `entityTypeName`
2. `ValidateDetachComponent(schema, entityTypeName, compName)` → `vr`
   - `!vr.Valid()` → return `ValidationError`
3. `BeginTx` → `tx.DetachComponent(ctx, entityID, compName)` → `Commit`
4. On any error → `Rollback` → return error

### Task 5: Implement SQLite adapter methods — `internal/storage/entity.go` (append)

Extend the existing `sqliteTx` and `SQLiteStore`:

```go
// On SQLiteStore:

func (s *SQLiteStore) GetEntityType(ctx context.Context, entityID int64) (string, error) {
    // SELECT entity_type FROM entities WHERE id = ?
    // Returns error if not found.
}

func (s *SQLiteStore) HasComponent(ctx context.Context, entityID int64, compName string) (bool, error) {
    // SELECT 1 FROM comp_<lowercase(compName)> WHERE entity_id = ? LIMIT 1
    // Returns (false, nil) if no row found.
    // Returns (false, error) if the comp_* table doesn't exist (undeclared component).
}

// On sqliteTx:

func (t *sqliteTx) AttachComponent(ctx context.Context, entityID int64, compName string, values map[string]interface{}) error {
    // Reuse the same dispatch logic as InsertComponent.
    // The duplicate-attach check is done at the domain level before the transaction starts.
}

func (t *sqliteTx) DetachComponent(ctx context.Context, entityID int64, compName string) error {
    // DELETE FROM comp_<lowercase(compName)> WHERE entity_id = ?
}
```

**Duplicate-attach protection:** The domain checks `HasComponent` _before_ `BeginTx`. The adapter's `AttachComponent` does a plain INSERT. If a race were to occur, the UNIQUE constraint on `(entity_id)` in the component table would catch it, but in practice attach/detach are sequential operations in the interpreter's tick loop.

### Task 6: Update mock — `internal/world/mock_test.go`

Add mock implementations to satisfy the extended `Tx` and `EntityStore` interfaces:

```go
// mockTx additions:
func (m *mockTx) AttachComponent(ctx context.Context, entityID int64, compName string, values map[string]interface{}) error {
    return m.attachCompErr
}
func (m *mockTx) DetachComponent(ctx context.Context, entityID int64, compName string) error {
    return m.detachCompErr
}

// mockStore additions:
func (m *mockStore) GetEntityType(ctx context.Context, entityID int64) (string, error) {
    return m.entityTypes[entityID], m.getEntityTypeErr
}
func (m *mockStore) HasComponent(ctx context.Context, entityID int64, compName string) (bool, error) {
    return m.hasComponent, m.hasComponentErr
}
```

### Task 7: Unit tests for attach validation — `internal/world/validate_attach_test.go`

Table-driven tests:

| Test Case | Schema | Inputs | Expected |
|---|---|---|---|
| Valid optional component | Goblin: req=[Position,Health], opt=[Velocity] | Velocity, not attached | No error |
| Valid required component | Goblin (same) | Health, not attached | No error |
| Undeclared component name | Goblin (same) | "MagicShield" (not in Components) | Error: not declared |
| Disallowed extra, strict | Goblin: req=[Position], allowExtra=false | Velocity | Error: not allowed |
| Disallowed extra, warning mode | Goblin: req=[Position], allowExtra=false, level=warning | Velocity | Warning, no error |
| Already attached | Goblin: req=[Position] | Position, already attached=true | Error: already attached |
| Unknown entity type | Empty entity types | Any | Error: unknown type |
| Allowed extra, allowExtra=true | Goblin: req=[Position], allowExtra=true | Velocity, not attached | No error |

### Task 8: Unit tests for detach validation — `internal/world/validate_detach_test.go`

Table-driven tests:

| Test Case | Schema | Inputs | Expected |
|---|---|---|---|
| Optional component detach | Goblin: req=[Position,Health], opt=[Velocity] | Velocity | No error |
| Required component detach | Goblin (same) | Health | Error: cannot detach required |
| Required component detach, warning mode | Goblin: level=warning | Position | Error: cannot detach required (always) |
| Undeclared component detach | Goblin (same) | "MagicShield" | Error: not declared |
| Unknown entity type | Empty entity types | Any | Error: unknown type |
| Detach extra on strict type | Goblin: req=[Position], allowExtra=false | Velocity | No error (detaching an allowed extra is fine) |

### Task 9: Service tests — `internal/world/service_test.go` (append)

Mock-based tests for attach and detach:

| Test Case | Mock Behavior | Expected |
|---|---|---|
| `AttachComponent` success | GetEntityType→"Goblin", HasComponent→false, AttachComponent→nil, Commit→nil | No error |
| `AttachComponent` — entity not found | GetEntityType→error | Error, no Tx started |
| `AttachComponent` — already attached | HasComponent→true | Error, no Tx started |
| `AttachComponent` — validation error (disallowed) | GetEntityType→"Goblin", skip Tx | Error |
| `AttachComponent` — warning mode proceeds | HasComponent→false, warnings returned | No error, Commit called |
| `AttachComponent` — rollback on failure | AttachComponent→error | Error, Rollback called, no Commit |
| `DetachComponent` success | GetEntityType→"Goblin", DetachComponent→nil, Commit→nil | No error |
| `DetachComponent` — required | GetEntityType→"Goblin", validation error | Error, no Tx |
| `DetachComponent` — entity not found | GetEntityType→error | Error, no Tx |
| `DetachComponent` — rollback on failure | DetachComponent→error | Error, Rollback called |
| `GetEntityType` returns correct type | store returns types map | Service reads correct type name |

### Task 10: Integration tests — `internal/storage/attach_detach_test.go` (new file)

Using `:memory:` SQLite via `NewSQLiteStore`:

| Test Case | Setup | Verify |
|---|---|---|
| Attach valid optional | Create Goblin (Position, Health), attach Velocity | Row in `comp_velocity` |
| Attach disallowed component | Create Goblin, attach "Velocity" when not allowed | Error, no row in `comp_velocity` |
| Attach duplicate | Create Goblin with Position, attach Position again | Error, no duplicate row |
| Attach to non-existent entity | entityID=99999 | Error |
| Detach optional | Create Goblin + Velocity attached, detach Velocity | No row in `comp_velocity` |
| Detach required | Create Goblin, detach Health | Error, row still in `comp_health` |
| Detach from non-existent entity | entityID=99999 | Error |
| Full attach + detach lifecycle | Create Goblin, attach Velocity, verify row exists, detach, verify row gone | Row exists → row deleted |
| Transactional rollback on attach | Attach that fails mid-operation (via mock or error path) | No partial data |

### Task 11: Compile-time interface checks — `internal/storage/entity.go` (append)

Verify new methods satisfy the extended interfaces:

```go
var (
    _ world.Tx         = (*sqliteTx)(nil)
    _ world.EntityStore = (*SQLiteStore)(nil)
)
```

These already exist — just need to confirm compilation succeeds after the interface additions.

### Task 12: Run linter, formatter, and coverage

```bash
mise exec -- gofumpt -w internal/world/ internal/storage/
mise exec -- golangci-lint run ./...
go test -cover ./internal/world/
go test -cover ./internal/storage/
```

Target: ≥90% on `internal/world/`, maintain ≥80% on `internal/storage/`.

### Task 13: Update story checkbox status

After implementation, update `docs/stories/epic-1/05-component-attach-detach-validation.md`:
- Mark all tasks done with notes
- Mark all acceptance criteria checked
- Add coverage section

### Task 14: Update `docs/plan.md`

Mark Story 4 as complete (was missed at commit time) and add a summary line.

---

## Files to Create/Modify

| File | Action | Est. Lines | Reason |
|------|--------|------------|--------|
| `internal/world/port.go` | **Modify** | +8 | Add `AttachComponent`, `DetachComponent` to `Tx`; `GetEntityType`, `HasComponent` to `EntityStore` |
| `internal/world/validate_attach.go` | **Create** | ~50 | `ValidateAttachComponent` pure function |
| `internal/world/validate_detach.go` | **Create** | ~40 | `ValidateDetachComponent` pure function |
| `internal/world/validate_attach_test.go` | **Create** | ~80 | Table-driven attach validation tests |
| `internal/world/validate_detach_test.go` | **Create** | ~60 | Table-driven detach validation tests |
| `internal/world/service.go` | **Modify** | +60 | `AttachComponent` + `DetachComponent` methods |
| `internal/world/service_test.go` | **Modify** | +100 | Attach/detach service tests with mocks |
| `internal/world/mock_test.go` | **Modify** | +20 | Extend mocks for new interface methods |
| `internal/storage/entity.go` | **Modify** | +60 | `GetEntityType`, `HasComponent`, `AttachComponent`, `DetachComponent` |
| `internal/storage/attach_detach_test.go` | **Create** | ~120 | Integration tests |
| 2 doc files | **Modify** | ~30 | Update story + plan status |

**Total scope:** ~620 lines across 2 new files + 7 modified files + 1 new test file.

---

## Design Decisions

### Why no upsert semantics on attach

Duplicate component attachment is a bug in the caller (the interpreter), not an idempotent operation. The entity type contract assumes one instance of each component per entity. An upsert would silently hide logic errors and overwrite component data that was intentionally set during entity creation or a previous attach. Fail loud.

### Why `HasComponent` is on `EntityStore`, not `Tx`

The duplicate-attach check happens before the transaction starts. The service reads the current state, decides whether the operation is valid, then opens a transaction to perform the mutation. This is a deliberate **read-before-write** pattern:

```
Read:  GetEntityType + HasComponent  (no Tx)
Validate:  pure function
Write:  BeginTx → AttachComponent → Commit
```

This is safe because the tick loop processes events sequentially. If this changes in the future, the `AttachComponent` INSERT will be protected by the UNIQUE constraint on `entity_id` in the component table.

### Why detaching a required component is always an error, even in warning mode

A required component is part of the entity type's identity contract. Removing it would leave the entity in an inconsistent state (it still claims to be a "Goblin" but has no "Health" component). Unlike creation — where warning mode permits flexibility because the entity doesn't exist yet — detach is a destructive operation on live data. There's no safe "proceed anyway."

### Why `AttachComponent` and `InsertComponent` share the same adapter logic

Both methods INSERT into a `comp_*` table with the same shape. The difference is purely in _when_ and _why_ they're called:
- `InsertComponent` is called during entity creation (transactional, batch)
- `AttachComponent` is called post-creation (standalone transaction, validated separately)

The adapter code is identical. In practice, `AttachComponent` on `sqliteTx` will dispatch to the same type-specific methods (`insertObjectComponent`, `insertScalarComponent`, etc.) that `InsertComponent` uses. This can be refactored to avoid duplication.

---

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| `HasComponent` queries wrong table (case sensitivity) | Low | Use `strings.ToLower(compName)` matching DDL generation |
| Component table doesn't exist for declared component | Low | Schema loader (Story 2) ensures all declared components have tables |
| Attaching with wrong value types for component properties | Medium | Same type-coercion logic as `InsertComponent` in `entity.go` |
| Detach on component with no entity_id column | Very Low | All comp_* tables have entity_id by DDL convention |
