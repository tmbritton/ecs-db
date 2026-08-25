# Story 9: An entity-ref does not outlive its target

**Epic:** 1 — Schema-driven data foundation
**Status:** ✅ Complete
**Priority:** High — the last restrict in the schema, and an FK that is missing entirely

**Depends on:** Story 7, which is what made foreign keys real

## Context

Story 7 turned `PRAGMA foreign_keys` on for every connection, and that made a
declaration nobody had thought about start doing something. `target_entity_id`
is declared `REFERENCES entities(id)` with **no `ON DELETE` clause**, which
SQLite treats as a restrict:

```
comp_carrier: target_entity_id -> entities.id  ON DELETE NO ACTION
```

So deleting an entity that another entity points at is refused. Before Story 7
it silently succeeded and left a reference to an entity that no longer existed,
which is worse — but a refusal is not a decision either, and this is the one
place in the schema where deleting an entity can fail because of some *other*
entity's data.

The decision taken is to **cascade the component row**: deleting the target
removes the pointing component from the holder, and the holder itself survives.
It matches what `comp_*.entity_id` already does, and it means a goblin whose
Target dies simply loses its Target and re-acquires on the next tick.

### And a second thing, found while measuring the first

`entity-ref` is two things in this schema: a component whose **type** is
entity-ref, and a **property** of an object component. Only the first gets a
foreign key when its table is created:

| | as built by `CREATE TABLE` |
|---|---|
| component of type entity-ref | `target_entity_id -> entities.id ON DELETE NO ACTION` |
| entity-ref property of an object component | **no foreign key at all** |

The property path emits `REFERENCES entities(id)` only from `ALTER TABLE ADD
COLUMN` — so the same schema produces different constraints depending on whether
the component was there from the start or added later, and a table rebuild
quietly drops the constraint an ALTER had added. The two paths disagree about
`NOT NULL` as well.

## Acceptance Criteria

- [x] Deleting an entity that another entity's entity-ref points at succeeds
- [x] The pointing component is gone; the entity that held it is not
- [x] An entity-ref property of an object component carries the same foreign key
      as a component of type entity-ref, whether the table was created, altered
      or rebuilt
- [x] The three paths agree about `NOT NULL` — nullable, not NOT NULL; see below
- [x] `DeleteEntity` no longer carries an explanation for a refusal this version
      cannot produce — and says plainly when an older database does
- [x] What an existing database does — and does not — get is stated
- [x] `go test ./...` passes

## Notes

- Applying "cascade the component row" to a *property* is the literal reading of
  the decision and deletes a component that may hold unrelated data: a `Holder`
  with `owner` and `hp` loses both. The alternative is `SET NULL`, which needs
  the column nullable and makes every reader handle a null target — the option
  that was explicitly not chosen at the component level. One rule for entity-ref
  is worth more than a second rule that contradicts the first.
- **This changes generated DDL without changing any schema.** `schema.Diff`
  compares schemas, so an unchanged `schema.json` produces no migration and an
  existing database keeps the old constraint. Worth stating plainly rather than
  implying a migration that does not exist.

## As Implemented

One constant, `entityRefReference`, says what a reference to an entity means,
and the create, ALTER and rebuild paths all compose from it:

```
REFERENCES entities(id) ON DELETE CASCADE
```

The target goes, and the component that pointed at it goes with it. The entity
holding that component does not.

### What the review changed about the answer

The story planned `NOT NULL` everywhere, on the reasoning that a reference
should point somewhere. The review found that this wedges a database, and the
sequence is not exotic:

1. Add an entity-ref property to a component that already has rows. `ALTER TABLE
   ADD COLUMN` has nothing to backfill with, so the column is nullable and the
   existing row's value is NULL — which is what the code already did, and what
   `TestSmoke_AddEntityRefProperty_RoundTrip` has always asserted.
2. Later, change any *other* property of that component. That forces a rebuild,
   and a rebuild declaring the reference `NOT NULL` fails its `INSERT...SELECT`
   on the row step 1 left.
3. `NewSQLiteStore` returns that error, so **every subsequent open fails the
   same way**, and nothing can repair it: `IntrospectComponentTable` does not
   read `notnull`, so the diff can never see what happened.

So the existing behaviour was right and the new `NOT NULL` was the mistake. An
entity-ref **property** is nullable in all three paths, because a reference is
the whole of a `Carrier` and only part of a `Holder` — "no owner yet" is a state
a Holder can legitimately be in, and it is the state every existing row is in
the moment somebody adds the property. A component whose **type** is a reference
keeps `NOT NULL`: the row is nothing else.

That is not a retreat from the decision. The decision was about what a *delete*
does, and with CASCADE a delete never produces a NULL — it produces no row.

### The second defect, which the first one uncovered

`entity-ref` is two things, and only one of them used to get a foreign key when
its table was created:

| | before | now |
|---|---|---|
| component of type entity-ref | `NO ACTION` | `CASCADE` |
| entity-ref property of an object component | **no foreign key at all** | `CASCADE` |

The property form got a constraint only from `ALTER TABLE ADD COLUMN`, so the
same schema produced a constraint or none depending on whether the component
existed from the start — and a rebuild silently dropped what an ALTER had added.

### What went, and what came back

`explainDelete`/`referencesTo` — added in Story 7 to turn `FOREIGN KEY
constraint failed (787)` into a sentence naming the holder — are gone, because
no foreign key this version emits restricts. The review pointed out that the
refusal is still reachable on a database built *before* this change, where the
message would now be bare, so `DeleteEntity` keeps one sentence for that case:
it says something still references the entity and that the database predates the
cascade. Five lines rather than fifty, and it names the actual cause.

### What an existing database gets

Nothing. This changed generated DDL without changing any schema, and the
migration runner decides by diffing schemas — constraints are not in the shape
it introspects, so a migration for some other reason runs straight past. A
database built before this keeps `NO ACTION` on the component form and **no
foreign key at all** on the property form, which is the worse half: a silent
dangling reference, the behaviour this epic set out to remove.

Nothing is broken by it today, because no shipped schema declares an entity-ref
in either form, so no database has such a column — and the project owner has
said there is no data worth preserving. It is pinned as a test that documents
the gap, runs a real version-bumping migration to prove the gap is real rather
than an artifact of an early return, and names what would close it: foreign-key
introspection and a rule turning a constraint mismatch into a rebuild.

### Verification

- 16/16 mutations caught, 0 survived.
- `go test ./...` clean including `-count=2`, `-race` clean, lint clean.
- `storage` coverage 88.4%; `schema.json` still validates; 192 e2e tests pass.

Three of those mutations only started failing after the tests were fixed to
discriminate. `TestGenRebuild_AnEntityRefComponentIsRebuiltWithTheCascade`
asserted that `ON DELETE CASCADE` appeared *somewhere* in the CREATE — which the
`entity_id` column has always satisfied, so it passed whatever the reference
column said. The migration tests counted columns to prove which path had run,
which a rebuild and a drop-and-recreate both satisfy; they now assert a
surviving row and a recorded default respectively.

### Left for later

- **A retyped scalar component reaching a rebuild fails on the column rename.**
  `string` → `entity-ref` builds the new table correctly — with the cascade —
  and then its `INSERT...SELECT` looks for `target_entity_id` in a table whose
  column is called `value`. Pre-existing, and orthogonal to this story.
- **Foreign-key introspection**, which is what would close the migration gap
  above and let the generator's constraints be diffed like its columns are.
- **A machine can lose a component underneath it.** The holder keeps its row and
  its interpreter state and loses a component mid-tick, which the restrict made
  impossible. Intended — it is the whole point of cascading — but it is a state
  no machine has had to survive before.
