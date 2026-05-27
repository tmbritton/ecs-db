# Story 6: Backup Before Migrate

**Epic:** 2 — Schema versioning & auto-migrations  
**Status:** ✅ Complete  
**Priority:** Medium — cheap insurance, but migrations must work without it

**Depends on:** Story 4 (auto-migration runner)

## Context

Before applying auto-generated migration DDL to a database that contains real game data, copy the database file to `world.sqlite.bak.v{version}`. If the migration corrupts something (a bug in table-rebuild logic, an edge case we didn't think of), the user has a restore point. Configurable retention (keep last N backups) prevents disk bloat over long development.

## Acceptance Criteria

- [x] Database is copied before migration DDL begins
- [x] Backup path follows the pattern `{basename}.bak.v{version}`
- [x] If backup fails, migration proceeds with a warning (migration safety > backup completeness)
- [x] Retention configurable: keep last N backups, older ones are deleted
- [x] The backup file is a valid SQLite database (can be opened and queried)
- [x] 100% test coverage on backup creation and retention logic

## Notes

- The runner calls backup before starting the migration transaction. The backup is outside the transaction (it's a file copy, not a database write).
- "Warning" means log to stderr or a provided logger — the user sees it, but it doesn't block.
- Retention default: keep last 3 backups. Configurable via a `MigrationConfig` struct passed to `NewSQLiteStore`.
