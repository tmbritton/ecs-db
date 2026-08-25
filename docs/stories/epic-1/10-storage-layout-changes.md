# Story 10: A component that changes shape is rebuilt, not patched

**Epic:** 1 — Schema-driven data foundation
**Status:** ✅ Complete
**Priority:** High — one schema edit wedges the database it is edited against

**Depends on:** Story 9, which is where this surfaced

## Context

A component's type decides the **shape of its table**, and there are three
shapes rather than two:

| component type | data columns |
|---|---|
| `object` | one per property |
| `entity-ref` | `target_entity_id` |
| `string`, `integer`, `number`, `boolean`, `array` | `value` |

The diff only knows two. It asks `dbIsObject != fileIsObject`
(`schema/diff.go:161`) and treats any change across that line as remove-and-add
— which is right, and is why `object` ↔ `string` works. Everything on the
non-object side is then handed to `diffScalarComponent`, whose comment says
"for scalar components there's exactly one data column (named `value`)".

For `entity-ref` that column is not called `value`. So changing a component
between `entity-ref` and any other scalar type is reported as a *property type
change*, which the generator answers with a table rebuild, whose copy step reads
a column that is not there:

```
scalar: string -> entity-ref   MIGRATION FAILED: rebuild_table "x" — statement 2/4
  INSERT INTO comp_x_new (entity_id, target_entity_id)
    SELECT entity_id, target_entity_id FROM comp_x
  no such column: target_entity_id
entity-ref -> scalar            MIGRATION FAILED: the same, for value
```

The migration rolls back, so no data is lost — and `NewSQLiteStore` returns the
error, so **every subsequent open fails the same way**. The only way out is to
edit `schema.json` back to what the database already has.

Measured across every component change the diff can produce, these two are the
only failures: object property retyped, object property removed, property
renamed, `string` → `integer`, `string` → `array` and both directions of
`object` ↔ scalar all migrate and keep their rows.

## Acceptance Criteria

- [x] Changing a component between `entity-ref` and another scalar type migrates
- [x] It is treated as the shape change it is: the old table goes and a new one
      is created — in the direction where that is unavoidable; see below, the
      other direction turned out not to need it
- [x] The statements that lose data are marked destructive, so
      `MigrationConfirm` can refuse them
- [x] A change *within* a shape still rebuilds and still keeps its rows
- [x] There is one statement of what shape each component type has, and the
      generator and the diff cannot disagree about it
- [x] `go test ./...` passes

## Notes

- The generator implements the layout and the diff has to predict it. Those are
  two places to say the same thing, which is the drift Stories 8 and 9 both had
  to close; a test that derives one from the other is cheaper than a rule.
- Data is lost when a component changes shape, and that is inherent — there is
  no conversion from a `value` to an entity id. What matters is that it is
  marked destructive rather than silent, so the confirm policy sees it.

## As Implemented

`schema.StorageLayout` names the three shapes, and the diff asks whether the
columns a table already has can be altered into the ones the file wants.

### The question the diff was asking was the wrong one

The plan was to compare the db's layout against the file's. The review found
that this is not answerable: the database has no record of a component's
declared type. `storage.InferComponentType` guesses it from the columns, and an
object component whose single property happens to be called `value` builds a
table indistinguishable from a `string` component's. Measured, with the schema
**unchanged** and only the version bumped:

```
inferred type of an object{value:number} table = "number"
diff of the same schema against itself = [added_component, removed_component]
rows surviving: 0
```

That is the shape Forge gives every new object component
(`schemaedit.go:299`). Create a component in the editor, put data in it, bump
the version for any unrelated reason, and the table is dropped. It predates this
story — the old `dbIsObject != fileIsObject` did the same — and the fix does not
work unless it is closed.

So the comparison is about columns, which the database does record: **can the
columns that are there be altered into the ones this layout needs?** An object's
can be reached from anything, because properties are added, dropped and retyped
one at a time and additions are ordered before rebuilds. A fixed-column layout's
cannot be reached at all, because no ALTER renames a column.

That also made one direction less destructive than planned. A scalar becoming an
object now keeps its table: the new properties are added, the old column is
dropped by a rebuild, and the rows survive — so the entities keep the component,
where before they lost it. The reverse is still a drop, and cannot not be.

### What else the review found

- **A statement the generator could not build was committed around.** A change
  the generator refuses becomes `Statement{Kind: "error", SQL: ""}`, and
  `tx.Exec("")` succeeds — so a migration whose CREATE was refused ran the
  matching DROP, committed, and reported success. The half a broken change can
  still express is the dangerous half. The runner now refuses to execute an
  error statement.
- **Destructive statements were logged at the same level as everything else.**
  Under `MigrationAuto` — the default, and what the engine runs — that log line
  is the only notice anyone gets that a table went. They are warnings now.
- **The confirmation did not say why.** "Drop component table comp_x" reads the
  same whether somebody deleted a component or changed its shape. `schema.Change`
  carries a `Reason`, and the description says the shape changed and that no
  column can be altered into it.
- **`assertChanges` never compared `OldType` or `NewType`.** Every SQL type
  written down in a diff test was decoration, including in the test named for
  "every scalar type maps to the expected SQL type". It compares them now, and
  the coverage the earlier edit had cost is back.
- **The agreement test did not agree about anything new.** Its list of component
  types was hardcoded, so adding an eighth type with a fourth shape passed
  silently. It enumerates `schema.ComponentTypes()` now — verified by adding a
  real `date` type with a `date_value` column and watching it fail.

### Verification

- 20/20 mutations caught, 0 survived.
- `go test ./...` clean including `-count=2`, `-race` clean, lint clean.
- Coverage: `schema` 92.0%, `storage` 88.5%. `schema.json` validates; 192 e2e
  tests pass.

### Left for later

- **`string` → `array` leaves the wrong default.** Both are TEXT in a `value`
  column, so the diff emits nothing — and the column keeps `DEFAULT ''` where a
  freshly built array component gets `DEFAULT '[]'`. Not a shape change, so out
  of this story's scope, but it is the same class: a difference the diff cannot
  see because it only compares types.
- **Foreign-key introspection**, which is the other half of that: constraints
  are not in the shape the diff compares either.
