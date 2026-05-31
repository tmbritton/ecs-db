# Story 7: Built-In Actions and Guards

**Epic:** 3 — Agents (behavior-as-data) runtime  
**Status:** ✅ Done  
**Priority:** High — built-ins are what make the wandering_goblin example run

**Depends on:** Story 3 (registry and context types), Story 5 (interpreter, for lifecycle integration)

## Context

Actions and guards are registered Go implementations of the `ActionHandler` and `GuardHandler` interfaces. They are the only code that runs in response to machine transitions — the JSON machine files cannot contain or invoke arbitrary code.

All built-in actions write through `WorldWriter` and read through `WorldReader`. They never hold a raw `*sql.Tx` or `*sql.DB`. This is the constraint that will make the future Lua migration straightforward: the interfaces become the Lua table API.

Built-ins live in `internal/agent/builtins/` and are registered into a `*Registry` via a `RegisterBuiltins(r *Registry)` function called at interpreter startup.

## Acceptance Criteria

**Actions** (each registered with metadata including description and param schemas):

- [x] `moveTowardTarget` — read `target_x`/`target_y` from entity's component, update `Position.x`/`Position.y` by one step toward target at `speed` (or `speed * speed_mult` if `speed_mult` param provided)
- [x] `dealDamage` — decrement `Health.hp` on the target entity by `amount`; target is `params.target` (entity ID or `"$player"` sentinel)
- [x] `spawnEntity` — create a new entity of type `params.entity_type` at optional position; return new entity ID (stored where? TBD in integration — for now, log it)
- [x] `attachComponent` — attach component `params.component` to the entity with `params.data` as initial field values; triggers behavior-machine lifecycle if component declares `"behavior"`
- [x] `detachComponent` — detach component `params.component` from the entity
- [x] `setTimer` — write `params.ticks` to a timer field identified by `params.key` on the entity (the component field must exist in the entity's context manifest)
- [x] `log` — write `params.message` to the interpreter logger; no-op for the database
- [x] `pickRandomTarget` — pick a random position within `params.radius` of the entity's current position; write to `target_x`/`target_y` component fields
- [x] `setPursueTarget` — write the player entity's current position to the entity's `target_x`/`target_y` component fields

**Guards** (each registered with metadata):

- [x] `timerExpired` — returns true if the timer field identified by `params.key` has reached zero or below
- [x] `atTarget` — returns true if the entity's `Position` is within 1 unit of `target_x`/`target_y`
- [x] `inRange` — returns true if the distance between this entity and `params.target` entity is ≤ `params.distance`
- [x] `hasComponent` — returns true if the entity has the component named `params.component`
- [x] `healthAbove` — returns true if `Health.hp > params.threshold`

**Registration:**

- [x] `RegisterBuiltins(r *Registry)` registers all of the above
- [x] Each registration includes a description string and param schemas
- [x] Tests: each action/guard has at least one test exercising its core behavior against a real SQLite DB (integration-style)

## Notes

- `internal/agent/builtins/actions.go` and `internal/agent/builtins/guards.go`
- `moveTowardTarget`, `pickRandomTarget`, `setPursueTarget`, `setTimer` all write to component fields that must exist in the entity's context manifest — they use `WorldWriter.SetComponentValue`. They should handle gracefully if the field isn't present (log warning, no-op) rather than panicking, but this should be rare if validation is working.
- `dealDamage` writes to a *target* entity — it calls `WorldWriter.SetComponentValue` with the target's entity ID, not the current entity's ID. The `"$player"` sentinel will need a lookup mechanism; keep it simple for now (e.g. query entities table for `entity_type = "Player"`).
- `spawnEntity` is the most complex built-in — it needs to call through to the entity creation logic from Epic 1. Consider accepting the entity creation as a method on `WorldWriter` rather than a separate lookup.
- Built-in actions and guards do not need to be exhaustive implementations for this story — they need to be correct enough for the wandering_goblin integration test (Story 8) to pass.
