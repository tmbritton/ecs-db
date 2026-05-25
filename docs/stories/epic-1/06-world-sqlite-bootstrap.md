# Story 6: world.sqlite Bootstrap & Schema Version Management

**Epic:** 1 — Schema-driven data foundation  
**Status:** ✅ Done — committed 7eb4a7b  
**Priority:** High — Must write schema version into `meta` for Epic 2 migrations to work

## Context

`storage.InitDb` currently creates/opens the database and creates tables. However, it has two problems:
1. **The schema version is stored in the filename** (`path + "-" + schema.Version`) instead of in the `meta` table. This is incompatible with the architecture doc's migration model, which reads `meta.schema_version` to detect mismatches.
2. **No schema version comparison** — On opening an existing database, it doesn't check whether the stored schema version matches the current `schema.json`. Epic 2 will need this to trigger migrations, but the infrastructure must exist now.
3. **No build info** — The architecture doc says `meta` should hold build info for debugging.

## Tasks

- [ ] **Write `schema_version` to `meta` on database creation** — After DDL generation, INSERT `('schema_version', <current schemaVersion>)` into `meta` within the same transaction.
- [ ] **Stop embedding version in the filename** — Change `InitDb` to use the `path` directly without appending the schema version. The version belongs in `meta`, not the filename.
- [ ] **Read `schema_version` from `meta` on open** — When opening an existing database (tables already exist), read `meta.schema_version` and compare it to the current `schema.json` version.
- [ ] **Report schema version mismatch** — If versions differ, return an error (or a typed sentinel error like `ErrSchemaVersionMismatch{dbVersion, fileVersion}`) that the caller can handle (e.g., trigger migrations in Epic 2). Until Epic 2 exists, this is a hard error.
- [ ] **Write build info to `meta`** — Store build metadata such as:
  - `build_time` — timestamp of when the database was created/opened
  - `schema_hash` — hash of `schema.json` contents (for detecting content changes without version bumps)
  - Optional: `git_sha` if the binary is built with embedded commit info
- [ ] **Handle empty/new database vs existing database** — `InitDb` should distinguish between "creating fresh" (no tables exist) and "opening existing" (tables exist, compare versions). A reliable way: query `meta` before creating tables. If `meta` exists and has a row, it's an existing database.
- [ ] **Make `meta` table the first table created** — Ensure `meta` is created before any version-dependent tables, so version comparison is possible even if other tables fail to create.
- [ ] **Add tests**:
  - Fresh database creation writes `schema_version` and build info to `meta`
  - Opening an existing database with matching versions succeeds
  - Opening an existing database with mismatched versions returns `ErrSchemaVersionMismatch`
  - Filename no longer contains the version string

## Acceptance Criteria

- [ ] `InitDb("./ecs.db", schema)` creates a file named exactly `ecs.db` (no version suffix).
- [ ] After `InitDb`, querying `meta` returns a `schema_version` key matching `schemaVersion` from `schema.json`.
- [ ] After `InitDb`, querying `meta` returns a `build_time` key with a valid timestamp.
- [ ] Calling `InitDb` on an existing database with the **same** `schemaVersion` succeeds without error and does not recreate tables (tables already exist, data preserved).
- [ ] Calling `InitDb` on an existing database with a **different** `schemaVersion` returns a `ErrSchemaVersionMismatch` error that includes both the stored version and the current version in its error message.
- [ ] A test exists that creates a database at version 1, then attempts to open it at version 2, and verifies the mismatch error.
- [ ] The `meta` table is created first (before other fixed tables and component tables) during initialization.
- [ ] The `initSchema` function in `sqlite.go` properly detects whether tables exist (e.g., by querying `meta`) before creating them.
