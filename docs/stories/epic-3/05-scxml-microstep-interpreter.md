# Story 5: SCXML Microstep Interpreter

**Epic:** 3 — Agents (behavior-as-data) runtime  
**Status:** 🔲 Not started  
**Priority:** Critical — this is the core of Epic 3

**Depends on:** Stories 1–4 (tables, parsed tree, registry, validator)

## Context

The interpreter implements the W3C SCXML microstep execution algorithm, the same algorithm XState v4 uses internally. Flat-state machines are a special case of this algorithm; implementing it correctly from the start handles hierarchical, parallel, final, and history states without rework.

XState v4 source reference: `packages/core/src/StateNode.ts` (machine definition and transition resolution) and `packages/core/src/interpreter.ts` (event delivery and microstep execution). Clone the v4 repo and use these as the semantic reference when edge cases are unclear.

Each event delivery is one atomic SQLite transaction. A crash mid-event leaves the database consistent.

**Machine startup** (first event delivery or explicit activation): the interpreter ensures every component declared in the machine's `context` block exists on the entity. Missing components are attached and seeded with the initial value. Existing components are left unchanged.

**Component-machine lifecycle**: when `attachComponent` runs for a component that declares `"behavior"` in `schema.json`, the interpreter activates the named machine for that entity. When any machine reaches a final state, the interpreter detaches the associated component (if the machine was activated by component attachment).

## Acceptance Criteria

- [ ] `Agent` type: `Definition *MachineDefinition`, `Configuration []*StateNode`, `EntityID int64`, `History map[string][]*StateNode`
- [ ] `SendEvent(agent *Agent, event Event, tick int64, tx *sql.Tx) error` runs the full microstep
- [ ] **Eligible transition selection:**
  - Walk active configuration deepest-first
  - For each state, check `On[event.Type]` in document order
  - Evaluate `cond` via `WorldReader` (read-only); first true transition wins per state
  - Deeper (more specific) states preempt ancestors
  - Parallel regions each independently select an eligible transition
- [ ] **Exit set computation:** all states that will be exited, ordered leaf→root
- [ ] **Exit actions:** run for each state in exit set; cancel `after` rows in `event_queue` for exited states
- [ ] **Transition actions:** run in declared order
- [ ] **Entry set computation:** all states to enter, ordered root→leaf; resolve compound `initial`, parallel regions (all children entered), history targets (recorded or default)
- [ ] **Entry actions:** run for each state in entry set; schedule `after` rows in `event_queue` for entered states
- [ ] **History recording:** on exit of a compound/parallel state that has a history child, record the current configuration subset for that history node
- [ ] **Final state handling:** entering a final state triggers component detach if the machine was activated by component attachment
- [ ] **Persistence:**
  - Write updated `current_states` to `behavior_components` via `WorldWriter`
  - Append row to `transitions` with `from_states`, `to_states`, `event`, `cond_result`, `actions_run`, `tick`, `wall_ms`
- [ ] **Machine startup:** for each `context` key, if the entity lacks the component, attach it seeded with the initial value; components already present are left unchanged
- [ ] **Transactional:** all of the above happens inside the caller-provided `*sql.Tx`; no commits or rollbacks inside `SendEvent`
- [ ] All mutations go through `WorldWriter`; no raw `tx.Exec` inside `SendEvent` or action dispatch
- [ ] Unit tests for microstep edge cases: transition preemption by depth, parallel region independence, history shallow vs deep, compound initial resolution, final state lifecycle
- [ ] ≥90% coverage on `internal/agent/interpreter.go`

## Notes

- `internal/agent/interpreter.go` and `internal/agent/agent.go`
- **Configuration** is a `[]*StateNode` slice, not a single pointer. For a flat machine it always has one element. For a parallel machine it has one element per active region.
- **Transition conflict resolution** (two eligible transitions from states at the same depth level): XState v4 uses document order — the first eligible transition in the JSON wins. States lower in the hierarchy (more specific) always preempt those higher up regardless of document order.
- **Parallel regions:** each region has its own eligible transition. The exit/entry sets for all regions are merged before executing. This means actions from multiple regions may interleave; the SCXML spec defines the order.
- **History nodes:** never appear in `Configuration` themselves. When the configuration would enter a history node, the interpreter looks up the recorded history for that node's parent; if found, enters those states; if not found, takes the history node's `target` (default transition).
- The `WorldWriter` implementation must handle activating behavior-component machines when `AttachComponent` is called for a behavior-bearing component. This is where the lifecycle hook fires.
- `cond_result` in `transitions`: `1` if a cond was evaluated and was true, `0` if evaluated and false, `NULL` if the transition was unconditional.
