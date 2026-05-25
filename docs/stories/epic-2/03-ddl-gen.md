# Story 3: DDL Generation from Schema Diff

**Epic:** 2 — Schema versioning & auto-migrations  
**Status:** ⚠️ Not started  
**Priority:** High — translates the diff into executable SQL statements

## Context

Once the schema diff knows what changed between the database and `schema.json`, we need to generate the SQL statements to bring the database up to date. This reuses the existing `componentTableSQL` for CREATE statements, adds ALTER TABLE for property additions, and generates table-rebuild sequences for destructive operations (SQLite lacks `DROP COLUMN` in older versions).

This lives in `internal/storage/` — it produces SQL strings but doesn't execute them.

## Acceptance Criteria

- [ ] `add_component` diff produces a `CREATE TABLE comp_<name>` statement via existing `componentTableSQL`
- [ ] `add_property` diff produces an `ALTER TABLE comp_<name> ADD COLUMN` statement
- [ ] `remove_property` diff produces a table-rebuild sequence (create temp, SELECT INSERT, drop old, rename)
- [ ] `remove_component` diff produces a `DROP TABLE comp_<name>` statement
- [ ] SQL type change produces a table-rebuild sequence (columns need to be recreated)
- [ ] Entity type changes produce no DDL (validation rules are purely in-memory)
- [ ] Statement ordering is safe: CREATEs before ALTERs before DROPs
- [ ] Empty diff produces no statements
- [ ] Destructive changes (DROP TABLE, column removal) are flagged so the runner can warn or require confirmation
- [ ] 100% test coverage on DDL generation

## Notes

- `ALTER TABLE ... ADD COLUMN` is safe in all SQLite versions. `ALTER TABLE ... DROP COLUMN` was only added in SQLite 3.35.0 (March 2021). Use the table-rebuild approach for maximum compatibility unless we're willing to require 3.35+.
- The "table rebuild" pattern for removing columns: create a temp table without the column, copy data (`INSERT INTO temp SELECT cols FROM old`), `DROP TABLE old`, `ALTER TABLE temp RENAME TO comp_<name>`.
- FK constraints need to be temporarily disabled during the rebuild phase (or set before/after the whole migration transaction).
