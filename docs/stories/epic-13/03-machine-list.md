# Story 3: Machine list & context manifest

**Epic:** 13 — Forge: AGENTS mode  
**Status:** 🔲 Not started  
**Priority:** Medium — the left panel, and the first thing AGENTS mode shows

**Depends on:** Story 2

## Context

`MACHINES ◂ behaviors/` lists what the project resolved, one row per machine,
with an `override` tag on any that shadows an earlier mod's machine of the same
id. Below it, `CONTEXT MANIFEST` shows the fields the bound machine seeds and
the components they come from — `speed 2.0`, `aggroRange 80`, `target_x/y null`
— under the line "the components this machine attaches & seeds on spawn".

Both halves already exist as data. `project.Machine` carries the id, the file,
the mod, and whether it overrides — Epic 12 built it for the ENTS binding
dropdown. `MachineDefinition.ContextManifest` maps each context key to the
component that declares it.

The manifest has a catch worth designing around rather than discovering.
`ValidateMachine` populates it only when it finds no errors at all, so a machine
with a single unregistered action has an empty manifest — and an empty manifest
renders identically to a machine that genuinely seeds nothing. The panel that
tells you what a machine seeds must not say "nothing" when the truth is "not
computed".

## Acceptance Criteria

- [ ] Machines listed in resolved order with the mod each came from
- [ ] A machine shadowing an earlier mod's is tagged `override` and says which
      mod won — Epic 12's `machineOptions` already derives this and must not
      grow a second implementation
- [ ] Selecting a machine is a URL, so it survives a reload and can be linked
- [ ] The selected machine's file path and `XState v4 · round-trips with Stately`
      source line, as the prototype shows them
- [ ] A validity readout: `✓ valid · saves & hot-swaps into the running game`
      when it validates, and the count of problems when it does not
- [ ] `CONTEXT MANIFEST` renders key → value → component from
      `ContextManifest`, read-only
- [ ] **An unpopulated manifest is rendered as unpopulated**, naming the reason —
      the machine does not validate, so the engine has not worked out where its
      context comes from — and never as "this machine seeds no context"
- [ ] A machine that genuinely seeds no context says exactly that, and is
      distinguishable from the above
- [ ] A context key whose component has since been renamed or deleted shows as
      unresolved rather than blank, the way ENTS's seeds panel already does
- [ ] The list is empty-state-correct: a project with no machines offers to
      create one rather than rendering an empty box
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/13-machine-list.spec.js`.

- [ ] The fixture's `e2e-wander` is listed, with its mod
- [ ] Selecting a machine is a URL that survives a reload
- [ ] The manifest shows `hp` against the component that declares it
- [ ] A machine broken on purpose shows the "not computed" manifest state, and
      the text is not the "seeds nothing" text — asserted as different strings,
      because that distinction is the whole point of the state
- [ ] A second mod's machine shadowing the first is tagged `override` and names
      the winning mod
- [ ] The validity readout flips when the machine is edited into and out of
      validity, without a reload

## Notes

- The `override` rule lives in `project.loadMachines`, derived from what the
  loader actually did. Read it; do not recompute it. Epic 12 Story 5 already
  paid for one copy of this rule and deliberately did not make a second.
- The manifest is read-only and says so. Context values live in the machine's
  `context` block and are edited on the canvas side; a field that looks editable
  and silently is not is worse than one that plainly is not — the same rule
  ENTS's context-seeds panel follows.
- Resist showing a machine's state count or transition count as a headline
  number. It is easy to compute and answers no question anyone has.
