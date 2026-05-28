# Story 1 Implementation Plan: Interpreter-Managed Tables and Schema Extensions

**Epic:** 3 — Agents (behavior-as-data) runtime  
**Goal:** Create the three interpreter-owned tables and extend `schema.json` parsing to support `"behavior"` fields and the `"Behavior"` reserved-name check.

---

## Assessment

| Requirement | Current State | Gap |
|---|---|---|
| `behavior_components`, `transitions`, `event_queue` tables | ❌ Do not exist | `EnsureInterpreterTables(db *sql.DB) error` in `internal/storage/` |
| `"behavior"` field on Component | ❌ No such field | Add `Behavior string` to `Component` struct |
| `"behavior"` field on EntityType | ❌ No such field | Add `Behavior string` to `EntityType` struct |
| Reserved-name check for `"Behavior"` | ❌ No check exists | Add to `validateStructure()` in `validate.go` |
| Machine file existence validation | ❌ No check exists | New exported `ValidateBehaviorRefs(s, behaviorsDir)` |

---

## Architecture

### `EnsureInterpreterTables` — `internal/storage/tables.go`

Three `CREATE TABLE IF NOT EXISTS` calls in sequence. No transaction needed — DDL in SQLite is
autocommitted; `IF NOT EXISTS` makes each call idempotent. Call this after `NewSQLiteStore` so
the `entities` table referenced by `behavior_components`'s FK already exists.

```sql
CREATE TABLE IF NOT EXISTS behavior_components (
    entity_id      INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    machine_id     TEXT NOT NULL,
    current_states TEXT NOT NULL,   -- JSON array of active state IDs
    updated_at     INTEGER NOT NULL, -- tick
    PRIMARY KEY (entity_id, machine_id)
);

CREATE TABLE IF NOT EXISTS transitions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    tick        INTEGER NOT NULL,
    wall_ms     INTEGER NOT NULL,
    entity_id   INTEGER NOT NULL,
    machine_id  TEXT NOT NULL,
    from_states TEXT NOT NULL,   -- JSON array
    to_states   TEXT NOT NULL,   -- JSON array
    event       TEXT NOT NULL,
    cond_result INTEGER,         -- 1/0/NULL
    actions_run TEXT NOT NULL    -- JSON array of action type names
);

CREATE TABLE IF NOT EXISTS event_queue (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id   INTEGER NOT NULL,
    machine_id  TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    payload     TEXT,            -- JSON, nullable
    target_tick INTEGER NOT NULL
);
```

### Schema type extensions — `internal/schema/`

`Component` and `EntityType` each get a `Behavior string` field with `json:"behavior,omitempty"`.
The `Component.UnmarshalJSON` two-pass approach decodes `Behavior` automatically via the alias
pass — no logic changes needed.

### Reserved-name check — `validate.go` `validateStructure()`

```go
for name := range s.Components {
    if strings.EqualFold(name, "Behavior") {
        return fmt.Errorf("'Behavior' is a reserved component name")
    }
}
```

Added after the empty-components check, before the entity-types check.

### `ValidateBehaviorRefs` — `validate.go`

Separate exported function (not part of `ValidateSchema`) because the schema loader doesn't know
the `mods/` path. The interpreter startup path calls both `ValidateSchema` and then
`ValidateBehaviorRefs`. For each non-empty `Behavior` field, checks that
`filepath.Join(behaviorsDir, behavior+".json")` exists via `os.Stat`.

---

## Tasks

### Task 1: `EnsureInterpreterTables` — `internal/storage/tables.go` ✅

New file. Three DDL statements executed sequentially.

**Tests** (`internal/storage/tables_test.go`):
- `TestEnsureInterpreterTables_CreatesAllThreeTables`
- `TestEnsureInterpreterTables_Idempotent`
- `TestEnsureInterpreterTables_BehaviorComponents_Schema` — PRAGMA table_info
- `TestEnsureInterpreterTables_Transitions_Schema` — PRAGMA table_info
- `TestEnsureInterpreterTables_EventQueue_Schema` — PRAGMA table_info
- `TestEnsureInterpreterTables_BehaviorComponents_CompositeKey` — same entity_id, two machine_ids succeeds; duplicate (entity_id, machine_id) fails

### Task 2: `Behavior` field on `Component` — `internal/schema/component.go` ✅

One-line struct addition; no UnmarshalJSON logic changes needed.

**Tests** (added to `load_validate_test.go`):
- `TestLoadSchema_ComponentWithBehavior`
- `TestLoadSchema_ComponentWithoutBehavior`

### Task 3: `Behavior` field on `EntityType` — `internal/schema/types.go` ✅

One-line struct addition; standard JSON unmarshalling.

**Tests**:
- `TestLoadSchema_EntityTypeWithBehavior`
- `TestLoadSchema_EntityTypeWithoutBehavior`

### Task 4: Reserved-name check — `internal/schema/validate.go` ✅

`strings.EqualFold` loop in `validateStructure()`.

**Tests**:
- `TestValidateSchema_BehaviorComponentNameRejected` — exact "Behavior"
- `TestValidateSchema_BehaviorLowercaseRejected` — "behavior"
- `TestValidateSchema_BehaviorMixedCaseRejected` — "BEHAVIOR"
- `TestValidateSchema_NonBehaviorComponentAccepted` — "BehaviorData" is not rejected

### Task 5: `ValidateBehaviorRefs` — `internal/schema/validate.go` ✅

New exported function. Uses `os.Stat`; fails fast on first missing file.

**Tests**:
- `TestValidateBehaviorRefs_AllFilesExist`
- `TestValidateBehaviorRefs_MissingComponentBehavior`
- `TestValidateBehaviorRefs_MissingEntityTypeBehavior`
- `TestValidateBehaviorRefs_NoBehaviorFields`
- `TestValidateBehaviorRefs_EmptyBehaviorsDir`

---

## Files

| File | Action | Lines |
|------|--------|-------|
| `internal/storage/tables.go` | **Create** | ~45 |
| `internal/storage/tables_test.go` | **Create** | ~110 |
| `internal/schema/component.go` | **Edit** — add `Behavior string` field | +2 |
| `internal/schema/types.go` | **Edit** — add `Behavior string` field | +2 |
| `internal/schema/validate.go` | **Edit** — reserved-name check + `ValidateBehaviorRefs` | +45 |
| `internal/schema/load_validate_test.go` | **Edit** — 13 new test cases | +160 |

---

## Acceptance criteria → test mapping

| Criterion | Tests |
|---|---|
| `behavior_components` composite PK | `_BehaviorComponents_Schema`, `_CompositeKey` |
| `transitions` columns correct | `_Transitions_Schema` |
| `event_queue` columns correct | `_EventQueue_Schema` |
| Idempotent | `_Idempotent` |
| `"behavior"` on Component | `TestLoadSchema_ComponentWithBehavior` |
| `"behavior"` on EntityType | `TestLoadSchema_EntityTypeWithBehavior` |
| "Behavior" rejected (all cases) | `TestValidateSchema_Behavior*Rejected` |
| Behavior file refs checked | `TestValidateBehaviorRefs_Missing*` |
| No false positives | `TestValidateSchema_NonBehaviorComponentAccepted`, `TestValidateBehaviorRefs_NoBehaviorFields` |
| Existing tests pass | `go test ./...` — zero changes to existing code paths |

---

## Risks

| Risk | Mitigation |
|------|------------|
| `behavior_components` FK to `entities` — bare `:memory:` DB lacks `entities` table | FK not enforced without `PRAGMA foreign_keys = ON`; table tests use raw `:memory:` safely. Document that `EnsureInterpreterTables` must be called after `NewSQLiteStore`. |
| `ValidateBehaviorRefs` is a separate call — easy to forget | Documented in godoc; interpreter startup (Story 5) is where both are wired up. |
