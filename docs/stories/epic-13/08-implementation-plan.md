# Epic 13 Story 8: Inline validation — Implementation Plan

**Goal:** every reason the engine would refuse the machine being edited, shown
against the thing that caused it — a node, an edge, or the machine — while it is
being edited, and all of them at once.

---

## Verified before planning

| Claim | Verified |
|---|---|
| `ValidateMachine` returns every error rather than the first | True — `validator.go:35`, and `ValidateMachineError` joins them precisely so a caller can pull the list back apart. |
| Every `ValidationError` carries `MachineID`, `StateID`, `Field` | True — `validator.go:13`. |
| A machine-level error carries an empty `StateID` | True — the two context-key errors and "machine has child states but no initial state". |
| `Field` is overloaded | True: an action type, a guard name, a transition target, an after duration, or a context key, depending on which of the ten error sites produced it. |
| Blocking is what `ValidateMachineError` decides | True — `machines.go:150` is the session's validate hook and the save goes through it. |
| The errors already reach Forge | True — `Inspection.Errors` since Story 3, computed per render on AGENTS and already on `Data`. |
| **"no narrowing trick needed to work out what a message is about"** | **False, and it is this story's central problem.** See below. |
| **`ValidateMachine`'s error order is deterministic** | **False.** `validator.go:71` is `for _, node := range def.States` — a map range. |

### `StateID` does not name a node

`StateNode.ID` is `machineID + "." + name` (`machine.go:280`) — the state's
**leaf name**, not its path — and an authored `"id"` replaces it outright.
Probed:

| canvas path | `StateID` |
|---|---|
| `idle` | `m.idle` |
| `combat` | `m.combat` |
| `combat.attacking` | **`m.attacking`** |
| `combat.idle` | **`m.idle`** |
| `named` (with `"id": "hand-written"`) | **`hand-written`** |

Three consequences, in order of severity:

1. **Two states can share one `StateID`.** `idle` and `combat.idle` are
   different states and the same id. An error carrying `m.idle` names both, and
   nothing in the error says which.
2. A nested state's `StateID` never equals the dotted path the canvas keys nodes
   on, so a naive match attaches *nothing* to any nested state.
3. An authored `id` is unrelated to the path in either direction.

So the story's "easier: already attributed" is not true as written. The
attribution has to be built.

---

## Design

### The engine carries the path

`ValidationError` gains a `StatePath` field, set where the error is made —
`validateStateNode` and `validateTransition` already recurse the tree and can
thread a prefix. This is engine work inside a Forge story, and it is justified
rather than convenient: the information exists at the point the error is
created, the field is additive (no existing construction or assertion breaks),
and the alternative is a permanently approximate answer to the one question this
story exists to answer. `StateID` stays exactly as it is — it is what the
engine's own log lines print.

That turns attribution into a lookup rather than a guess, and makes the
colliding-leaf-name case exact instead of ambiguous.

### The errors are sorted

`ValidateMachine` ranges a map, so two runs over one machine can return the same
errors in a different order. On a two-second stream that is a list that
reshuffles by itself, and it defeats the patch suppression the canvas relies on
— the same defect Story 4 hit with `findState`. Forge sorts by
`(StatePath, Field, Message)` before rendering; the engine is left alone,
because its own callers join into a log line where order does not show.

### A sibling package, not an extension

`internal/forge/machinevalidation` (name to settle at implementation), with the
same `Problem` shape `internal/forge/validation` uses, so both render through
`components.Problems` without either pretending to be the other — the story's
own instruction, and the reason is that `validation.Owner` is components and
entity types.

It takes `[]agent.ValidationError` and a chart, and returns problems grouped by
where they belong:

| Group | What lands there |
|---|---|
| a node, by path | everything with a `StatePath` that matches one |
| an edge | a transition-target, guard or after-duration error, matched by source path plus `Field` against the edge's target/guard/event |
| the machine | an empty `StatePath`, **and anything whose path matches no node** |

The last clause is the "nothing is invisible" AC: a message with nowhere to go
goes to the machine rather than being dropped.

### Marks on the canvas, messages in the rail

A node or edge carrying an error gets a mark — the design system's red, plus a
`data-invalid` attribute so a test can find it without reading a class list —
because a message in a rail is no use for a node scrolled out of view. The
inspector then renders the messages themselves, associated with the field they
are about through `ProblemsID`, exactly as Stories 6 and 7 do for the two
warnings they already carry.

The machine's own errors render above the canvas, where the "did not load"
panel already goes.

### Blocking

`Problem.Blocking` is true for every one of these, because every one of them is
a reason `ValidateMachineError` fails and therefore a reason the save is
refused. That is the whole rule; nothing else disables the button. The footer
already counts blocking problems across open machines.

### Cost

The story asks for measurement. `Inspect` already runs per render per open
machine and clones the machine to do it, so this story adds a walk of the error
list and a chart lookup, not a new validation. `BenchmarkBuild` gives the shape
for a benchmark over a project with many machines; if the answer is bad, the fix
is caching keyed on the working value, not skipping the check.

---

## Files

| File | What |
|---|---|
| `internal/agent/validator.go` | `ValidationError.StatePath`, threaded through the walk |
| `internal/forge/machinevalidation/` | new — grouping errors onto nodes, edges and the machine |
| `internal/forge/templates/modes/canvas.templ` | marks on nodes and edges |
| `internal/forge/templates/modes/inspector.templ`, `transition.templ` | the messages |
| `internal/forge/templates/modes/agents.templ` | machine-level errors |
| `e2e/specs/13-inline-validation.spec.js` | new |
