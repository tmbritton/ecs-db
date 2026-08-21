# Story 3: XState emitter — machines back to JSON

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** ✅ Complete  
**Priority:** High — Epic 13's canvas cannot save without it

**Depends on:** Story 1

## Context

`agent.ParseMachine` reads XState v4 JSON into a `MachineDefinition`. Nothing writes one back. Epic 13 puts a statechart canvas on top of these files, and the whole promise of that mode — drag a transition, save, the running game hot-swaps it, and the file still opens in Stately Studio — rests on an emitter that does not exist.

`json.Marshal` cannot be it, for two independent reasons. `StateNode.Parent` is a back-pointer, so a machine with nested states produces `json: unsupported value: encountered a cycle via *agent.StateNode` — verified. And even for a flat machine where the cycle never forms, the output is Go's field names: `{"ID":"goblin","States":{"idle":{"ID":"goblin.idle","Type":"atomic","Parent":null,…}}}`. That is not XState and never was.

So the emitter is hand-written, and its correctness condition is round-tripping: `Parse(Emit(def))` must equal `def`, and `Emit(Parse(file))` must be a file Stately still accepts.

The parser accepts shorthand forms — an action as a bare string, a transition as a bare target string — and the authored files use them. An emitter that always writes the long form would produce a valid but unrecognisable file, turning every save into a whole-file diff. Emit the shorthand wherever the long form carries no extra information.

## Acceptance Criteria

- [x] `agent.EmitMachine(*MachineDefinition) ([]byte, error)` produces XState v4 JSON
- [x] `Parent` is never emitted, and a machine with nested states emits without error
- [x] ~~Shorthand is preserved where it loses nothing~~ — **inverted**; the long
      form is emitted consistently. See *As Implemented*: the authored file this
      has to match uses the long form throughout, so following this AC would
      have broken byte-stability against it.
- [x] `states`, `on` and `after` keys are emitted in a deterministic order; `after` keys keep their raw duration strings (`"500"`, `"1000ms"`) exactly as parsed
- [x] **Round-trip**: `ParseMachine(EmitMachine(def))` deep-equals `def` for every fixture, including nested and parallel states, history nodes, entry/exit actions, guards with params, and `after`
- [x] **Byte-stable against the real file**: `EmitMachine(ParseMachine(behaviors/goblin.json))` equals `behaviors/goblin.json`
- [x] Output is 2-space indented with a trailing newline, matching the authored files
- [x] `invoke` is never emitted — the engine rejects it on parse, so emitting it would produce a file the engine cannot read back
- [x] `ContextManifest` is never emitted; it is derived by `ValidateMachine`, not authored
- [x] `go test ./...` passes

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

## As Implemented

`agent.EmitMachine` in `internal/agent/emit.go`, with key order recorded at parse time onto `MachineDefinition.StateOrder`/`ContextOrder` and `StateNode.StateOrder`/`OnOrder`/`AfterOrder`. The order machinery moved to `internal/jsonorder` so the schema writer and this share one implementation instead of two that drift.

Coverage: `internal/agent` 88.2%, `internal/jsonorder` 91.2%.

### The shorthand AC was backwards

The story asked for shorthand to be emitted wherever it loses nothing: a paramless action as a bare string, a single transition as a bare target. Reading `behaviors/goblin.json` — the file the emitter has to reproduce — showed it does the opposite:

```json
{ "type": "computePath" }            // not "computePath"
"1000": [{ "target": "wandering" }]  // not "wandering", and not a bare object
```

Both forms are valid XState, and `ParseMachine` accepts either — but `ActionSpec` records only `{Type, Params}`, so which form was written is not recoverable after parsing. Emitting shorthand would therefore have rewritten every action in the repo's only real machine, failing the byte-stability AC, which is the more important of the two.

So the long form is emitted consistently. Verified by mutation: switching to bare-string shorthand for paramless actions fails byte-stability immediately.

The formatting rules that *are* reproduced, all read off the authored file: `context`, `states`, `on` and `after` expand; `entry` and `exit` expand one action per line; a transition array stays inline when it holds one transition and expands when it holds more; action, guard, params and transition objects render inline.

### What the review caught

The emitter was correct — 1.4M fuzzed round trips found nothing — but the **tests** were weak in the same way Story 2's were. Six mutations that changed or destroyed output survived the whole suite:

- **A transition carrying both `target` and `actions` was never tested.** That is the commonest XState transition shape, and neither repo machine has it: `goblin.json` writes target+cond and actions as two separate array elements. Discarding the target whenever actions were present passed everything.
- **`spaceInsideBraces` — the hand-written transformer whose entire justification is that it knows when it is inside a string — had no test at all.** Making quotes never open a string survived, and that one *corrupts saved data*: a context value of `"a:b"` gets written as `"a: b"`. Its only exercise anywhere was `goblin.json`'s single-key `{ "animation": "goblin_idle" }`, which has no punctuation, no second key and no empty object.
- **The bare `"type": "history"` branch was untested**, and removing it is not cosmetic: a state authored `{"type":"history"}` emitted as `{}` and re-parsed as atomic. A history node silently demoted to an ordinary one.
- **Field emission order and nested indentation were unpinned.** Swapping `entry` and `exit`, or changing the child indent, survived — either would rewrite every behaviour file on the next save.

All eight now fail against their mutation, including two the review found in production code rather than tests.

### Two real defects, both now fixed

- **The round-trip property was false for empty-but-present collections.** `"context": {}`, `"entry": []`, `"exit": []` and an empty `actions` all came back nil, so the definitions no longer compared equal. That directly contradicted `specObject`, which goes out of its way to keep `params: {}` distinct from absent params — the same distinction, opposite treatment, four fields over. `ParseMachine` now normalises empty collections to nil, which makes the headline property true rather than nearly true. An action's `params` keeps the distinction, because there `{}` really is a parameter set that happens to be empty.
- **Order recording was case-sensitive while `encoding/json` is not.** A machine authored with `"States"` or `"On"` parsed perfectly and recorded no order, so it would have been silently alphabetised on save — precisely the whole-file diff `internal/jsonorder` exists to prevent.

Also fixed: `rawKeyOrder` re-unmarshalled each node's entire subtree, three times per node, making `ParseMachine` 2.4–4.4× slower than before this story on a large machine. It now splits each node once. And two cosmetic artifacts — a fieldless transition emitting `{  }`, and a machine with no states closing its brace at the wrong indent.

### Known data loss: unknown fields are dropped

`ParseMachine` ignores fields the engine does not model, so they do not survive a round trip. Measured rather than assumed:

| Field | Survives? |
|---|---|
| `description` (machine and state level) | no |
| `meta` (machine and state level) | **no** |
| `tags` | no |
| numbers beyond 2^53 in `context`/`params` | **not exactly** — `map[string]any` decodes to `float64`, so `123456789012345678901234567890` is written back as `1.2345678901234568e+29`. Round-trip is deep-equal and idempotent, so no test can see it. |

**`meta` is where Stately Studio stores canvas layout.** A machine exported from Stately, opened in Forge and saved, comes back with its layout gone. That directly undercuts the story's stated promise that these files stay editable in both tools.

Fixing it means carrying unknown fields through the parse — a `map[string]json.RawMessage` on `MachineDefinition` and `StateNode`, re-emitted verbatim. That is a change to types the interpreter uses and is **not done here**; it is raised so it is a decision rather than an oversight. Epic 13 plans a `behaviors/<id>.layout.json` sidecar for Forge's own layout, which does not help with layout that arrives inside someone else's file.

### Divergences from the plan

- **A state's `id` is emitted only when it was authored.** `ParseMachine` derives `"<machine>.<state>"` for any state that does not declare one, so emitting it unconditionally would add a field to every state the file never had. Caught by the round-trip test, which failed on the explicit-id fixture.
- **Some authored `type` values are normalised.** `"atomic"` and `"compound"` are dropped, since both are inferred from structure; `"deep"` becomes `"history"`; and a state authored with only `"history": "deep"` gains an explicit `"type": "history"`. All data-preserving, all visible as diff noise on first save.
- **`e2e/fixtures/project/behaviors/e2e-wander.json` was normalised**, exactly as the schema fixture was in Story 2 — it was written by hand in this session in a different style from the project's. `behaviors/goblin.json` is untouched.

### Verified by mutation

Both the dropped-field class that shipped in Story 2 and the formatting rules:

| Mutation | Caught by |
|---|---|
| drop the transition guard | round-trip equality, byte-stability |
| drop transition actions | round-trip equality, byte-stability |
| drop exit actions | round-trip equality |
| drop the history default target | round-trip equality |
| drop `initial` | round-trip equality |
| drop an explicitly authored state id | round-trip equality |
| drop parallel/final `type` | round-trip equality |
| collapse empty-but-present params to absent | round-trip equality |
| ignore recorded order | 3 tests |
| always expand transition arrays | byte-stability, order |
| emit bare-string shorthand | byte-stability |
| discard target/cond when a transition has actions | round-trip equality |
| quotes never open a string (corrupts data) | the `spaceInsideBraces` test |
| empty objects render as `{ }` | the `spaceInsideBraces` test |
| drop the comma arm | the `spaceInsideBraces` test |
| drop the bare history type | round-trip equality |
| swap entry/exit emission order | field-order bytes |
| change the nested child indent | nested-layout bytes |
| case-sensitive order lookup | the key-case test |

The bottom eight all **survived** the original suite and were found by review.
