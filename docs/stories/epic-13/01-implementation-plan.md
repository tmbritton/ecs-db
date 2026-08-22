# Epic 13 Story 1: Round-trip fidelity — Implementation Plan

**Goal:** a Forge save stops deleting what Forge does not model.

---

## Measured first

Before designing anything, the current round trip was run over every machine
file in the repo. The result splits into three unrelated problems, and only one
of them is what the story is about.

| File | Round-trips byte-identically? |
|---|---|
| `behaviors/goblin.json` | yes |
| `e2e/fixtures/project/behaviors/e2e-wander.json` | yes |
| `internal/agent/testdata/behaviors/burning.json` | no |
| `internal/agent/testdata/behaviors/wandering_goblin.json` | no |

The two live files — the ones Forge will actually edit — are already exact. The
two testdata files are hand-authored in a style that predates the emitter, and
their differences are:

1. **Transition shorthand expands.** `"EXTINGUISH": "extinguished"` becomes
   `[{ "target": "extinguished" }]`.
2. **Number formatting is lost.** `"speed": 2.0` becomes `"speed": 2`.
3. **Whitespace style is canonicalised.** A single-element `entry` written
   inline is expanded; a transition array written expanded is inlined.

None of those is data loss — the machine means the same thing afterwards.
**The data loss is separate and total:** any field the parser does not model is
gone, because `ParseMachine` ignores it and `EmitMachine` never knew about it.

## Two acceptance criteria in the story are wrong

Both were written before this measurement, and both assume something about the
emitter that is not true. Correct them in the story file as part of this work
rather than quietly building to a different spec.

- *"A machine file that Forge has never edited is byte-identical after a parse →
  emit round trip"* — **not achievable, and not the right goal.** The emitter has
  one canonical style by design; a file authored in another one is reformatted.
  Epic 11 accepted exactly this trade for `schema.json`. Replace with the three
  properties below.
- *"Retained fields keep their authored position"* — **not achievable either.**
  `writeStateNode` emits *known* fields in a fixed order (id, type, history,
  target, initial, entry, exit, on, after, states), not authored order. Slotting
  unknown fields into authored positions among already-reordered known ones
  produces a jumble. Replace with: retained fields keep their order relative to
  each other and follow the known fields.

The three properties that replace them:

1. **No data loss.** Every key in the input is present in the output with a
   JSON-equal value.
2. **Idempotence.** `emit(parse(emit(parse(x)))) == emit(parse(x))`. This is what
   `editable.Dirty` needs — it compares marshalled bytes against the file on
   disk, so a machine that does not settle is permanently dirty for no visible
   reason.
3. **No regression.** The two live files stay byte-identical.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Edit | `internal/agent/machine.go` | Capture unknown fields and their order |
| Edit | `internal/agent/emit.go` | Write them back |
| Edit | `internal/agent/machine_test.go`, `emit_test.go` | |
| Create | `internal/agent/roundtrip_test.go` | The three properties, over real files |
| Edit | `docs/stories/epic-13/01-round-trip-fidelity.md` | The two corrections |

---

## Task 1: Capture

```go
type MachineDefinition struct {
    …
    // Extra holds top-level fields this package does not model, so a save does
    // not delete them.
    Extra      map[string]json.RawMessage
    ExtraOrder []string
}

type StateNode struct {
    …
    Extra      map[string]json.RawMessage
    ExtraOrder []string
}
```

Order comes from `jsonorder.Keys(data)`, which already returns an object's keys
in document order and is what the parser uses for `states` and `context`.

**The known-key set is derived by reflection over the `json` tags of
`rawMachine` and `rawStateNode`**, not typed out a second time. A hand-written
list is a list that goes stale the first time someone adds a field to the
parser, and the failure is silent and bad: the new field is captured as an
extra *and* parsed normally, so the emitter writes it twice and the file has two
`initial` keys. Deriving it means adding a field to the parser automatically
stops it being an extra.

`invoke` is in those tags, so it is excluded by construction — which is the
right answer, since it is a known field the engine refuses rather than an
unknown one, and preserving it would produce a file Forge accepts and the engine
will not load.

## Task 2: Emit

Append extras after the known fields, in `ExtraOrder`, at both levels.

Values are re-indented with `json.Indent` rather than spliced verbatim: the
stored bytes carry the source file's indentation, which is wrong at the
destination. A pleasant side effect is that scalars pass through untouched, so
an extra's number formatting survives exactly — `"weight": 2.0` stays `2.0`
even though `context`'s does not.

## Task 3: Prove the properties

`roundtrip_test.go`, driven from files rather than from Go literals:

- Every machine file in the repo, plus a corpus of awkward ones under
  `testdata/roundtrip/`: a Stately-shaped export with `description`, `tags` and
  `meta`; deeply nested `meta`; an extra whose name collides with a known field
  in a different case; unicode and escapes in keys and values.
- **No data loss** is asserted structurally — unmarshal both sides to `any` and
  compare — not by grepping for field names. A test that looks for
  `"description"` is a test about `description`; a test that compares the whole
  decoded value is a test about the property, and it catches the field nobody
  thought of.
- **Idempotence** by emitting twice.
- **No regression** by byte-comparing the two live files.

## Task 4: The normalization inventory

The three normalizations above are accepted, but they must not grow silently. A
test pins the exact list: for each testdata file, the expected round-trip output
is a golden file. A new normalization changes a golden file and has to be
explained in the same commit.

This is also what Story 2 needs to know: a file in non-canonical style is dirty
the moment it is opened, before any edit, and the session has to say
"reformatting" rather than a bare "unsaved changes".

## Task 5: Decide where layout lives

The plan proposed `behaviors/<id>.layout.json`. That is out: `ScanDir` loads
every `*.json` in a behaviours directory, and the sidecar registers *successfully*
as a machine with an empty id.

Proposed instead: **per-state, in the state node's own `meta`, under a `forge`
key.** Reasons, to be written into the story:

- XState sanctions `meta` for arbitrary per-state data, and it is preserved from
  Task 1 onwards.
- It is colocated: deleting a state deletes its layout with it, so there is no
  second file to fall out of step and no orphan to clean up.
- One file means one conflict path, one atomic write, one dirty flag.
- The engine ignores it — `rawStateNode` has no `meta` field, so it reaches
  `Extra` and nothing in the interpreter or the validator reads it. Assert this
  rather than claiming it.

The cost is that Forge must *merge* into `meta` rather than replace it, since a
user may have their own keys there. Prove that is viable with a focused test in
this story; build the accessor in Story 4, where the canvas's needs are known.

---

## Verification

Mutation-check the parts that can be quietly wrong:

- Drop a key from the known-key set → the emitter writes it twice.
- Capture extras but never emit them → the loss test fails.
- Emit extras before the known fields → idempotence still holds, so this is
  caught by a golden file rather than by a property; make sure one covers it.
- Splice raw bytes without re-indenting → idempotence fails on nested objects.
