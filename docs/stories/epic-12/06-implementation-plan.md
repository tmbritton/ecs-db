# Epic 12 Story 6: Usage panel — Implementation Plan

**Goal:** answer "who uses this" without inventing an answer.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/usage/usage.go` | File-derived and live counts |
| Create | `internal/forge/usage/usage_test.go` | |
| Modify | `internal/forge/templates/modes/{schema,ents}.templ` | The panel |

---

## Task 1: Two kinds of fact, kept apart

```go
// UsedBy is derived from the schema alone and is always available — which
// matters, because it is the fact you need when deciding whether deleting a
// component is safe.
func UsedBy(s schema.DatabaseSchema, component string) []string

// Live counts come from a database that another process is writing. Absent is
// a real answer and is not zero.
type Counts struct {
	Available bool
	Reason    string
	ByType    map[string]int
}

func LiveCounts(dbPath string) Counts
```

One query for the whole panel:

```sql
SELECT entity_type, COUNT(*) FROM entities GROUP BY entity_type
```

Read-only DSN, as everywhere.

## Task 2: Rendering the difference

File facts take the ordinary text colour. Live counts take the cyan the design
reserves for live data — the same signal the engine-status readout and the map's
query layers use. Someone should be able to tell at a glance which numbers will
be different in a second.

## Task 3: Spawns

Not shown. Not zero, not "—", but a line saying spawn counts arrive with map
object layers in Epic 14. A zero here is indistinguishable from a real answer
and would be acted on.

## Task 4: Refresh

Live counts re-render on the page-level SSE stream, on the same poll as the
engine status. Not a second stream and not a second ticker; the pattern is
established.

---

## Verification

`UsedBy` against fixture schemas including a component used by nothing.
`LiveCounts` against the seeded fixture database — three `TestDummy`, one
`TestGoblin` — and against a missing one.

The e2e spec's sharpest assertion: remove the database and the count changes to
the unavailable state within a poll interval, without a reload. That proves it
is live rather than rendered once.
