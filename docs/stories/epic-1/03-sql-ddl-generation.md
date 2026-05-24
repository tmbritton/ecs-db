# Story 3: SQL DDL Generation from schema.json

**Epic:** 1 — Schema-driven data foundation  
**Status:** ✅ Done — Full DDL generation with fixed tables, indexes, and component tables  
**Priority:** Critical

## Context

Story 3 completes the database schema initialization layer. After Stories 1-2, the schema loader and validator produce a fully validated `DatabaseSchema` with all component types. Story 3 ensures that when `NewSQLiteStore` opens a database, it creates:

- **6 fixed tables** (`meta`, `world`, `entities`, `event_queue`, `input_events`, `transitions`) with architecture-doc-accurate DDL
- **Indexes** on query-hot columns
- **Component tables** (`comp_*`) generated per-component from `schema.json`
- **Foreign key cascade** — deleting an entity cascades to all component tables
- **Pragmas** (WAL, synchronous, busy_timeout, foreign_keys) applied at connection

## Tasks Summary

| Task | Status | Notes |
|------|--------|-------|
| Fixed tables (6) | ✅ Done | All match architecture doc schema |
| Indexes | ✅ Done | `idx_entity_type`, `idx_event_queue_tick`, `idx_input_events_consumed`, `idx_transitions_entity_id` |
| Component tables from schema | ✅ Done | `componentTableSQL` wired into `createTables` loop |
| ON DELETE CASCADE | ✅ Done | `entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE` |
| Pragmas at init | ✅ Done | WAL, synchronous, busy_timeout, foreign_keys |
| `meta` table records schema_version | ✅ Done | INSERT OR REPLACE on init |
| Cascade delete test | ✅ Done | `TestNewSQLiteStore_CascadeDelete` |
| `schema` table removed | ✅ N/A | Never existed in current code |
| `migrate.go` repurposed/cleaned | ✅ Done | Kept as clean public API for Epic 2 migrations |

## Acceptance Criteria

- [x] `NewSQLiteStore` creates all 6 fixed tables with correct schema
- [x] Pragmas applied: `journal_mode=WAL`, `synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=ON`
- [x] Component tables created for each declared component with correct column types
- [x] Each `comp_*` table has `entity_id` as PRIMARY KEY with `ON DELETE CASCADE`
- [x] Cascade delete verified in test
- [x] No custom `schema` table (uses `meta` instead)
- [x] Tests verify all tables and indexes exist
- [x] `migrate.go` is clean public API (no dead code)

## Coverage

See `docs/stories/epic-1/03-implementation-plan.md` for the implementation plan and `internal/storage/storage_test.go` for test coverage.
