# Story 4: Auto-Migration Runner

**Epic:** 2 — Schema versioning & auto-migrations  
**Status:** ⚠️ Not started  
**Priority:** High — the engine that actually performs migrations on startup

**Depends on:** Story 1 (introspection), Story 2 (diff), Story 3 (DDL generation)

## Context

When `NewSQLiteStore` detects a schema version mismatch, it currently errors out and refuses to open the database. With the migration runner in place, the flow becomes: introspect the existing DB → diff against `schema.json` → generate DDL → execute in a transaction → update `meta.schema_version` → return a usable store. The user never writes migration files — they just edit `schema.json` and bump `schemaVersion`.

This modifies `internal/storage/sqlite.go` (the existing `NewSQLiteStore` flow) and adds the runner logic.

## Acceptance Criteria

- [ ] `NewSQLiteStore` integrates auto-migration when a version mismatch is detected
- [ ] On mismatch, introspects the DB, computes diff, generates DDL, executes in a single transaction
- [ ] `meta.schema_version` and `meta.build_time` are updated after successful migration
- [ ] All DDL statements run within the same transaction — no partial state on failure
- [ ] Migration error includes which change failed (component name, change type), the underlying SQL error, and which statement caused it
- [ ] If the diff contains destructive changes (DROP TABLE, column removal), a `MigrationPolicy` controls behavior: `auto` proceeds, `confirm` returns an error requiring manual intervention
- [ ] No migration runs when db version matches current `schema.json` version
- [ ] 100% test coverage on the runner orchestration

## Notes

- The runner is not a file-based migration system. There are no `.sql` files. The entire migration is computed at runtime from the difference between the existing database state and the current `schema.json`.
- The migration transaction should wrap ALL DDL statements plus the meta update — either everything succeeds or nothing changes.
- The `MigrationPolicy` default should be `auto` (fully automatic). The `confirm` policy is a safety valve for destructive changes in production-critical workflows.
