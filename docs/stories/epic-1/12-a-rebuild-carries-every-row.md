# Story 12: A rebuild carries every row, and nullability is part of a column's shape

**Epic:** 1 — Schema-driven data foundation
**Status:** ✅ Complete
**Priority:** High — one schema edit still makes a database refuse to open, and
Story 11 removed the last thing that made it hard to reach

**Depends on:** Story 9 (which made one property kind nullable), Story 11 (which
made a rebuild happen without a version bump)

## Context

### The wedge

An `entity-ref` property is the one column the generator declares nullable, for
the reason Story 9 records: a reference is the whole of a `Carrier` and only part
of a `Holder`, so "no owner yet" is a state a Holder can legitimately be in — and
it is the state every existing row is in the moment somebody adds the property,
because `ALTER TABLE ADD COLUMN` has no value to backfill with.

Retype that property to anything else and it becomes `NOT NULL`. The rebuild's
copy then reads NULLs into a column that refuses them:

```
open 0 with the retyped schema: err = migration failed: rebuild_table "holder" — statement 2/4
open 1 with the retyped schema: err = migration failed: rebuild_table "holder" — statement 2/4
open 2 with the retyped schema: err = migration failed: rebuild_table "holder" — statement 2/4
open with the ORIGINAL schema again: err = <nil>
```

Nothing is lost — the migration rolls back — but `NewSQLiteStore` returns the
error, so **every subsequent open fails the same way**, and the only way out is to
edit `schema.json` back to what the database already has. Story 10 fixed exactly
this shape of defect for a different cause; this is the one it did not reach.

### Why it is indefensible rather than merely unfortunate

The engine already invents a value for this situation. Adding a `NOT NULL`
property emits `ALTER TABLE … ADD COLUMN hp INTEGER NOT NULL DEFAULT 0`, and
every existing row silently gets `0`. So "this column now has to hold a value and
these rows do not have one" has an established answer, and the rebuild path
answers the same question by refusing to open the database ever again. One of
those two is wrong, and it is not the one that has been shipping since Epic 1.

### The other half: nullability is not diffed

Story 11 taught the diff to compare foreign keys. It did not teach it to compare
`NOT NULL`, and `ToDiffSchema` still drops the flag. So a column that ought to be
`NOT NULL` and is not stays that way indefinitely:

```
hp is nullable and the file says integer: plan empty=true changes=[]
```

That is only reachable by hand-editing today. It matters because it is the same
gap Story 11 closed for constraints, left half-open — and because a nullable
column is exactly the thing that makes some *later* rebuild fail.

The two halves belong in one story: diffing nullability without fixing the copy
would mean repairing a nullable column by wedging the database.

### What SQLite reports

Measured on the tables the generator builds, so the comparison is against real
values:

```
comp_position.entity_id        INTEGER notnull=0 default=""
comp_position.x                INTEGER notnull=1 default=""
comp_carrier.target_entity_id  INTEGER notnull=1 default=""
comp_holder.hp                 INTEGER notnull=1 default=""
comp_holder.owner              INTEGER notnull=0 default=""
```

Two things follow. `owner` — the entity-ref property — is the only nullable data
column, which is the whole blast radius: it is the only way the engine can
produce a NULL. And **`entity_id` reports `notnull=0`** even though it is the
primary key, because SQLite says that of every `INTEGER PRIMARY KEY` (it is a
rowid alias and assigns itself). The flag says nothing there, so the primary key
is left out of the comparison rather than having the quirk encoded into an
expectation.

## Acceptance Criteria

- [x] Retyping an entity-ref property to a non-nullable type migrates, and the
      rows come across
- [x] The value substituted is the same one `ALTER TABLE ADD COLUMN` uses for
      that type, so adding a property and retyping one agree
- [x] It is loud: the log says which column, which value, and how many rows
- [x] The substitution is only emitted where a NULL can actually be there, so an
      ordinary rebuild's SQL is unchanged
- [x] Introspection reads `notnull`, and `ToDiffSchema` carries it
- [x] A column whose nullability differs from what the generator would emit is a
      change, answered with a rebuild
- [x] A database that already matches produces no changes, so opening one twice
      does not rebuild every table
- [x] There is one statement of which columns are nullable, and the generator and
      the diff cannot disagree about it
- [x] `go test ./...` passes

## Design

### One statement of nullability

The same shape as Story 11's `ColumnReferences` and Story 10's `StorageLayout` —
the rule lives in `schema`, because that is where the diff is, and `storage`
builds its DDL from it:

```go
// schema
func ColumnNullable(comp Component) map[string]bool   // lowercase column name → may be NULL
```

An entity-ref property is the only true entry. The primary key is absent from
the map rather than false, because it is not compared.

### Carrying the rows

`buildNewColumns` currently returns `[]string` of DDL fragments, and `genRebuild`
recovers each column's name with `strings.Fields(colDef)[0]`. It becomes a small
struct instead, so the generator can ask a column what its default is rather than
parse it back out of its own output:

```go
type rebuildColumn struct {
    Name    string
    DDL     string // the whole definition, as it goes into CREATE TABLE
    NotNull bool
    Default string // the DEFAULT expression, "" when there is none
}
```

The copy then reads `COALESCE(owner, 0)` in place of `owner` for a column that is
`NOT NULL` in the new table, has a default to substitute, and **is nullable in
the old one**. That last clause is what keeps ordinary rebuilds byte-identical:
without drift there is nothing to coalesce, and the SQL Forge previews does not
change.

### What is left failing, deliberately

A `NOT NULL` column whose default is `NULL` has nothing to substitute. That is
only the `target_entity_id` of an entity-ref *component*, and there is no honest
value for it — Story 9's rule is that a Carrier pointing at nothing is not a
Carrier. It keeps the Story 11 hint rather than inventing an entity id.

**This was written as "the engine cannot produce such a row, so it needs a
hand-edited database to reach", and that was wrong.** See *As Implemented*.

## As Implemented

Built as designed, with one claim overturned and one acceptance criterion that
was not actually met.

**The remaining wedge was reachable in one save, with no SQL editor.** The story
said `target_entity_id` could only hold NULL in a hand-edited database, because
a component of type entity-ref declares that column `NOT NULL`. But a component's
*type* can change, and `canBecome` deliberately allows object → entity-ref when
the object's single data column is already called `target_entity_id`. So:

```
Link: {type: object, properties: {target_entity_id: {type: entity-ref}}}
   →  Link: {type: entity-ref}
```

Both schemas pass `ValidateSchema`. The property form is nullable, so the engine
writes a NULL for any entity created without a value for it — through
`CreateEntity`, no hand-editing — and the retype then rebuilds:

```
rows the engine wrote with target_entity_id NULL: 1
open 0: NOT NULL constraint failed: comp_link_new.target_entity_id
open 1: NOT NULL constraint failed: comp_link_new.target_entity_id
```

`identifier.go` had the wrong reason written down: "value and target_entity_id
belong to component kinds that have no properties, so they cannot collide". True
of any one component, not true over time. `target_entity_id` is now reserved as a
property name, so this is a schema that will not load — naming the property — in
place of a database that will not open. **`value` is deliberately not reserved**:
an object whose one property is called `value` is the shape Forge gives every new
component, both forms refuse NULL, and retyping between them copies its rows
across.

**"One statement of which columns are nullable" was not met.** The rule was
written out independently in five places — `ColumnNullable`, the CREATE TABLE
builder, the ALTER builder, the rebuild's column list, and the diff — and
nothing derived any of them from the others, unlike Story 11's
`entityRefReference`. Measured: making the rebuild's `NotNull` flag contradict
the DDL it emits for the same column produced *zero* test failures, and was
invisible only because `copyExpression`'s second guard masked the first. There is
now a `schema.PropertyNullable` that all four DDL sites read, and a test that
enumerates every property type and asserts each column's flag against its own
`NOT NULL`.

**The hint diagnosed the cause this story removed.** Its text still said "this is
what retyping an entity-ref property does" — the one thing that now works. The
test asserted only that a hint existed.

Two more assertions that could not fail: `nullabilityReason`'s "has to accept
NULL" branch was never checked, though a pre-Story-9 database emits it for real;
and `"1 row"` is a substring of the ungrammatical `"1 rows are"` that dropping the
singular produces, so the plural branch was the only one under test.

The review found nothing in the two categories most worth worrying about. No
schema or as-built database makes `COALESCE` land in the wrong column or
overwrite a real value — including the case where a column is added and the table
rebuilt in one migration, where `g.domain` is the pre-ALTER snapshot: every
column an ALTER adds is either `NOT NULL DEFAULT <value>` (no NULLs to find) or
an entity-ref (nullable in the new table too). And no table the generator builds
rebuilds twice.

## Notes

- **Defaults are still not diffed**: `string` → `array` leaves `DEFAULT ''` where
  a fresh array component gets `DEFAULT '[]'`, and a rebuild strips the `DEFAULT`
  an `ALTER TABLE ADD COLUMN` had to give a column. Nothing reads them —
  `insertObjectComponent` names every column and binds `nil` for absent values —
  so this is cosmetic until something starts relying on a default.
