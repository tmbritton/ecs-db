# Story 6: State inspector

**Epic:** 13 — Forge: AGENTS mode  
**Status:** ✅ Complete  
**Priority:** Medium — the other half of authoring a state

**Depends on:** Story 4

## Context

Selecting a node fills the inspector: its name, its entry and exit actions, and
whether it is the machine's initial state.

The actions are the interesting part, and the reason `agent.Registry` has had
`Actions()` sitting unused since Epic 2. Every built-in carries a written
description and a parameter schema — `dealDamage` takes `amount` (number,
required) and `target` (string, optional, defaulting to `$player`) — so the
control that adds an action can be a list of what exists rather than a text
field, and the form for its parameters can be generated rather than written.

That distinction is worth stating plainly: **an action name is chosen, never
typed.** A typed name that is not registered produces a machine the engine
refuses to load, discovered at startup rather than at the moment of the mistake.
The registry is the list of what the engine will accept, and offering exactly
that list makes the mistake unreachable rather than merely reported.

## Acceptance Criteria

- [x] The selected state's name, editable, with the rename going through the
      Story 2 session
- [x] Renaming a state that other transitions target updates those targets, or
      refuses and says why — a rename that silently dangles three transitions is
      the worst of the three options
- [x] Entry and exit actions listed in authored order, each removable
- [x] Adding an action offers the registered actions and nothing else, each with
      its description from the registry
- [x] The catalogue is built exactly as `project.buildRegistry` builds it, so a
      project with no map does not offer `computePath` — the engine would not
      register it, and a machine using it would not load
- [x] Action parameters render as a form generated from `ParamSchema`: name,
      type, required, and the registered default as the placeholder
- [~] A required parameter left empty is reported before the save, not after —
      **reported, and deliberately not blocking.** `ValidateMachine` does not
      look at parameters, so this is Forge's rule and not the engine's
- [x] "Set as initial" is available and moves the machine's `initial`
- [x] The current initial state does not offer "set as initial" — an action that
      does nothing is worse than an action that is absent
- [x] A compound state's `initial` is its own, and is edited here rather than
      confused with the machine's
- [x] Deleting the state is offered here as well as from the canvas menu, with
      the same warning
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/13-state-inspector.spec.js`.

- [x] Selecting a node fills the inspector with that node's values
- [x] Renaming a state updates the canvas and the file, and transitions
      targeting it still target it
- [x] The action dropdown lists registered actions, and its contents match
      `agent.Registry` rather than a hard-coded list — assert against a name the
      fixture project's registry has and one it does not
- [x] Adding `dealDamage` generates a form with `amount` and `target`, and
      `target` shows its registered default
- [x] A registered action's description is shown, from the registry
- [x] Removing an action removes it from the file
- [x] "Set as initial" moves the `◉` on the canvas without a reload
- [x] The initial state does not offer to become the initial state
- [x] Accessibility: the generated parameter inputs are labelled, and a
      validation message is associated with its field rather than merely near it
      — Epic 12 Story 7 established the pattern and the primitives

## Notes

- The registry is metadata only here. `buildRegistry` passes a throwaway
  `tilemap.NewTileGrid(0,0)` to the builtins that capture one, because Forge
  wants the names and schemas and will never call a handler.
- **Custom actions are compile-time Go.** `config.ModConfig.Actions`/`Guards` are
  parsed and nothing reads them; registration happens in Go. So the dropdown is
  a closed list, and it should say so rather than appearing extensible — a modder
  cannot add an action without rebuilding the binary, and that is the Lua epic,
  not this one.
- `ParamSchema.Type` is a string — `"string"`, `"number"`, `"boolean"`,
  `"object"`. Map it to an input type, and decide what `"object"` renders as
  before `attachComponent`'s `data` parameter forces the decision at an awkward
  moment.
- Entry actions appear on the canvas node too (Story 4 draws them). One
  implementation of "what does this state's action list look like", or the node
  and the inspector will disagree about the same list.

## As Implemented

The prototype's right rail, beside the canvas: the selected state's name, what
its container enters, its entry and exit actions, and the way to delete it. The
actions come from `agent.Registry`, which is the caller `Actions()` has been
waiting for since Epic 2.

### An action name is chosen, never typed

The add control is a dropdown of exactly what the registry holds, and
`AddAction` refuses anything else — so a name the engine would not accept is
unreachable rather than merely reported. That matters more than it sounds:
`ValidateMachine` catches an unregistered action, but only when the machine is
next loaded, which is at the next `ecs-db run`.

The catalogue is `project.BuildRegistry(hasMap).Actions()`, built exactly where
the engine builds its own. The gating is the interesting part and it is real:
the fixture project has no map, so `computePath` and `stepAlongPath` are not on
the list, because the engine would not register them and a machine using one
would not load. The browser test asserts against a name the registry has and two
it does not, which is an assertion about the rule rather than about a list
someone typed.

The list is closed and the note under it says so: custom actions are
compile-time Go today, and `config.ModConfig.Actions` is parsed and read by
nothing.

### The generated form

From `ParamSchema`, never written out: the registry is where a parameter's name,
type, requiredness and default live, and a hand-written form is a second copy
that goes stale the first time a built-in gains one.

| Declared type | Control |
|---|---|
| `string` | text |
| `number` | number, and the value is stored as a number |
| `boolean` | checkbox |
| `object` | a textarea holding JSON, refused if it does not parse |

`object` was decided here rather than when `attachComponent`'s `data` forced it.
A textarea is the honest answer: the engine takes an arbitrary object there and
the registry has no schema for its contents, so any richer control would be
inventing one.

**A registered default is a placeholder, never a value.** Writing `$player` into
`dealDamage.target` as a value would put the default into the file as though
someone had chosen it — and there would then be no way to say "leave this
alone". **Clearing a parameter removes it** rather than writing `""`, for the
same reason in reverse: an absent optional parameter is what the engine expects,
and an empty string is a value.

### Where a required parameter is reported, and why it does not block

The AC marked `[~]`. `ValidateMachine` checks that an action is registered and
stops; it never looks at parameters. So "required and empty" is Forge's rule,
not the engine's, and blocking the save for it would be inventing a rule the
engine does not have — the line `internal/forge/validation` already draws for
its ambiguity warning.

It renders as a warning against the field, through Epic 12's
`components.Problems` and `ProblemsID`, so the message is *associated* with its
input rather than merely near it. The message says what actually happens: the
machine still loads, and the action fails when it runs.

### What is shared with the canvas, and what is not

The node draws its entry actions as a label; the inspector renders them as rows
of controls. There is nothing to share but the data — the same
`[]agent.ActionSpec` in both — so they cannot disagree about content, and
`chart.actionLabels` stayed where it was rather than being pulled into a shared
helper with one caller.

`machines.StateAt` is exported, though, and that one does matter: the inspector
addresses the node the canvas named, so it must resolve a path exactly rather
than through `agent.FindState`, whose job is to resolve what an author wrote in
a transition target. One implementation, and it is the one every mutation uses.

### What the review caught

Two defects made a value impossible to type back, which is the worst shape a
form can have.

**An object parameter could be edited once.** `formatParam` had cases for
string, number and bool and fell through to `%v` for everything else — and an
object comes back from the parser as a `map[string]any`, which prints as
`map[hp:3 max:10]`. So the textarea showed something that had never been JSON:
type `{"hp": 3}`, blur, and it saves; change one digit and it is refused with
"invalid character 'm'". The only way to edit an object was to select all and
retype it, with nothing on screen saying the text was not the value. It marshals
now.

**Half-typing a number deleted the value that was there.** The `value` of an
`input type=number` is the empty string whenever its contents are not a valid
float, and empty is how a parameter is cleared — so typing `1e` on the way to
`1e5` and tabbing out removed the parameter, silently. The same rule made
`convert`'s "amount takes a number" refusal unreachable from the UI: the browser
never let the bad text out. It is a text input with `inputmode="decimal"` now,
so what you typed arrives, an unfinished number is refused with a message, and
empty means empty.

**And `NaN` wedged the machine permanently** — the same guard Story 5 added, two
files away, for exactly the same reason. `ParseFloat` accepts `NaN` and `±Inf`;
`encoding/json` refuses them; and every later edit clones through the emitter,
so one bad POST left a machine that could not be edited, rendered or saved, with
`Discard` the only way out and everything else unsaved lost with it. Numbers now
go through `json.Unmarshal`, which accepts exactly the syntax the file can hold
— that also refuses `1_000` and `0x1p4`, which `ParseFloat` was silently
reinterpreting as 1000 and 16.

Three smaller ones. `null` was accepted where an object is declared, because it
unmarshals into a nil map without error. Setting a parameter and clearing it
again left a spurious `{"type": "dealDamage"}` where a bare `"dealDamage"` had
been — the clone destroys the bare flag as soon as an action legitimately emits
as an object, so the canonical form is restored when the last parameter goes.
And the checkbox branch pointed at no message at all: it rendered the problem
list with an id and nothing referenced it, which is the exact failure
`aria-describedby` exists to prevent, in the one branch no test reached.

Two things were documented and not true. `GuardCatalogue` and `ParamsOf` had no
callers and no tests while the story said one of them was tested — both are
deleted; Story 7 adds the guard catalogue when it has a reader. And both the
plan and the story claimed the panel says the action list is closed. It did not
say anything of the sort until the review pointed it out.

### What the mutation battery caught

42 mutations, all caught, over three runs.


Four test gaps, and one of them is the same mistake for the third time in this
epic: **asserting on the whole page rather than on the element**. The
description test looked for "Decrement Health.hp" anywhere in the output, and
that text is on every option in the add dropdown — so the action row could carry
no description at all and the test would pass. It is scoped to the row now.

The others were absences rather than mistakes: nothing checked that an action
edit resolves its state path exactly rather than through the engine's fuzzy
resolver, nothing posted an action edit naming a machine the project does not
have open, and nothing asserted end-to-end that the inspector's delete carries
the same warning the canvas menu does.

One guard was deleted rather than kept, on the same terms as Story 4's: the
prefix check in `selectedState` could not be reached, because trimming
`"state:"` off an edge selection leaves it unchanged and no state has a path
like `idle|on|GO|0`. The exact-path lookup already refuses everything that is
not a state; a second guard over its answer reads as a safety net and is not
one.

Two mutations were badly shaped rather than pointing at gaps. One targeted a template
helper that cannot be wrong on its own — the panel renders from `SelectedState`,
which the server derives from the same string — so it was retargeted at the
server's check, which is the one that decides. The other renamed the first
`aria-describedby` in a generated file that has three, leaving the input's
intact; the harness can now replace every occurrence, which is what a mutation
of an attribute name should do.

## Left for later

- **A required parameter is a warning, not a refusal.** If `ValidateMachine`
  ever checks parameters, this becomes blocking and the comment on
  `paramProblems` is where to say so.
- **Exit actions have no home on the canvas.** Story 4 draws entry actions under
  a node's name and nothing draws exit actions, so the inspector is the only
  place they are visible.
- **The `object` textarea has no formatting or validation beyond "is it JSON".**
  It is refused if it does not parse and accepted otherwise, whatever the shape.
- **`SelectedStateWarning` takes the session lock a second time.** It is
  computed from `TransitionsTargeting` while the selected state came from the
  earlier inspection snapshot, so the warning can describe a machine one edit
  old. `CanvasMenuWarning` already had this shape. The consequence is a stale
  sentence, not a stale edit.
- **A very large number loses precision.** `9007199254740993` is stored as
  `9007199254740992` — inherent to JSON numbers and `any`, and the same for the
  engine, but Forge is where someone would type it.
- **A string parameter's value is not trimmed.** `"  hi  "` keeps its spaces;
  only a field that is empty after trimming counts as cleared. That is
  deliberate — a leading space may be meant — but the rule is subtler than
  "empty" and worth knowing.
- **The catalogue is fetched per render.** `BuildRegistry` constructs a registry
  and registers ten built-ins each time; it is cheap and uncached, on the same
  terms as the chart.
- **Guards have no catalogue yet.** It is one line beside the actions', and it
  belongs to Story 7, which is the reader — writing it here would have been
  production code with no caller and no test.
