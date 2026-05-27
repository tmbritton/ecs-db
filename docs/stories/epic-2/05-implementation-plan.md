# Implementation Plan: Story 5 — Smoke Test Round-Trip Migration

## Context

Stories 1–4 built and unit-tested each layer of the migration pipeline (introspection, diff, DDL generation, runner). Story 5 proves the pieces compose correctly end-to-end: open a real file-backed SQLite database, insert real data, reopen it with an updated schema, and assert the migration ran correctly with no data loss. This is the "does it actually work?" integration test.

## What to Build

One new file: `internal/storage/migration_smoke_test.go`

- Package: `package storage` (white-box; reuses helpers from `migration_test.go`)
- Two tests, each using `t.TempDir()` for the database path
- Uses `NewSQLiteStore` (the 3-arg backward-compat form) — no need for `StoreConfig`
- Accesses DB internals via `store.DB()` after reopening

## Test 1: `TestSmoke_AddComponent_RoundTrip`

1. Open DB at v1 with a `Position` object component (`x`, `y` REAL).
2. Insert one entity: `INSERT INTO entities (entity_type, created_tick) VALUES ('Player', 0)`.
3. Insert one component row: `INSERT INTO comp_position (entity_id, x, y) VALUES (1, 1.0, 2.0)`.
4. Close the store.
5. Reopen with v2 schema — same `Position` component plus a new `Velocity` object component (`vx`, `vy` REAL).
6. Assert:
   - `tableExists(t, db, "comp_velocity")` is true
   - Original `comp_position` row has `entity_id=1, x=1.0, y=2.0` (data intact)
   - `readMetaValue(t, db, "schema_version")` == `"2"`

## Test 2: `TestSmoke_AddColumn_RoundTrip`

1. Open DB at v1 with a `Position` object component (`x` REAL only).
2. Insert one entity and one component row (`entity_id=1, x=3.5`).
3. Close the store.
4. Reopen with v2 schema — same `Position` component now has both `x` and `z` REAL.
5. Assert:
   - `columnExists(t, db, "comp_position", "z")` is true
   - Original `x` value for entity 1 is still `3.5`
   - `readMetaValue(t, db, "schema_version")` == `"2"`

## Reusable Helpers (from `migration_test.go`)

- `readMetaValue(t, db, key)` — reads a value from the `meta` table
- `tableExists(t, db, name)` — checks `sqlite_master`
- `columnExists(t, db, table, col)` — checks `PRAGMA table_info`

All are in `package storage` so they are directly available in the new file.

## Critical Files

| Action | File |
|--------|------|
| **Create** | `internal/storage/migration_smoke_test.go` |
| Read (API reference) | `internal/storage/sqlite.go` |
| Read (helpers) | `internal/storage/migration_test.go` |

## Verification

```bash
go test ./internal/storage/... -run TestSmoke -v
go test ./...
```

Both tests must pass. No new non-test files are needed.
