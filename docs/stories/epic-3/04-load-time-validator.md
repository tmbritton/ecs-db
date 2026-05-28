# Story 4: Load-Time Validator

**Epic:** 3 — Agents (behavior-as-data) runtime  
**Status:** 🔲 Not started  
**Priority:** High — gates what machines the interpreter is allowed to run

**Depends on:** Story 2 (parsed MachineDefinition), Story 3 (Registry)

## Context

The validator is the gatekeeper between parsing and execution. A parsed machine can be structurally valid JSON but still reference an action that doesn't exist, point a transition at a state that was never declared, or use a context key that has no matching component field. The validator catches all of this at load time — before any entity tries to run the machine — so errors are loud and immediate rather than silent runtime failures.

The hot-reload contract is strict: a machine that fails validation is skipped. The previous in-memory version (if any) is retained, and the game keeps running. This lets modders fix typos without restarting.

## Acceptance Criteria

- [ ] `ValidateMachine(def *MachineDefinition, registry *Registry, schema *schema.Schema) []ValidationError`
- [ ] Every `cond.type` name exists in the guard registry; unknown name → error naming the machine, state, and transition
- [ ] Every action `type` name exists in the action registry; unknown name → error naming the machine, state, and action list (entry/exit/transition)
- [ ] Every transition `target` resolves to a state defined in the same machine; unknown target → error
- [ ] Every `after` target resolves to a state defined in the same machine
- [ ] History node `target` (default) resolves to a defined state if present
- [ ] Every `context` key matches exactly one component field in `schema.json` by field name
  - Zero matches → error: `"context key 'foo' does not match any component field"`
  - Multiple matches → error: `"context key 'speed' is ambiguous: found in Movement and Goblin"`
- [ ] `invoke` in the parsed tree → error (belt-and-suspenders; parser should already reject it, but validator double-checks)
- [ ] A machine with zero validation errors passes through unchanged
- [ ] A machine with any validation errors is rejected as a whole (no partial acceptance)
- [ ] Loader wrapper: `LoadMachine(path string, ...) (*MachineDefinition, error)` — parse → validate → return or log-and-skip
- [ ] Hot-reload semantics: `LoadMachine` on an already-loaded machine ID replaces the in-memory definition only on success; on failure, retains the previous version and returns the errors
- [ ] 100% test coverage on validation logic

## Notes

- `internal/agent/validator.go`
- Context key matching is by field name across all components in the schema — not by component name. If `schema.json` has `Position.x` and some other component `NavTarget.x`, then `"context": { "x": 0 }` is ambiguous and must be rejected. Modders fix this by renaming one field.
- The validator does not know about the filesystem; `LoadMachine` is the entry point that combines file reading, parsing, and validation. The interpreter calls `LoadMachine` at startup and on hot-reload events.
- `ValidationError` should carry enough context for a clear log line: machine ID, state (if applicable), field/key, and a human-readable message.
- Compound `cond` shorthand strings (e.g. `"cond": "atTarget"`) are treated as `{ "type": "atTarget", "params": {} }` — the name is looked up in the guard registry the same way.
