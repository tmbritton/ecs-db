# Epic 11 Story 3: XState emitter — Implementation Plan

**Goal:** `MachineDefinition` → JSON that both the engine and Stately Studio read back.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/agent/emit.go` | `EmitMachine` |
| Create | `internal/agent/emit_test.go` | Round-trip, shorthand and byte-stability tests |
| Modify | `internal/agent/machine.go` | Record state key order at parse time |

---

## Task 1: Order at parse time

`rawMachine.States` is `map[string]json.RawMessage`, so document order is
available during parse and gone afterwards. Record it the same way Story 2 does
for `schema.json` — walk the raw bytes with `json.Decoder.Token()` and store the
key sequence on `MachineDefinition` and `StateNode` (`StateOrder []string`).

Shared shape with Story 2 is deliberate: one idea, applied twice, rather than
two mechanisms that drift.

## Task 2: The emitter

Build an ordered intermediate and encode it. Never touch `Parent`.

```go
func EmitMachine(def *MachineDefinition) ([]byte, error)
```

Shorthand rules, applied bottom-up:

| Value | Long form | Emitted as |
|---|---|---|
| `ActionSpec{Type: "x", Params: nil}` | `{"type":"x"}` | `"x"` |
| `Transition{Target:"a", Cond:nil, Actions:nil}` | `{"target":"a"}` | `"a"` |
| `[]Transition{t}` | `[…]` | the element |

A params map that is present but empty is **not** the same as nil and must keep
the long form — `{"type":"x","params":{}}` round-trips to an empty map, and
collapsing it to `"x"` would lose that distinction. Worth a test of its own,
because it is the case that silently breaks equality.

## Task 3: Round-trip tests

The property is `Parse(Emit(def)) == def`. Assert with `reflect.DeepEqual` on
the definition with `Parent` and `ContextManifest` cleared (both are derived).

Write a `normalise(def)` helper that walks the tree clearing those, and use it
on both sides — otherwise the comparison fails for reasons that have nothing to
do with the emitter.

Fixtures: flat machine; nested compound; parallel; history node (shallow and
deep); entry/exit actions; guard with params; `after` with mixed key formats
(`"500"`, `"1000ms"`); an action with empty-but-present params.

Plus byte-stability against `behaviors/goblin.json`.

## Task 4: Mutation checks

Before trusting the suite, verify each of these fails:

- emit `Parent` → nested fixture must fail (cycle or mismatch)
- always emit long form → byte-stability against `goblin.json` must fail
- sort `after` keys numerically instead of preserving raw strings → the mixed-format fixture must fail
- drop `Cond` → the guard fixture must fail

---

## Verification

`go test ./internal/agent/`, plus a manual check that
`EmitMachine(ParseMachine(behaviors/goblin.json))` is byte-identical to the file
and that the result still loads in the running engine (`ecs-db run` with the
re-emitted file in place).
