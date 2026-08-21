# Epic 12 Story 3: Generated-SQL panel — Implementation Plan

**Goal:** show the real DDL, and make the real DDL deterministic first.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Modify | `internal/storage/componentTableBuilder.go` | Emit columns in authored order |
| Modify | `internal/storage/ddlgen.go` | Same rule in the rebuild path |
| Modify | `internal/storage/*_test.go` | Determinism tests |
| Create | `internal/forge/templates/modes/generatedsql.templ` | The panel |

---

## Task 1: The determinism fix, first

`componentTableSQL` (`componentTableBuilder.go:21`):

```go
for propName, prop := range comp.Properties {   // map order — changes per call
```

becomes an ordered walk using `jsonorder.Apply(comp.PropertyOrder, comp.Properties)`,
which yields authored order and falls back to sorted for anything unrecorded.
`buildNewColumns` in `ddlgen.go` currently sorts; give it the same call so the
two halves of the generator agree.

**Check the introspection tests before assuming this is free.** Column order is
visible in `PRAGMA table_info`, which `introspect.go` reads, and some tests may
encode the sorted order.

The test that matters:

```go
// A map-ordered generator passes a single-property fixture every time. This is
// the one that catches it.
func TestComponentTableSQL_IsDeterministic(t *testing.T) {
    // six properties, generated 50 times, all outputs byte-identical
}
```

Six properties, not two: with two, map iteration produces the same order often
enough to pass.

## Task 2: The panel

```go
sql, err := storage.MigrateComponent(name, comp)
```

That is the whole data path. The test that keeps it honest asserts the panel's
rendered text *equals* `MigrateComponent`'s output — so a future hand-tweak to
the display breaks the test rather than the trust.

Render into the `--inset` block with mono text, per the design.

## Task 3: Arrays, honestly

An array component generates `value TEXT NOT NULL DEFAULT '[]'`. The design
implies a junction table for `array‹entity-ref›`; the engine has no such thing.
Render what is generated and add a line saying arrays are stored as JSON in one
column. Do not offer an item-type control that implies otherwise.

## Task 4: Live updates

The panel re-renders on the page-level SSE stream when the selected component
changes, using the same patch mechanism as everything else. It has an `id` and
Datastar morphs it in place.

---

## Verification

`go test ./internal/storage/ ./internal/forge/...`, plus:

- generate the DDL for the repo's own `schema.json` before and after the
  ordering change, and read the diff — it should reorder columns to match the
  file and nothing else
- `make build && ./bin/ecs-db run` against a fresh database, to confirm the
  engine still bootstraps with the new column order
