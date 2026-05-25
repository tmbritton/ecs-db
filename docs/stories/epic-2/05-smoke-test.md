# Story 5: Smoke Test — Round-Trip Migration

**Epic:** 2 — Schema versioning & auto-migrations  
**Status:** ⚠️ Not started  
**Priority:** High — proves the full pipeline works end-to-end before relying on it

**Depends on:** Story 4 (auto-migration runner)

## Context

Write a single integration test that exercises the complete migration cycle: create a database with a simple schema, insert entity data, bump `schemaVersion` and add a new component, reopen the database with the updated schema, and verify that the new table was created, the original data is intact, and `meta.schema_version` reflects the new version. This is the "does it actually work?" test — everything is covered by unit tests in Stories 1–4, but only this proves the pieces work together.

## Acceptance Criteria

- [x] Test creates a database at schema version 1 with at least one object component
- [x] Test inserts at least one entity with component data
- [x] Test updates the schema to version 2 by adding a new component
- [x] Test reopens the database — migration runs automatically
- [x] The new component's table exists and is queryable
- [x] The original entity's component data is unmodified
- [x] `meta.schema_version` reflects version 2
- [x] A second test proves column addition: v1 component has 1 property, v2 schema adds a 2nd property, migration succeeds, original column data intact

## Notes

- This is one file (`internal/storage/migration_smoke_test.go`) with two tests: one for adding a component, one for adding a column to an existing component.
- Use `t.TempDir()` for the database file — no cleanup needed.
