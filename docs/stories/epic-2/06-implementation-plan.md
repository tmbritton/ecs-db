# Implementation Plan: Story 6 — Backup Before Migrate

## Context

Stories 1–5 built and tested the full migration pipeline. Story 6 adds a safety net: before any migration DDL executes, copy the database file to a versioned backup path (`{basename}.bak.v{version}`). If migration corrupts data, the user has a restore point. Backup failure is non-blocking — it logs a warning and migration proceeds. Configurable retention prevents disk bloat.

## What to Build

Four changes:

1. **New file**: `internal/storage/backup.go` — `backupDatabase` (VACUUM INTO) and `pruneBackups` (glob → sort by version → delete oldest)
2. **Modify**: `internal/storage/sqlite.go` — add `BackupRetention int` to `StoreConfig`; extend `checkAndMigrate` to accept `dbPath string` and call backup
3. **New file**: `internal/storage/backup_test.go` — unit tests for both backup functions
4. **Modify**: `internal/storage/migration_integration_test.go` — add `TestSmoke_BackupCreatedBeforeMigration`

## `backup.go`

Two unexported functions:

```go
// backupDatabase creates {dbPath}.bak.v{version} via VACUUM INTO.
// Removes any pre-existing backup at that path first. Returns the backup path.
func backupDatabase(db *sql.DB, dbPath string, version int) (string, error)

// pruneBackups glob-finds {dbPath}.bak.v* files, sorts by version number,
// deletes all but the newest `retention` files. Logs warnings for failures.
func pruneBackups(dbPath string, retention int, logger MigrationLogger)
```

**Why VACUUM INTO**: Flushes WAL, coalesces all pages, and produces a standalone valid SQLite file from an open connection. `io.Copy` on a WAL-mode file risks an incomplete state.

**Path injection safety**: Escape `'` → `''` in the path before interpolating into the VACUUM INTO SQL string.

## `sqlite.go` changes

Add to `StoreConfig`:
```go
// BackupRetention is the number of versioned backups to keep before migration.
// 0 (the default) disables backup. A positive value enables backup and retention.
BackupRetention int
```

Add unexported helper:
```go
func isMemoryDB(path string) bool {
    return path == "" || strings.Contains(path, ":memory:")
}
```

Change `checkAndMigrate` to `checkAndMigrate(db *sql.DB, dbPath string, cfg StoreConfig) error`.

After extracting `mismatch.DBVersion`, add backup call before `runner.Run()`:
```go
if cfg.BackupRetention > 0 && !isMemoryDB(dbPath) {
    backupPath, backupErr := backupDatabase(db, dbPath, mismatch.DBVersion)
    if backupErr != nil {
        cfg.Logger.Warnf("backup failed (migration will proceed): %v", backupErr)
    } else {
        cfg.Logger.Infof("backup created: %s", backupPath)
        pruneBackups(dbPath, cfg.BackupRetention, cfg.Logger)
    }
}
```

Update the one call site in `NewSQLiteStoreWithConfig`:
```go
if err := checkAndMigrate(db, dbPath, cfg); err != nil {
```

`NewSQLiteStore` (3-arg backward-compat form): no change — zero-value `BackupRetention` = 0 = disabled.

## `backup_test.go`

All tests use `t.TempDir()` for real file paths.

| Test | What it checks |
|------|---------------|
| `TestBackupDatabase_CreatesValidFile` | VACUUM INTO succeeds; returned path readable; meta queryable via a fresh `sql.Open` |
| `TestBackupDatabase_PathPattern` | returned path == `dbPath + ".bak.v" + strconv.Itoa(version)` |
| `TestBackupDatabase_OverwritesExisting` | backup succeeds even if a file already exists at that path |
| `TestBackupDatabase_Failure` | path with nonexistent parent dir → returns error |
| `TestPruneBackups_KeepsNewestN` | create 5 backup files (v1–v5), prune retention=3 → v1 and v2 deleted, v3–v5 remain |
| `TestPruneBackups_NoOpWhenBelowRetention` | 2 files, retention=3 → no deletions |
| `TestPruneBackups_EmptyDirectory` | no matching files → no panic |
| `TestPruneBackups_SkipsNonNumericSuffix` | file `world.sqlite.bak.vfoo` is not parsed and not deleted |

## `migration_integration_test.go` addition

`TestSmoke_BackupCreatedBeforeMigration`:
1. Open v1 via `NewSQLiteStoreWithConfig` with `BackupRetention: 3`
2. Insert one entity + component row; close
3. Reopen as v2 (migration runs, backup fires first)
4. Assert `{path}.bak.v1` exists on disk
5. Open the backup with `sql.Open`; query meta; assert `schema_version = "1"` (valid pre-migration DB)

## Critical Files

| Action | File |
|--------|------|
| **Create** | `internal/storage/backup.go` |
| **Create** | `internal/storage/backup_test.go` |
| **Modify** | `internal/storage/sqlite.go` |
| **Modify** | `internal/storage/migration_integration_test.go` |

## Verification

```bash
go test ./internal/storage/... -run TestBackup -v
go test ./internal/storage/... -run TestSmoke -v
go test ./...
go test -coverprofile=cover.out ./internal/storage/... && go tool cover -func=cover.out
```

All tests must pass. `backup.go` must reach 100% coverage.
