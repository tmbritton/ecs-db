# Story 8: Integration Tests and Stately Round-Trip

**Epic:** 3 — Agents (behavior-as-data) runtime  
**Status:** 🔲 Not started  
**Priority:** High — proves the whole pipeline works end-to-end

**Depends on:** Stories 1–7 (all of them)

## Context

With all the pieces in place, this story proves they actually work together: a real machine file loaded, events delivered, state transitions persisted, component values updated. The `wandering_goblin` example from `docs/game-engine-arch.md` is the canonical fixture. If it runs correctly end-to-end, Epic 3 is done.

A secondary fixture — a component with a declared `"behavior"` — tests the component-machine lifecycle: attach triggers activation, final state triggers detach.

A real Stately Studio v4 export is checked into `testdata/` and parsed in CI. This is the concrete proof that "Stately export → drop in → works" is true, not aspirational.

## Acceptance Criteria

**wandering_goblin integration test:**

- [ ] `wandering_goblin.json` checked into `testdata/behaviors/` matching the example in `game-engine-arch.md`
- [ ] Schema with `Position`, `Sprite`, `Health`, `GoblinBehavior` (with `speed`, `aggroRange`, `target_x`, `target_y` fields) and `Goblin` entity type
- [ ] Test: create Goblin entity → machine activates → `behavior_components` row exists for `(goblin_id, "wandering_goblin")`
- [ ] Test: deliver `TICK` event → machine starts in `idle` state → `setTimer` entry action runs → timer field updated in component
- [ ] Test: deliver `TICK` events until `timerExpired` guard fires → machine transitions to `wandering` → `behavior_components.current_states` updated → row in `transitions`
- [ ] Test: deliver `PLAYER_NEARBY` event → machine transitions to `pursuing` regardless of current state
- [ ] Test: each transition appends a row to `transitions` with correct `from_states`, `to_states`, `event`, `cond_result`, `actions_run`

**Component-machine lifecycle test:**

- [ ] Schema includes a `Burning` component with `"behavior": "burning"` and a simple `burning.json` machine in `testdata/` with a final state
- [ ] Test: attach `Burning` component to entity → `behavior_components` row created for `(entity_id, "burning")`
- [ ] Test: deliver events until `burning` machine reaches its final state → `Burning` component detached → `behavior_components` row deleted

**Stately round-trip:**

- [ ] A real Stately Studio v4 export (any machine, exported as JSON) checked into `testdata/stately-export.json`
- [ ] CI test: `ParseMachine(stately-export.json)` succeeds without error
- [ ] CI test: `ValidateMachine(...)` on a Stately export that uses only supported features passes

**Validation rejection tests:**

- [ ] Machine with `invoke` → clear error message, not panic
- [ ] Machine with unknown action type → clear error message
- [ ] Machine with unknown cond type → clear error message
- [ ] Machine with transition to undefined state → clear error message
- [ ] Machine with ambiguous context key → clear error message

## Notes

- Tests live in `internal/agent/` with real SQLite databases (following Epic 1/2 conventions).
- The `wandering_goblin` schema needs component fields for context keys: `speed`, `aggroRange`, `target_x`, `target_y`. These can live on a `GoblinBehavior` component or across purpose-built components — whichever makes the fixture most readable. The point is that all context keys resolve to exactly one component field.
- The Stately export in `testdata/stately-export.json` should be a real export from `app.stately.ai`, not hand-crafted JSON. Export the `wandering_goblin` machine or any other machine from Stately and commit it verbatim.
- If the `spawnEntity` built-in's entity-ID return mechanism is not fully resolved by Story 7, the integration test can skip that specific action — the other behaviors are more important to prove out in this story.
