# Epic 13 Story 6: State inspector — Implementation Plan

**Goal:** selecting a node fills a panel with what that state is — its name, its
entry and exit actions, and whether it is the one its container enters — and
lets you change all three. The actions come from `agent.Registry`, which is what
makes an action name chosen rather than typed.

---

## Verified before planning

| Claim | Verified |
|---|---|
| `Registry.Actions()` returns metadata sorted by name, with a description and a `[]ParamSchema` each | True — `registry.go:90`. Sorted, so the catalogue is deterministic without the caller doing anything. |
| `dealDamage` takes `amount` (number, required) and `target` (string, optional, default `$player`) | True, verbatim — `builtins/register.go:55`. The story's example is the real thing. |
| `buildRegistry` gates the pathfinding and line-of-sight builtins on whether the project has a map | True — `project.go:267`, and its comment already explains why: the engine registers them only when a grid exists, so offering `computePath` to a mapless project would report a machine the engine will refuse. |
| `computePath` has no parameters at all | True. So the generated form has to render "no parameters" as a state, not as an empty box. |
| `attachComponent` takes a `data` parameter of type `"object"` | True, and it is the only one. The story asks for that decision to be made here rather than when it forces itself. |
| Every action's metadata carries a written `Description` | True for all ten built-ins. |
| Nothing reads `config.ModConfig.Actions`/`Guards` | True — parsed, never consulted. Registration is compile-time Go, so the catalogue is closed and should say so. |
| The state mutations exist from Story 5 | `RenameState`, `SetInitial`, `DeleteState` and `TransitionsTargeting` are all there and tested. Only the action list is new. |
| Story 5's `stateAt` resolves a dotted path exactly | True, and it is the rule this needs too — the inspector is addressing the node the canvas named, not searching for one. |

---

## Design

### One catalogue, built where the engine's is

`data.Actions` is `project.BuildRegistry(hasMap).Actions()`, computed on AGENTS
only, like the inspection and the free id. Not a list in the template: a
hard-coded catalogue is a second copy of the engine's vocabulary, and the first
thing it does is go stale.

**An action name is chosen, never typed.** The add control is a dropdown of
exactly what the registry holds. A name that is not registered makes a machine
the engine refuses to load, found at startup rather than at the moment of the
mistake — offering only the real list makes that unreachable rather than
reported.

The list is closed and says so: custom actions are compile-time Go today, so a
line under the dropdown states that rather than leaving it looking extensible.

### The generated parameter form

From `ParamSchema`: the name as the label, the type as the input type, required
as `required` plus an `aria-invalid` when it is empty, and the registered
default as the **placeholder** — not as the value, because a value would write
the default into the file as though someone had chosen it.

| `ParamSchema.Type` | Input |
|---|---|
| `string` | `type="text"` |
| `number` | `type="text"` with `inputmode="decimal"` |
| `boolean` | a checkbox, with its registered default said beside it |
| `object` | a textarea holding JSON, refused on save if it does not parse |

**A number is a text field**, which the first version got wrong. The value of an
`input type=number` is the empty string for anything it cannot parse, and empty
is how a parameter is cleared — so typing an exponent halfway deleted the value
that was already there, and the refusal message for a bad number was unreachable
because the browser never let the text out.

`object` is decided now rather than when `attachComponent`'s `data` forces it.
A textarea is honest: the engine takes an arbitrary object there, and pretending
otherwise would mean inventing a schema the registry does not have.

### Two new mutations, and no more

```go
func (s *Session) AddAction(path, statePath, kind, name string) error
func (s *Session) RemoveAction(path, statePath, kind string, index int) error
func (s *Session) SetActionParam(path, statePath, kind string, index int, param, value string) error
```

`SetActionParam` converts with `json.Unmarshal` rather than with `strconv`, and
that is not a preference: `ParseFloat` accepts `NaN`, `±Inf` and Go's own
literal spellings, none of which a JSON file can hold — and storing a non-finite
float wedges the machine, because every later edit clones through the emitter
and fails.

`kind` is `"entry"` or `"exit"`. Added with no parameters, then filled in — the
same shape the canvas uses for a transition, and it keeps the add control from
needing a form before it knows what it is adding.

`SetActionParam` takes the value as a string and converts by the registry's
declared type, because that is where the type is known. A number that will not
parse is refused with the parameter named.

**A parameter set to empty is removed rather than written as `""`.** An absent
optional parameter is what the engine expects; an empty string is a value, and
for `dealDamage.target` it is a different one from the default.

### Where required parameters are reported

`ValidateMachine` does not check parameters — it checks that the action is
registered and stops. So a required parameter left empty is a Forge-side rule,
and reporting it as though the engine would refuse the save would be a lie.

It renders as a **warning against the field**, using Epic 12's `components.Problems`
and `ProblemsID` so the message is associated with its input rather than merely
near it. Not blocking: the engine loads the machine and the action fails at
runtime, which is worse, but it is what the engine does and Forge does not
invent rules it cannot enforce.

### The initial control

"Set as initial" acts on the state's **container** — `SetInitial` already does
this, and Story 5 tested it. The inspector says which container: for a nested
state it reads "the state combat enters", for a top-level one "the machine
starts here". The control is absent, not disabled, when the state is already the
initial one.

### What is shared with the canvas

The node draws its entry actions as a label; the inspector renders them as rows
of inputs. There is nothing to share but the data, which is the same
`[]agent.ActionSpec` in both cases — so they cannot disagree about content, and
`chart.actionLabels` stays where it is rather than being pulled into a shared
helper that has one caller.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Edit | `internal/forge/machines/states.go` | `AddAction`, `RemoveAction`, `SetActionParam`, exported `StateAt` |
| Edit | `internal/forge/machines/states_test.go` | |
| Edit | `internal/forge/server/canvasedit.go` | `/forge/agents/action` |
| Edit | `internal/forge/server/canvasedit_test.go` | |
| Create | `internal/forge/templates/modes/inspector.templ` | The panel |
| Create | `internal/forge/templates/modes/inspector.go` | Its helpers |
| Create | `internal/forge/templates/modes/inspector_test.go` | |
| Edit | `internal/forge/templates/modes/agents.templ` | Render it beside the canvas |
| Edit | `internal/forge/templates/modes/data.go` | `Actions`, `SelectedState` |
| Edit | `internal/forge/server/server.go` | Fill them |
| Edit | `internal/forge/web/static/css/forge.css` | The rail |
| Create | `e2e/specs/13-state-inspector.spec.js` | |

---

## Tests, and the mutation that has to kill each one

| Guard | Mutation it must fail against |
|---|---|
| The catalogue is the registry's | Return a fixed list |
| The catalogue follows `hasMap` | Build it with `hasMap` always true |
| A description is shown | Drop it from the option |
| The form comes from `ParamSchema` | Render every parameter as text |
| A default is a placeholder, not a value | Write it into `value` |
| An empty optional parameter is removed | Write `""` |
| A number that will not parse is refused | Store the string |
| Required-but-empty is reported | Report nothing |
| The report is associated with its field | Position it nearby without `aria-describedby` |
| Set-as-initial acts on the container | Always set the machine's |
| The current initial offers nothing | Render it disabled instead of absent |
| Actions are listed in authored order | Sort them |
| Removing takes the one named | Remove the first |

---

## Playwright

`e2e/specs/13-state-inspector.spec.js`, with the accessibility block.

The catalogue test asserts against **a name the fixture's registry has and one
it does not** — the fixture project has no map, so `computePath` is the second
one and the assertion is about the real gating rather than about a list someone
typed.
