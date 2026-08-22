# Epic 13 Story 3: Machine list & context manifest — Implementation Plan

**Goal:** the left panel — what this project resolved, which mod won, and what
the selected machine seeds — plus the honest rendering of the two states the
manifest can be in.

---

## Verified before planning

Every claim below was read out of the code rather than trusted.

| Claim | Verified |
|---|---|
| `ValidateMachine` populates `ContextManifest` only when it finds no errors at all | True — `validator.go:80`, guarded by `if len(errs) == 0`. |
| The manifest it builds names only components in the schema it was given | True — built from `buildFieldIndex(s)`, and a key matching no component is itself an error, so the guarded block is unreachable with an unresolvable key. |
| `project.Machine` carries `Overrides` and `Mod`, derived from what the loader did | True — `project.go:120`, from the source string changing across a scan. Nothing recomputes it. |
| `Session.Machines()` hands back the resolved `Definition` with its manifest intact | True — Story 2's review fix. It overrides `ID` only. |
| `Session.Working()` returns a *parse-derived* definition | True — `clone()` is `EmitMachine` → `ParseMachine`, and `ParseMachine` never sets a manifest. **`data.Machine.ContextManifest` is always nil.** |
| A machine held with unsaved work that leaves the resolved set is kept and reported | True — `reload()`, Story 2. |
| Nothing can reach that machine to discard it | **True, and it is a hole.** `Changes`/`dirty`/`Save`/`DiscardAll` all iterate `s.order`, and `Paths()` — which `machinePath` validates against — is `s.order` too. The work is kept and is unreachable. |
| `add-machine` posts a hardcoded `NewMachine` | True — `agents.templ:78`. A second create is refused until the first is renamed. |
| The e2e fixture has one mod | True, so `override` and the mod-choice create control have never run in a browser. |

### The consequence that shapes the story

The selected machine's manifest **cannot** come from `data.Machine`, which is
parse-derived and has none, and **must not** come from `Machines()[i].Definition`,
which is the *resolved* machine — the file as last read, not what is being
edited. A rename in progress, an added context key, a broken transition: none of
it is in the resolved definition.

So the panel needs the manifest computed from the *working* value, on demand.
That is one call to `ValidateMachine` against a clone, which also produces the
validity readout the story asks for in the same pass — the two are the same
computation, and computing them separately is how they come to disagree.

---

## The AC that describes an unreachable state

> *"A context key whose component has since been renamed or deleted shows as
> unresolved rather than blank, the way ENTS's seeds panel already does"*

ENTS needs that state because its manifest is **stale by construction**: it was
computed by `agent.Loader` against `schema.json` as it was at startup, and the
schema being edited has moved on. A component renamed since is still named in
that manifest.

AGENTS computes the manifest fresh, against the schema the session currently
holds, every render. A key naming no component is not a stale entry — it is a
validation error (`context key "hp" does not match any component field`), which
means the machine does not validate, which means **the manifest is not computed
at all** and the panel is already in its "not computed" state, with that exact
message in the problem list beside it.

The state is therefore unreachable here, and rendering an "unresolved" marker
for it would be dead code that reads like a safety net. The AC is met by a
stronger guarantee instead: **a computed manifest names only components the
schema declares**, pinned by a test rather than asserted in a comment. The story
records the amendment.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/machines/inspect.go` | `Inspection`, `Inspect`, `Stranded`, `Held`, `FreeID` |
| Create | `internal/forge/machines/inspect_test.go` | |
| Edit | `internal/forge/machines/machines.go` | `Discard` re-resolves, so a discarded stray is let go |
| Edit | `internal/forge/server/machineedit.go` | Discard reaches held-but-unresolved paths; new `Data` fields |
| Edit | `internal/forge/server/server.go` | Fill `Inspection`, `StrandedMachines`, `NewMachineID` |
| Edit | `internal/forge/templates/modes/data.go` | The three new fields |
| Edit | `internal/forge/templates/modes/agents.templ` | List rows, header, validity, manifest, problem list, empty state |
| Edit | `internal/forge/templates/modes/agents.go` | `ManifestState`, `machineSeeds`, `validity`, `machineProblems` |
| Edit | `internal/forge/templates/modes/agents_test.go` | |
| Edit | `internal/forge/web/static/css/forge.css` | `.manifest__*`, `.badge-override`, `.list-row__mod`, `.machine-header` |
| Create | `e2e/fixtures/project/overlay/behaviors/e2e-shadowed.json` | The overriding machine |
| Create | `e2e/fixtures/project/behaviors/e2e-shadowed.json` | The one it shadows |
| Edit | `e2e/fixtures/project/game.toml` | A second mod |
| Create | `e2e/specs/13-machine-list.spec.js` | |
| Edit | `e2e/specs/13-machine-session.spec.js` | Create now names a mod; restore both behaviour dirs |

---

## Design

### `machines.Inspection`

```go
// Inspection is what the engine says about one machine as it stands.
type Inspection struct {
    Definition *agent.MachineDefinition // the value that was validated
    Errors     []agent.ValidationError  // every reason, not the first
    Manifest   map[string]string        // context key → component; meaningful only if Computed
    Computed   bool
}

func (s *Session) Inspect(path string) (Inspection, error)
```

Validated against a **clone**, not `f.Current`: `ValidateMachine` writes
`ContextManifest` onto whatever it is handed, and a render that mutated the
session's working value is the race Story 2's review already found once.

`Computed` is a field rather than `len(Errors) == 0` re-derived at each call
site, because "the manifest is missing" and "the machine is broken" are the same
fact and must not be two.

`Definition` is handed back rather than fetched separately, and that is not
convenience. The panel takes context *keys and values* from the definition and
their *components* from the manifest; two calls means two acquisitions of the
session lock, and a key added between them renders with no component beside it —
a blank cell in the one place this story exists to keep from being blank.

### The two absences, kept apart

| State | Condition | What the panel says |
|---|---|---|
| `ManifestUnavailable` | `!Computed` | *not computed — this machine does not validate, so the engine has not worked out where its context comes from* |
| `ManifestEmpty` | `Computed`, no context keys | *This machine seeds no context.* |
| `ManifestMapped` | `Computed`, keys present | key · value · component |

The e2e spec asserts the two texts are **different strings**, read out of the
DOM, because a refactor that collapsed them would otherwise pass every test.

### The clone in `Inspect` cannot be tested from outside the package

Every route out of `machines` — `Working`, `Read`, `Dirty` — goes through
`EmitMachine`, which does not serialise `ContextManifest`. So validating the
session's own value in place is **unobservable** from a `machines_test` package:
the manifest written onto `f.Current` never reaches a caller, never changes an
emitted byte, and never makes the session dirty. The first version of this
story's test asserted all three and passed with the clone removed.

The guard is pinned in `inspect_internal_test.go`, in package `machines`,
reaching `s.files[path].Current` directly. That is the only place the difference
exists.

### Reaching a stranded machine

`Session.Held()` is every path the session holds an open file for — the resolved
set plus anything kept back by `reload()` because it had unsaved work. Discard,
and only discard, resolves against it: rename and delete stay on the resolved
set, where they mean something.

`Discard` gains a `reload()` so a stray that has just been given up is let go
rather than held for the life of the process. `DiscardAll` deliberately does not
reach strays: the footer's Discard means "throw away the work in front of you",
and silently including a file that is not on screen is how a bulk control
destroys something nobody was looking at.

### The create control stops guessing

`Session.FreeID(base)` proposes an id nothing has claimed, consulting both what
is on disk (`fileDeclaring`, which sees files that do not load) and the working
ids of open files (an unsaved rename has already claimed its name). `NewMachine`,
then `NewMachine2`, and so on. It proposes only — `Create` still refuses a
genuine collision, so the guard is not moved into the control that calls it.

---

## Tests, and the mutation that has to kill each one

| Guard | Mutation it must fail against |
|---|---|
| Manifest computed only when valid | Drop the `Computed` check; the unavailable panel renders as "seeds nothing" |
| Manifest names only live components | Build the index from an empty schema |
| Validity readout counts problems | Report `len(Errors) > 0` as a boolean |
| `Inspect` does not mutate the session | Validate `f.Current` instead of a clone — **only a white-box test can fail here**, see below |
| Override tag reads `m.Overrides` | Return `false`; tag never appears |
| Override names the winning mod | Print the first mod rather than `m.Mod` |
| Held reaches strays | Resolve discard against `Paths()` |
| Discard lets a stray go | Drop the `reload()` |
| `FreeID` sees unloadable files | Consult the resolved set only |
| Empty state offers creation | Render the box with no control |

---

## Playwright

`e2e/specs/13-machine-list.spec.js`, against a fixture that grows a second mod
so `override` and the mod-choice create control are exercised by a browser for
the first time — a gap Story 2 recorded rather than closed.

The overriding pair is a **new** machine id (`e2e-shadowed`) in both mods, not a
shadow of `e2e-guard`: overriding an existing fixture machine would move the
resolved path of a file Story 2's spec edits by name, and the two specs would
fight over which file they mean.
