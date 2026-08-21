# Story 6: Usage panel

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** 🔲 Not started  
**Priority:** Medium — answers "is this safe to change?"

**Depends on:** Stories 2 and 5

## Context

Before deleting a component or changing its shape, the question is who uses it. The panel answers that in two ways: which entity types declare it (from the file, always available), and how many entities currently have it (from the database, available only when there is one).

Those two are different kinds of fact and should not be run together into one number. The first is a statement about the schema; the second is a statement about a running world that will be different in a second. Presenting them alike would invite reading a live count as a design fact.

The design also shows a spawn count. **Spawns do not exist yet** — they arrive with TMX object layers in Epic 14. Showing `0` would be a lie that looks like an answer, so this story shows nothing there and says why.

## Acceptance Criteria

- [ ] For a component: the entity types that declare it required or optional, as chips
- [ ] A component used by no type says so plainly — that is the state that makes deletion safe, so it is worth stating rather than leaving blank
- [ ] For an entity type: the number of live entities of that type, read from the database
- [ ] Live counts are visually distinct from file facts, using the source lens colour the design already reserves for live data
- [ ] With no database, the live count says it is unavailable rather than showing zero
- [ ] Spawn counts are **absent with an explanation**, not zero
- [ ] The database is opened read-only, as everywhere else
- [ ] Live counts refresh over the page-level SSE stream rather than on a reload
- [ ] Counting is one query for all types, not one per type
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/12-usage-panel.spec.js`. The seeded fixture has exactly three
`TestDummy` and one `TestGoblin` — counts chosen so they cannot be confused with
an off-by-one.

- [ ] Selecting `Position` lists both entity types that require it
- [ ] Selecting `E2EProbe` lists only `TestDummy`
- [ ] Selecting `TestGoblin` in ENTS shows a live count of 1; `TestDummy` shows 3
- [ ] Removing the database changes the count to the unavailable state within one poll interval, without a reload
- [ ] Restoring it brings the count back
- [ ] Nothing anywhere displays a spawn count of zero

## Notes

- **A zero that means "not implemented" is the worst possible output.** It is indistinguishable from a real answer, and the person who acts on it has been misled by the tool rather than by their own mistake.
- The live-instance query is `SELECT entity_type, COUNT(*) FROM entities GROUP BY entity_type`, which is one round trip for the whole panel.
- A component's usage is derivable from the file alone and must not require a database — that is the fact you need most when deciding whether a deletion is safe.
- This panel reads the same database the migration panel does. One read-only open per refresh, not one per panel.
