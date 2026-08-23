# Epic 13 Story 7: Transition inspector — Implementation Plan

**Goal:** selecting an edge fills a panel with what that transition *is* — the
event or duration that fires it, where it goes, the guard that gates it and the
actions it runs — and lets you change every one of them. The guard and its
parameters come from `agent.Registry.Guards()`, which is the caller that
metadata has been waiting for since Epic 2.

---

## Verified before planning

| Claim | Verified |
|---|---|
| `Registry.Guards()` returns `GuardMeta{Name, Description, Params []ParamSchema}` sorted by name | True — `registry.go:100`, the same shape and the same sort as `Actions()`. So the generated form is literally the same form. |
| A transition's `cond` is a single optional guard | True — `Transition.Cond *CondSpec` (`machine.go:50`). One, or nil. Not a list. |
| `CondSpec` carries the same `Bare`/`Params` pair `ActionSpec` does | True — `machine.go:37`. The shorthand rule is therefore the same rule, and `condObject` (`emit.go:483`) writes the bare string only when `Bare && Params == nil && len(Extra) == 0`. |
| Setting a `cond` on a transition authored as a bare target string emits correctly | True — `emit.go:429` writes the bare form only when `t.Bare && t.Cond == nil && len(t.Actions) == 0 && len(t.Extra) == 0`, so `Bare` never has to be cleared by hand. |
| `ValidateMachine` reports `transition target %q is not a known state` | True — `validator.go:265`. Offering only real states makes that error unreachable rather than merely reported. |
| `ValidateMachine` reports `guard %q is not registered` | True — `validator.go:272`. Same argument for the guard dropdown. |
| `ParseDurationMs` takes `"500"`, `"1s"`, `"1.5s"`, `"2m"` and refuses `"1 second"` | True — `scheduler.go:11`: `strconv.ParseInt` first, then `time.ParseDuration`. Both of the story's examples are the real behaviour. |
| Transition order within one event is semantics, not presentation | True — `interpreter.go:139` iterates `cur.On[event]` in slice order and `break`s on the first eligible one. Reordering the list changes what the machine does. |
| `inLineOfSight`'s `target` defaults to `$player` | True — `builtins/register.go:125`. **But** it is registered by `RegisterLineOfSight`, which the project only calls when a map is configured, and the e2e fixture has no `[map]`. See "Where the story and the fixture disagree". |
| The chart's edge id is `from|kind|key|index` | True — `chart.go:546`. Positional, so reordering or renaming an event moves a selection. See "Selection has to follow the transition". |
| Guarded edges already draw differently from unguarded ones | True — `chart-edge--guarded` (`canvas.go:86`, `forge.css:1861`). The AC is about the edit reaching the canvas, not about new styling. |
| `GuardCatalogue` does not exist | True. Story 6 deliberately did not write it: it would have been production code with no caller and no test. This story is the reader. |

---

## Design

### One generated parameter form, not two

Story 6's `actionParamForm` is generalised into `paramForm`, which takes a
**scope** (a string that names the form in test ids and in problem ids), the
`[]ParamSchema` to render, the values the file currently holds, and how to post
a change. Three callers: a state's entry actions, a state's exit actions, a
transition's actions — and the guard.

That is the point of the story's note. Guards and actions both carry
`[]ParamSchema` and the form is the same form; two implementations is how the
guard form grows a feature the action form does not have. The scope keeps the
test ids stable: `scope = "entry-0"` reproduces `input-entry-0-amount` exactly
as Story 6 emitted it.

| Scope | What it is |
|---|---|
| `entry-<i>` / `exit-<i>` | a state's actions — unchanged from Story 6 |
| `guard` | the selected transition's `cond` |
| `taction-<i>` | the selected transition's actions |

The parameter *writing* is shared too, and lower down: `setParam` takes a
`*map[string]any` and a `*bool` (the `Bare` flag) and is what both
`SetActionParam` and `SetGuardParam` call. Empty clears; clearing the last one
restores the bare shorthand; `convert` refuses what the file cannot hold. All of
that was settled in Story 6 and none of it is written twice.

### The event field is text; the target and guard fields are not

A transition's event is an **authored** name — any string is a legal event, and
nothing validates it. So it is a text field, per the AC, with the events already
used in this machine offered as an `<input list=…>` datalist: suggestions that
do not prevent a new one. The story's trap is real (a typo produces a transition
that never fires and nothing complains) and a datalist is the only control that
helps without lying about what is allowed.

The **target** is a state, so it is a dropdown of this machine's states —
every one of them, in authored order, dotted-path deep — plus an explicit
*"(none — internal)"*. The none is not decoration: `NUDGED` in the e2e fixture
is a transition with actions and no target, and a dropdown that could not
express it would silently retarget it the first time anyone touched the panel.

The **guard** is a dropdown of `GuardCatalogue()` plus an explicit *"(none)"*,
each option carrying its registered description exactly as Story 6's action
options do.

### `after` transitions

An `after` transition's key *is* its duration, so the same field means something
different: it is validated by `agent.ParseDurationMs` — the engine's parser,
not a regex written here — and refused with an example when it does not parse.

The refusal is reported **against the field**, which needs one new piece of
machinery: `Server.editProblemField`, set beside `editProblem` and cleared with
it. `Data.ProblemField` names the field the last refusal was about, and the
inspector renders `components.Problems` under it — for the event, the target and
the guard. *(As built: three of the ten ops name a field. A refused parameter
goes to the banner like every action parameter's does, because the generated
form's problem slot carries the registry's "required" warning. Naming a field
nothing renders would be a claim with nothing behind it.)*

### Selection has to follow the transition

The chart's edge id is positional — `from|kind|key|index` — so two of this
story's edits move the thing that is selected:

- **reorder** changes the index;
- **renaming an event** changes the key, and moves the transition to the end of
  the destination event's list if that event already exists.

Leaving the selection where it was is not a cosmetic problem: after a
"move down", the selection would name the transition that took the old index,
and clicking "move down" again would undo the first move. The button would not
repeat.

Selection lives in the URL (Story 4's decision: it survives a reload and can be
linked), and the URL only changes by navigating. Only the server knows the new
index. So the two relocating ops answer with an SSE `Redirect` to the machine
URL carrying the new selection, rather than the 204 every other edit answers
with. `datastar.ServerSentEventGenerator.Redirect` is in the SDK already and is
a `PatchElements` of a `<script>` appended to `body`.

Everything else — target, guard, guard params, transition actions — leaves the
id alone and keeps the plain 204 + stream redraw.

### Reordering is priority, and the panel says so

Up and down buttons on each transition sharing the selected transition's event,
rendered as a numbered list with the selected one marked. The panel states the
rule in words — XState takes the first whose guard passes — because "these are
in an order" is invisible otherwise, and the order is the logic.

An unguarded transition that is not last is worth a note: nothing after it can
ever fire. That is a warning and not an error, on the same terms as Story 6's
required-parameter warning — `ValidateMachine` does not check it, so blocking a
save for it would invent a rule the engine does not have.

---

## Where the story and the fixture disagree

The story's Playwright step asks for `inLineOfSight` and its `$player` default.
`inLineOfSight` is registered by `RegisterLineOfSight`, which the project calls
only when a map is configured, and the e2e fixture deliberately has no `[map]`
section. So in the browser the guard catalogue is the mapless five —
`atTarget`, `hasComponent`, `healthAbove`, `inRange`, `timerExpired` — and the
spec asserts `inLineOfSight` is **absent**, which is the same gating Story 6
asserts for `computePath` and for the same reason.

The registered-default behaviour is still tested, in Go, against a registry that
has a guard with a default — and `inRange` gives the browser a two-parameter
guard (`target`, `distance`) to generate, one string and one number.

---

## Files

| File | What |
|---|---|
| `internal/forge/machines/transitions.go` | new — `GuardCatalogue`, `TransitionRef`, `TransitionAt`, `StateTargets`, `EventNames`, and the nine mutations |
| `internal/forge/machines/actions.go` | `addActionTo`/`removeActionFrom`/`setActionParamOn`/`setParam` extracted so transitions reuse them |
| `internal/forge/machines/states.go` | `DeleteTransition` takes a `TransitionRef` |
| `internal/forge/chart/chart.go` | `Edge.Index`, so the ref can be built without splitting the id back apart |
| `internal/forge/templates/modes/inspector.templ` | `inspector` dispatches; `paramForm` generalised |
| `internal/forge/templates/modes/transition.templ` / `.go` | new — the transition panel |
| `internal/forge/server/canvasedit.go` | the transition ops, the field-scoped refusal, the redirect |
| `internal/forge/server/server.go` | `Guards`, `SelectedTransition`, `SelectedEdge`, `StateTargets`, `EventNames`, `ProblemField` |
| `e2e/specs/13-transition-inspector.spec.js` | new |
