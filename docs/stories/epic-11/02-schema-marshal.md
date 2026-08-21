# Story 2: `schema.Marshal` — a writer that produces clean diffs

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** 🔲 Not started  
**Priority:** High — Epic 12 cannot save anything without it

**Depends on:** Story 1

## Context

`schema.json` is a file a human wrote and a human reads. It is in version control, it is reviewed in diffs, and its ordering carries intent: `Position, Health, Sprite` are grouped because they belong together, not because P precedes H.

Go maps have no order. `json.Marshal(DatabaseSchema)` emits keys alphabetically, so the first time Forge saves a file it never meant to reorder, every component moves and the diff is the whole file. Verified against this repo: authored order is `Position, Health, Sprite, Tile, Path, Speed, GoblinStats`; Go emits `GoblinStats, Health, Path, Position, Speed, Sprite, Tile`. The same happens to the properties inside each component, which are also a map.

There are two honest options. Emit a fixed order and accept a one-time reordering of the checked-in file; or record the authored order at load time and honour it on save. This story takes the second: Forge is an authoring tool, the order is the author's, and a tool that silently rearranges your file the first time you use it has damaged something you cared about. It also generalises — Epic 12 lets you *reorder* components in the UI, which needs order as data anyway.

The nil-slice trap is smaller but real. `EntityType.OptionalComponents` has no `omitempty`, so an entity type that omits the key round-trips to `"optionalComponents": null`. This repo's file writes `[]` explicitly so it does not bite today; it bites the first schema that does not.

## Acceptance Criteria

- [ ] `schema.Marshal(DatabaseSchema) ([]byte, error)` writes `schema.json` in **authored order**: components, the properties within each component, and entity types
- [ ] Order is recorded by `LoadSchema` and carried on the parsed value; a schema built in code with no recorded order emits deterministically (sorted), never randomly
- [ ] Items not present in the recorded order — a component added through the UI — are appended in a deterministic position, not interleaved arbitrarily
- [ ] **Byte-stable round-trip**: `Marshal(LoadSchema(f)) == f` for this repo's `schema.json`, asserted in a test against the real file
- [ ] A nil `optionalComponents`/`requiredComponents` emits `[]`, never `null`
- [ ] Output formatting matches the authored file: 2-space indent, trailing newline
- [ ] `Marshal` output is always re-loadable: `LoadSchema(Marshal(s))` succeeds and equals `s` for every fixture, including edge shapes (array components, entity-ref, a component with `behavior`)
- [ ] Adding, removing and renaming one component produces a diff touching only that component
- [ ] `go test ./...` passes

## Playwright steps

None. This story is pure serialisation with no browser surface — it is called by
Story 4's save path, which Epic 12 puts a button on. The round-trip guarantee is
a Go-level property and is tested as one.

Recorded here rather than omitted so the absence is a decision rather than an
oversight.

## Notes

- **The round-trip test must run against the real `schema.json`,** not only fixtures. It is the file that actually has to survive, and a fixture written to suit the writer proves nothing.
- Order tracking is additive to `schema.DatabaseSchema` and `schema.Component`. The interpreter ignores the new fields; nothing about loading changes.
- Prefer `json.Encoder` with `SetEscapeHTML(false)`. The default escapes `<`, `>` and `&` into `<` and friends, which would corrupt nothing but would make the diff noisy for any string containing them.
- Do not sort *properties* alphabetically as a fallback and *components* by recorded order — one rule, applied the same way at every level, or the file becomes unpredictable.
- Marshal is not validation. It writes what it is given; `ValidateSchema` is a separate call the save path makes first. Writing an invalid schema on request is correct behaviour — the user may be halfway through an edit.
