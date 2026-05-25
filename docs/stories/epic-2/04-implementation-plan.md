# Story 4 Implementation Plan: Auto-Migration Runner

**Epic:** 2 — Schema versioning & auto-migrations  
**Goal:** Integrate auto-migration into the `NewSQLiteStore` startup flow. When a schema version mismatch is detected, the runner: introspects the DB → computes diff → generates DDL → executes all statements in a single transaction → updates `meta.schema_version`. Zero manual migration files — the engine handles it.

**Depends on:** Story 1 (introspection), Story 2 (diff), Story 3 (DDL generation)

---

## Assessment

| Requirement | Current State | Gap |
|---|---|---|
| `NewSQLiteStore` bootstrap flow | ✅ Epic 1 completed | Creates tables or returns `SchemaVersionMismatchError` on version mismatch |
| `SchemaVersionMismatchError` | ✅ Epic 1 completed | Caller receives error; migration not yet triggered |
| `IntrospectAll` → `DomainSchema` | ✅ Story 1 completed | Reconstructs DB state from SQLite |
| `schema.Diff()` → `[]Change` | ✅ Story 2 completed | Pure diff: DB state vs file schema |
| `ddlgen.Generator.Generate()` → `[]Statement` | ✅ Story 3 completed | SQL statements from diff |
| `SchemaMigrationError` | ❌ No type exists | Rich error with change name, SQL text, underlying error |
| Transactional migration execution | ❌ No code exists | Run all statements in one tx + update meta |
| `MigrationPolicy` (auto/confirm) | ❌ No type exists | Controls destructive change behavior |
| `NewSQLiteStore` integration with runner | ❌ No code exists | Wire runner into mismatch path |

---

## Architecture

### MigrationError type

```go
// SchemaMigrationError occurs when a migration fails partway through.
type SchemaMigrationError struct {
    Change       string // Component/property name that failed
    ChangeKind   string // "added_component", "removed_property", etc.
    SQL          string // The specific SQL statement that failed
    Underlying   error  // The underlying driver error
    StatementIdx int    // Index within the statement batch
    TotalStmts   int    // Total statements in the batch
}
```

Implements `error` with a formatted message:
```
migration failed: added_component "position" — statement 1/6
SQL: CREATE TABLE comp_position (...)
underlying: table comp_position already exists
```

### MigrationPolicy type

```go
type MigrationPolicy string

const (
    // MigrationAuto always runs migrations without prompting.
    MigrationAuto MigrationPolicy = "auto"
    // MigrationConfirm blocks on destructive changes (DROP TABLE,
    // column removal, type change). RunMigrate returns
    // *MigrationRequiresConfirmation with a list of destructive
    // statements for the caller to handle (CLI prompt, config flag, etc.).
    MigrationConfirm MigrationPolicy = "confirm"
)

// MigrationRequiresConfirmation is returned when policy=confirm and
// destructive changes are detected.
type MigrationRequiresConfirmation struct {
    DestructiveStatements []Statement // The destructive statements to review
}
```

Implements `error` with a formatted message listing all destructive changes.

### MigrationRunner struct

```go
type MigrationRunner struct {
    db       *sql.DB                // live connection (already opened)
    file     schema.DatabaseSchema  // current schema.json
    policy   MigrationPolicy
    logger   MigrationLogger        // interface for structured logging (or fmt fallback)
}
```

### MigrationLogger interface

```go
type MigrationLogger interface {
    Infof(format string, args ...interface{})
    Warnf(format string, args ...interface{})
}
```

A `nopLogger` satisfies this for tests. A `log`-based adapter for production.

### Run() method — the core orchestration

```go
func (r *MigrationRunner) Run() error {
    // 1. Introspect DB → DomainSchema
    domain, err := storage.IntrospectAll(r.db)
    // (Handle case where meta table exists but DB is empty — still valid for first migration)

    // 2. Compute diff
    changes := schema.Diff(domain, &r.file, nil /* no oldFile on first migration */)
    if len(changes) == 0 {
        return nil // nothing to do
    }

    // 3. Generate DDL
    gen := ddlgen.NewGenerator(&r.file, domain, ddlgen.Config{StrictDrop: true})
    stmts := gen.Generate(changes)
    if len(stmts) == 0 {
        return nil // changes exist (e.g. entity type only) but produce no DDL
    }

    // 4. Check policy for destructive changes
    if r.policy == MigrationConfirm {
        destructive := make([]ddlgen.Statement, 0)
        for _, s := range stmts {
            if s.Destructive {
                destructive = append(destructive, s)
            }
        }
        if len(destructive) > 0 {
            return &MigrationRequiresConfirmation{DestructiveStatements: destructive}
        }
    }

    // 5. Begin transaction
    tx, err := r.db.Begin()
    // ...

    // 6. Execute each statement
    for i, stmt := range stmts {
        _, err := tx.Exec(stmt.SQL)
        if err != nil {
            _ = tx.Rollback()
            return &SchemaMigrationError{
                Change:       stmt.Component,
                ChangeKind:   inferChangeKindFromStatement(stmt),
                SQL:          stmt.SQL,
                Underlying:   err,
                StatementIdx: i,
                TotalStmts:   len(stmts),
            }
        }
    }

    // 7. Update meta
    _, err = tx.Exec("UPDATE meta SET value = ? WHERE key = 'schema_version'", 
        r.file.SchemaVersion)
    _, err = tx.Exec("UPDATE meta SET value = ? WHERE key = 'build_time'",
        time.Now().UTC().Format(time.RFC3339))
    if err != nil {
        _ = tx.Rollback()
        return fmt.Errorf("updating meta after migration: %w", err)
    }

    // 8. Commit
    if err := tx.Commit(); err != nil {
        return fmt.Errorf("committing migration: %w", err)
    }

    r.logger.Infof("migration complete: %d statements applied, version %d → %d",
        len(stmts), domain.SchemaVersion, r.file.SchemaVersion)

    return nil
}
```

---

## Tasks

### Task 1: Add `SchemaMigrationError` type

New section in `internal/storage/migration.go`:

```go
type SchemaMigrationError struct { ... }
func (e *SchemaMigrationError) Error() string   // formatted message
func (e *SchemaMigrationError) Unwrap() error   // returns Underlying
```

**Tests in** `internal/storage/migration_test.go`:
- `TestSchemaMigrationError_ErrorFormat` — verify the formatted message
- `TestSchemaMigrationError_Unwrap` — errors.Is works on underlying

### Task 2: Add `MigrationPolicy`, `MigrationRequiresConfirmation`

In `internal/storage/migration.go`:

```go
type MigrationPolicy string
const MigrationAuto, MigrationConfirm MigrationPolicy = "auto", "confirm"

type MigrationRequiresConfirmation struct { DestructiveStatements []Statement }
func (e *MigrationRequiresConfirmation) Error() string
```

**Tests:**
- `TestMigrationRequiresConfirmation_ErrorListsDestructive` — message lists all destructive statements

### Task 3: Add `MigrationLogger` interface + `nopLogger`

In `internal/storage/migration.go`:

```go
type MigrationLogger interface {
    Infof(format string, args ...interface{})
    Warnf(format string, args ...interface{})
}

type nopLogger struct{} // satisfies interface, does nothing
func NopLogger() MigrationLogger { return nopLogger{} }
```

**Tests:** `TestNopLogger_DoesNotPanic` — call both methods, no crash

### Task 4: Implement `MigrationRunner.Run()` — happy path

In `internal/storage/migration.go`:

```go
type MigrationRunner struct { db, file, policy, logger }
func NewMigrationRunner(db *sql.DB, file schema.DatabaseSchema, policy MigrationPolicy, logger MigrationLogger) *MigrationRunner
func (r *MigrationRunner) Run() error
```

Core flow: introspect → diff → generate → exec → update meta → commit.

**Tests:**
- `TestMigrate_NoChanges_ReturnsNil` — DB at same version as file, no changes
- `TestMigrate_AddComponent_Success` — single added_component, table created
- `TestMigrate_AddProperty_Success` — single added_property, column added
- `TestMigrate_MixedChanges_Success` — multiple statement types, all succeed
- `TestMigrate_EntityTypeOnlyChanges_NoDDLButNoError` — only entity type changes, succeeds silently
- `TestMigrate_UpdatesMetaVersion` — schema_version updated after commit
- `TestMigrate_UpdatesMetaBuildTime` — build_time updated after commit

### Task 5: Implement `MigrationRunner.Run()` — transactional rollback

When any statement fails, rollback and return `SchemaMigrationError`.

**Tests:**
- `TestMigrate_FailedStatement_Rollback` — mock a failing statement, verify no partial state
- `TestMigrate_FailedStatement_ErrorIncludesDetails` — SchemaMigrationError has correct change, SQL, underlying
- `TestMigrate_MetaNotUpdated_OnFailure` — meta unchanged after rollback
- `TestMigrate_CommitFailure_ReturnsError` — simulate commit failure after all statements succeed

### Task 6: Implement `MigrationPolicy = confirm` blocking

When `policy == MigrationConfirm` and destructive statements exist, return `MigrationRequiresConfirmation`.

**Tests:**
- `TestMigrate_ConfirmPolicy_DestructiveBlocks` — DROP TABLE triggers confirmation error
- `TestMigrate_ConfirmPolicy_NonDestructiveProceeds` — only CREATE/ALTER statements, auto-proceeds
- `TestMigrate_ConfirmPolicy_EmptyDestructiveList` — no destructive changes, returns nil
- `TestMigrationRequiresConfirmation_ContainsDropStatement` — destructive list includes DROP

### Task 7: Implement `MigrationPolicy = auto` (default)

When `policy == MigrationAuto`, always proceed regardless of destructive changes.

**Tests:**
- `TestMigrate_AutoPolicy_DestructiveProceeds` — DROP TABLE runs without blocking
- `TestMigrate_DefaultPolicyIsAuto` — zero-value or default config uses auto

### Task 8: Integrate runner into `NewSQLiteStore`

Modify `internal/storage/sqlite.go`:
1. Change `NewSQLiteStore` signature to accept a `StoreConfig` struct:
```go
type StoreConfig struct {
    Schema       schema.DatabaseSchema
    SchemaHash   string
    MigrationPolicy MigrationPolicy
    Logger       MigrationLogger  // optional, nil → nopLogger
    // future: BackupPath, MigrationTimeout, etc.
}
```
2. On version match: proceed as before
3. On version mismatch: create `MigrationRunner`, call `Run()`, handle results:
   - `nil` → migration succeeded, return the store
   - `*MigrationRequiresConfirmation` → return it as error (caller decides)
   - any other error → return it (migration failed)
4. On success (migration or no-migration): return `*SQLiteStore`

**Backward compatibility:** Keep the existing 3-argument `NewSQLiteStore(dbPath, schema, hash)` as a wrapper that calls the new `NewSQLiteStoreWithConfig` with `MigrationAuto` and `NopLogger()`.

**Tests:**
- `TestNewSQLiteStore_VersionMatch_NoMigration` — same version, no runner invoked
- `TestNewSQLiteStore_VersionMismatch_MigratesAndReturnsStore` — different version, migrates, returns store
- `TestNewSQLiteStore_MigrationFails_ReturnsError` — migration fails, no store returned
- `TestNewSQLiteStore_ConfirmPolicy_DestructiveReturnsConfirmationError` — confirms blocked
- `TestNewSQLiteStore_BackwardCompatible_ThreeArgSignature` — old API still works

### Task 9: Wire into hexagonal architecture

The runner is a domain use case (orchestration). Per AGENTS.md, the hexagonal boundary means:
- Domain port: `MigrationExecutor` interface in `internal/ports.go` (TBD) with `Run() error`
- Implementation: `MigrationRunner` in `storage/`
- Composition root: `cmd/` wires concrete runner with `*sql.DB`, file schema, policy

Since the runner needs `*sql.DB` and the file schema, it lives entirely in `storage/` for now. When the hexagonal boundary is fully realized (ports.go exists), extract the interface.

**No new files needed for now** — the interface extraction is deferred until Epic 3+ when multiple components need it.

---

## Files

| File | Action | Est. lines |
|------|--------|------------|
| `internal/storage/migration.go` | **Create** — `SchemaMigrationError`, `MigrationPolicy`, `MigrationRequiresConfirmation`, `MigrationLogger`, `MigrationRunner` | ~200 |
| `internal/storage/migration_test.go` | **Create** — comprehensive runner tests | ~400 |
| `internal/storage/sqlite.go` | **Edit** — add `StoreConfig`, `NewSQLiteStoreWithConfig`, integrate runner | ~80 |
| `internal/storage/sqlite_test.go` | **Edit** — add integration tests for migration in NewSQLiteStore | ~120 |

**Total: ~680 new lines, ~200 lines modified across 2 existing files.**

---

## Acceptance criteria → test mapping

| Criteria | Tests |
|---|---|
| `NewSQLiteStore` integrates auto-migration | `TestNewSQLiteStore_VersionMismatch_MigratesAndReturnsStore` |
| Introspect → diff → DDL → exec in single transaction | `TestMigrate_MixedChanges_Success` |
| Meta updated after migration | `TestMigrate_UpdatesMetaVersion`, `TestMigrate_UpdatesMetaBuildTime` |
| All statements in same transaction | `TestMigrate_FailedStatement_Rollback` (rollback proves single tx) |
| Error includes component name, change type, SQL, underlying | `TestMigrate_FailedStatement_ErrorIncludesDetails`, `TestSchemaMigrationError_ErrorFormat` |
| `MigrationPolicy` controls destructive behavior | `TestMigrate_ConfirmPolicy_DestructiveBlocks`, `TestMigrate_AutoPolicy_DestructiveProceeds` |
| No migration when versions match | `TestNewSQLiteStore_VersionMatch_NoMigration`, `TestMigrate_NoChanges_ReturnsNil` |
| 100% coverage on orchestration | `go test -cover ./internal/storage/ -run "Migrat"` |

---

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| `IntrospectAll` fails on empty DB (no meta table yet) | High | Handle gracefully: return empty `DomainSchema{SchemaVersion: 0}`; diff will produce all additions |
| Transaction locks during long migrations (e.g. large table rebuilds) | Medium | WAL mode allows concurrent reads; writes are exclusive. Runner should log each statement so progress is visible |
| `MigrationRequiresConfirmation` leaks the runner's internal state | Low | Only expose `DestructiveStatements` as a slice copy, not internals |
| Meta update succeeds but commit fails (e.g. disk full) | High | Meta update is inside the same transaction as DDL — commit failure rolls back everything |
| Schema version updated but build_time update fails within same tx | Low | Both updates in the same `Exec` batch or separate `Exec` calls — either both commit or neither does |
| `NewSQLiteStore` backward compatibility broken | Medium | Keep the 3-arg constructor as a thin wrapper; all existing callers unchanged |
| Diff returns entity-type-only changes → zero DDL statements | Low | Handled: `Run()` returns `nil` when `len(stmts) == 0` even if `len(changes) > 0` |
| `schema.Diff()` needs `oldFile` (nil = first migration) | Low | Pass `nil` from `NewSQLiteStore` — only `ChangedEntityType` detection is skipped, which is correct since there's no baseline |
