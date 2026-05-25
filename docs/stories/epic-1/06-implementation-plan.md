# Story 6 Implementation Plan: world.sqlite Bootstrap & Schema Version Management

## Problem

`storage.InitDb` (currently `NewSQLiteStore`) writes `schema_version` to `meta` but has no version comparison on open, no build info, and no atomic transaction wrapping for DDL + meta writes. Epic 2 migrations need the version infrastructure in place.

## Steps (in order)

### Step 1: Define `ErrSchemaVersionMismatch` sentinel error (`internal/storage/errors.go`)
- Typed struct `SchemaVersionMismatchError` with `DBVersion` and `FileVersion` int fields
- `Error()` method: `"schema version mismatch: database has version %d, schema.json has version %d"`
- Top-level `var ErrSchemaVersionMismatch error = &SchemaVersionMismatchError{}` sentinel for `errors.Is`
- Test: error message format, `errors.Is` compatibility, struct field access

### Step 2: Tests-first — `internal/storage/bootstrap_test.go`
Table-driven integration tests (`:memory:` per test):
- `TestNewSQLiteStore_FreshDatabaseWritesSchemaVersionToMeta` — verify `schema_version` row matches input
- `TestNewSQLiteStore_FreshDatabaseWritesBuildTimeToMeta` — verify `build_time` row is valid RFC 3339
- `TestNewSQLiteStore_FreshDatabaseWritesSchemaHashToMeta` — verify `schema_hash` row when hash provided
- `TestNewSQLiteStore_ExistingDatabaseMatchingVersionSucceeds` — open twice with same version, no error, no duplicate meta rows
- `TestNewSQLiteStore_ExistingDatabaseMismatchedVersionReturnsError` — create at v1, open with v2 → `ErrSchemaVersionMismatch`
- `TestNewSQLiteStore_FilenameHasNoVersionSuffix` — file created is exactly the path passed
- `TestNewSQLiteStore_MetaTableCreatedFirst` — `meta` exists before component tables during creation

### Step 3: Modify `NewSQLiteStore` in `internal/storage/sqlite.go`
- Wrap entire DDL + meta sequence in a single `BEGIN`/`COMMIT` transaction
- Fresh vs. existing detection: `SELECT name FROM sqlite_master WHERE type='table' AND name='meta'`
- **Fresh path**: create `meta` first (own `Exec` before transaction, as DDL auto-commits), begin transaction, create remaining fixed tables, create component tables, write meta rows (`schema_version`, `build_time`, `schema_hash`), commit
- **Existing path**: query `meta` for `schema_version`, compare → mismatch returns `*SchemaVersionMismatchError`, match succeeds without recreating tables
- Signature: `func NewSQLiteStore(dbPath string, s schema.DatabaseSchema, schemaHash string) (*SQLiteStore, error)`

### Step 4: Update all call sites
- Update `internal/storage/storage_test.go` call sites for new signature (empty hash is fine)
- Update `internal/storage/attach_detach_test.go` if it calls `NewSQLiteStore`
- Update `cmd/cli` if it exists and calls `NewSQLiteStore`

## Acceptance Criteria → Test Mapping

| Acceptance Criterion | Test |
|---|---|
| File named exactly `ecs.db` (no version suffix) | `TestNewSQLiteStore_FilenameHasNoVersionSuffix` |
| `meta.schema_version` matches `schema.json` version | `TestNewSQLiteStore_FreshDatabaseWritesSchemaVersionToMeta` |
| `meta.build_time` is valid timestamp | `TestNewSQLiteStore_FreshDatabaseWritesBuildTimeToMeta` |
| Same version re-open succeeds, data preserved | `TestNewSQLiteStore_ExistingDatabaseMatchingVersionSucceeds` |
| Different version returns `ErrSchemaVersionMismatch` with both versions | `TestNewSQLiteStore_ExistingDatabaseMismatchedVersionReturnsError` |
| `meta` table created first | `TestNewSQLiteStore_MetaTableCreatedFirst` |
| Existing DB detection via `meta` query | `TestNewSQLiteStore_ExistingDatabaseMatchingVersionSucceeds` |
