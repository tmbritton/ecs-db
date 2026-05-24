# Story 4: Entity Creation with Type Validation

**Epic:** 1 — Schema-driven data foundation  
**Status:** ❌ Not started — no code exists for entity creation  
**Priority:** High — must be built before the tick loop (Epic 5) can function

## Context

The architecture doc specifies that entities are created with a declared type, and the interpreter validates that the entity's components match the type's contract. This is a deliberate departure from pure ECS — entity types with `requiredComponents`, `optionalComponents`, `allowExtraComponents`, and `validationLevel` enforce structure at creation time.

No code currently exists for creating entities. This story covers the initial entity creation API and the validation logic that enforces entity type contracts.

## Tasks

- [ ] **Define the entity creation API** — A function (e.g., `CreateEntity(db, entityType string, initialComponents map[string]interface{})`) that accepts an entity type and initial component data.
- [ ] **Validate entity type exists** — Reject creation if the `entityType` doesn't match any declared type in `schema.json`.
- [ ] **Enforce `requiredComponents`** — All components listed as required for the entity type must be provided in `initialComponents`. Missing required component = error (in `strict` mode).
- [ ] **Enforce `allowExtraComponents: false`** — When `allowExtraComponents` is `false` (the default), reject any component in `initialComponents` that isn't in `requiredComponents` or `optionalComponents`.
- [ ] **Honor `validationLevel`** — In `strict` mode (default), validation failures are hard errors that abort creation. In `warning` mode, log the violation but proceed with creation.
- [ ] **Execute in a single transaction** — Entity creation writes one row to `entities` (with `entity_type` and `created_tick`) and one row to each `comp_*` table for the initial components. All succeed or all rollback.
- [ ] **Generate and assign entity IDs** — Use SQLite's `AUTOINCREMENT` for `entities.id`. Return the assigned ID to the caller.
- [ ] **Set `created_tick`** — Set `entities.created_tick` to the current `world.tick` value (read from the `world` table).
- [ ] **Add integration tests** — Create an entity with valid data, verify rows exist in `entities` and component tables. Attempt creation with missing required components, verify rejection. Attempt creation with extra components on a strict type, verify rejection.

## Acceptance Criteria

- [ ] Calling `CreateEntity` with a valid entity type and all required components inserts one row into `entities` and one row into each component table.
- [ ] Calling `CreateEntity` with a valid entity type but **missing a required component** returns an error in `strict` mode and does not insert any rows.
- [ ] Calling `CreateEntity` with a valid entity type and an **extra component not in the allowed set** (when `allowExtraComponents: false`) returns an error in `strict` mode and does not insert any rows.
- [ ] Calling `CreateEntity` with a valid entity type and an extra component when `allowExtraComponents: true` succeeds (warning logged if `validationLevel: warning`).
- [ ] Calling `CreateEntity` with an **unknown entity type** returns an error.
- [ ] The `created_tick` field in the `entities` table is set to the current tick from the `world` table.
- [ ] On creation failure (validation error), no partial data is written — the transaction rolls back cleanly.
- [ ] At least 5 integration tests cover: entity type validation, missing required components, extra components rejected, extra components allowed, unknown entity type, and transaction rollback on failure.
