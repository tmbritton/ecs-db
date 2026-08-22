# Story 7: Transition inspector

**Epic:** 13 — Forge: AGENTS mode  
**Status:** 🔲 Not started  
**Priority:** Medium — where a machine's logic actually lives

**Depends on:** Stories 4 and 6

## Context

Selecting an edge fills the `TRANSITION` panel: the event that fires it, the
guard that gates it, and the actions that run on the way through. The
prototype's caption says what makes it work — *guard params form is generated
from the guard's registered schema — dropdowns list only registered actions &
guards*.

It is the same machinery as Story 6 pointed at `Registry.Guards()`, with two
differences that matter. A transition's `cond` is a single optional guard rather
than a list, so its control is a dropdown with an explicit "none" rather than an
add-and-remove list. And a transition's `target` is a state, which means the
control that sets it has to offer the states of this machine — the check that
catches a bad one is `ValidateMachine`'s "transition target is not a known
state", and offering only real states makes it unreachable.

`after` transitions share this panel and differ in one field: they are keyed by
a duration string rather than an event name, and that string is validated by
`ParseDurationMs`. `"500"` is milliseconds; `"1s"` and `"1.5s"` work;
`"1 second"` does not.

## Acceptance Criteria

- [ ] The selected transition's source and target states, both readable
- [ ] The event, editable — a transition's event is an authored name, not a
      registered one, so this is a text field and not a dropdown
- [ ] The target offered as the states of this machine, so a target that is not
      a state cannot be chosen
- [ ] The guard offered as the registered guards plus an explicit "none",
      described from the registry
- [ ] Guard parameters generated from `ParamSchema`, on the same terms as
      Story 6's action parameters and through the same code
- [ ] Actions on the transition, listed, added and removed as in Story 6
- [ ] An `after` transition shows its duration instead of an event, and the
      duration is validated by `agent.ParseDurationMs` — not by a regex written
      here
- [ ] An invalid duration is reported against that field, with an example that
      works
- [ ] Changing a transition to or from having a guard is reflected on the canvas,
      which draws guarded transitions differently
- [ ] A state with two transitions on one event keeps them ordered — XState takes
      the first whose guard passes, so the order is the logic and reordering it
      silently would change what the machine does
- [ ] Reordering them is possible, since it is the only way to express priority
- [ ] Deleting the transition is offered here and from the canvas menu
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/13-transition-inspector.spec.js`.

- [ ] Selecting an edge fills the panel with that transition's values
- [ ] The guard dropdown lists registered guards and an explicit "none"
- [ ] Choosing `inLineOfSight` generates its `target` parameter with the
      registered default `$player`
- [ ] Clearing the guard removes `cond` from the file entirely, rather than
      writing an empty one
- [ ] The target dropdown offers only this machine's states
- [ ] Changing the target moves the edge on the canvas without a reload
- [ ] An `after` transition shows a duration field; `1 second` is rejected with
      an example and `1s` is accepted
- [ ] Two transitions on one event render in file order, and reordering them
      changes the file
- [ ] The generated parameter form is the same component as Story 6's — assert
      one shared test id, not two parallel ones

## Notes

- **One generated-form implementation.** Actions and guards both carry
  `[]ParamSchema` and the form is the same form. Two implementations is how the
  guard form grows a feature the action form does not have.
- A `cond` is `{type, params}` or a bare string shorthand. `ParseMachine` accepts
  both and `EmitMachine` writes what it was given; a guard with no params should
  come back as the shorthand it was authored as rather than being expanded into
  an object, or every save rewrites files nobody edited.
- Transition order is the one place in this epic where "preserve authored order"
  is not about diffs — it is semantics. Say so in the code.
- The event name is free text and it is a real trap: a typo produces a
  transition that simply never fires, and nothing validates it because any
  string is a legal event. Consider offering the events already used in this
  machine as suggestions, without preventing a new one.
