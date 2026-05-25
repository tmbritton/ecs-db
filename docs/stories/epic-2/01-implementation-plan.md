# Story 1 Implementation Plan: Database Introspection

**Epic:** 2 — Schema versioning & auto-migrations  
**Goal:** Reconstruct a `DomainSchema` from a live SQLite database by querying `sqlite_master` and `PRAGMA table_info` on each `comp_*` table.

---

## Assessment

| Requirement | Current State | Gap |
|---|---|---|
| List component tables | ❌ No code exists | Need `sqlite_master` query for `comp_*` tables |
| Reconstruct component shape | ❌ No code exists | Need `PRAGMA table_info` parsing into `DomainComponent` |
| Round-trip fidelity | ❌ No tests exist | Need integration tests proving CREATE→introspect→compare works |
| Read `schema_version` from meta | ✅ `checkSchemaVersion` reads it | Need an exported convenience wrapper |
| Handle all component types | ❌ No code exists | object, scalar, entity-ref, array all need coverage |

---

## Architecture

New file: `internal/storage/introspect.go` (storage layer, imports `database/sql`).  
Output types are defined in the same file — they're not domain types, they're a read-only projection of SQLite structure.

### Output types

```
DomainSchema
├── SchemaVersion   int                    ← from meta table
├── Components      map[string]DomainComponent  ← key = lowercase name ("position"), not "comp_position"
└── EntityTypeNames map[string]bool        ← SELECT DISTINCT entity_type FROM entities

DomainComponent
├── Type    string         ← recovered from column structure
└── Columns []DomainColumn  ← ordered by PRAGMA cid

DomainColumn
├── Name    string  ← "entity_id", "x", "value", etc.
├── SQLType string  ← "INTEGER", "REAL", "TEXT", "BOOLEAN"
└── IsPK    bool    ← true for entity_id
```

### Key design decisions

**Lowercase keys.** `componentTableSQL` always generates `comp_<lowercase(name)>`. During introspection we `TrimPrefix("comp_")` to get the lowercase name back (e.g., `"comp_position"` → `"position"`). Story 2's diff layer will lowercase `schema.json` component names before comparison.

**SQL types directly.** No attempt to reverse-map `INTEGER` → `boolean` vs `integer` vs `entity-ref`. The diff layer converts `schema.Property` → SQL type via `propertySQLType()` and compares SQL-to-SQL. The `DomainComponent.Type` field is a best-effort inference but the diff compares columns, not type strings.

**Type inference from column structure.** After stripping the `entity_id` PK column:
- Zero data columns → `"object"` (empty object)
- One column named `"value"` + `PRAGMA dflt_value = '[]'` → `"array"`
- One column named `"value"` + type `"BOOLEAN"` → `"boolean"`
- One column named `"value"` + type `"TEXT"` (not `'[]'` default) → `"string"`
- One column named `"value"` + type `"INTEGER"` → `"integer"`
- One column named `"value"` + type `"REAL"` → `"number"`
- One column named `"target_entity_id"` → `"entity-ref"`
- Two or more data columns → `"object"`

---

## Tasks

### Task 1: `ListComponentTables(db *sql.DB) ([]string, error)`

Query: `SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'comp_%' ORDER BY name`

**Tests (new, `introspect_test.go`):**
- `TestListComponentTables_EmptyDatabase_ReturnsEmptySlice` — bootstrap with no components, assert `len == 0` and slice is non-nil
- `TestListComponentTables_MultipleComponents_ReturnsSorted` — manually `CREATE TABLE` three `comp_*` tables, assert exact list in order
- `TestListComponentTables_IgnoresNonComponentTables` — create tables named `foo`, `data`, `events`; assert none appear

### Task 2: `IntrospectComponentTable(db *sql.DB, tableName string) ([]DomainColumn, error)`

Query: `PRAGMA table_info(<tableName>)` — returns `cid | name | type | notnull | dflt_value | pk`. Each row → one `DomainColumn`.

Errors if the table has zero columns. Does not validate that `entity_id` is the first PK column — that's the caller's concern.

**Tests:**
- `TestIntrospectComponentTable_ObjectComponent` — bootstrap with `Position{x: number, y: number}`, assert 3 columns with correct names/types, `entity_id` is PK
- `TestIntrospectComponentTable_ScalarTypes` — table-driven: string, integer, number, boolean — each asserts 2 columns with correct type names
- `TestIntrospectComponentTable_EntityRef` — assert `target_entity_id INTEGER` column
- `TestIntrospectComponentTable_Array` — assert `value TEXT` with `dflt_value = '[]'`
- `TestIntrospectComponentTable_NonExistentTable_ReturnsEmpty` — call on a table that doesn't exist, expect empty slice and no error (PRAGMA on missing table just returns zero rows)
- `TestIntrospectComponentTable_EmptyObject` — object with zero properties → only `entity_id` column, 1 column total

### Task 3: `InferComponentType(cols []DomainColumn) string`

Pure function (no DB). Strips `entity_id` from the column list, then applies the type inference rules from the design decisions above.

**Tests:**
- `TestInferComponentType` — table-driven, one subtest per component type including edge cases (empty object, zero columns)

### Task 4: `ReadSchemaVersion(db *sql.DB) (int, error)`

Same SQL as existing `checkSchemaVersion`: `SELECT value FROM meta WHERE key = 'schema_version'`. Parsed via `strconv.Atoi`.

**Tests:**
- `TestReadSchemaVersion_ReturnsStoredVersion` — bootstrap at version 5, assert 5
- `TestReadSchemaVersion_MetaMissing_ReturnsError` — fresh DB with no `meta` table
- `TestReadSchemaVersion_KeyMissing_ReturnsError` — `meta` exists but no `schema_version` row

### Task 5: `IntrospectAll(db *sql.DB) (*DomainSchema, error)`

Pipeline:
1. `ReadSchemaVersion` → set on `DomainSchema` (if meta missing, return `SchemaVersion: 0` with a wrapped error)
2. `ListComponentTables` → list of `comp_*` names
3. For each: `IntrospectComponentTable` → `DomainColumn` slice → `InferComponentType` → build `DomainComponent`
4. `SELECT DISTINCT entity_type FROM entities` → populate `EntityTypeNames` (empty map if table doesn't exist)
5. Return assembled `DomainSchema`

**Tests:**
- `TestIntrospectAll_EmptySchema` — bootstrap with zero components, assert `SchemaVersion` set, `Components` map non-nil but empty, `EntityTypeNames` empty
- `TestIntrospectAll_FullSchema` — bootstrap with 3 components (object, scalar, entity-ref), assert all three present with correct shapes, no fixed tables in Components
- `TestIntrospectAll_WithEntityTypes` — insert an entity row `'some-entity-uuid', 'Player', 0`, assert `EntityTypeNames["Player"] == true`

### Task 6: Round-trip integration test suite

Table-driven test proving the complete cycle: `schema.DatabaseSchema` → `NewSQLiteStore` → `IntrospectAll` → verify.

**`TestIntrospectAll_RoundTrip`:**

| Case | Schema | Verify |
|------|--------|--------|
| empty | `SchemaVersion: 7`, no components | `SchemaVersion == 7`, 0 components |
| single object property | `Position{x: number}` | `position` component, columns: `entity_id(INTEGER PK), x(REAL)`, type `"object"` |
| three mixed properties | `Stats{hp: integer, name: string, active: boolean}` | `stats` component, 4 columns, correct types |
| all scalar types | 6 components: string, integer, number, boolean, entity-ref, array | each recovered with correct inferred type |
| empty object | `Marker{}` (zero properties) | `marker` component, 1 column (`entity_id`), type `"object"` |
| multi-component schema | `Position`, `Health`, `Sprite` | all three present, correct shapes |

For each subtest: bootstrap → `IntrospectAll` → for each component, compare column count, names, SQL types, PK flags, and inferred type.

---

## Files

| File | Action | Est. lines |
|------|--------|------------|
| `internal/storage/introspect.go` | **Create** — types + 5 functions | ~120 |
| `internal/storage/introspect_test.go` | **Create** — unit + integration tests | ~200 |

**Total: ~320 new lines. Zero changes to existing files.**

---

## Acceptance criteria → test mapping

| Criteria | Tests |
|---|---|
| List all `comp_*` tables | `TestListComponentTables_*` |
| Produce representation of columns and SQL types | `TestIntrospectComponentTable_*` |
| Round-trip fidelity | `TestIntrospectAll_RoundTrip` |
| All component types recovered | `TestIntrospectComponentTable_*` + `TestInferComponentType` |
| Full-schema + schema_version | `TestIntrospectAll_FullSchema`, `TestReadSchemaVersion_*` |
| Fixed tables excluded | `TestListComponentTables_IgnoresNonComponentTables`, `TestIntrospectAll_FullSchema` |
| 100% coverage | `go test -cover ./internal/storage/ -run Introspect` |

---

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| `PRAGMA table_info` returns `BOOLEAN` as the declared type (not `INTEGER`) | Low | `componentTableBuilder.go` emits `BOOLEAN`; PRAGMA preserves it |
| `dflt_value` in PRAGMA output has unexpected quoting/format | Medium | Inspect actual PRAGMA output in the first test; adjust inference to match |
| Column ordering from PRAGMA differs from CREATE order | Low | PRAGMA returns `cid` (creation order); `ORDER BY cid` is implicit |
| Map iteration order in `componentTableSQL` makes property ordering nondeterministic | Low (for introspection) | Properties are stored in whatever order Go iterates the map; introspection just reads what's there. Diff compares by column name, not position |
