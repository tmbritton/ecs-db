# Story 3: SQL DDL Generation from schema.json

**Epic:** 1 — Schema-driven data foundation  
**Status:** ⚠️ Partially done — ComponentTableBuilder exists but is incomplete and not wired into InitDb  
**Priority:** Critical — Without this, the database has no useful schema

## Context

`internal/storage/` has three files relevant to DDL:
  - `sqlite.go`: Creates `entities` and `schema` tables but **misses five fixed tables** (`meta`, `world`, `event_queue`, `input_events`, `transitions`) and all pragmas.
  - `componentTableBuilder.go`: Can generate `CREATE TABLE` SQL for text/integer/reference/bool components with a single `value` column, but **is not called by `initSchema`** and doesn't handle all component types.
  - `migrate.go`: Stub `CreateSqlForComponent` returns errors for every type (dead code). The file name is misleading — this isn't a migration runner.

The architecture doc specifies a **multi-column-per-component** model: each component property becomes its own SQL column. The current `ComponentTableBuilder` uses a single `value` column. The decision from Story 1 determines which approach is correct. **This story assumes the architecture doc's multi-column approach.**

## Current vs Required Fixed Tables

| Table | Status |
|-------|--------|
| `meta` | ❌ Not created |
| `world` | ❌ Not created |
| `entities` | ⚠️ Created but schema differs from architecture doc (missing `created_tick`, uses `TIMESTAMP` instead of integer) |
| `event_queue` | ❌ Not created |
| `input_events` | ❌ Not created |
| `transitions` | ❌ Not created |
| `schema` (custom table) | ⚠️ Created but not in architecture doc — should be replaced by `meta` |
| Pragmas (WAL, synchronous, busy_timeout, foreign_keys) | ❌ Not set |
| `ON DELETE CASCADE` from comp tables | ❌ Not implemented |

## Tasks

- [ ] **Add pragma execution at database init** — Run `PRAGMA journal_mode=WAL`, `PRAGMA synchronous=NORMAL`, `PRAGMA busy_timeout=5000`, `PRAGMA foreign_keys=ON` as the first statement after opening the connection.
- [ ] **Create all five fixed tables** (`meta`, `world`, `entities`, `event_queue`, `input_events`) plus `transitions` with exact schema from the architecture doc:
  - `meta(key TEXT PRIMARY KEY, value TEXT NOT NULL)`
  - `world(key TEXT PRIMARY KEY, value TEXT NOT NULL)`
  - `entities(id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL)`
  - `event_queue(id INTEGER PRIMARY KEY AUTOINCREMENT, tick INTEGER NOT NULL, target_entity INTEGER, kind TEXT NOT NULL, payload TEXT NOT NULL DEFAULT '{}')`
  - `input_events(id INTEGER PRIMARY KEY AUTOINCREMENT, received_at_ms INTEGER NOT NULL, kind TEXT NOT NULL, payload TEXT NOT NULL DEFAULT '{}', consumed INTEGER NOT NULL DEFAULT 0)`
  - `transitions(id INTEGER PRIMARY KEY AUTOINCREMENT, tick INTEGER NOT NULL, wall_ms INTEGER NOT NULL, entity_id INTEGER NOT NULL, machine_id TEXT NOT NULL, from_state TEXT NOT NULL, to_state TEXT NOT NULL, event TEXT NOT NULL, guard_result TEXT, actions_run TEXT)`
- [ ] **Remove the custom `schema` table** — Replace with `meta` (or merge its purpose into `meta`). `meta` is the architecture doc's canonical table for build info and schema version.
- [ ] **Rewrite component table generation** — For each component declared in `schema.json`, generate a `comp_*` table with:
  - `entity_id INTEGER PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE`
  - One column per property in the component's type definition, with correct SQL type (`REAL` for numbers, `INTEGER` for integers, `TEXT` for strings/refs, etc.)
  - `NOT NULL` on required fields (or nullable if the architecture permits)
- [ ] **Wire `ComponentTableBuilder` (or its replacement) into `initSchema`** — Iterate over `schema.Schema.Components`, generate SQL for each, and exec it.
- [ ] **Create indexes** — `idx_entity_type` on `entities(type)` (already done). Add indexes on `event_queue.tick`, `input_events.consumed`, `transitions.entity_id`, and component table `entity_id` columns.

## Acceptance Criteria

- [ ] `InitDb` creates a SQLite database with all six fixed tables (`meta`, `world`, `entities`, `event_queue`, `input_events`, `transitions`).
- [ ] After `InitDb`, `PRAGMA journal_mode` returns `wal`, `PRAGMA synchronous` returns `normal`, `PRAGMA busy_timeout` returns `5000`, and `PRAGMA foreign_keys` returns `1`.
- [ ] For a `schema.json` with three declared components, `InitDb` creates three `comp_*` tables with the correct columns.
- [ ] Each `comp_*` table has `entity_id` as the PRIMARY KEY with `REFERENCES entities(id) ON DELETE CASCADE`.
- [ ] Deleting an entity from `entities` automatically deletes all associated rows in all `comp_*` tables (foreign key cascade verified in a test).
- [ ] `sqlite.go` no longer creates the custom `schema` table.
- [ ] A test exists that loads `schema.json`, calls `InitDb`, queries `sqlite_master`, and verifies all expected tables exist.
- [ ] `internal/storage/migrate.go` is either repurposed (for Epic 2 migration support) or its dead code is removed.
