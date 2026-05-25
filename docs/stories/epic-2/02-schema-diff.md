# Story 2: Schema Diff Computation

**Epic:** 2 — Schema versioning & auto-migrations  
**Status:** ✅ Done  
**Priority:** High — feeds the DDL generator; no migrations without a diff

## Context

Once we can introspect the database (Story 1), the next step is to compute what's different between the database and the current `schema.json`. The diff is a pure domain function — no I/O, no SQLite calls. It takes two representations (the as-built database schema and the file schema) and produces an ordered list of structural changes.

This lives in `internal/schema/` — it's domain logic that compares two data structures.

## Acceptance Criteria

- [ ] Produces a "diff" between the in-database component set and the file component set
- [ ] Detects new components present in `schema.json` but missing from the database
- [ ] Detects components removed from `schema.json` that still have tables in the database
- [ ] Detects properties added to an existing object component
- [ ] Detects properties removed from an existing object component
- [ ] Detects SQL type changes on existing columns (e.g. `TEXT` → `REAL`)
- [ ] New entity types in `schema.json` are detected (metadata only — no DDL impact)
- [ ] Entity types removed from `schema.json` are detected (metadata only)
- [ ] Entity type requirement changes (required ↔ optional, strict ↔ warning) are detected
- [ ] Change ordering is deterministic and safe: additions first, then modifications, then removals
- [ ] Identical schemas produce an empty diff
- [ ] 100% test coverage on the diff logic

## Notes

- The diff compares SQL types directly (via `propertySQLType()`), not semantic JSON types. This avoids ambiguity where `boolean` and `integer` both map to `INTEGER` in SQLite.
- "Type change" means the underlying SQL column type changed. `number` → `integer` (`REAL` → `INTEGER`) is a type change; `string` → `string` is not, even if validation rules differ.
- Entity type diffs are metadata-only: they affect in-memory validation but produce no DDL statements. A game designer can change which components are "required" on an entity type without any database migration.
