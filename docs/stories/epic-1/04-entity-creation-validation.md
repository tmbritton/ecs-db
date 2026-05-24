# Story 4: Entity Creation with Type Validation

**Epic:** 1 — Schema-driven data foundation  
**Status:** ✅ Done — Hexagonal domain layer + SQLite adapter with comprehensive tests  
**Priority:** High

## Context

The architecture doc specifies that entities are created with a declared type, and the interpreter validates that the entity's components match the type's contract. This is a deliberate departure from pure ECS — entity types with `requiredComponents`, `optionalComponents`, `allowExtraComponents`, and `validationLevel` enforce structure at creation time.

## Tasks Summary

| Task | Status | Notes |
|------|--------|-------|
| Entity creation API | ✅ Done | `EntityService.CreateEntity` in `internal/world/service.go` |
| Entity type existence check | ✅ Done | `ValidateEntityCreation` checks `schema.EntityTypes` |
| `requiredComponents` enforcement | ✅ Done | Missing required → error (strict) or warning |
| `allowExtraComponents` enforcement | ✅ Done | Disallowed extra → error (strict) or warning |
| `validationLevel` (strict/warning) | ✅ Done | `ValidationStrict` (default) aborts, `ValidationWarning` logs |
| Transactional entity + component insert | ✅ Done | `BeginTx` → inserts → `Commit`, or `Rollback` on error |
| Entity ID auto-assign | ✅ Done | SQLite `AUTOINCREMENT` via `LastInsertId()` |
| `created_tick` from `world` table | ✅ Done | `GetCurrentTick` reads `world.current_tick`, defaults to 0 |
| Integration tests | ✅ Done | 13 integration tests + 12 unit tests + 34 service tests |

## Architecture

```
internal/world/           # Domain — no storage imports
  port.go                 # EntityStore + Tx interfaces (hexagonal ports)
  entity.go               # Entity, EntityComponent domain types
  validate.go             # ValidateEntityCreation — pure function
  service.go              # EntityService orchestrating creation
internal/storage/
  entity.go               # SQLite adapter: sqliteTx, BeginTx, GetCurrentTick
```

## Acceptance Criteria

- [x] `CreateEntity` with valid type + all required components → rows in `entities` + all `comp_*` tables
- [x] `CreateEntity` missing required component → error (strict), no rows inserted
- [x] `CreateEntity` with extra component on strict type → error, no rows inserted
- [x] `CreateEntity` with extra component when `allowExtraComponents=true` → success
- [x] `CreateEntity` with unknown entity type → error
- [x] `created_tick` read from `world` table and stored correctly
- [x] Validation failure → transaction rollback, no partial data
- [x] At least 5 integration tests covering all scenarios
- [x] Domain packages import nothing from `storage/`
- [x] Coverage ≥90% on `internal/world/`

## Coverage

- `internal/world/`: 98.5% (domain — near-perfect)
- `internal/storage/`: 81.8% (adapter — acceptable, see AGENTS.md)
- `internal/schema/`: 93.1% (unchanged)
