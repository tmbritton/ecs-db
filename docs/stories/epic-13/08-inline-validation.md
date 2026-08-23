# Story 8: Inline validation

**Epic:** 13 — Forge: AGENTS mode  
**Status:** ✅ Complete  
**Priority:** High — closes the loop on the epic

**Depends on:** Stories 4, 6 and 7

## Context

`agent.ValidateMachine` returns every error it finds rather than the first, and
it was written that way on purpose: the doc comment on `ValidateMachineError`
says a first-failure-only report "sends someone round the edit-save loop once
per mistake". This story is what makes that pay off.

It is the same job Epic 12 Story 7 did for `schema.json`, and it is easier in
one way and harder in another. Easier: `ValidateMachine` already returns a list,
already attributed — every `ValidationError` carries `MachineID`, `StateID` and
`Field`, so there is no narrowing trick needed to work out what a message is
about. Harder: the things to attach messages to are nodes and edges on a canvas,
not rows in a form, and a message on a node scrolled out of view is a message
nobody reads.

Epic 12's `internal/forge/validation` is the wrong package to extend — it is
about schemas — but its shape is right, and the `components.Problems` primitive,
the `ProblemsID` association helper and the blocked-save footer are all already
built and shared.

One rule carries over exactly. **Blocking means the save itself would be
refused**, which here means `ValidateMachineError` fails, because that is what
the Story 2 session validates with. Nothing else disables the button.

## Acceptance Criteria

- [x] Errors come from `agent.ValidateMachine` — no rule restated
- [x] All of them at once, not the first
- [x] Each error attaches to what caused it, using the `StateID` and `Field` it
      already carries: a node, an edge, or the machine as a whole
- [x] A node or edge with an error is marked on the canvas, so a problem on
      something off-screen is still findable — the lesson from Epic 12 Story 7,
      where a problem on an unselected row was invisible behind a disabled Save
- [x] Selecting a marked node shows its errors in the inspector, associated with
      the field they are about — a target, guard or duration error is rendered
      under the control whose value failed and pointed at from it; an error
      about an action is a row rather than a control and renders in the panel
- [x] Machine-level errors — a machine with child states and no initial — render
      against the machine rather than being hung on an arbitrary node
- [x] The save footer reflects validity across every open machine, not just the
      selected one
- [~] An error names the fix where the fix is knowable: an unregistered action
      names the registry, an unknown target names the states that exist
- [x] Validation runs on every edit and travels the page stream
- [x] Nothing is invisible: an error whose `StateID` matches no node still
      renders somewhere, rather than being dropped
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/13-inline-validation.spec.js`.

- [x] A transition retargeted to a state that does not exist reports it, against
      that edge, while typing
- [~] An unregistered action is reported against the state that carries it
- [x] A machine with child states and no initial reports it against the machine
- [~] An invalid `after` duration is reported against that transition
- [x] Two errors at once produce two messages, not one — the whole point of
      `ValidateMachine` returning a list
- [x] A node carrying an error is marked on the canvas, and the mark is visible
      without selecting it
- [x] Fixing the problem clears the message without a reload
- [x] An error blocks the save; the footer says how many
- [x] Errors are associated with their field for assistive tech, not merely
      positioned nearby

## Notes

- **Do not extend `internal/forge/validation`.** It is `schema.json`'s validator
  and its `Owner` kinds are components and entity types. A sibling package with
  the same `Problem` shape lets both render through `components.Problems`
  without either pretending to be the other. If the `Problem` type itself wants
  sharing, move that alone.
- `ValidationError.Field` is overloaded by the engine: it holds an action type,
  a guard name, a transition target, a context key or a duration depending on
  the error. Read `validator.go` before mapping it to a control, and expect the
  mapping to be a switch on the message's origin rather than on the field.
- The context-key errors — "does not match any component field", "is ambiguous"
  — are about `schema.json` as much as about the machine, and Epic 12 Story 7
  already warns about the ambiguous case from the other side. Point at the
  schema from here rather than describing it twice.
- Watch the cost. This runs on every render of every open stream, over every
  open machine, and `ValidateMachine` walks the whole state tree. Epic 12's
  equivalent was cheap because schemas are small; measure before assuming the
  same of a project with fifty machines.

## As Implemented

### The story's "easier" was this story's hard part

> *Easier: `ValidateMachine` already returns a list, already attributed — every
> `ValidationError` carries `MachineID`, `StateID` and `Field`, so there is no
> narrowing trick needed to work out what a message is about.*

That is not true, and it is worth setting out exactly how, because the whole
story turns on it. `StateNode.ID` is `machineID` plus the state's **leaf name**
(`machine.go:280`), and an authored `"id"` replaces it outright. Probed:

| canvas path | `StateID` |
|---|---|
| `idle` | `m.idle` |
| `combat` | `m.combat` |
| `combat.attacking` | **`m.attacking`** |
| `combat.idle` | **`m.idle`** |
| `named` (with `"id": "hand-written"`) | **`hand-written`** |

So a nested state's `StateID` is never its path; an authored id is unrelated to
its path in either direction; and — worst — **two states in different branches
share one id**. An error carrying `m.idle` names two different nodes and
carries nothing to tell them apart.

**`ValidationError` gained a `StatePath`**, set where the error is made. Engine
work inside a Forge story, and justified rather than convenient: the walk that
creates the error already knows the path, the field is additive (no existing
construction or assertion broke), and the alternative is a permanently
approximate answer to the one question this story exists to answer. `StateID` is
untouched — it is what `Error()` prints into the loader's log lines.

### The engine's error order was not deterministic

`ValidateMachine` iterated `def.States`, `node.On` and `node.After` — all maps —
so two runs over one machine could return the same errors in a different order.
This list renders on a two-second stream that suppresses a patch when the markup
is unchanged, so an unstable list both flickers and defeats the suppression: the
defect Story 4 hit with the state resolver, in the same shape.

The walk now goes in the file's order at every level. Fixed in the engine rather
than sorted in Forge, because that is where the disorder was — and because
authored order is a better answer than sorted, which is all Forge could have
produced.

### Placing them

`internal/forge/machinevalidation` groups the errors onto what the chart drew.
Two decisions worth naming:

**Edges are matched structurally, not by reading the message.** An error whose
source state is the edge's source and whose `Field` is that edge's target,
guard, duration or one of its actions is about that edge. The story suggested a
switch on the message's origin; the engine's wording is not an API, and ten
message prefixes here would be a second copy of `validator.go` that goes stale
the first time one is reworded. That needed `chart.Edge` to carry its action
types — everything else an error can name, the edge already had.

**An error with nowhere to go goes to the machine.** An empty `StatePath`, or
one naming a state the chart never drew: both land at the top rather than being
dropped, which is the "nothing is invisible" criterion.

### What the footer blocks on

`Session.Invalid()` counts problems per machine over the set `Save` actually
writes. A machine nobody edited is not written and so cannot make the save fail
— including one that only *became* invalid because `schema.json` changed
underneath it, which is a real case and is tested. That machine still shows its
problems on AGENTS; it just does not take the Save button away.

`reformatOnly` is what decides the set, and `dirty()` is a cost prefilter in
front of it. The two are not interchangeable claims and the comment says which
does which: no test can tell them apart by output, because `reformatOnly`
compares emitted-to-emitted and is therefore true of every unedited machine. The
prefilter is still worth having — without it every held machine would be cloned
through the emitter and parser and validated on every tick.

### Story 3's flat list is gone, as it said it would be

`machineProblems` rendered every error in one list with `"state <id>: "` glued on
the front, and its own comment said: *"Story 8 places each of these against the
node that caused it. Until it does, a count with no way to see what is wrong
would be a worse answer than a list."* It now returns only what belongs to no
state.

### A blocked Save was painted as the call to action

`.save-footer--blocked` set the border and the text colour and not the
background, so `.save-footer--dirty`'s amber won — the two rules are six hundred
lines apart and only their cascade decided. The button that could not be pressed
was the most prominent thing on screen, which is what `SaveFooterProps.Blocked`'s
own comment says it exists to prevent. It was genuinely `disabled` and carried
the reason as its `title` throughout; only the paint was wrong. Fixed and pinned
in `palette_test.go`, because it is a one-line rule whose absence is invisible.

### Where the ACs are `[~]`

**"An error names the fix where the fix is knowable."** Not done, and it would be
a second copy of the engine's message. `ValidateMachine` says `entry action "x"
is not registered`; adding "the registry offers a, b, c" here means Forge
composing a sentence the engine did not, which is the one thing this package
refuses to do everywhere else. The catalogue is already a dropdown of exactly
those names in the panel beside it, which is the same information in the place
it can be acted on.

**Two Playwright steps.** An unregistered action and an invalid `after` duration
are both **unreachable through the UI**, because the earlier stories closed those
doors: an action name is chosen from `Registry.Actions()`, and a duration is
refused by `ParseDurationMs` before it can be stored. Reaching either from a
browser would mean writing a file behind Forge's back, which tests the fixture
rather than the feature. Both are covered in Go, against the attribution layer
directly.

That is the honest shape of this story: most state-level errors are now
unreachable by construction, and the ones that remain reachable — a transition
left pointing at a deleted state, a context key the schema no longer has — are
what the browser spec exercises.

### What review changed

Ten findings. Four were behaviour, and two of those were mine to have caught:

- **A fifth map range.** `for key := range def.Context` was the one loop the walk
  did not convert — and `machineProblems`' `sort.Slice`, which had been
  compensating for it, was deleted by this story. A machine with two bad context
  keys would have re-patched the whole mode on every tick, taking the id and
  filename inputs someone was typing in with it. The plan's "the validator
  walked three maps" undercounted: there were five.
- **Structural edge attribution was under-constrained**, demonstrated with two
  reachable inputs: an entry action named `boom` on a state that also runs
  `boom` on a transition, and a compound state whose `initial` names something a
  sibling transition targets. In both the *state's* error landed on the edge, so
  the node was not marked at all and its message was filed under a transition.
  `Field` genuinely cannot separate them — same value, same state — so
  `ValidationError` gained an **`Origin`** (machine, state or transition) set
  where the error is made. This story had deferred exactly that in "Left for
  later"; the deferral was wrong.
- **`aria-label` on a bare `<span>`** names a `role="generic"`, which ARIA
  forbids and screen readers may ignore, so "named as well as coloured" was not
  reliably true. Now `role="img"`. The node's `title` was also *replacing* the
  state-kind label, so an invalid parallel or history state stopped saying what
  it was; it appends.
- **A documented decision was silently inverted.** Story 2 wrote that the AGENTS
  footer is deliberately not blocked by validity, because `Session.Save` is
  per-file and "a half-built machine on the canvas cannot hold finished work
  hostage." This story asks for the opposite in as many words. The newer
  instruction wins, but the reversal and the argument against it are now both in
  the comment rather than one of them being erased — see Left for later.

Three claims did not hold up and were corrected rather than coded around:
`Invalid()`'s comment said `dirty()` was only a cost prefilter (it is not —
`s.order` is the resolved set and `s.files` is wider, so it excludes stranded
machines too); `machinesFooter`'s said the footer renders in every mode (it
renders on AGENTS alone); and a test was named for behaviour that does not
exist. Three more tests could not fail — one asserted on the whole page when it
meant one element, one used a fixture whose zero-value report guaranteed the
pass, and one filtered on a message with no "found it" flag.

The review also found a performance defect I had introduced and then a second
one myself while it ran: `BuildRegistry` and `Schema()` were loop invariants
inside `Invalid()`'s per-machine loop — and `Schema()` deep-copies the whole
schema under a lock — and `Data.InvalidMachines` was written on every render and
read by nothing, paying the entire cost of `Invalid()` a second time per tick.

### Left for later

- **Cost is unmeasured**, which the story asked for. `Inspect` already ran per
  render and cloned the machine to do it, so this adds a walk of the error list
  and a chart lookup rather than a new validation — but `Invalid()` does add a
  clone-and-validate per dirty machine per tick. A project with fifty dirty
  machines has not been tried.
- **`Field` is still overloaded**, but `Origin` now says what an error is about,
  so the structural match only has to choose *which* transition rather than
  whether it is one at all.
- **The AGENTS Save is all-or-nothing.** `Session.Save` writes what it can and
  reports what it could not, per file; the footer now refuses the whole thing
  when any dirty machine is invalid. With one machine half-built, a different
  machine's finished work cannot be written, and the only way out is Discard —
  which discards every machine. If that trade is wrong, the fix is a per-machine
  save or a per-machine discard, not a Save that half-works in silence.
- **An error matching two edges lands on the first**, in file order. Two
  transitions out of one state naming the same missing guard are two faults with
  one message between them, and marking both would double the count the footer
  reports.
