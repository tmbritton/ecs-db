# Epic 12 Story 5: ENTS mode — Implementation Plan

**Goal:** edit entity types against the same session SCHEMA mode edits.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/templates/modes/ents.templ` | List, editor, chips, seeds |
| Modify | `internal/forge/server/server.go` | Entity-type actions |
| Create | `e2e/specs/12-ents-mode.spec.js` | |

---

## Task 1: Structure mirrors SCHEMA

Same shape as Story 2 — list in `EntityTypeOrder`, selection in the URL
(`/forge/ents?type=Goblin`), edits through `Session.Edit`. Deliberately the
same, so the two modes do not diverge into two idioms for one file.

## Task 2: Chips

`Chip` exists from Epic 10 Story 4 with `ChipRequired` (locked) and
`ChipOptional` (removable). Required chips have no remove action at all rather
than a disabled one — the lock is the contract, not a styling state.

Adding: a dropdown of components not already on the type. A component may not
appear in both lists, so the add control for each excludes the other's members.

## Task 3: Context seeds

Read-only, and visibly so.

```go
def, ok := project.MachineByID(et.Behavior)
```

The values are `def.Context`; the mapping to components is
`def.ContextManifest`, which **`ValidateMachine` only populates on success**. So
three states, all reachable:

| State | Panel shows |
|---|---|
| no behaviour bound | "no machine bound — nothing is seeded" |
| bound, validates | the seeds, with their component |
| bound, fails validation | the context keys, and that the mapping is unavailable until the machine validates |

The third is the one that would otherwise render as a confusing blank. Story 7's
validation reports *why* it fails; this panel only needs to not lie about it.

Label the panel with where the values live and where they are edited: they come
from the behaviour file and change in AGENTS mode.

## Task 4: A dangling behaviour

An entity type may name a machine that no longer exists. The dropdown shows the
dangling name rather than silently resetting to "none" — resetting would be an
edit the user did not make, to a field they did not touch, discovered later.
Story 7 reports it as an error.

---

## Verification

Go render tests for the three seeds states. Then the e2e spec against the
fixture project, which has `TestGoblin` bound to `e2e-wander` with an `hp`
context key — real data for the seeds panel.

The assertion worth writing carefully: the seeds panel's controls are absent or
disabled, not merely unstyled. "Looks read-only" and "is read-only" differ, and
only one of them survives a stylesheet change.
