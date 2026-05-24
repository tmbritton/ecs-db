# Story 5: Component Attach/Detach with Type Validation

**Epic:** 1 — Schema-driven data foundation  
**Status:** ✅ Complete  
**Priority:** High — entity lifecycle requires component mutation post-creation  
**Coverage:** `internal/world/` 95.0%, `internal/storage/` 80.1%

## Context

Entities need to gain and lose components during their lifetime (e.g., a goblin picks up a weapon, a particle dissolves and loses its sprite). The same entity type validation rules that apply at creation time must also apply to post-creation component mutations.

The architecture doc is explicit: "Validation is strict by default: the interpreter refuses operations that violate the schema." This applies equally to attach and detach.

## Tasks

- [x] **Implement `ValidateAttachComponent`** — Pure validation function (Task 1) — `internal/world/validate_attach.go`
- [x] **Implement `ValidateDetachComponent`** — Pure validation function (Task 2) — `internal/world/validate_detach.go`
- [x] **Extend domain ports** — Added `AttachComponent`, `DetachComponent` to `Tx` interface; `GetEntityType`, `HasComponent` to `EntityStore` — `internal/world/port.go`
- [x] **Extend `EntityService`** — `AttachComponent` and `DetachComponent` service methods with validation, transactional execution, and rollback — `internal/world/service.go`
- [x] **Implement SQLite adapter methods** — `GetEntityType`, `HasComponent` on `SQLiteStore`; `AttachComponent` (reuses `insertComponent`), `DetachComponent` on `sqliteTx` — `internal/storage/entity.go`
- [x] **Update mock** — Extended `mockTx` and `mockStore` in `internal/world/mock_test.go`
- [x] **Attach validation tests** — Table-driven tests in `internal/world/validate_attach_test.go` (8 cases)
- [x] **Detach validation tests** — Table-driven tests in `internal/world/validate_detach_test.go` (6 cases)
- [x] **Service tests** — Mock-based tests for AttachComponent and DetachComponent in `internal/world/service_test.go` (12 new tests)
- [x] **Integration tests** — SQLite end-to-end tests in `internal/storage/attach_detach_test.go` (13 cases)
- [x] **Error types** — `ErrAlreadyAttached`, `EntityNotFoundError`, `ComponentMutationError`
- [x] **Execute in transactions** — Each attach/detach runs in its own transaction with rollback on failure

## Acceptance Criteria

- [x] Calling `AttachComponent` on an entity with a valid, allowed, unattached component inserts a row into the corresponding `comp_*` table.
- [x] Calling `AttachComponent` with an **undeclared component name** returns an error containing the component name and the message that it is not declared.
- [x] Calling `AttachComponent` with a component **not allowed on the entity type** (when `allowExtraComponents: false`) returns an error containing the entity type and component name.
- [x] Calling `AttachComponent` when the component is **already attached** returns an error (no upsert, no duplicate).
- [x] Calling `DetachComponent` on an optional component deletes the row from the `comp_*` table.
- [x] Calling `DetachComponent` on a **required component** returns an error and does NOT delete the row.
- [x] Attempting attach/detach on a **non-existent entity ID** returns an error.
- [x] Each attach/detach operation is transactional — rollback verified via mock and integration tests.
- [x] **Warning mode** on attach: disallowed extra components produce warnings and proceed.
- [x] **Warning mode does NOT allow detach of required components** — always an error (no safe "proceed anyway" for destructive operations).

## Notes

- `InsertComponent` and `AttachComponent` share the same adapter implementation (`insertComponent` private method) per the design decision in the implementation plan.
- `AttachComponent` converts SQLite UNIQUE constraint violations to the domain-level `ErrAlreadyAttached` sentinel.
- Detach verification ensures at least one row was deleted (zero rows → error message).
