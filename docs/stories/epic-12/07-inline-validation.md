# Story 7: Inline validation

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** 🔲 Not started  
**Priority:** High — closes the loop on the whole epic

**Depends on:** Stories 2 and 5

## Context

Everything in this epic can produce an invalid schema, and the save path refuses one. Refusing at save is correct but late: it means finding out at the end of a train of thought rather than during it.

This story surfaces the same checks while editing. Not new checks — the same functions the engine runs, so what Forge says and what the engine does cannot diverge. `ValidateSchema` covers structure, reserved names, and cross-references. `ValidateBehaviorRefs` checks that a `behavior` field names a machine file that exists; it has been written and tested since Epic 2 and **has never had a caller anywhere in the codebase**. This is that caller.

There is one check worth adding that the engine only performs later and more expensively. `ValidateMachine` treats a context key matching two components' fields as a hard error, because it cannot tell which one to seed from. That failure surfaces at machine-load time, a long way from the schema edit that caused it — adding a `hp` field to a second component. `buildFieldIndex` already computes exactly the map needed to catch it while typing.

## Acceptance Criteria

- [ ] Validation runs on every edit, not only on save, and results render inline near what caused them
- [ ] Errors come from `schema.ValidateSchema` — no reimplementation
- [ ] `schema.ValidateBehaviorRefs` is called, giving it its first caller, and a `behavior` naming a missing machine is reported against that field
- [ ] A context-key ambiguity warns at schema-edit time, naming both components — before the machine fails to load
- [ ] Warnings and errors are distinguishable: an error blocks the save, a warning does not
- [ ] The save footer reflects validity, so it is clear before pressing save that it will be refused
- [ ] `ValidateSchema` short-circuits by phase, so it reports the first phase's failures only — the UI says that rather than implying the list is complete
- [ ] Validation state is pushed on the page-level SSE stream
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/12-inline-validation.spec.js`.

- [ ] Naming a component `Behavior` shows an error while typing, before any save
- [ ] An entity type requiring a component that does not exist is reported
- [ ] Binding a behaviour that does not exist is reported against that field
- [ ] Adding a field named `hp` to a second component warns about the ambiguity and names both components
- [ ] A warning does not disable save; an error does
- [ ] Fixing the problem clears the message without a reload
- [ ] Errors are associated with their field for assistive tech (`aria-describedby` / `aria-invalid`), not merely positioned nearby

## Notes

- **`ValidateSchema` short-circuits by phase.** Epic 11 Story 6 found this while wiring save reports: it returns the first phase's failures and stops. Fixing that is engine work and is not in this story — but the UI must not imply completeness it does not have. Either say "first problems found" or make the engine change deliberately, as its own piece of work.
- The ambiguity check is a *warning*, not an error. The schema is legal; it is the machine that will later refuse. Blocking a save for it would be Forge inventing a rule the engine does not have.
- Validation runs on every keystroke's worth of state. These schemas are small, so this is affordable — but it is another caller of `schema.Marshal` via the dirty check, so watch that the two together stay cheap.
- Inline validation is the last story of the epic because it needs both modes to exist to be worth anything.
