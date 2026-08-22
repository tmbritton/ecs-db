# Story 5: ENTS mode — entity types

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** ✅ Complete  
**Priority:** High — the other half of `schema.json`

**Depends on:** Story 1

## Context

An entity type is a contract: these components must be present, these may be, extras are or are not allowed, and violations are refused or warned about. It may also name a behaviour machine, which runs on every entity of that type at spawn.

The editor mirrors that: a behaviour dropdown, two sets of component chips, a validation level, an allow-extras toggle, and a read-only panel showing the bound machine's context seeds.

The context seeds panel is read-only on purpose, and the reason is worth stating in the UI. Those values live in the behaviour file — they are the machine's `context` block — and the machine is edited in AGENTS mode. Showing them here is orientation, not a second place to change them. A field that looks editable and silently is not is worse than one that plainly is not.

Required chips carry a `🔒`: they cannot be detached, because the type requires them. Optional chips carry a `✕`. That distinction is the contract made visible, and it is also what Epic 15's spawn inspector will read.

## Acceptance Criteria

- [x] Entity type list in **authored order** (`EntityTypeOrder`), with the `♟` glyph
- [x] Selecting a type is a URL, as with components
- [x] Behaviour dropdown listing machines resolved from the project's mods, plus "none"; the mod each came from is visible where two mods define the same ID
- [x] Required and optional component chips, each offering only components that exist in the schema
- [x] A component cannot be both required and optional on one type
- [x] Validation level control (`strict` / `warning`) with the consequence stated
- [x] Allow-extras checkbox
- [x] Read-only CONTEXT SEEDS panel from the bound machine's `ContextManifest` and `Context` values, labelled as coming from the behaviour file and stating where to edit them
- [x] With no machine bound, the seeds panel says so rather than rendering empty
- [x] Add and delete entity types, delete behind a confirmation
- [x] Every edit marks the shared session dirty; nothing writes on its own
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/12-ents-mode.spec.js`. The fixture project has `TestDummy` and
`TestGoblin`, and `TestGoblin` binds `e2e-wander` — so the seeds panel has real
data to show.

- [x] The type list renders in authored order
- [x] Selecting `TestGoblin` shows `e2e-wander` as its behaviour
- [x] The context seeds panel shows `hp` from the machine's context block
- [x] The seeds panel is **not editable** — assert its controls are absent or disabled, not merely unstyled
- [x] Adding an optional component marks the footer dirty and the chip appears
- [x] A component already required is not offered as optional
- [x] Removing a required chip is not possible — the `🔒` is not decorative
- [x] Saving writes the change and the file still loads
- [x] Accessibility: chips announce whether they are removable; the validation control is a labelled group

## Notes

- **`optionalComponents` must serialise as `[]`, never `null`.** Epic 11 Story 2 handles it, and this is the mode that will produce a type with none.
- Two `behavior` fields exist in this schema — one on a component, one on an entity type — with different meanings. This mode edits the entity type's. Do not reuse the component editor's copy.
- The seeds panel needs the machine's *values*, which are in `MachineDefinition.Context`, and the *mapping*, which is in `ContextManifest` and is only populated by a successful `ValidateMachine`. A machine that fails validation has no manifest, so the panel must handle that rather than showing nothing without explanation.
- A type whose bound machine no longer exists is a real state — Story 7's `ValidateBehaviorRefs` is what reports it — and the dropdown should show the dangling name rather than silently resetting to "none".

## As Implemented

`EntsMode` mirrors SCHEMA deliberately — list in `EntityTypeOrder`, selection in
the URL, every edit through the shared session — because they are two halves of
one file and two idioms for editing it would be one too many.

Coverage: `server` 91.3%, `modes` 68.4%.

### The finding: the behaviour dropdown was empty in the running app

`server.Config.Machines` existed from Story 2 and was **never set** in
`cmd/ecs-db/forge.go`. Every component's Behavior dropdown has offered only
"none" since it shipped. Nothing caught it: the Go tests each pass their own
`Data` fixture, and no browser test had asserted what the dropdown contained.

Fixed by wiring `proj.Machines` through — as whole `project.Machine` values
rather than IDs, which ENTS needs anyway for the seeds panel and for naming the
mod when one machine shadows another.

The first version of the browser test that was supposed to pin this **passed
with the wiring still absent**: a binding whose machine does not resolve is
deliberately still offered, so an option reading `e2e-wander` appeared either
way. It now asserts the option is not labelled `— missing`, and fails against
the unwired build.

### Divergences from the plan

- **Required chips gained a demote control.** The story says twice that a
  required chip cannot be detached, and it cannot — there is no `✕` on it. But
  the lock alone makes a component required by mistake unremovable for good, so
  each required chip carries a "make optional" control and each optional one a
  "make required". Removing a required component is then demote-then-remove.
  This is an affordance the story did not ask for; the alternative was a
  contract that can only ever be tightened.
- **There is no `SeedsUnmapped` state.** The plan called for three seeds states,
  the third being a machine that loaded but did not validate, so its
  `ContextManifest` was never built. That state cannot occur:
  `agent.Loader.LoadMachine` returns an error and stores nothing when
  `ValidateMachine` reports anything, so every machine reaching this package
  validated and its manifest covers every context key. Such a machine is simply
  absent, and indistinguishable here from one nothing declares — so the missing
  state names both possibilities rather than asserting the wrong one. Saying
  which it is needs the project's problem list, which is Story 7.
- **A seed whose component has since been renamed away is marked, not printed.**
  The manifest was computed against `schema.json` as it was when Forge started;
  the session has moved on. A component the manifest names that the session no
  longer declares reads "no longer in the schema" rather than being shown with
  the same confidence as a live one.
- **Deleting the last entity type is refused at edit time**, as deleting the
  last component already was. The engine requires one, so the session would
  otherwise enter a state that cannot be saved, and the reason would arrive
  later as a failed save rather than beside the button that caused it.

### What the review round caught

- **Component and entity-type renames shared one namespace.** `recordRename`
  wrote both into `Server.renamedTo`, keyed by bare name — and a tag component
  `Player` beside an entity type `Player` is an ordinary ECS idiom. Renaming the
  component then dragged the ENTS editor onto whatever type sorted first, with
  the address bar still naming the old one and the delete button acting on the
  new. One map per namespace now, chosen by the query parameter the name
  arrived under rather than by the mode — the ENTS page subscribes its stream
  as `?component=`, so the mode is not a reliable indicator.
- **The rename trail outlived the renames.** Discard and reload undo them on
  disk, leaving every page following a trail to a name that no longer exists.
  Both now forget it. Pre-existing for components; this story would have
  doubled the blast radius.
- **`EntsMode` was never scanned by the Datastar bundle guard** — the guard
  whose own comment records that the editor once shipped entirely inert. Only
  `SchemaMode` was rendered into it, so every handler in the new mode was
  unchecked. The tell was already in the tree: `dstest` had been given an
  exemption for `data-type`, an attribute only ENTS emits and no scan had ever
  seen.
- **`hasSignal` was a trap.** `datastar.ReadSignals` drains the request body, so
  any branch entered by a signal check would then read the value back as empty,
  answer 204 and change nothing. Dead today only because `valueAction` puts the
  value in the query string. Deleted rather than left for whoever next gives a
  dropdown a `Signal:`.
- **The two add controls differ only in the parameter their action names**, and
  swapping them — so "+ require a component" attaches an optional one — passed
  every test at every level: the option sets are identical, and nothing
  connected a rendered control to a route.
- **Three assertions that could not fail.** The e2e authored-order test compared
  against `["TestDummy", "TestGoblin"]`, which *is* alphabetical, so sorting the
  list was undetectable — the fixture's two types are now deliberately the other
  way round, which strengthens the schema-mode order test too. The selection
  test's second disjunct was satisfied by any active row. And the `[]`-vs-`null`
  tests marshalled through `encoding/json` rather than `schema.Marshal`, which
  already normalises nil to `[]` — so they measured a property no production
  path exercises.

Every fix was mutation-checked: one shared rename map, a trail surviving reload,
the dash form in `ents.templ`, and the unwired machines each fail the tests that
now cover them.
