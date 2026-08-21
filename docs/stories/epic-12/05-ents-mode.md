# Story 5: ENTS mode — entity types

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** 🔲 Not started  
**Priority:** High — the other half of `schema.json`

**Depends on:** Story 1

## Context

An entity type is a contract: these components must be present, these may be, extras are or are not allowed, and violations are refused or warned about. It may also name a behaviour machine, which runs on every entity of that type at spawn.

The editor mirrors that: a behaviour dropdown, two sets of component chips, a validation level, an allow-extras toggle, and a read-only panel showing the bound machine's context seeds.

The context seeds panel is read-only on purpose, and the reason is worth stating in the UI. Those values live in the behaviour file — they are the machine's `context` block — and the machine is edited in AGENTS mode. Showing them here is orientation, not a second place to change them. A field that looks editable and silently is not is worse than one that plainly is not.

Required chips carry a `🔒`: they cannot be detached, because the type requires them. Optional chips carry a `✕`. That distinction is the contract made visible, and it is also what Epic 15's spawn inspector will read.

## Acceptance Criteria

- [ ] Entity type list in **authored order** (`EntityTypeOrder`), with the `♟` glyph
- [ ] Selecting a type is a URL, as with components
- [ ] Behaviour dropdown listing machines resolved from the project's mods, plus "none"; the mod each came from is visible where two mods define the same ID
- [ ] Required and optional component chips, each offering only components that exist in the schema
- [ ] A component cannot be both required and optional on one type
- [ ] Validation level control (`strict` / `warning`) with the consequence stated
- [ ] Allow-extras checkbox
- [ ] Read-only CONTEXT SEEDS panel from the bound machine's `ContextManifest` and `Context` values, labelled as coming from the behaviour file and stating where to edit them
- [ ] With no machine bound, the seeds panel says so rather than rendering empty
- [ ] Add and delete entity types, delete behind a confirmation
- [ ] Every edit marks the shared session dirty; nothing writes on its own
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/12-ents-mode.spec.js`. The fixture project has `TestDummy` and
`TestGoblin`, and `TestGoblin` binds `e2e-wander` — so the seeds panel has real
data to show.

- [ ] The type list renders in authored order
- [ ] Selecting `TestGoblin` shows `e2e-wander` as its behaviour
- [ ] The context seeds panel shows `hp` from the machine's context block
- [ ] The seeds panel is **not editable** — assert its controls are absent or disabled, not merely unstyled
- [ ] Adding an optional component marks the footer dirty and the chip appears
- [ ] A component already required is not offered as optional
- [ ] Removing a required chip is not possible — the `🔒` is not decorative
- [ ] Saving writes the change and the file still loads
- [ ] Accessibility: chips announce whether they are removable; the validation control is a labelled group

## Notes

- **`optionalComponents` must serialise as `[]`, never `null`.** Epic 11 Story 2 handles it, and this is the mode that will produce a type with none.
- Two `behavior` fields exist in this schema — one on a component, one on an entity type — with different meanings. This mode edits the entity type's. Do not reuse the component editor's copy.
- The seeds panel needs the machine's *values*, which are in `MachineDefinition.Context`, and the *mapping*, which is in `ContextManifest` and is only populated by a successful `ValidateMachine`. A machine that fails validation has no manifest, so the panel must handle that rather than showing nothing without explanation.
- A type whose bound machine no longer exists is a real state — Story 7's `ValidateBehaviorRefs` is what reports it — and the dropdown should show the dangling name rather than silently resetting to "none".
