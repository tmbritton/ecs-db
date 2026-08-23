# Story 7: Transition inspector

**Epic:** 13 — Forge: AGENTS mode  
**Status:** ✅ Complete  
**Priority:** Medium — where a machine's logic actually lives

**Depends on:** Stories 4 and 6

## Context

Selecting an edge fills the `TRANSITION` panel: the event that fires it, the
guard that gates it, and the actions that run on the way through. The
prototype's caption says what makes it work — *guard params form is generated
from the guard's registered schema — dropdowns list only registered actions &
guards*.

It is the same machinery as Story 6 pointed at `Registry.Guards()`, with two
differences that matter. A transition's `cond` is a single optional guard rather
than a list, so its control is a dropdown with an explicit "none" rather than an
add-and-remove list. And a transition's `target` is a state, which means the
control that sets it has to offer the states of this machine — the check that
catches a bad one is `ValidateMachine`'s "transition target is not a known
state", and offering only real states makes it unreachable.

`after` transitions share this panel and differ in one field: they are keyed by
a duration string rather than an event name, and that string is validated by
`ParseDurationMs`. `"500"` is milliseconds; `"1s"` and `"1.5s"` work;
`"1 second"` does not.

## Acceptance Criteria

- [x] The selected transition's source and target states, both readable
- [x] The event, editable — a transition's event is an authored name, not a
      registered one, so this is a text field and not a dropdown
- [x] The target offered as the states of this machine, so a target that is not
      a state cannot be chosen
- [x] The guard offered as the registered guards plus an explicit "none",
      described from the registry
- [x] Guard parameters generated from `ParamSchema`, on the same terms as
      Story 6's action parameters and through the same code
- [x] Actions on the transition, listed, added and removed as in Story 6
- [x] An `after` transition shows its duration instead of an event, and the
      duration is validated by `agent.ParseDurationMs` — not by a regex written
      here
- [x] An invalid duration is reported against that field, with an example that
      works
- [x] Changing a transition to or from having a guard is reflected on the canvas,
      which draws guarded transitions differently
- [x] A state with two transitions on one event keeps them ordered — XState takes
      the first whose guard passes, so the order is the logic and reordering it
      silently would change what the machine does
- [x] Reordering them is possible, since it is the only way to express priority
- [x] Deleting the transition is offered here and from the canvas menu
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/13-transition-inspector.spec.js`.

- [x] Selecting an edge fills the panel with that transition's values
- [x] The guard dropdown lists registered guards and an explicit "none"
- [~] Choosing `inLineOfSight` generates its `target` parameter with the
      registered default `$player`
- [x] Clearing the guard removes `cond` from the file entirely, rather than
      writing an empty one
- [x] The target dropdown offers only this machine's states
- [x] Changing the target moves the edge on the canvas without a reload
- [x] An `after` transition shows a duration field; `1 second` is rejected with
      an example and `1s` is accepted
- [x] Two transitions on one event render in file order, and reordering them
      changes the file
- [x] The generated parameter form is the same component as Story 6's — assert
      one shared test id, not two parallel ones

## Notes

- **One generated-form implementation.** Actions and guards both carry
  `[]ParamSchema` and the form is the same form. Two implementations is how the
  guard form grows a feature the action form does not have.
- A `cond` is `{type, params}` or a bare string shorthand. `ParseMachine` accepts
  both and `EmitMachine` writes what it was given; a guard with no params should
  come back as the shorthand it was authored as rather than being expanded into
  an object, or every save rewrites files nobody edited.
- Transition order is the one place in this epic where "preserve authored order"
  is not about diffs — it is semantics. Say so in the code.
- The event name is free text and it is a real trap: a typo produces a
  transition that simply never fires, and nothing validates it because any
  string is a legal event. Consider offering the events already used in this
  machine as suggestions, without preventing a new one.

## As Implemented

### One generated form, not two

Story 6's `actionParamForm` became `paramForm`, which takes a **scope** — a
string that names the form in test ids and in problem ids — the `[]ParamSchema`
to render, the values the file holds, and how to post a change. Four callers: a
state's entry actions, a state's exit actions, a transition's actions, and the
guard. `actionList` was generalised the same way, so the row, the description,
the remove control and the add dropdown are also one implementation with three
owners.

The scope keeps the ids the browser suite selects on: `entry-0` still emits
`input-entry-0-amount` and `problem-entry-0-amount`. The sibling ids did change —
`entry-param-0-amount` became `param-entry-0-amount`, `entry-noparams-0` became
`noparams-entry-0` — and nothing outside Go tests referred to them. The guard's
scope is `guard`, so its parameters are `input-guard-distance`: the shared id
shape the story asked the browser test to assert, rather than two parallel
components each naming itself.

Underneath, `setParam` is one function taking a `*map[string]any` and a `*bool`.
`ActionSpec` and `CondSpec` carry the same `Params`/`Bare` pair and `EmitMachine`
applies the same rule to both, so "clearing the last parameter restores the bare
shorthand" is written once and both get it.

### Selection has to follow the transition

The chart's edge id is `from|kind|key|index`. Two of this story's edits move the
thing that is selected: reordering changes the index, and renaming an event
changes the key — and moves the transition to the *end* of the destination
event's list when that event already exists.

Leaving the selection alone is not a cosmetic problem. After a "move down" the
selection would name whatever took the old index, so a second click would undo
the first: the button would not repeat.

Selection lives in the URL (Story 4's decision — it survives a reload and can be
linked), the URL only changes by navigating, and only the server knows the new
index. So those two ops answer with an SSE `Redirect` to the machine URL
carrying the new selection instead of the 204 every other edit answers with.
`datastar.ServerSentEventGenerator.Redirect` was already in the SDK; it is a
`PatchElements` of a `<script>` appended to `body`, and it works — the browser
test waits on the URL, not on a timer.

**Deleting is the third**, and it was missed until review. A delete shifts every
sibling after it down one, so a selection left where it was would name the
survivor that took the index — and a second Delete would remove that one too.
It redirects to the machine with *no* selection, since the thing that was
selected is gone.

A rename to the name it already has moves nothing and answers 204, so a stray
blur does not reload the page.

Everything else — target, guard, guard parameters, transition actions — leaves
the id alone and keeps the plain 204 plus stream redraw.

### The transition handler names its operations

`/forge/agents/transition` dispatches on `op=` rather than on which parameter is
present, which is what every other editor in Forge does. The reason is specific
to this handler: two of its ops write an **empty value on purpose**. Clearing a
target makes a transition internal — it runs its actions and changes no state —
and clearing a guard removes the `cond`. A dispatch keyed on "which parameter is
non-empty" cannot tell either of those from a parameter nobody sent.

`connect` and `delete` moved onto the same scheme rather than being left as the
two exceptions, and `DeleteTransition` now takes the `TransitionRef` the other
nine mutations take.

### Refusals report against the field

`Server.editProblemField` is set beside `editProblem` and cleared with it, in one
write, and read back in one — a field left over from an earlier refusal would
make the next one point at the wrong control, and two reads could pair one
refusal's message with the next one's field. `Data.ProblemField` names it and the
panel renders `components.Problems` under that field.

**Three of the ten ops name a field**: `event`, `target` and `guard`. The rest
name none, and that is the decision rather than an omission. A refused delete or
move is about the transition and not about one control; a refused *parameter*
goes to the banner exactly as every action parameter's does, because the
generated form already has one problem slot per parameter and it carries the
registry's "required" warning. Naming a field nothing renders would be a claim
with nothing behind it.

The banner still carries every message too. It is on screen everywhere and this
panel is not.

### Order, and what it means

The panel lists every transition sharing the selected one's event, numbered, with
the selected one marked and up/down buttons on each row — on *each* row, not only
on the selected one: sorting three by selecting each in turn is a chore, not
reordering. It states the rule in words, because "these are in an order" is
invisible otherwise and the order is the machine's logic:
`interpreter.go:139` iterates `cur.On[event]` in slice order and stops at the
first eligible transition.

Anything below an unconditional transition can never fire, and the panel says so
— as a warning. `ValidateMachine` does not check it, so blocking a save for it
would invent a rule the engine does not have, the same line Story 6 drew for a
required parameter left empty.

### Where the story and the fixture disagree

The story's Playwright step asks for `inLineOfSight` and its `$player` default.
`inLineOfSight` is registered by `RegisterLineOfSight`, which the project calls
only when a map is configured, and the e2e fixture deliberately has no `[map]`.
So the browser asserts `inLineOfSight` and `pathComplete` are **absent** — the
same gating Story 6 asserts for `computePath`, and for the same reason: offering
a guard the engine would not register produces a machine that will not load.

The registered-default behaviour is tested in Go against a guard that has one,
and `inRange` gives the browser a two-parameter guard to generate — one string,
one number.

That step is `[~]` for that reason and no other.

### What review changed

Nine findings, four of them behaviour:

- **Delete did not carry the selection**, above.
- **Clearing a target left `Bare` set**, so a transition authored as
  `"GO": "resting"` emitted as `"GO": ""` — a transition aimed at the state
  called empty-string. It parses back as internal and breaks nothing, which is
  exactly why it would have gone unnoticed.
- **`aria-invalid` was set for a warning.** `components.Blocking` decides it and
  says so in its own doc — "a warning must not set it: the field's value is
  acceptable" — and `Dropdown` already obeyed that while the generated parameter
  form did not. Inherited from Story 6 and now fixed for both.
- **The required-parameter message said "the action fails"** under a guard's
  parameters. The form was generalised; its copy was not. `ParamForm.What`
  supplies the noun.

And four that were only ever going to be found by reading:

- **`modeData` took three session locks per render**, each cloning the whole
  machine through the emitter and the parser, and could build the dropdowns from
  a different snapshot than the chart. `StateTargets` and `EventNames` are now
  functions over the definition the page already holds.
- **`Problem` and `ProblemField` were read under separate locks**, which could
  pair one refusal's message with the next one's field — the same defect the
  write side was careful to avoid.
- **An `after` transition offered the machine's event names as suggestions**,
  every one of which `ParseDurationMs` would then refuse.
- **AGENTS mode was never scanned by `dstest.AssertAttrs`.** It carries more
  `data-on:` bindings than the other two modes together, and a Datastar
  attribute naming a plugin that does not exist renders perfectly and does
  nothing — the failure that scan exists for. Adding it needed sixteen new
  exemptions for the plain data attributes the canvas carries, each written down
  with why.

### Two things found by driving a browser

**A refused duration keeps what was typed.** The server renders `value="500"`
after refusing `1 second`, but the field still shows `1 second`: the markup did
not change, so the stream never patches that element, and a live input's value is
not reset by a morph that does not touch it. This is the better behaviour and the
spec now asserts it — snapping the text back would throw away the attempt with no
more explanation than the message already gives — but it is worth knowing that it
is the morph's doing rather than a decision.

**A native `<select>`'s popup was unreadable.** The list a select opens is painted
by the browser: it takes the control's colour — cream — and its own default white
ground, so the whole catalogue was pale-on-pale. Nothing about the page shows it,
because the popup exists only while it is open, and no screenshot of the page
catches it. Fixed at the source with `color-scheme: dark` on `:root`, which is
what makes the browser paint its own widgets dark, plus an explicit `option`
rule; `palette_test.go` pins both, because it is a one-line change that looks
like tidying and is not.

### The fork is built by the test, not baked into the fixture

The order test needs two transitions on one event. The first attempt put them in
`e2e-nested.json` and broke two canvas specs: one double-clicks empty space by
coordinate and one clicks a node, and an extra edge moved the layout under both.

So the spec makes its own fork instead — it renames `POKED` onto `SPOTTED`, which
merges it into `SPOTTED`'s list. That exercises the merge (appended, because
appended means lowest priority) and the redirect that has to follow it, leaves
the fixture's geometry alone, and puts the unreachable warning on screen so the
browser can watch it go away when the guarded one is moved up.

### Left for later

- **`GuardCatalogue` and `ActionCatalogue` build a registry each call.**
  `project.BuildRegistry` is cheap and both are called once per render, but two
  registries per page is two more than necessary.
- **`EventNames` walks the whole machine on every render**, for a datalist. So
  does `StateTargets`. Both are functions over the definition the page already
  holds rather than `Session` methods, which is the part that mattered: a
  `Session.Read` clones by emitting and re-parsing the whole machine, so asking
  the session for them would have serialised and re-parsed it twice more every
  stream tick — and built the dropdowns from a different snapshot than the chart
  beside them. The walks themselves are not measured.
- **A dangling target is offered as an option but cannot be chosen.** It renders
  as itself so the control says what the file says, and it is the pre-selected
  one, so nothing fires. Selecting it deliberately — only possible from a stale
  page, since it leaves the list as soon as the target resolves — is refused
  with a banner rather than being inert.
- **Deleting a transition lets go of the selection entirely**, including when
  the selection was a different edge. The delete arrives from the canvas menu
  without a selection to compare against, and any delete shifts every later
  index on that event, so dropping it is the answer that is always right rather
  than the one that is usually unnecessary.
- **Reordering is one place at a time.** Two buttons, not a drag. Enough for the
  two- and three-way forks that occur in practice, and a drag would be the third
  pointer surface in this epic.
- **An event rename that merges into an existing event appends**, which is the
  only sane default but is not offered as a choice. Reorder afterwards.
- **The unreachable warning does not appear on the canvas**, only in the panel —
  so a machine with a dead transition looks fine until you select the event.
