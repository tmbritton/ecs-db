# Epic 11 — Forge: project model & engine file I/O

Forge writes the files the engine reads. This epic is the read/write spine
everything after it stands on: resolve a project, round-trip its files, know
what is dirty, and save without ever letting the engine's fsnotify watcher see a
half-written file.

**The engine has no file-writing code at all today.** There is no `schema.json`
writer and no XState emitter — only readers. Both halves are built here.

## Verified before planning

The three traps named in `docs/plan.md` were checked against the real code rather
than assumed. All three are real, and two are worse or narrower than described:

| Trap | Verified behaviour |
|---|---|
| Key reordering | Real. `schema.json` is authored `Position, Health, Sprite, Tile, Path, Speed, GoblinStats`; `json.Marshal` emits alphabetical `GoblinStats, Health, Path, Position, Speed, Sprite, Tile`. Property order inside each component is lost the same way — `Component.Properties` is also a map. |
| Nil slices → `null` | Real, but this repo's `schema.json` writes `"optionalComponents": []` explicitly, so it does not bite today. It bites the first schema that omits the key: `EntityType.OptionalComponents` has no `omitempty`, so nil marshals as `null`. |
| `StateNode.Parent` back-pointer | Real, and cleaner than expected: Go detects the cycle and returns `json: unsupported value: encountered a cycle via *agent.StateNode` rather than overflowing the stack. It only fires for machines with nested states — but the naive output is unusable regardless, emitting Go field names (`"ID"`, `"Parent":null`, `"Type":"atomic"`) rather than XState. |

The last one settles the design: a hand-written emitter is required, not a
struct-tag fix.

## Found while building

**`ValidateMachine` accepts an `initial` that names a state which does not
exist.** A transition targeting a missing state is rejected —
`transition target "nowhere" is not a known state` — but the machine's own
`initial` is not checked. Verified against the real validator:

| Machine | Result |
|---|---|
| `"initial":"nowhere"`, no such state | **accepted** |
| `"on":{"GO":"nowhere"}`, no such state | rejected |
| unregistered entry action | rejected |
| unregistered guard | rejected |

**Fixed.** `ValidateMachine` now checks that every `initial` names a child of
the state declaring it, and that a compound state with children names one at all.

Scope was the subtle part. Transition targets are validated against every state
in the machine, but `initial` chooses among *children* — validating it against
the machine-wide set would accept a compound state whose `initial` names a state
in an unrelated branch, which is exactly as broken at runtime as naming one that
does not exist. Both the bare child key and the fully qualified dotted id are
accepted, since the codebase treats those interchangeably for targets and making
`initial` the one place a dotted id is rejected would be a trap rather than a
rule. Parallel states are exempt: their regions are all entered at once, so an
initial would be meaningless rather than missing.

This mattered for Epic 13. Deleting a state on the statechart canvas is one
click; if the deleted state was the machine's `initial`, the file saved clean,
passed every validation Forge could run, and failed at runtime.

| Shape | Before | After |
|---|---|---|
| `initial` names a missing state | accepted | rejected |
| the state that was `initial` is deleted | accepted | rejected |
| nested `initial` names a missing child | accepted | rejected |
| nested `initial` names a state in another branch | accepted | rejected |
| compound state with children and no `initial` | accepted | rejected |
| valid flat, nested, and parallel machines | accepted | accepted |

Both real machines in the repo and all 19 test packages still pass — a rule that
rejected working files would be worse than the gap it closes.

## Stories

| # | Story | Delivers |
|---|---|---|
| 1 | [Project model](01-project-model.md) | Resolve `game.toml`, mod load order, behavior override semantics |
| 2 | [`schema.Marshal`](02-schema-marshal.md) | A `schema.json` writer that produces clean `git diff`s |
| 3 | [XState emitter](03-xstate-emitter.md) | `MachineDefinition` back to JSON that still imports into Stately |
| 4 | [Dirty tracking & save/discard](04-dirty-tracking.md) | Change state against an on-disk snapshot, driving the save footer |
| 5 | [Atomic writes](05-atomic-writes.md) | Temp file + rename, so hot reload never sees a partial file |
| 6 | [Hot-reload feedback](06-hot-reload-feedback.md) | Reload success/failure in the UI; wire `Reconciler.Reconcile` |

## Process

Per `AGENTS.md`: TDD, a fresh-context code review before each commit, and
`make test` + `make e2e` + the linter green. Each story file carries a
**Playwright steps** section written before the code.

Stories 2 and 3 are pure serialisation with no browser surface; their Playwright
sections say so rather than leaving the absence to be guessed at.
