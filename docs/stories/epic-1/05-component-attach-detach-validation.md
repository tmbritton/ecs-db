# Story 5: Component Attach/Detach with Type Validation

**Epic:** 1 — Schema-driven data foundation  
**Status:** ❌ Not started — no code for attaching or detaching components  
**Priority:** High — entity lifecycle requires component mutation post-creation

## Context

Entities need to gain and lose components during their lifetime (e.g., a goblin picks up a weapon, a particle dissolves and loses its sprite). The same entity type validation rules that apply at creation time must also apply to post-creation component mutations.

The architecture doc is explicit: "Validation is strict by default: the interpreter refuses operations that violate the schema." This applies equally to attach and detach.

## Tasks

- [ ] **Implement `AttachComponent(db, entityID, componentName, componentData)`** — Attaches a component instance to an existing entity.
- [ ] **Validate component is declared** — Reject attaching a component name not declared in `schema.json` with a clear error: "component 'X' is not declared in schema".
- [ ] **Validate component allowed on entity type** — Check the component is in the entity type's `requiredComponents` or `optionalComponents`. If not, and `allowExtraComponents: false`, reject it.
- [ ] **Prevent duplicate attachment** — If the component is already attached to the entity, return an error (or upsert, depending on semantics — clarify and document the decision).
- [ ] **Implement `DetachComponent(db, entityID, componentName)`** — Removes a component instance from an entity by deleting from the corresponding `comp_*` table.
- [ ] **Prevent detaching required components** — If the component is in the entity type's `requiredComponents`, reject the detach with an error: "cannot detach required component 'X' from entity type 'Y'".
- [ ] **Honor `validationLevel`** — In `warning` mode, log validation violations during attach but proceed with the attach. Detach violations on required components are always errors (there's no safe "warning" mode for removing required data).
- [ ] **Execute in transactions** — Each attach or detach runs in its own transaction.
- [ ] **Add integration tests** — Cover:
  - Attaching a valid optional component
  - Attaching an undeclared component → error
  - Attaching a disallowed component on an `allowExtraComponents: false` type → error
  - Detaching an optional component → success, component table row deleted
  - Detaching a required component → error, row not deleted
  - Attach on non-existent entityID → error

## Acceptance Criteria

- [ ] Calling `AttachComponent` on an entity with a valid, allowed, undeclared component (not yet attached) inserts a row into the corresponding `comp_*` table.
- [ ] Calling `AttachComponent` with an **undeclared component name** returns an error containing the component name and the message that it is not declared.
- [ ] Calling `AttachComponent` with a component **not allowed on the entity type** (when `allowExtraComponents: false`) returns an error containing the entity type and component name.
- [ ] Calling `AttachComponent` when the component is **already attached** returns an error (no upsert, no duplicate).
- [ ] Calling `DetachComponent` on an optional component deletes the row from the `comp_*` table.
- [ ] Calling `DetachComponent` on a **required component** returns an error and does NOT delete the row.
- [ ] Attempting attach/detach on a **non-existent entity ID** returns an error.
- [ ] Each attach/detach operation is transactional — verified by killing the database mid-operation in a test or by rollback verification.
- [ ] Integration tests cover all six scenarios listed in Tasks (at minimum).
