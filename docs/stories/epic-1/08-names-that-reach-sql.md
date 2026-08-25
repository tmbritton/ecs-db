# Story 8: Names that reach SQL are identifiers

**Epic:** 1 — Schema-driven data foundation
**Status:** ✅ Complete
**Priority:** High — three ways a schema silently builds the wrong database

**Depends on:** Story 2 (the validator), Story 3 (the generator)

## Context

Component and property names from `schema.json` are interpolated into DDL and
into every INSERT: a component becomes the table `comp_` + lowercase(name), a
property becomes a column named lowercase(name). `validateSQLCompatibility`
asks whether SQL can hold each property's **type**. Nothing asks whether it can
hold the **name**.

`internal/storage` has a `validateIdentifier` and a `safeIdentifier` regex for
exactly this, and uses them on the agent runtime's write path, on
`SetComponentValues` and on `DeleteEntity`. The generator that creates the
tables does not use them, and neither does `insertObjectComponent`, which is the
path every entity creation takes.

Measured, against a schema `ValidateSchema` accepts without complaint:

| property or component name | what the database actually gets |
|---|---|
| `two words` | column `two`, of type `words INTEGER` |
| `x" , "extra` | column `x`, of type `" , "` |
| components `Probe` and `probe` | **one** table for the two of them |
| `a-b`, `x) --`, `Pr"obe` | a SQLite syntax error, at bootstrap |

The first three are silent. The schema says one thing, the database is built as
another, and the mismatch surfaces later as "no such column" from somewhere with
no idea why.

This matters more now than it did when the generator was written. Forge's SCHEMA
mode is a text box that writes `schema.json`, so "a property name with a space
in it" is one keystroke, not a hand-edited file.

## Acceptance Criteria

- [x] A component or property name that cannot be a SQL identifier is refused
      where the schema is validated, naming the component and the name
- [x] Two components whose names differ only in case are refused, and so are two
      properties of one component
- [x] The refusal happens before any DDL runs, so a bad name never half-builds a
      database
- [x] `insertObjectComponent` validates the identifiers it interpolates, as its
      neighbours already do
- [x] The generator validates before it emits, so a caller that skipped
      `ValidateSchema` still cannot produce a malformed table
- [x] `NewSQLiteStore("")` is refused rather than bootstrapping a schema into a
      temporary file that is unlinked on close
- [x] `pruneBackups` finds its backups for a database path containing a glob
      metacharacter, or says why it cannot
- [x] The repo's own `schema.json` still loads
- [x] `go test ./...` passes

## Notes

- The rule has to be stated in one place and used by both ends. Two regexes that
  agree today are two regexes.
- Reserved words are a lesser problem: `PRAGMA` or `select` as a property name
  fails loudly at `CREATE TABLE` rather than silently. Worth a thought, not
  necessarily a rule — quoting identifiers would solve it and change every
  statement the generator emits.
- Entity-type names are values in `entities.entity_type`, not identifiers, so
  they are deliberately not in scope.

## As Implemented

The rule lives in `internal/schema` — one place, exported as `ValidIdentifier`
and `ValidColumnName` so Forge validates as you type rather than keeping a
second opinion — and is called from `validateSQLCompatibility`, which already
asked whether SQL could hold each property's *type*.

Two rules, not one:

- **A name must be an identifier**: `^[A-Za-z_][A-Za-z0-9_]*$`. Case is allowed
  and folded downstream, because the engine's own names are mixed-case.
- **Two names that lowercase to the same thing are one name.** That is what the
  folding costs: `Probe` and `probe` are one table, `Hp` and `hp` one column.

Plus defence in depth at both ends — `componentTableSQL` refuses before it
emits, and `insertComponent` refuses before it interpolates, table name and
field names alike.

### What the review turned up, and why the rule grew

The story's own notes ranked reserved words as "worth a thought, not necessarily
a rule". The review found the thought was load-bearing, because one family of
them fails the same way `two words` does — silently:

> `current_time`, `current_date` and `current_timestamp` are perfectly good
> identifiers. The column builds, the INSERT succeeds, and every read comes back
> as the clock, because SQLite resolves `CURRENT_TIME` as a keyword expression
> before it resolves a column of that name. Stored 42, read `23:54:37`.

So the rule gained a third part: **a set of names that are identifiers and still
cannot be columns.** It is not a blanket ban on SQLite's keywords, because 84 of
its 147 are perfectly good column names — `action`, `key`, `first`, `last`,
`row`, `match`, `range` — and refusing those would refuse names a game schema
wants. The list is the 60 SQLite actually rejects, plus those three, plus
`entity_id`, which is not a keyword at all but is the primary key every
component table already has.

The list was **derived from SQLite rather than guessed at**, and
`TestReservedColumnNames_MatchesWhatSQLiteActuallyRefuses` re-derives it on
every run: it builds a table, writes 42 and reads it back for each of the 147
keywords, and fails if the schema package's answer differs in either direction.
A SQLite upgrade that changes the answer fails a test rather than somebody's map.

### The one that was not about names

`bootstrapDatabase` created `meta` **outside** its transaction, under a comment
saying it was "so that `tablesExist` works after partial failure" — and a
parenthetical claiming "DDL auto-commits in SQLite", which is false. But
`tablesExist` asks whether `meta` exists, and that is precisely how the store
decides between building a database and migrating one. A bootstrap that failed
left behind the single table that makes the next open take the **migration**
path, against a database with no `entities` table and no recorded version. The
ordering was not detecting the half-built state; it was creating it. Bootstrap is
one transaction now, and a failure rolls back to nothing.

### Two smaller ones

`NewSQLiteStore("")` is refused. SQLite opens `""` as a private temporary
database that is unlinked on close, so it used to bootstrap a complete schema
into a file that then evaporated — and `config.Defaults` hands out an empty path
when a `game.toml` omits `[database]`.

`pruneBackups` escapes the database path before using it as a glob, and cleans
it first. Both halves were broken: a directory named `brack[et]dir` matched
nothing, and a path of `./ecs.db` — again, the default — matched four files and
then failed to parse a version from any of them, because `filepath.Glob` returns
cleaned paths and the prefix being trimmed was raw. Both produced the same
symptom, which is the one this function exists to prevent: backups accumulate
and nothing says so.

### Where a problem is shown, which the change nearly broke

`internal/forge/validation` narrows a schema error to the component or entity
type that caused it, and its correctness argument rests on a stated invariant:
*SQL compatibility looks at one component at a time*. The new collision rule is
the first one that needs **two** components to fail, so no single-component
carrier reproduced it and every entity type's carrier did — which put a red
blocking error about components on every entity type in the file, the exact
failure that narrowing exists to prevent. A carrier of all components and no
real entity type now sits between the two loops, and the collision is reported
unattached, because the message names both and picking one to blame would be
arbitrary.

### Verification

- 33/33 mutations caught, 0 survived.
- `go test ./...` clean, `-race` clean, `golangci-lint` clean.
- Coverage: `schema` 91.8%, `storage` 88.4%.
- The repo's `schema.json` and the e2e fixture's both still load, and the full
  Forge e2e suite passes: 192 tests.

### Left for later

- **`Generate` interpolates names into ALTER TABLE and DROP TABLE unchecked**,
  unlike `componentTableSQL`. Reachable only from a caller that skipped
  validation. Closing it means `Generate` returning an error, which is a
  signature every caller uses.
- **The emitted DDL quotes no identifiers.** Quoting would make the reserved-word
  list unnecessary and let a schema use any name at all; it would also change
  every statement the generator emits and the preview Forge renders from it.
  A real alternative to the rule above, not a refinement of it.
- **ASCII only**, which is stricter than SQL — SQLite would take `Größe`. Stated
  as a choice in the code: Unicode case folding is not the ASCII lowercasing the
  generator does, so the collision check would be answering a different question
  from the one the DDL asks.
- `isMemoryDB`'s empty-path branch is now unreachable from the store.
