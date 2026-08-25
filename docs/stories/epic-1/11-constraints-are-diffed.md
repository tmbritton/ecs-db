# Story 11: A database is repaired when the generator changes, not only when the version does

**Epic:** 1 — Schema-driven data foundation
**Status:** ✅ Complete
**Priority:** High — Story 9 changed the DDL the engine emits and no existing database gets it

**Depends on:** Story 9 (which is what changed), Story 10 (a rebuild can now
find the column it copies)

## Context

Story 9 put `ON DELETE CASCADE` on every reference to an entity. Story 10 made a
shape change rebuild rather than fail. Both changed **what the generator emits
for the same schema file** — and a database built before either change keeps
what it was built with, because there are two gates between "the generator
changed" and "the table is rebuilt", and a constraint passes through neither.

### Gate 1 — versions match, so nothing is even looked at

`checkAndMigrate` (`storage/sqlite.go:200`) reads `meta.schema_version`, and
returns `nil` the moment it equals the file's. `IntrospectAll` is never called;
the diff is never computed. Upgrading the engine binary does not change
`schema.json`, so this is the everyday case:

```
gate 1, versions equal: comp_carrier = [entity_id ON DELETE CASCADE
                                        target_entity_id ON DELETE NO ACTION]
```

The same gate is why an edit saved in Forge without a version bump does
nothing, which Forge already has to say out loud
(`forge/migration/preview.go:65` — "the engine will run none of them, which is
the one case where a correct list of changes is still a misleading answer").

### Gate 2 — the diff cannot see a constraint

Introspection reads `PRAGMA table_info`, which reports name, type, default and
primary key. It never reads `pragma_foreign_key_list`, and `ToDiffSchema` drops
what it does read down to name/type/PK. So even with the version bumped, a
foreign key that changed is invisible:

```
gate 2, version bumped: 1 changes: [added_entity_type Thing]
```

— and that one change is unrelated (`EntityTypeNames` comes from
`SELECT DISTINCT entity_type FROM entities`, so an empty database reports every
type as new). Nothing about `comp_carrier` appears.

What each gate leaves behind, for a database built before Story 9:

| form | as built | as the generator now emits it | consequence |
|---|---|---|---|
| component of type `entity-ref` | `target_entity_id … REFERENCES entities(id)` | `… ON DELETE CASCADE` | deleting the target is **refused** |
| `entity-ref` property of an object | no foreign key at all | `… REFERENCES entities(id) ON DELETE CASCADE` | a **dangling reference**, silently |

The second is the one this epic set out to remove.

### What SQLite reports

Measured, so the comparison is against a real string rather than an assumed one:

```
comp_holder: from=owner            to=id   on_delete=CASCADE
comp_holder: from=entity_id        to=id   on_delete=CASCADE
comp_old:    from=target_entity_id to=id   on_delete=NO ACTION
comp_bare:   from=entity_id        to=NULL on_delete=CASCADE     -- REFERENCES entities
```

Three things follow. Rows come back in reverse declaration order, so they must
be keyed by the `from` column rather than zipped against `table_info`. An
absent `ON DELETE` clause reads as `NO ACTION` rather than as nothing, so
"no clause" and "no foreign key" are different answers and only the second is an
empty row set. And `to` is NULL for `REFERENCES entities` without a column,
which no generated table has but a hand-edited one might.

## Acceptance Criteria

- [x] Introspection reads foreign keys, and `ToDiffSchema` carries them
- [x] A foreign key that differs from what the generator would emit is a change,
      and the generator answers it with a table rebuild
- [x] Both forms are repaired: the `entity-ref` component's `target_entity_id`
      and an `entity-ref` property of an object component
- [x] `entity_id` is checked too — it is a reference like any other, and the
      diff skips primary-key columns everywhere else
- [x] A database that already matches produces no changes, so opening one twice
      does not rebuild every table
- [x] The version gate no longer hides drift: a migration runs when there is
      something to run, whether or not `schemaVersion` moved
- [x] One rebuild per component, not one per change that wants one
- [x] There is one statement of what foreign key a reference carries, and the
      generator and the diff cannot disagree about it
- [x] `TestMigration_DoesNotNoticeAConstraintThatOnlyTheGeneratorChanged` is
      gone, replaced by its opposite
- [x] `go test ./...` passes

## Design

### One statement of the reference

`storage.entityRefReference` is the only place the engine says what a reference
to an entity means, and the diff lives in `schema`, which cannot import
`storage`. Same shape as Story 10's `StorageLayout`: the rule moves to `schema`
and `storage` derives its DDL fragment from it, with a test deriving one from
the other so they cannot drift.

```go
// schema
const EntityReference = "entities(id) ON DELETE CASCADE"
func ColumnReference(propertyType string) string   // "" for anything but entity-ref

// storage
const entityRefReference = "REFERENCES " + schema.EntityReference
```

The constant is written in the form `pragma_foreign_key_list` reports, because
that is the form the comparison happens in.

### The change

A constraint change is not a type change — `OldType` and `NewType` would be the
same INTEGER — so it gets its own kind rather than borrowing one that would
describe it wrongly:

```go
ChangeChangedConstraint ChangeKind = "changed_constraint"   // phase 2
OldRef, NewRef string                                        // on Change
```

The generator routes it to `genRebuild`, which is already the answer to every
"this column has to be declared differently" change.

### One rebuild per component

`genRebuild` ignores the change it was given: it builds the new table entirely
from the file schema, so two rebuild-causing changes on one component produce
two identical four-statement sequences. That is a pre-existing wart — it happens
to work, because the second rebuild copies from a table that is already the new
shape — and this story would make it common, since a constraint change and a
property change on the same component are exactly what an old database has. The
generator collapses them to the first.

### The version gate

`checkAndMigrate` keeps its `checkSchemaVersion` call, for the clear errors it
gives on a corrupt or missing `meta` row, but stops returning early on a match.
The runner splits into `Plan` and `Apply` so the backup can happen between them
— today it happens on a version mismatch, and it needs to happen when there is
something to apply:

```go
func (r *MigrationRunner) Plan() (*MigrationPlan, error)
func (r *MigrationRunner) Apply(*MigrationPlan) error
func (r *MigrationRunner) Run() error              // Plan + Apply, unchanged
```

A plan is empty when it has no statements **and** no version to move. Not "no
changes": an entity-less database reports every entity type as added, and those
produce no DDL, so a changes-based test would back up and migrate on every open.

Forge's `Preview.WillMigrate` encodes gate 1 and has to follow — which makes it
say something better than it does now, since an edit saved without a version
bump will actually be applied.

## As Implemented

Built as designed. What the design did not anticipate, all found by the review:

**A neighbouring table made the migration repeat forever.** `ListComponentTables`
matched `name LIKE 'comp_%'` with no `ESCAPE`, and in SQL `_` is a
single-character wildcard — so the pattern means "comp" plus *any* character. A
table called `compact_things` was introspected as a component, `TrimPrefix` left
its name alone because it does not begin with `comp_`, and the diff asked to drop
`comp_compact_things`, which does not exist. The `DROP` succeeded, changed
nothing, and was asked for again:

```
open 0: plan empty=false (1 stmts)
open 1: plan empty=false (1 stmts)
open 2: plan empty=false (1 stmts)
```

Latent before this story — it fired only on a version bump, and the bump made it
converge. Opening the gate turned it into an introspection, a `VACUUM INTO` and a
migration transaction on every engine start, permanently. The pattern now escapes
its underscore.

**Backups stopped being restore points.** `.bak.v{version}` was unique while
backups happened only *at* a version transition, and the old file was deleted
before writing to keep it so. Migrating at a constant version made the slot
reusable, and the second repair overwrote what the first had saved:

```
backups after repair 1: [w.sqlite.bak.v1]  carrier: target_entity_id ON DELETE NO ACTION
backups after repair 2: [w.sqlite.bak.v1]  carrier: target_entity_id ON DELETE CASCADE
```

`BackupRetention` could not help: it counted version-named files and there was
only ever one. Names now carry a UTC timestamp, and pruning sorts by version as a
number and then by stamp — a lexical sort of the whole name puts `v10` before
`v9`, and this function deletes the oldest.

**The phase is load-bearing in one direction only.** Moving
`ChangeChangedConstraint` to phase 3 is unobservable, as the story assumed:
`genRebuild` ignores the change it is handed and `rebuildReasons` is computed
over all changes first, so phases 2 and 3 emit identical statements. Phase *1* is
not. A component that both gained a property and drifted a constraint would emit
the rebuild before the `ALTER TABLE ADD COLUMN`, and the copy would read a column
that is not there yet — Story 10's failure by another route, and the ordinary
shape of an upgrade. Now a test.

**A column can carry more than one foreign key.** Keeping whichever arrived last
made a column with the canonical key *plus* a stray one read as correct and never
get repaired. All of a column's keys are joined, sorted, so two keys match no
expectation and the table is rebuilt into the one key it should have.

**Two of the reasons were wrong.** `referenceReason` described any foreign key
pointing somewhere other than `entities` as "no longer a reference to an entity"
— a sentence about something that never was. And no reason named its column, so
three columns gaining a key deduplicated into one line naming none.

**`Preview.WillMigrate` had no callers left.** Following the version gate through
Forge deleted both of the templates that branched on it, leaving an exported
method whose only exercise was its own tests. It is gone; the panel already
answers the same question with an empty statement list.

Also: two Go assertions on `data-testid="migration-inert"` outlived the element
they name and could no longer fail; they are statement counts now.

## Notes

Not in this story, and worth writing down:

- **Nullability is still not diffed**, and the version gate was the last thing
  standing between an ordinary save and the wedge. `ToDiffSchema` drops `notnull`,
  so a column that should be `NOT NULL` and is not stays that way — and retyping
  an entity-ref property, the one kind that may be NULL, to anything else makes
  the rebuild's `INSERT … SELECT` fail on the existing NULLs. The migration rolls
  back, so nothing is lost, but `NewSQLiteStore` returns that error on every
  subsequent open and only editing `schema.json` back can clear it. Until now
  that needed a `schemaVersion` bump to reach; it is now one save away. What it
  gets here is a hint naming the component and the file to edit
  (`migrationHint`); the fix is for the copy to substitute the column's default,
  which is its own story.
- **Defaults are not diffed** either; `string` → `array` leaves `DEFAULT ''`
  where a fresh array component gets `DEFAULT '[]'`. Relatedly, a rebuild strips
  the `DEFAULT` an `ALTER TABLE ADD COLUMN` had to give a column. Nothing depends
  on it — `insertObjectComponent` names every column and binds `nil` for absent
  values, so the default was never consulted — but this story makes it happen to
  every existing database at once.
- **`MigrationConfirm` has changed meaning.** It blocked a version-bump migration
  that would destroy data; it now blocks every open of any database the engine
  wants to repair, and a repair is the expected state of the whole installed base
  on first upgrade. Nothing in the engine sets it — the default is
  `MigrationAuto` — so this matters only if Forge ever runs a migration itself.
  Worth deciding then whether a constraint rebuild, which copies every row and
  loses nothing, should count as destructive.
