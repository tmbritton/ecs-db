# Story 2: `schema.Marshal` — a writer that produces clean diffs

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** ✅ Complete  
**Priority:** High — Epic 12 cannot save anything without it

**Depends on:** Story 1

## Context

`schema.json` is a file a human wrote and a human reads. It is in version control, it is reviewed in diffs, and its ordering carries intent: `Position, Health, Sprite` are grouped because they belong together, not because P precedes H.

Go maps have no order. `json.Marshal(DatabaseSchema)` emits keys alphabetically, so the first time Forge saves a file it never meant to reorder, every component moves and the diff is the whole file. Verified against this repo: authored order is `Position, Health, Sprite, Tile, Path, Speed, GoblinStats`; Go emits `GoblinStats, Health, Path, Position, Speed, Sprite, Tile`. The same happens to the properties inside each component, which are also a map.

There are two honest options. Emit a fixed order and accept a one-time reordering of the checked-in file; or record the authored order at load time and honour it on save. This story takes the second: Forge is an authoring tool, the order is the author's, and a tool that silently rearranges your file the first time you use it has damaged something you cared about. It also generalises — Epic 12 lets you *reorder* components in the UI, which needs order as data anyway.

The nil-slice trap is smaller but real. `EntityType.OptionalComponents` has no `omitempty`, so an entity type that omits the key round-trips to `"optionalComponents": null`. This repo's file writes `[]` explicitly so it does not bite today; it bites the first schema that does not.

## Acceptance Criteria

- [x] `schema.Marshal(DatabaseSchema) ([]byte, error)` writes `schema.json` in **authored order**: components, the properties within each component, and entity types
- [x] Order is recorded by `LoadSchema` and carried on the parsed value; a schema built in code with no recorded order emits deterministically (sorted), never randomly
- [x] Items not present in the recorded order — a component added through the UI — are appended in a deterministic position, not interleaved arbitrarily
- [x] **Byte-stable round-trip**: `Marshal(LoadSchema(f)) == f` for this repo's `schema.json`, asserted in a test against the real file
- [x] A nil `optionalComponents`/`requiredComponents` emits `[]`, never `null`
- [x] Output formatting matches the authored file: 2-space indent, trailing newline
- [x] `Marshal` output is always re-loadable: `LoadSchema(Marshal(s))` succeeds and equals `s` for every fixture, including edge shapes (array components, entity-ref, a component with `behavior`)
- [x] Adding, removing and renaming one component produces a diff touching only that component
- [x] `go test ./...` passes

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

## As Implemented

`schema.Marshal` in `internal/schema/marshal.go`, with order recorded by `LoadSchema` onto `DatabaseSchema.ComponentOrder`, `DatabaseSchema.EntityTypeOrder` and `Component.PropertyOrder` (all `json:"-"` — they are not part of the file format). `orderedKeys` in `order.go` applies one rule at every level: recorded keys first, then anything unrecorded, sorted.

Coverage: `internal/schema` 91.0%.

### The formatting turned out to be reproducible

The plan assumed a choice between preserving authored order and accepting a one-time reformat, and expected the second to be needed for layout. Reading the actual file closely showed that was not so — its formatting is unusual but entirely deterministic:

- properties render inline: `{ "type": "number" }`
- **within one `properties` block, keys are padded so every value starts at the same column** — `"hp":    {` beside `"maxHp": {`, which is the longest key plus one space
- alignment applies *only* inside properties blocks; component and entity-type fields take a single space
- arrays of component names render inline: `["Position", "Health", "Sprite"]`

Every one of those is a rule, not a hand-made accident, so the writer reproduces them and the round trip is byte-stable with **no reformat of anyone's file at all**. That is a better outcome than the plan anticipated: the story's core promise is that saving does not damage a file someone arranged, and reformatting it would have been a smaller version of exactly that damage.

Confirmed the way it actually matters — load `schema.json`, marshal it, write it back over itself, and `git diff` is empty.

### Design decisions worth knowing later

- **Nested properties sort rather than preserving order.** `Property` carries no order field, so a nested object's keys are emitted sorted. Deterministic, but it is a real (small) asymmetry with top-level properties: if Epic 12 lets someone reorder nested fields, `Property` needs the same treatment `Component` got.
- **`jsonString` turns HTML escaping off.** The default would turn `<`, `>` and `&` into `\u003c` and friends — semantically identical, and unreadable in a diff.
- **A leaf property inlines; anything with nested properties or items expands.** Inlining a deep object would produce a line nobody can read.
- **Marshal is not validation.** It writes what it is given, because the caller may be halfway through an edit. `ValidateSchema` is a separate call the save path makes first.

### What the review caught

Two of these would have destroyed data the first time Epic 12 saved a file.

- **`EntityType.Behavior` was never written.** `writeComponent` emitted the component's `behavior`; `writeEntityType` did not emit the entity type's. The output was valid JSON and passed `ValidateSchema`, because the field is optional — so nothing complained. The e2e fixture binds `TestGoblin` to `e2e-wander`, so opening and saving that project would have silently unbound the goblin's state machine.
- **A component carrying both `properties` and `items` wrote a file that no longer loads.** Both were emitted from a `switch`, so the first arm won and the other value was dropped. Nothing rejects that shape on the way in — `UnmarshalJSON` checks that an object has properties and an array has items, never that the other is absent — so it can already exist in a hand-authored file. Marshalling one produced `"type": "array"` with no `items`, which `LoadSchema` then refuses. A corrupt `schema.json` on disk, and the engine will not boot.
- **`keyOrder` rejected JSON `null`, which was a regression in the engine, not just in Forge.** A component written `"properties": null` loaded and validated fine before this story and became unloadable after it — enough to stop `InitSchema`, and so the engine, from starting.

**The reason the first two shipped is the third finding, and it is the one worth remembering.** `TestMarshal_RoundTripsEveryComponentShape` loaded, marshalled, reloaded, marshalled again, and compared *the two marshalled outputs*. It never compared the reloaded schema to the input — so any field `Marshal` dropped entirely still produced output that reloaded and was idempotent, and the test passed. Three separate mutations survived it, including one against a case literally named "component with a behavior". The story's own AC said "`LoadSchema(Marshal(s))` succeeds **and equals `s`**"; the equality half was ticked but never implemented.

It now compares the schemas. A round trip does change one thing on purpose — a nil slice comes back empty, because `Marshal` never writes `null` — so the comparison normalises that explicitly rather than pretending it is not there.

### The limit of byte-stability

Entity-type fields are a fixed set written in a fixed order, so a schema that authored them differently is normalised on first save. That is deliberate and different from component order: field order within an entity type carries no intent, while `Position, Health, Sprite` sitting together does.

`e2e/fixtures/project/schema.json` was one such file and has been normalised in this commit — a five-line, semantically-null diff. Both schemas in the repo now round-trip byte-for-byte, and both are asserted.

### Verified by mutation

Every formatting and ordering rule was checked against a deliberate defect before being trusted:

| Mutation | Caught by |
|---|---|
| ignore recorded order, iterate the map | 4 tests, byte-stability included |
| sort everything alphabetically | 4 tests |
| drop the column alignment | byte-stability, diff-stability |
| emit nil slices as `null` | the nil-slice test |
| expand leaf properties instead of inlining | 3 tests |
| drop the entityType `behavior` (the shipped bug) | round-trip equality |
| drop the component `behavior` | round-trip equality |
| drop a property's `items` | round-trip equality |
| hardcode `validationLevel` | round-trip equality |
| naive string quoting, no escaping | the escaping test |
| turn HTML escaping back on | the escaping test |
| reject JSON `null` in `keyOrder` | the null-sections test |

The bottom six all **survived** the original suite and were found by review.
