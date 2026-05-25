# Story 2 Implementation Plan: Schema Diff Computation

**Epic:** 2 — Schema versioning & auto-migrations  
**Goal:** Compute a pure, ordered diff between the as-built database schema and the current `schema.json` representation. Produces a list of `Change` structs that feed the DDL generator (Story 3).

---

## Assessment

| Requirement | Current State | Gap |
|---|---|---|
| `DomainSchema` from introspection | ✅ Story 1 completed | Available in `internal/storage/introspect.go` |
| `DatabaseSchema` from file | ✅ Epic 1 completed | Available in `internal/schema/types.go` |
| `propertySQLType()` mapping | ✅ Exists in `componentTableBuilder.go` | Unexported in `storage/`; needs domain-accessible version |
| Diff computation | ❌ No code exists | Pure function needed: compare two schemas → list of `Change` |
| Deterministic ordering | ❌ No code exists | Additions → Modifications → Removals, alphabetically within |

---

## Architecture

**Location:** `internal/schema/diff.go` — pure domain logic, no storage imports, no I/O.

**Problem:** The diff compares `storage.DomainSchema` (SQL types: `TEXT`, `REAL`) with `schema.DatabaseSchema` (semantic types: `string`, `number`). Acceptance criteria require SQL-to-SQL comparison to avoid false positives (e.g. `integer` → `boolean` both map to `INTEGER`).

**Solution:**
1. Move `PropertySQLType` to `internal/schema/` so the diff can map `schema.Property` → SQL type without importing `storage/`.
2. Refactor `storage.propertySQLType` to delegate to `schema.PropertySQLType` (DRY).
3. The `Diff` function operates on a domain-side `DomainSchema` struct (defined in `diff.go`) that the storage layer converts to before calling `Diff`.

### Domain-side DomainSchema (for diff)

```go
// Defined in internal/schema/diff.go — NOT in storage.
type DomainSchema struct {
    SchemaVersion   int
    Components      map[string]DomainComponent  // key = lowercase name
    EntityTypeNames map[string]bool
}

type DomainComponent struct {
    Type    string          // "object", "string", etc.
    Columns []DomainColumn  // each column has Name, SQLType, IsPK
}

type DomainColumn struct {
    Name    string
    SQLType string
    IsPK    bool
}
```

This is a thin mirror of the storage-side `DomainSchema`. The storage layer provides a `ToDiffSchema()` method.

### Change types

```go
type ChangeKind string

const (
    ChangeAddedComponent    ChangeKind = "added_component"
    ChangeRemovedComponent  ChangeKind = "removed_component"
    ChangeAddedProperty     ChangeKind = "added_property"
    ChangeRemovedProperty   ChangeKind = "removed_property"
    ChangedPropertyType     ChangeKind = "changed_property_type"
    ChangeAddedEntityType   ChangeKind = "added_entity_type"
    ChangeRemovedEntityType ChangeKind = "removed_entity_type"
    ChangeChangedEntityType ChangeKind = "changed_entity_type"
)

type Change struct {
    Kind     ChangeKind
    Component string  // lowercase component name
    Property  string  // lowercase property name (for property-level changes)
    OldType   string  // old SQL type (for type changes)
    NewType   string  // new SQL type (for type changes)
    ETName    string  // entity type name (for entity-type changes)
    OldET     *EntityType
    NewET     *EntityType
}
```

### Diff algorithm

```
1. Compare component sets (lowercase keys):
   a. In file but not in DB → AddedComponent
   b. In DB but not in file → RemovedComponent
   c. In both → compare structure:
      - If structural incompatibility (object↔scalar) → RemovedComponent + AddedComponent
      - If both object:
         * Property in file but not DB columns → AddedProperty
         * Property in DB columns but not file → RemovedProperty
         * Property in both, different SQL type → ChangedPropertyType
      - If both scalar:
         * If file's PropertySQLType ≠ DB column's SQLType → ChangedPropertyType
         * Else (same SQL type, e.g. integer↔boolean) → no change

2. Compare entity type sets:
   a. In file but not DB EntityTypeNames → AddedEntityType
   b. In DB but not file → RemovedEntityType
   c. In both → compare RequiredComponents, OptionalComponents, AllowExtraComponents, ValidationLevel
      * Any difference → ChangedEntityType with OldET + NewET

3. Sort changes by phase (deterministic):
   Phase 1 (additions): AddedComponent, AddedProperty, AddedEntityType
   Phase 2 (modifications): ChangedPropertyType, ChangedEntityType
   Phase 3 (removals): RemovedComponent, RemovedProperty, RemovedEntityType
   Within each phase: sort by Component name, then Property name
```

**Key rule:** `entity_id` column is never compared (always present, never in file schema).

---

## Tasks

### Task 1: Add `PropertySQLType` to domain

New file: `internal/schema/sqltype.go` containing `PropertySQLType(Property) string`.

Tests in `internal/schema/sqltype_test.go` — table-driven, one subtest per property type:
- `string` → `TEXT`
- `integer` → `INTEGER`
- `number` → `REAL`
- `boolean` → `INTEGER`
- `entity-ref` → `INTEGER`
- `object` → `TEXT`
- `array` → `TEXT`
- unknown → `TEXT` (default case)

### Task 2: Refactor storage to delegate

In `internal/storage/componentTableBuilder.go`:
```go
func propertySQLType(p schema.Property) string {
    return schema.PropertySQLType(p)
}
```

No changes to callers in `componentTableSQL`. One line changed.

### Task 3: Define domain-side DomainSchema + DomainComponent + DomainColumn + Change

In `internal/schema/diff.go`: define the domain-side types (`DomainSchema`, `DomainComponent`, `DomainColumn`, `ChangeKind` constants, `Change` struct).

These are exported so `storage` can convert to them. They deliberately mirror the storage-side types but live in the domain.

### Task 4: Implement `Diff(domain *DomainSchema, file *DatabaseSchema) []Change`

Pure function in `internal/schema/diff.go`. Algorithm as described above.

Returns an empty (non-nil) slice for identical schemas.

### Task 5: Add `ToDiffSchema()` to storage's `DomainSchema`

In `internal/storage/introspect.go`, add:
```go
func (ds *DomainSchema) ToDiffSchema() *schema.DomainSchema
```

Converts `storage.DomainColumn` → `schema.DomainColumn`, strips `Default` field (not needed for diff), keeps all relevant fields.

### Task 6: Comprehensive diff tests

File: `internal/schema/diff_test.go`. Table-driven tests:

| Subtest | DB state | File state | Expected changes |
|---|---|---|---|
| `IdenticalSchemas_Empty` | 0 components, 0 ETs | Same | `len == 0`, slice not nil |
| `IdenticalSchemas_NonEmpty` | Position{x,y}, Health{hp} | Same | `len == 0` |
| `AddedComponent_One` | 0 components | Position{x,y} | 1× `added_component` |
| `AddedComponent_Multiple` | 0 components | Position, Health, Sprite | 3×, sorted alphabetically |
| `RemovedComponent_One` | Position{x,y} | 0 components | 1× `removed_component` |
| `RemovedComponent_Multiple` | Position, Health, Sprite | 0 components | 3×, sorted |
| `AddedProperty_Object` | Position{x} | Position{x, y} | 1× `added_property` (y) |
| `RemovedProperty_Object` | Position{x, y} | Position{x} | 1× `removed_property` (y) |
| `ChangedPropertyType_Object` | Position{x: TEXT} | Position{x: REAL} | 1× `changed_property_type` (x: TEXT→REAL) |
| `ChangedPropertyType_Scalar` | Health{value: TEXT} | Health{type: integer} | 1× `changed_property_type` (value: TEXT→INTEGER) |
| `SameSQLType_NoChange` | Health{value: INTEGER} (integer) | Health{type: boolean} | `len == 0` (both INTEGER) |
| `ObjectToScalar_RemoveAdd` | Position{x,y} (object) | Position{type: string} | `removed_component` + `added_component` |
| `ScalarToObject_RemoveAdd` | Position{value: TEXT} (string) | Position{x,y} (object) | `removed_component` + `added_component` |
| `Ordering_MixedChanges` | Complex mix | Different mix | additions → modifications → removals |
| `AddedEntityType` | 0 ETs | Player{req: [Position]} | 1× `added_entity_type` |
| `RemovedEntityType` | Player in DB | 0 ETs | 1× `removed_entity_type` |
| `ChangedEntityType_Required` | Player{req: [Position]} | Player{req: [Position, Health]} | 1× `changed_entity_type` |
| `ChangedEntityType_Validation` | Player{level: strict} | Player{level: warning} | 1× `changed_entity_type` |
| `ChangedEntityType_AllFields` | Full spec A | Full spec B | 1× with OldET + NewET |
| `ChangedEntityType_NoChange` | Player{strict, req: [Position]} | Same | `len == 0` |
| `RemovedProperty_Multiple` | Stats{hp, name, active} | Stats{hp} | 2× `removed_property`, sorted |
| `AddedProperty_Multiple` | Stats{hp} | Stats{hp, name, active} | 2× `added_property`, sorted |
| `FullComplexScenario` | All change types mixed | All change types mixed | Correct count, correct order |

For changed entity types, `OldET.RequiredComponents` is compared as a set (order-insensitive): sort both slices before comparison.

---

## Files

| File | Action | Est. lines |
|------|--------|------------|
| `internal/schema/sqltype.go` | **Create** — `PropertySQLType` | ~20 |
| `internal/schema/sqltype_test.go` | **Create** — tests for `PropertySQLType` | ~40 |
| `internal/schema/diff.go` | **Create** — types + `Diff()` | ~200 |
| `internal/schema/diff_test.go` | **Create** — table-driven diff tests | ~450 |
| `internal/storage/introspect.go` | **Edit** — add `ToDiffSchema()` method | ~20 |
| `internal/storage/componentTableBuilder.go` | **Edit** — delegate to domain | ~1 |

**Total: ~730 new lines, ~21 lines modified across 2 existing files.**

---

## Acceptance criteria → test mapping

| Criteria | Tests |
|---|---|
| Detect new components | `TestDiff_AddedComponent_*` |
| Detect removed components | `TestDiff_RemovedComponent_*` |
| Detect added properties | `TestDiff_AddedProperty_*` |
| Detect removed properties | `TestDiff_RemovedProperty_*` |
| Detect SQL type changes | `TestDiff_ChangedPropertyType_*` |
| Detect new entity types | `TestDiff_AddedEntityType` |
| Detect removed entity types | `TestDiff_RemovedEntityType` |
| Detect entity type requirement changes | `TestDiff_ChangedEntityType_*` |
| Identical schemas → empty diff | `TestDiff_IdenticalSchemas_*` |
| Deterministic ordering | `TestDiff_Ordering_MixedChanges` |
| 100% coverage | `go test -cover ./internal/schema/ -run "Diff|PropertySQLType"` |

---

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Map iteration order makes diffs nondeterministic | High | Collect all changes, then `sort.Slice` with total ordering |
| Component type change (object↔scalar) misdetected as property changes | High | Check structural compatibility first; if incompatible, emit remove+add |
| `EntityType.RequiredComponents` slice comparison is order-sensitive | Medium | Sort both slices before `reflect.DeepEqual` or use maps |
| `storage.DomainSchema` vs `schema.DomainSchema` confusion | Medium | Clear naming: storage types stay in `storage/`, domain types in `schema/`. `ToDiffSchema()` conversion is explicit |
| `entity_id` column accidentally compared | Low | Filter: skip columns where `IsPK == true` (that's `entity_id`) |
