# Story 3: XState emitter — machines back to JSON

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** 🔲 Not started  
**Priority:** High — Epic 13's canvas cannot save without it

**Depends on:** Story 1

## Context

`agent.ParseMachine` reads XState v4 JSON into a `MachineDefinition`. Nothing writes one back. Epic 13 puts a statechart canvas on top of these files, and the whole promise of that mode — drag a transition, save, the running game hot-swaps it, and the file still opens in Stately Studio — rests on an emitter that does not exist.

`json.Marshal` cannot be it, for two independent reasons. `StateNode.Parent` is a back-pointer, so a machine with nested states produces `json: unsupported value: encountered a cycle via *agent.StateNode` — verified. And even for a flat machine where the cycle never forms, the output is Go's field names: `{"ID":"goblin","States":{"idle":{"ID":"goblin.idle","Type":"atomic","Parent":null,…}}}`. That is not XState and never was.

So the emitter is hand-written, and its correctness condition is round-tripping: `Parse(Emit(def))` must equal `def`, and `Emit(Parse(file))` must be a file Stately still accepts.

The parser accepts shorthand forms — an action as a bare string, a transition as a bare target string — and the authored files use them. An emitter that always writes the long form would produce a valid but unrecognisable file, turning every save into a whole-file diff. Emit the shorthand wherever the long form carries no extra information.

## Acceptance Criteria

- [ ] `agent.EmitMachine(*MachineDefinition) ([]byte, error)` produces XState v4 JSON
- [ ] `Parent` is never emitted, and a machine with nested states emits without error
- [ ] Shorthand is preserved where it loses nothing:
  - an action with no params emits as a bare string, not `{"type": …}`
  - a transition with no cond and no actions emits as a bare target string
  - a single-element transition list emits as the element, not a one-element array
- [ ] `states`, `on` and `after` keys are emitted in a deterministic order; `after` keys keep their raw duration strings (`"500"`, `"1000ms"`) exactly as parsed
- [ ] **Round-trip**: `ParseMachine(EmitMachine(def))` deep-equals `def` for every fixture, including nested and parallel states, history nodes, entry/exit actions, guards with params, and `after`
- [ ] **Byte-stable against the real file**: `EmitMachine(ParseMachine(behaviors/goblin.json))` equals `behaviors/goblin.json`
- [ ] Output is 2-space indented with a trailing newline, matching the authored files
- [ ] `invoke` is never emitted — the engine rejects it on parse, so emitting it would produce a file the engine cannot read back
- [ ] `ContextManifest` is never emitted; it is derived by `ValidateMachine`, not authored
- [ ] `go test ./...` passes

## Playwright steps

None. Pure serialisation, no browser surface until Epic 13's canvas. Stated so
the absence is deliberate.

The round-trip property is the test that matters and is a Go-level one. When
Epic 13 lands, its spec will assert the user-visible half: edit on the canvas,
save, and the file the engine reloads is the file the canvas showed.

## Notes

- **Stately compatibility is a real requirement, not a nice-to-have.** The story's promise is that these files stay editable in both tools. If a fixture cannot be checked automatically, note explicitly which shapes were verified by hand rather than implying all were.
- The parser ignores unknown fields. That means a hand-authored file with keys the engine does not model — `description`, `meta`, Stately's layout hints — loses them on round-trip. **This is a real data-loss risk and must be called out in the story's As Implemented**, even if preserving them is out of scope here. Epic 13's layout sidecar exists partly for this reason.
- `MachineDefinition.States` is a map, so top-level state order is lost at parse time, exactly as with `schema.json`. Either record order as Story 2 does, or sort — but decide, and make it deterministic.
- Emit is not validation. `ValidateMachine` is a separate call the save path makes first.
