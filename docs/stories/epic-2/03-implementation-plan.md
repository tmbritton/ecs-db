# Story 3 Implementation Plan: DDL Generation from Schema Diff

**Epic:** 2 — Schema versioning & auto-migrations  
**Goal:** Translate the ordered list of `Change` structs from `schema.Diff()` into a safe, executable sequence of SQL statements. The generator produces SQL strings; execution is a separate concern (Story 4).

---

## Assessment

| Requirement | Current State | Gap |
|---|---|---|
| `Diff()` produces `[]Change` | ✅ Story 2 completed | Available in `internal/schema/diff.go` |
| `componentTableSQL` for CREATE | ✅ Epic 1 completed | Exists in `internal/storage/componentTableBuilder.go` |
| Component/Property lookup by lowercase name | ❌ No helper | Need `ComponentByName` and `PropertyByName` in `schema/` |
| DDL generation | ❌ No code exists | `Generator` struct + `Generate()` method needed |
| Table-rebuild pattern for DROP COLUMN | ❌ No code exists | CREATE+INSERT+DROP+RENAME sequence |
| Destructive change flagging | ❌ No code exists | `Statement.Destructive` field |

---

## Architecture

**Location:** `internal/storage/ddlgen.go` — infrastructure that turns domain changes into SQL strings.

**Dependencies:**
- `schema.Diff()` → `[]Change` (input)
- `componentTableSQL()` → `CREATE TABLE` statements (reuse existing)
- `schema.PropertySQLType()` → SQL types for new columns (existing)
- `schema.DomainSchema` → needed for table-rebuilds (know current column layout)

### Generator interface

```go
type Generator struct {
    file   *schema.DatabaseSchema    // file schema (component definitions)
    domain *schema.DomainSchema      // current DB state (needed for rebuilds)
    config Config
}

type Config struct {
    // StrictDrop, if true, includes destructive statements (DROP TABLE,
    // column removal, type changes) in the output. If false, they are
    // omitted so the runner can do a safe-only dry-run.
    StrictDrop bool
}
```

### Statement struct

```go
type Statement struct {
    SQL         string  // The raw SQL to execute
    Kind        string  // "create_table", "alter_add_column", "rebuild_table", "drop_table"
    Destructive bool    // true for DROP TABLE, column removal, type change
    Component   string  // affected component (lowercase)
    Description string  // human-readable summary
}
```

Rebuild operations (remove property, change type) expand into **multiple `Statement` entries** — one per SQL line (CREATE temp, INSERT, DROP old, RENAME). They share the same `Kind: "rebuild_table"` and `Destructive: true`, executed sequentially.

### Generation algorithm

```
For each Change in order:
  added_component   → componentTableSQL() → 1 Statement
  added_property    → ALTER TABLE ADD COLUMN → 1 Statement
  removed_property  → table-rebuild sequence → 4-5 Statements
  changed_type      → table-rebuild sequence → 4-5 Statements
  removed_component → DROP TABLE IF EXISTS → 1 Statement
  entity_type_*     → skip (no DDL)

Special handling:
  Structural incompatibility (object↔scalar on same component) produces
  a removed_component + added_component pair. The drop MUST come before
  the create to avoid "table already exists" errors. Generate() detects
  this pattern and reorders accordingly.
```

---

## Tasks

### Task 1: Add lookup helpers to `schema/`

Append to `internal/schema/diff.go`:

```go
// ComponentByName returns the Component from a DatabaseSchema,
// looking up by lowercase name. Returns (comp, true) on success.
func ComponentByName(db *DatabaseSchema, name string) (Component, bool) { ... }

// PropertyByName returns the Property by lowercase name.
func PropertyByName(props map[string]Property, name string) (Property, bool) { ... }
```

**Tests in** `internal/schema/diff_test.go`:
- `TestComponentByName_Found` — exact match, case-insensitive match
- `TestComponentByName_NotFound` — unknown name, empty schema
- `TestPropertyByName_Found` / `TestPropertyByName_NotFound`

### Task 2: Add `DefaultValueForProperty` to `schema/`

New file: `internal/schema/ddldefaults.go` — maps a `Property` to its SQL `DEFAULT` expression for `ALTER TABLE ADD COLUMN`.

| Property type | Default value |
|---|---|
| `string` | `''` |
| `integer` | `0` |
| `number` | `0.0` |
| `boolean` | `0` |
| `entity-ref` | `NULL` |
| `object` | `'{}'` |
| `array` | `'[]'` |

These must align with the defaults in `componentTableBuilder.go`.

**Tests in** `internal/schema/ddldefaults_test.go` — one subtest per property type, including unknown → `NULL`.

### Task 3: Define `Statement`, `Config`, `Generator` in `internal/storage/ddlgen.go`

```go
type Statement struct {
    SQL, Kind, Component, Description string
    Destructive bool
}

type Config struct{ StrictDrop bool }

type Generator struct { file *schema.DatabaseSchema, domain *schema.DomainSchema, config Config }

func NewGenerator(file *schema.DatabaseSchema, domain *schema.DomainSchema, config Config) *Generator
```

`NewGenerator` panics if `file` is nil (diff is meaningless without a file schema). `domain` may be nil (first-run bootstrap, no DB yet).

**Tests:** `TestNewGenerator_NilFilePanics`, `TestNewGenerator_Valid`

### Task 4: Implement `Generate(changes []schema.Change) []Statement`

Main entry point. Iterates changes, dispatches to per-kind generators. Handles:
- Empty diff → empty slice (not nil)
- Entity type changes → skipped
- `StrictDrop == false` → filters out destructive statements
- Structural incompatibility (same-component drop+add) → reorders drop before create

**Tests:** `TestGenerate_EmptyDiff`, `TestGenerate_EntityTypeChangesSkipped`, `TestGenerate_StrictDropFilters`, `TestGenerate_StructuralChangeReordering`

### Task 5: Implement `genAddComponent`

```go
func (g *Generator) genAddComponent(c schema.Change) []Statement
```

Looks up the component definition via `schema.ComponentByName`, calls `componentTableSQL`. Returns 1 Statement, `Destructive: false`.

**Tests:** `TestGenAddComponent_Object`, `TestGenAddComponent_Scalar`, `TestGenAddComponent_EntityRef`, `TestGenAddComponent_Array`

### Task 6: Implement `genAddProperty`

```go
func (g *Generator) genAddProperty(c schema.Change) []Statement
```

Generates `ALTER TABLE comp_<name> ADD COLUMN <prop> <sqlType> NOT NULL DEFAULT <dflt>`.

**Tests:** `TestGenAddProperty_String`, `TestGenAddProperty_Integer`, `TestGenAddProperty_Number`, `TestGenAddProperty_Boolean`, `TestGenAddProperty_EntityRef`, `TestGenAddProperty_UnknownPropertySkipped`

### Task 7: Implement `genRemoveComponent`

```go
func (g *Generator) genRemoveComponent(c schema.Change) []Statement
```

Generates `DROP TABLE IF EXISTS comp_<name>`. Returns 1 Statement, `Destructive: true`.

**Tests:** `TestGenRemoveComponent_Simple`

### Task 8: Implement table-rebuild helper

```go
func (g *Generator) genRebuild(compName string, reason string) ([]Statement, error)
```

Shared logic for `genRemoveProperty` and `genChangePropertyType`:

1. Look up current columns from `g.domain.Components[compName]` (excluding `entity_id`)
2. Look up file component definition from `g.file.Components`
3. Build new column list from file schema (exclude removed property for removals, apply new types for type changes)
4. Generate statements:
   ```sql
   PRAGMA foreign_keys = OFF;
   CREATE TABLE comp_<name>_new (...);
   INSERT INTO comp_<name>_new SELECT <cols> FROM comp_<name>;
   DROP TABLE comp_<name>;
   ALTER TABLE comp_<name>_new RENAME TO comp_<name>;
   PRAGMA foreign_keys = ON;
   ```

All 6 statements share `Kind: "rebuild_table"`, `Destructive: true`.

**Tests:** `TestGenRebuild_ObjectPropertyRemoval`, `TestGenRebuild_ScalarTypeChange`, `TestGenRebuild_EntityRefUntouched`, `TestGenRebuild_MissingDomainReturnsError`

### Task 9: Implement `genRemoveProperty`

Delegates to `genRebuild`, passing the removed property name for exclusion.

**Tests:** `TestGenRemoveProperty_SingleColumn`, `TestGenRemoveProperty_MultiColumnObject`, `TestGenRemoveProperty_LastDataColumn`

### Task 10: Implement `genChangePropertyType`

Delegates to `genRebuild`, but the new column uses the new SQL type from `c.NewType`.

**Tests:** `TestGenChangePropertyType_Object`, `TestGenChangePropertyType_Scalar`

### Task 11: Structural change reordering

In `Generate()`, after collecting all statements, detect the pattern where the same component has both a `DROP TABLE` and a `CREATE TABLE`. Reorder so the DROP comes before the CREATE.

This handles the `object↔scalar` structural incompatibility case where `Diff()` emits `removed_component` + `added_component` for the same component.

**Tests:** Covered by `TestGenerate_StructuralChangeReordering` (Task 4).

---

## Files

| File | Action | Est. lines |
|------|--------|------------|
| `internal/storage/ddlgen.go` | **Create** — `Statement`, `Generator`, `Generate()`, helpers | ~300 |
| `internal/schema/ddldefaults.go` | **Create** — `DefaultValueForProperty` | ~30 |
| `internal/schema/ddldefaults_test.go` | **Create** — defaults tests | ~40 |
| `internal/storage/ddlgen_test.go` | **Create** — comprehensive tests | ~400 |
| `internal/schema/diff.go` | **Edit** — add `ComponentByName`, `PropertyByName` | ~30 |
| `internal/schema/diff_test.go` | **Edit** — add lookup tests | ~30 |

**Total: ~800 new lines, ~60 lines modified across 2 existing files.**

---

## Acceptance criteria → test mapping

| Criteria | Tests |
|---|---|
| `added_component` → `CREATE TABLE` | `TestGenAddComponent_*` |
| `added_property` → `ALTER TABLE ADD COLUMN` | `TestGenAddProperty_*` |
| `removed_property` → table-rebuild sequence | `TestGenRemoveProperty_*`, `TestGenRebuild_*` |
| `removed_component` → `DROP TABLE` | `TestGenRemoveComponent` |
| SQL type change → table-rebuild sequence | `TestGenChangePropertyType_*` |
| Entity type changes → no DDL | `TestGenerate_EntityTypeChangesSkipped` |
| CREATEs before ALTERs before DROPs | `TestGenerate_Ordering` |
| Empty diff → no statements | `TestGenerate_EmptyDiff` |
| Destructive changes flagged | `TestGenerate_DestructiveFlags` |
| `StrictDrop` filters destructive | `TestGenerate_StrictDropFilters` |
| Structural change reorder (drop before create) | `TestGenerate_StructuralChangeReordering` |
| 100% coverage | `go test -cover ./internal/storage/ -run "DDL\|Rebuild\|Gen"` |

---

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| `domain` is nil (first-run bootstrap) — rebuilds fail | High | `genRemoveProperty`/`genChangePropertyType` return error when `domain` is nil; `Generate()` skips or propagates |
| Map iteration order in rebuild column lists produces non-deterministic SQL | High | Sort column names alphabetically when building SELECT lists and CREATE TABLE column order |
| `ALTER TABLE ADD COLUMN` with FK reference on existing rows | Medium | Entity-ref columns default to `NULL`; FK constraint allows NULL; existing rows get NULL |
| Structural incompatibility reorder creates a window of dropped data | Medium | All statements run in one transaction (runner concern); rollback if any fail |
| PRAGMA foreign_keys is connection-scoped; must be in same batch | Medium | Rebuild statements include PRAGMA OFF/ON as first/last entries in the statement group |
| `componentTableSQL` uses `IF NOT EXISTS` which silently skips existing tables | Low | `Generate()` only emits CREATE for `added_component` (table confirmed absent by diff) |
| SQLite `DROP TABLE IF EXISTS` on non-existent table is a no-op | Low | Acceptable; diff guarantees the table exists when `removed_component` is emitted |
