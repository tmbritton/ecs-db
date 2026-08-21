# Epic 11 Story 2: `schema.Marshal` — Implementation Plan

**Goal:** save `schema.json` so the diff shows only what changed.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Modify | `internal/schema/types.go` | Order fields on `DatabaseSchema` |
| Modify | `internal/schema/component.go` | Order field on `Component` |
| Modify | `internal/schema/validate.go` | Record order in `LoadSchema` |
| Create | `internal/schema/marshal.go` | `Marshal` |
| Create | `internal/schema/marshal_test.go` | Round-trip and diff-stability tests |

---

## Task 1: Order as data

```go
type DatabaseSchema struct {
	SchemaVersion int
	Components    map[string]Component
	EntityTypes   map[string]EntityType

	// ComponentOrder and EntityTypeOrder record the order the keys appeared in
	// when this schema was loaded, so Marshal can write the file back the way
	// its author arranged it. Empty for a schema built in code, in which case
	// Marshal sorts — deterministic either way, never map order.
	ComponentOrder  []string `json:"-"`
	EntityTypeOrder []string `json:"-"`
}
```

`Component` gains `PropertyOrder []string` on the same terms.

`json:"-"` matters: these must not appear in the output, and `LoadSchema`
populates them from the raw bytes rather than from a JSON field.

**Recording order.** `LoadSchema` already unmarshals into a `raw` struct with
`json.RawMessage` for the maps, so the key order is still available there. Walk
it with `json.Decoder.Token()` — the decoder yields keys in document order,
which `map[string]T` has already thrown away by the time the value exists.

## Task 2: Ordering rule

One rule at every level:

```go
// order returns keys in recorded order first, then any not recorded, sorted.
// New keys sort among themselves and land at the end, so a component added
// through the UI appears in a predictable place instead of wherever a map
// iteration happened to put it.
func order(recorded []string, present map[string]bool) []string
```

Recorded keys that are no longer present are skipped (a deleted component).
Present keys that were never recorded are appended sorted (a new one).

## Task 3: Marshal

Hand-rolled through `json.Encoder` on an ordered intermediate, not
`json.Marshal` on the struct — the whole point is that the struct cannot express
order. Emit:

```
{
  "schemaVersion": N,
  "components": { … in order … },
  "entityTypes": { … in order … }
}
```

with 2-space indent and a trailing newline, `SetEscapeHTML(false)`.

`omitempty` semantics to preserve per field: `Component.Behavior` and
`Component.Items` stay omitted when empty; `EntityType.RequiredComponents` and
`OptionalComponents` emit `[]` when nil, never `null`.

## Task 4: The tests that matter

1. **Byte-stable round-trip against the repo's own `schema.json`.** Read it,
   load it, marshal it, compare bytes. This is the test; everything else is
   supporting detail.
2. **Round-trip equality** for fixtures covering each component shape:
   `object`, `array` with `items`, `entity-ref`, a component with `behavior`,
   an entity type omitting `optionalComponents`.
3. **Diff stability**: load, rename one component, marshal, and assert the
   output differs from the original in exactly the lines naming that component
   — a line-level diff count, not an eyeball.
4. **No recorded order → deterministic**: marshal a code-built schema twice and
   assert byte equality (guards against map iteration leaking in).
5. **Mutation check**: with the ordering rule replaced by plain map iteration,
   test 1 must fail. Run it and confirm before trusting the suite.

---

## Verification

`go test ./internal/schema/`, then `git diff --stat schema.json` after a
load-marshal-write round trip: it must be empty.
