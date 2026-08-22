# Story 6: State inspector

**Epic:** 13 — Forge: AGENTS mode  
**Status:** 🔲 Not started  
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

- [ ] The selected state's name, editable, with the rename going through the
      Story 2 session
- [ ] Renaming a state that other transitions target updates those targets, or
      refuses and says why — a rename that silently dangles three transitions is
      the worst of the three options
- [ ] Entry and exit actions listed in authored order, each removable
- [ ] Adding an action offers the registered actions and nothing else, each with
      its description from the registry
- [ ] The catalogue is built exactly as `project.buildRegistry` builds it, so a
      project with no map does not offer `computePath` — the engine would not
      register it, and a machine using it would not load
- [ ] Action parameters render as a form generated from `ParamSchema`: name,
      type, required, and the registered default as the placeholder
- [ ] A required parameter left empty is reported before the save, not after
- [ ] "Set as initial" is available and moves the machine's `initial`
- [ ] The current initial state does not offer "set as initial" — an action that
      does nothing is worse than an action that is absent
- [ ] A compound state's `initial` is its own, and is edited here rather than
      confused with the machine's
- [ ] Deleting the state is offered here as well as from the canvas menu, with
      the same warning
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/13-state-inspector.spec.js`.

- [ ] Selecting a node fills the inspector with that node's values
- [ ] Renaming a state updates the canvas and the file, and transitions
      targeting it still target it
- [ ] The action dropdown lists registered actions, and its contents match
      `agent.Registry` rather than a hard-coded list — assert against a name the
      fixture project's registry has and one it does not
- [ ] Adding `dealDamage` generates a form with `amount` and `target`, and
      `target` shows its registered default
- [ ] A registered action's description is shown, from the registry
- [ ] Removing an action removes it from the file
- [ ] "Set as initial" moves the `◉` on the canvas without a reload
- [ ] The initial state does not offer to become the initial state
- [ ] Accessibility: the generated parameter inputs are labelled, and a
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
