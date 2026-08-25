# Story 13: A rename keeps its data

**Epic:** 1 — Schema-driven data foundation
**Status:** ✅ Complete
**Priority:** High — the most ordinary edit there is silently destroys a table

**Depends on:** Story 10 (which established that the diff compares columns), and
Forge Epic 12, which puts a rename button on the screen

## Context

Renaming is not a corner case. It is what an author does when a name turns out
to be wrong, and Forge has buttons for it: `schemaedit.go` renames a component by
moving it to a new key, and a property the same way. Nothing in the engine can
tell that apart from deleting one thing and creating another, so that is what it
does.

### A component rename drops the table

```
comp_position rows before: 1
rename Position -> Placement produces: [added_component placement, removed_component position]
comp_placement rows after: 0 — 1 means the data came across
comp_position still exists: err=SQL logic error: no such table: comp_position
```

The migration reports success. Every entity's Position is gone.

### A property rename drops the column

```
rename x -> col_x produces: [added_property position.col_x, removed_property position.x]
after the rename, col_x = 0 (err=<nil>) — 42 means the data came across
```

Also silent, also reported as success. The `0` is the `DEFAULT 0` that
`ALTER TABLE ADD COLUMN` gave the new column.

### An entity type rename strands its entities

Entity types are not DDL — `entities.entity_type` is a text column — so a rename
produces no statements at all and every existing row keeps the old string. Forge
already knows: `usage.Counts.AgainstVersion` exists because "renaming a type in
the editor is the everyday case: the rows are still filed under the old name".
That comment describes a defect and treats it as weather.

### There is no way to say what you meant

A rename cannot be inferred. `x → col_x` and "delete x, add col_x" are the same
diff, and guessing wrong puts data in a column the author did not mean. So this
needs the author to be able to *say* it, and the format has nowhere to say it.

Two facts make that cheap. Unknown keys in a property are ignored rather than
rejected — `{"type":"integer","renamedFrom":"hp"}` parses today with no error —
so adding the field breaks no existing file. And SQLite does the work:

```
after RENAME COLUMN hp -> health: health = 42
foreign keys survive: [entity_id ON DELETE CASCADE  owner ON DELETE CASCADE]
NOT NULL survives: notnull=1
after renaming the reference: [entity_id ON DELETE CASCADE  keeper ON DELETE CASCADE]
after RENAME TO comp_keeper: [entity_id ON DELETE CASCADE  owner ON DELETE CASCADE]
```

`ALTER TABLE … RENAME COLUMN` and `ALTER TABLE … RENAME TO` keep the rows, the
types, the NOT NULL and the foreign keys — and a foreign key follows the column
it is on. SQLite 3.53.1 through `modernc.org/sqlite`.

## Acceptance Criteria

- [x] A component, a property and an entity type can each declare `renamedFrom`
- [x] Declaring it migrates the existing table, column or rows, keeping the data
- [x] The rename is not destructive, so `MigrationConfirm` does not block it
- [x] A rename and another change to the same thing in one migration both apply,
      in an order that works
- [x] `renamedFrom` left in the file after the migration has run is harmless, so
      an author is not required to go back and remove it
- [x] A rename nobody declared is still reported: the statements that drop a
      table or a column say what they are taking, and a drop-and-add that looks
      like a rename says so and names `renamedFrom`
- [x] `renamedFrom` survives a Forge save — the hand-written marshaller drops
      fields it does not know about
- [x] `schema.json` validates, and an unusable `renamedFrom` is refused where the
      schema is loaded
- [x] `go test ./...` passes

## Design

### Saying it

```json
"Placement": { "renamedFrom": "Position", "type": "object", "properties": {
    "col_x": { "type": "integer", "renamedFrom": "x" }
}}
```

On `Component`, `Property` and `EntityType`, keyed by the **new** name, because
that is the one the file now uses everywhere else.

### Applying it

The diff rewrites the introspected schema into the shape the renames describe,
*before* comparing anything:

```go
func applyRenames(domain *DomainSchema, file *DatabaseSchema) (*DomainSchema, []Change)
```

A renamed component's key becomes the new name; a renamed column's name becomes
the new name. Everything downstream — `canBecome`, the property diff, the
reference and nullability comparisons — then compares like for like, with no
knowledge that a rename happened. The alternative, suppressing the add/remove
pair after the fact, would leave every other comparison looking at a column it
believed was gone.

A rename is only emitted when the database actually has the old name and does not
have the new one. So running the migration twice is a no-op, and an author may
leave `renamedFrom` in the file forever.

### Ordering

Renames are phase 0, ahead of additions. A component renamed *and* given a new
property has to be renamed before the `ALTER TABLE … ADD COLUMN` names it.

### The safety net

A rename nobody declared still cannot be inferred, but it can be recognised and
said out loud. Where a component has exactly one removed and one added property
of the same SQL type, the removed property's reason says so and names
`renamedFrom`. Every dropped column and dropped table gets a reason naming what
it takes with it, on the same terms as Story 12's substitution reporting — the
migration runs under `MigrationAuto` by default, so the log is where this is
mentioned or it is not mentioned at all.

## As Implemented

Built as designed. The review found two ways this story reopened a wedge it was
supposed to be nowhere near, and one acceptance criterion that was false.

**The two halves of the migration disagreed about what things were called.**
`schema.Diff` renames a *copy* — the one `ToDiffSchema` makes — and the generator
was handed the original. So a component renamed *and* rebuilt in one migration
looked for its table under the new name in a snapshot that still held the old
one:

```
migration failed: error "keeper" — statement 2/2
underlying: ERROR: comp_keeper not found in domain schema
```

and a renamed column's copy looked for its nullable source the same way, found
nothing, emitted no `COALESCE`, and failed on the NULL — **reopening exactly the
wedge Story 12 closed**, with Story 12's hint then blaming a cause that was not
there. Both fail the migration, which the store returns from every subsequent
open.

The criterion "a rename and another change to the same thing in one migration
both apply" was ticked on the strength of a test covering an added column, which
is the one case that happens to work. `DomainSchema.withRenames` now replays the
rename changes onto the storage-side snapshot — replayed rather than recomputed,
so there is one statement of what a rename does.

**An entity type rename was skipped when the new name already had rows.** That
guard is right for a table, which cannot be renamed onto another table, and wrong
for an `UPDATE` moving rows between two type strings, which merges safely.
Measured: the migration produced zero statements, reported success, and left the
entity filed under a type the schema no longer declares — the third of the three
defects this story is about.

**A declared rename that could not be applied said nothing.** When the database
holds both names the rename is refused, correctly — and the old table was then
dropped with the generic "every row in it goes with it", so an author who wrote
`renamedFrom` watched their table go with no indication their declaration had
been disregarded. The drop now says so.

**The safety net contradicted its own comment.** It says "only when the guess is
unambiguous: exactly one column dropped and one added"; the code counted only the
added ones. Two columns dropped and one added told *each* of them it might be the
rename — two mutually exclusive suggestions, each stated as the answer, and
acting on the wrong one moves the wrong data. Both sides are counted now, for
columns and for tables.

**And `assertChanges` never compared `OldName`.** Every `OldName` in the rename
tests was decoration; setting all three to a constant left the whole schema suite
green. The storage integration tests caught it, so this was a hole in the unit
layer rather than an absence of coverage — but it is the third time a review has
found this helper silently skipping the field a test was named for.

Two smaller things: a `renamedFrom` below the top level had nowhere to act — a
nested property and an array's items live inside one JSON column — and was
accepted and ignored, which is the failure mode this whole story is about, so it
is now refused; and one dead condition (`len(fileTypes) == 0`, unreachable once
`len(added) == 1`) went the way of the two sorts.

**Not done here:** Forge's own rename buttons still move the key and leave
`RenamedFrom` empty, so the end-to-end path the Context describes is only fixed
for schemas edited by hand. Wiring `schemaedit.go` is Epic 12 work and wants the
UI question answered — whether renaming offers to record it, or always does.

## Notes

- `Marshal` is hand-written and renders a leaf property as `{ "type": "integer" }`,
  so a field it does not know about is dropped on the first Forge save. A test
  round-trips a schema carrying every field rather than only the ones today's
  `schema.json` happens to use.
