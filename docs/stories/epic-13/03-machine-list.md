# Story 3: Machine list & context manifest

**Epic:** 13 — Forge: AGENTS mode  
**Status:** ✅ Complete  
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

- [x] Machines listed in resolved order with the mod each came from
- [x] A machine shadowing an earlier mod's is tagged `override` and says which
      mod won — Epic 12's `machineOptions` already derives this and must not
      grow a second implementation
- [x] Selecting a machine is a URL, so it survives a reload and can be linked
- [x] The selected machine's file path and `XState v4 · round-trips with Stately`
      source line, as the prototype shows them
- [x] A validity readout: `✓ valid · saves & hot-swaps into the running game`
      when it validates, and the count of problems when it does not
- [x] `CONTEXT MANIFEST` renders key → value → component from
      `ContextManifest`, read-only
- [x] **An unpopulated manifest is rendered as unpopulated**, naming the reason —
      the machine does not validate, so the engine has not worked out where its
      context comes from — and never as "this machine seeds no context"
- [x] A machine that genuinely seeds no context says exactly that, and is
      distinguishable from the above
- [~] A context key whose component has since been renamed or deleted shows as
      unresolved rather than blank, the way ENTS's seeds panel already does —
      **amended**, see *As Implemented*: the state is unreachable in AGENTS and
      is met by a stronger guarantee instead
- [x] The list is empty-state-correct: a project with no machines offers to
      create one rather than rendering an empty box
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/13-machine-list.spec.js`.

- [x] The fixture's `e2e-wander` is listed, with its mod
- [x] Selecting a machine is a URL that survives a reload
- [x] The manifest shows `hp` against the component that declares it
- [x] A machine broken on purpose shows the "not computed" manifest state, and
      the text is not the "seeds nothing" text — asserted as different strings,
      because that distinction is the whole point of the state
- [x] A second mod's machine shadowing the first is tagged `override` and names
      the winning mod
- [x] The validity readout flips when the machine is edited into and out of
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

## As Implemented

### The two absences are different sentences, and a test says so

`ValidateMachine` builds `ContextManifest` inside `if len(errs) == 0`, so a
machine with one unregistered action has an empty manifest and a machine that
seeds nothing has an empty manifest. `machines.Inspection` carries `Computed` as
a field rather than letting each call site re-derive `len(Errors) == 0`, because
"the manifest is missing" and "the machine is broken" are one fact and two
copies of it eventually disagree.

The panel therefore has three states, not two, and the browser test reads both
absence texts out of the DOM and asserts they are **different strings** — a
refactor that collapsed them would otherwise pass every test that only checks
the panel shows something.

### AC 9 amended: the state it describes cannot happen here

ENTS needs an "unresolved" marker because its manifest is stale by construction
— `agent.Loader` computed it against `schema.json` as it stood at startup, and
the schema being edited has moved on. AGENTS computes it fresh, against the
schema the session holds, on every render.

`buildFieldIndex` is built from `s.Components`, so a manifest can only ever name
a component that schema declares; and a context key matching no component is
itself a validation error, so such a machine does not validate and the manifest
is not computed at all. The panel is already in its "not computed" state, with
`context key "hp" does not match any component field` in the list beside it —
strictly more information than the word "unresolved".

Rendering a marker for a state nothing can produce would be dead code that reads
like a safety net, which is the pattern this epic has already had to remove
once. The AC is met by a stronger guarantee, pinned by a test rather than
asserted in a comment: **a computed manifest names only components the schema
declares** — and the test checks it while a manifest exists, rather than only
after breaking the schema, which is what an earlier version did and which
asserted nothing but its own precondition.

The invariant belongs to `Inspect`, so the render has to come from one call to
it. `Inspection` carries the `Definition` it validated, and the server takes
`data.Machine` from there: reading the machine separately took the session lock
twice, and a context key added between the two reads would have rendered with an
empty component cell — the blank this AC is about.

### Two holes Story 2 left, closed

**Stranded work was kept and unreachable.** `Changes`, `dirty`, `Save`,
`DiscardAll` and `Paths` — which every route validated its `?machine=` against —
all work from the resolved set, so a machine held only because it had unsaved
work when it stopped resolving could not be addressed by anything. `Session.Held`
is the wider set; discard, and only discard, resolves against it. Rename and
delete stay on the resolved set, where they have a visible subject.

The first version of this put the problem list inside the machine editor, which
renders only when a machine is selected — so when the stranded machine was the
*only* machine, the page said "This project declares no behaviour machines yet",
showed a footer claiming everything was saved, and offered nothing. The panel and
the refusal banner both live in the mode's main column now, and the empty state
says why nothing resolved instead of asserting there is nothing.

**`add-machine` posted a fixed id**, so it worked once. `Session.FreeID` proposes
one nothing has claimed, reading both what is on disk — including files that do
not load, which is what `Create` checks against — and the *working* id of every
open machine, since an unsaved rename would collide the moment it was saved. It
proposes only; `Create` still refuses a real collision, so the guard stays in one
place. One walk of the behaviours directories per call rather than one per
candidate: this runs on every AGENTS render, including every tick of every open
page's stream.

### Sorting that is not cosmetic

`ValidateMachine` produces context errors by ranging `def.Context` and state
errors by ranging `def.States`, so the same broken machine yields a different
order every call. The page stream patches an element only when its markup
changed — so an unsorted problem list re-patched the whole mode content, the id
and filename inputs included, on every tick for as long as a machine was
invalid. `machineProblems` sorts, and a test renders every rotation of one error
set and requires identical output.

### The second mod in the e2e fixture

`override` and the mod-choice create control are both decisions about which file
the *game* will run, and neither could be reached from a browser while the
fixture had one mod — a gap Story 2 recorded rather than closed. The fixture
gained `e2e-overlay`, shadowing a machine written for the purpose rather than one
Story 2's spec edits by name: overriding an existing fixture machine would move
its resolved path and the two specs would disagree about which file they meant.

### Verification

40 mutations, all caught. Two of them are worth naming because the tests that
should have caught them could not:

- **Validating `f.Current` instead of a clone.** Every route out of `machines`
  goes through `EmitMachine`, which does not serialise `ContextManifest`, so the
  mutation is invisible from a `machines_test` package — the first version of
  that test asserted the manifest, the emitted bytes and the dirty flag, and
  passed with the clone removed. It is pinned now in `inspect_internal_test.go`,
  which reaches `s.files[path].Current` directly.
- **`data-valid` hardcoded to `true`.** Only Playwright asserted the attribute;
  the Go test asserted the sentence. Both do now, because the browser tests key
  on the attribute and would otherwise be asserting a constant.

143 browser tests, `go test ./...`, `-race` and the linter green.

### Left for later

- **The list is sorted by id at resolve time**, and `Session.Machines` overrides
  each row's id with the working one — so an unsaved rename leaves the list
  visibly out of order until it is saved. Cosmetic, and pre-existing.
- **There is no reload control in AGENTS.** Nothing watches `behaviors/`, so a
  file changed outside Forge needs an explicit re-read, and the only route to one
  is the endpoint the test suite posts to. The save report offers a reload for a
  conflict; a general one belongs with Epic 17's dialogs.
- **`DiscardAll` deliberately does not reach stranded machines.** The footer's
  Discard means "throw away the work in front of you", and silently including a
  file that is on no list is how a bulk control destroys something nobody was
  looking at. The problem panel's own control handles those.
- **The round-trip corpus does not include `overlay/behaviors/`**, so the
  overriding fixture has no golden file. `internal/agent/roundtrip_test.go` names
  its directories explicitly; widening it is that story's to do.
- **`validityLine` has a branch no render reaches** — the zero-value inspection,
  which only occurs where the header is not drawn. It is there so the function is
  total rather than answering "0 problems", and the test says as much.
