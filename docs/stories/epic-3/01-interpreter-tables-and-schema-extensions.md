# Story 1: Interpreter-Managed Tables and Schema Extensions

**Epic:** 3 — Agents (behavior-as-data) runtime  
**Status:** 🔲 Not started  
**Priority:** Critical blocker — everything else in Epic 3 depends on the database tables and schema shape

## Context

The behavior runtime needs three tables that are not generated from `schema.json`:

- `behavior_components` — tracks the active state of every running machine per entity. The composite primary key `(entity_id, machine_id)` allows multiple machines to run concurrently on one entity (primary machine + behavior-component machines).
- `transitions` — append-only audit log of every state transition; also the source of renderer effects.
- `event_queue` — holds both user-dispatched game events and scheduled `after` timer events.

These are interpreter-internal tables. They do not appear in `schema.json` because the interpreter owns them entirely, the same way Epic 2's migration infrastructure owns `schema_versions`. They are created with `CREATE TABLE IF NOT EXISTS` at interpreter startup.

This story also adds two new optional fields to `schema.json` component and entity type definitions:

- **Component `"behavior"` field** — names a machine file. When a component with this field is attached to an entity, the interpreter activates the named machine. When that machine reaches a final state, the component is detached. This is how status effects and equipment work.
- **Entity type `"behavior"` field** — names a machine file. When an entity of this type is created, the interpreter activates the named machine as the entity's primary machine.

`"Behavior"` is a reserved component name. Schema validation must reject any user-defined component with that name.

## Acceptance Criteria

- [ ] `behavior_components`, `transitions`, and `event_queue` tables created at interpreter startup via `CREATE TABLE IF NOT EXISTS` (not via schema DDL path)
- [ ] `behavior_components` has composite PK `(entity_id, machine_id)`; columns: `machine_id TEXT`, `current_states TEXT` (JSON array), `updated_at INTEGER`
- [ ] `transitions` columns: `id`, `tick`, `wall_ms`, `entity_id`, `machine_id`, `from_states TEXT` (JSON array), `to_states TEXT` (JSON array), `event TEXT`, `cond_result INTEGER`, `actions_run TEXT` (JSON array)
- [ ] `event_queue` columns: `id`, `entity_id`, `machine_id`, `event_type TEXT`, `payload TEXT`, `target_tick INTEGER`
- [ ] Schema loader accepts optional `"behavior": "<machine-name>"` on component definitions
- [ ] Schema loader accepts optional `"behavior": "<machine-name>"` on entity type definitions
- [ ] Schema validation rejects any component named `"Behavior"` with a clear error: `"'Behavior' is a reserved component name"`
- [ ] Schema validation rejects a `"behavior"` reference on a component or entity type if the named machine file does not exist in `mods/behaviors/` at startup
- [ ] Existing Epic 1 / Epic 2 tests continue to pass unmodified
- [ ] 100% test coverage on new validation logic

## Notes

- The three interpreter tables should be created by a dedicated `EnsureInterpreterTables(db *sql.DB) error` function in `internal/storage/`, called from interpreter startup after the schema DDL path runs.
- `"Behavior"` reserved-name check belongs in `internal/schema/` validation, alongside the existing component-name and entity-type-name checks.
- Machine file existence validation at schema load time: the interpreter startup path is the right place (not the schema loader itself), since the schema loader doesn't know the `mods/` directory path. Pass the behaviors directory to a separate validation step.
- `from_states` and `to_states` are JSON arrays to correctly represent the active configuration of hierarchical and parallel state machines (where multiple states are active simultaneously).
