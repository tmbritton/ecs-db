# Story 3: Registry and Context Types

**Epic:** 3 — Agents (behavior-as-data) runtime  
**Status:** 🔲 Not started  
**Priority:** High — validator and interpreter both depend on the registry; built-ins (Story 7) register into it

**Depends on:** Story 2 (ActionSpec / CondSpec types from the parser are referenced here)

## Context

Actions and guards are not stored in JSON — they are registered Go functions (or future Lua handlers). The registry is the dispatch layer between the machine JSON (which names actions/guards as strings) and the implementation code.

Two design constraints drive this story:

**Lua-readiness:** `ActionHandler` and `GuardHandler` must be interfaces, not bare `func` types. When Lua actions arrive in a future epic, a `LuaActionHandler` will implement `ActionHandler` without any interpreter changes.

**Visual editor readiness:** The registry stores metadata (description, parameter schemas) alongside each handler. `Registry.Actions()` and `Registry.Guards()` expose this metadata for a future web-based visual editor that lets authors pick actions and guards from a UI rather than typing strings.

`WorldWriter` and `WorldReader` are the domain-level interfaces through which actions and guards touch the database. They are the only path to the DB — built-in actions never hold a raw `*sql.Tx`. This is what makes the Lua migration easy: `LuaActionHandler` receives the same interfaces, just wrapped for scripting.

## Acceptance Criteria

- [ ] `ActionHandler` interface with `Run(ActionContext) error`
- [ ] `GuardHandler` interface with `Evaluate(GuardContext) bool`
- [ ] `Registry` type with `RegisterAction(ActionMeta, ActionHandler)` and `RegisterGuard(GuardMeta, GuardHandler)`
- [ ] `Registry.Actions() []ActionMeta` and `Registry.Guards() []GuardMeta` for introspection
- [ ] `Registry.GetAction(name string) (ActionHandler, bool)` and `Registry.GetGuard(name string) (GuardHandler, bool)`
- [ ] `ActionMeta` and `GuardMeta` carry: `Name string`, `Description string`, `Params []ParamSchema`
- [ ] `ParamSchema` carries: `Name string`, `Type string`, `Required bool`, `Default any`
- [ ] `WorldWriter` interface: `AttachComponent`, `DetachComponent`, `SetComponentValue`, `SpawnEntity`
- [ ] `WorldReader` interface: `GetComponentValue`, `HasComponent`
- [ ] `ActionContext` fields: `EntityID int64`, `Tick int64`, `World WorldWriter`, `Params map[string]any`, `Event Event`
- [ ] `GuardContext` fields: `EntityID int64`, `Tick int64`, `World WorldReader`, `Params map[string]any`, `Event Event`
- [ ] `Event` type: `Type string`, `Payload map[string]any`
- [ ] Registering two actions with the same name returns an error (or panics at startup — pick one and document it)
- [ ] 100% test coverage on registry logic

## Notes

- `internal/agent/registry.go` for the registry and handler interfaces; `internal/agent/context.go` for `ActionContext`, `GuardContext`, `WorldWriter`, `WorldReader`, `Event`.
- The concrete `WorldWriter` implementation (backed by `*sql.Tx`) and `WorldReader` (backed by `*sql.DB`) live in `internal/storage/` and implement these interfaces. The `internal/agent` package must not import `internal/storage/` — dependency flows inward.
- `SetComponentValue` writes a single field on an existing component row. Actions that need to write multiple fields call it multiple times (or the interface can be extended later).
- Duplicate registration: panicking at startup is the simpler choice and catches bugs early. An `init()`-style registration pattern makes the panic site obvious.
- `Params map[string]any` in both context types carries the static params object from the machine JSON (e.g. `{ "radius": 100 }` from `{ "type": "pickRandomTarget", "params": { "radius": 100 } }`). The handler is responsible for type-asserting what it needs.
