# Story 8: Inline validation

**Epic:** 13 — Forge: AGENTS mode  
**Status:** 🔲 Not started  
**Priority:** High — closes the loop on the epic

**Depends on:** Stories 4, 6 and 7

## Context

`agent.ValidateMachine` returns every error it finds rather than the first, and
it was written that way on purpose: the doc comment on `ValidateMachineError`
says a first-failure-only report "sends someone round the edit-save loop once
per mistake". This story is what makes that pay off.

It is the same job Epic 12 Story 7 did for `schema.json`, and it is easier in
one way and harder in another. Easier: `ValidateMachine` already returns a list,
already attributed — every `ValidationError` carries `MachineID`, `StateID` and
`Field`, so there is no narrowing trick needed to work out what a message is
about. Harder: the things to attach messages to are nodes and edges on a canvas,
not rows in a form, and a message on a node scrolled out of view is a message
nobody reads.

Epic 12's `internal/forge/validation` is the wrong package to extend — it is
about schemas — but its shape is right, and the `components.Problems` primitive,
the `ProblemsID` association helper and the blocked-save footer are all already
built and shared.

One rule carries over exactly. **Blocking means the save itself would be
refused**, which here means `ValidateMachineError` fails, because that is what
the Story 2 session validates with. Nothing else disables the button.

## Acceptance Criteria

- [ ] Errors come from `agent.ValidateMachine` — no rule restated
- [ ] All of them at once, not the first
- [ ] Each error attaches to what caused it, using the `StateID` and `Field` it
      already carries: a node, an edge, or the machine as a whole
- [ ] A node or edge with an error is marked on the canvas, so a problem on
      something off-screen is still findable — the lesson from Epic 12 Story 7,
      where a problem on an unselected row was invisible behind a disabled Save
- [ ] Selecting a marked node shows its errors in the inspector, associated with
      the field they are about
- [ ] Machine-level errors — a machine with child states and no initial — render
      against the machine rather than being hung on an arbitrary node
- [ ] The save footer reflects validity across every open machine, not just the
      selected one
- [ ] An error names the fix where the fix is knowable: an unregistered action
      names the registry, an unknown target names the states that exist
- [ ] Validation runs on every edit and travels the page stream
- [ ] Nothing is invisible: an error whose `StateID` matches no node still
      renders somewhere, rather than being dropped
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/13-inline-validation.spec.js`.

- [ ] A transition retargeted to a state that does not exist reports it, against
      that edge, while typing
- [ ] An unregistered action is reported against the state that carries it
- [ ] A machine with child states and no initial reports it against the machine
- [ ] An invalid `after` duration is reported against that transition
- [ ] Two errors at once produce two messages, not one — the whole point of
      `ValidateMachine` returning a list
- [ ] A node carrying an error is marked on the canvas, and the mark is visible
      without selecting it
- [ ] Fixing the problem clears the message without a reload
- [ ] An error blocks the save; the footer says how many
- [ ] Errors are associated with their field for assistive tech, not merely
      positioned nearby

## Notes

- **Do not extend `internal/forge/validation`.** It is `schema.json`'s validator
  and its `Owner` kinds are components and entity types. A sibling package with
  the same `Problem` shape lets both render through `components.Problems`
  without either pretending to be the other. If the `Problem` type itself wants
  sharing, move that alone.
- `ValidationError.Field` is overloaded by the engine: it holds an action type,
  a guard name, a transition target, a context key or a duration depending on
  the error. Read `validator.go` before mapping it to a control, and expect the
  mapping to be a switch on the message's origin rather than on the field.
- The context-key errors — "does not match any component field", "is ambiguous"
  — are about `schema.json` as much as about the machine, and Epic 12 Story 7
  already warns about the ambiguous case from the other side. Point at the
  schema from here rather than describing it twice.
- Watch the cost. This runs on every render of every open stream, over every
  open machine, and `ValidateMachine` walks the whole state tree. Epic 12's
  equivalent was cheap because schemas are small; measure before assuming the
  same of a project with fifty machines.
