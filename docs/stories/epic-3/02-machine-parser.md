# Story 2: Machine Parser and StateNode Tree

**Epic:** 3 — Agents (behavior-as-data) runtime  
**Status:** 🔲 Not started  
**Priority:** Critical blocker — the interpreter and validator both operate on the parsed tree

**Depends on:** Story 1 (schema extensions needed to resolve context keys against schema.json)

## Context

Agent files are XState v4 JSON. The parser's job is to turn a raw JSON file into a typed `MachineDefinition` containing a tree of `StateNode` objects. This tree is the in-memory representation the interpreter and validator operate on.

The parser must accept the full XState v4 format (minus `invoke`) and tolerate unknown fields Stately Studio adds (`description`, `meta`, `tags`, `version`, etc.) — Stately export → drop in → works.

`invoke` must be rejected loudly at any nesting level with a clear error naming the machine and state. Every other unsupported v4 feature that Stately might export should also produce a clear rejection rather than silent misinterpretation.

XState v4 source reference for semantics: `packages/core/src/StateNode.ts`.

## Acceptance Criteria

- [ ] `ParseMachine(data []byte) (*MachineDefinition, error)` parses valid XState v4 JSON
- [ ] Supported node types parsed correctly: atomic, compound (nested `states`), parallel (`type: "parallel"`), final (`type: "final"`), history (`type: "history"` or `type: "deep"`)
- [ ] `on`, `entry`, `exit`, `after`, `cond`, `target`, `actions`, `context`, `initial`, `id` all parsed
- [ ] Transition `cond` parsed as either a string shorthand (`"cond": "guardName"`) or object (`"cond": { "type": "...", "params": {...} }`)
- [ ] Action spec parsed as either a string shorthand (`"actions": ["actionName"]`) or object (`{ "type": "...", "params": {...} }`)
- [ ] `invoke` at any nesting level → immediate error: `"machine 'X': state 'Y': invoke is not supported"`
- [ ] Unknown top-level and per-state fields are silently ignored (Stately adds `description`, `meta`, `tags`)
- [ ] Malformed JSON → parse error with position information
- [ ] StateNode tree correctly links `Parent` pointers for every node
- [ ] `after` keys are preserved as strings (duration strings or ms integers) for the validator/scheduler to interpret
- [ ] History node's `history` field (`"shallow"` / `"deep"`) and optional `target` (default transition) are parsed
- [ ] Round-trip: the `wandering_goblin` example from `docs/game-engine-arch.md` parses without error

## Notes

- `internal/agent/machine.go` — keep parsing separate from validation; the parser produces a tree, the validator (Story 4) checks correctness.
- XState v4 uses `cond` for guards (not `guard` which is v5). The parser must use `cond`. Encountering `guard` where `cond` is expected should not silently succeed — it's an unknown field and will be ignored, which means the transition has no condition. This is probably fine given Stately v4 exports use `cond`.
- Parallel states have `type: "parallel"` and no `initial` field — all child states are entered simultaneously.
- History states have `type: "history"` (shallow, default) or the `history` field can be `"deep"`. Their `target` field is the default state to enter if no history has been recorded yet.
- The `StateType` values to support: `atomic` (default, no children), `compound` (has children + initial), `parallel` (has children, no initial, all active), `final` (terminal), `history` (shallow or deep).
