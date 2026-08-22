# Story 6: Usage panel

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** ✅ Complete  
**Priority:** Medium — answers "is this safe to change?"

**Depends on:** Stories 2 and 5

## Context

Before deleting a component or changing its shape, the question is who uses it. The panel answers that in two ways: which entity types declare it (from the file, always available), and how many entities currently have it (from the database, available only when there is one).

Those two are different kinds of fact and should not be run together into one number. The first is a statement about the schema; the second is a statement about a running world that will be different in a second. Presenting them alike would invite reading a live count as a design fact.

The design also shows a spawn count. **Spawns do not exist yet** — they arrive with TMX object layers in Epic 14. Showing `0` would be a lie that looks like an answer, so this story shows nothing there and says why.

## Acceptance Criteria

- [x] For a component: the entity types that declare it required or optional, as chips
- [x] A component used by no type says so plainly — that is the state that makes deletion safe, so it is worth stating rather than leaving blank
- [x] For an entity type: the number of live entities of that type, read from the database
- [x] Live counts are visually distinct from file facts, using the source lens colour the design already reserves for live data
- [x] With no database, the live count says it is unavailable rather than showing zero
- [x] Spawn counts are **absent with an explanation**, not zero
- [x] The database is opened read-only, as everywhere else
- [x] Live counts refresh over the page-level SSE stream rather than on a reload
- [x] Counting is one query for all types, not one per type
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/12-usage-panel.spec.js`. The seeded fixture has exactly three
`TestDummy` and one `TestGoblin` — counts chosen so they cannot be confused with
an off-by-one.

- [x] Selecting `Position` lists both entity types that require it
- [x] Selecting `E2EProbe` lists only `TestDummy`
- [x] Selecting `TestGoblin` in ENTS shows a live count of 1; `TestDummy` shows 3
- [x] Removing the database changes the count to the unavailable state within one poll interval, without a reload
- [x] Restoring it brings the count back
- [x] Nothing anywhere displays a spawn count of zero

## Notes

- **A zero that means "not implemented" is the worst possible output.** It is indistinguishable from a real answer, and the person who acts on it has been misled by the tool rather than by their own mistake.
- The live-instance query is `SELECT entity_type, COUNT(*) FROM entities GROUP BY entity_type`, which is one round trip for the whole panel.
- A component's usage is derivable from the file alone and must not require a database — that is the fact you need most when deciding whether a deletion is safe.
- This panel reads the same database the migration panel does. One read-only open per refresh, not one per panel.

## As Implemented

`internal/forge/usage` answers both halves and keeps them apart: `UsedBy` reads
the schema alone, `Read` opens the database read-only. `Counts` carries the
reason it has nothing when it has nothing, so the panel can say "nobody could
say" rather than "zero".

Coverage: `usage` 95.4%, `modes` 68.1%, `server` 91.5%.

### The finding: the component panel was answering a different question

The first implementation showed, under **Live entities** on a *component*, the
population of each entity type that declares it. That is only the same number
when the component is required. `Health` is optional on `TestDummy` and required
on `TestGoblin`, and the fixture creates three dummies without it and one goblin
with it — so the panel read 4 for a component one entity carries, and would have
read it in the direction that makes a destructive migration look worse than it
is. After a schema edit it can point the other way, which is the direction that
gets data deleted.

It now counts `comp_<name>` directly. The unit fixture had the same shape all
along and nothing looked at it; the e2e spec opened only components that are
required by every type that declares them — the one optional declaration in the
fixture was the one component no test selected.

### Divergences from the plan

- **`LiveCounts(dbPath)` became `Read(dbPath, component)`**, returning both
  `ByType` and `ByComponent`. The plan's single grouped query answers the ENTS
  panel; the SCHEMA panel needs a different table. Two queries in one visit,
  not one visit per panel.
- **A component with no table is absent, not zero.** A component added in the
  editor has no `comp_` table until the engine migrates. Reporting that as "0
  rows" would say the data is gone rather than not yet made — the same class of
  lie as the spawn count this story refuses to show.
- **Counts taken against a mismatched `schemaVersion` are marked stale.** The
  database's type names and table shapes describe an older world; renaming a
  type in the editor is the everyday case, where the rows are still filed under
  the old name and an unqualified "0 entities of this type" reads as "nothing
  here".
- **Reasons are sentences, not driver output.** `SQL logic error: no such
  table: entities (1)` was reaching the page. This panel is read carefully by
  someone deciding whether to delete something.

### What the review round caught

Besides the counting error above:

- **Five read-only database opens per tick per stream on SCHEMA.** `modeData`
  was computed twice per tick — once for the mode content, once for the
  confirmation region — and this story added a database open to each side. It
  is gathered once per tick now and shared.
- **No test anywhere asserted a rendered count.** Forcing every count to 999,
  making ENTS count the first type rather than the selected one, and rendering
  zero as "0 entity" all survived the whole suite. The tests asserted that the
  element existed, in the right block — the neighbour of the property again.
  All four mutations, including the original counting defect, now fail.
- **Two e2e assertions could not fail.** `not.toContainText(/\b0\b/)` on a
  static paragraph whose only digits are "14" is a tautology, and
  `toContainText("3")` is a substring match that 13, 30 and 36 satisfy — which
  threw away the whole point of choosing counts of 3 and 1.
- **The Datastar guard only ever saw the unavailable branch**, because both
  fixtures left `Counts` at its zero value.
- **A single NULL `entity_type` took the entire panel unavailable.** Now scanned
  as a nullable string, so the types that counted fine are still reported. The
  test for it skips: the schema forbids NULL there, so it cannot arise — the
  skip records that rather than the test pretending to prove something.

The component name is concatenated into SQL, because a table name cannot be a
bound parameter. It is matched against the engine's own identifier rule first,
and `TestRead_RefusesAComponentNameThatIsNotAnIdentifier` drives an injection
attempt through it and checks the entities table afterwards.
