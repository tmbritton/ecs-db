# Epic 12 Story 4: Migration framing — Implementation Plan

**Goal:** say what a save will do to the database, before it does it.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/migration/preview.go` | Diff the session against the live database |
| Create | `internal/forge/migration/preview_test.go` | Fixture databases |
| Create | `internal/forge/templates/modes/migration.templ` | The panel and the confirmation |

---

## Task 1: The preview

```go
type Preview struct {
	Available bool     // false when there is no database to compare against
	Reason    string   // why, when it is not
	Changes   []schema.Change
}

func Check(dbPath string, current, snapshot schema.DatabaseSchema) Preview
```

The pieces already exist and compose:

```go
domain, err := storage.IntrospectAll(db)   // as-built shape
changes := schema.Diff(domain.ToDiffSchema(), &current, &snapshot)
```

`snapshot` is the schema as last saved, which `editable.File` already holds —
nothing needs re-reading from disk.

Open read-only with the DSN `internal/forge/status` established. Reuse it rather
than writing a second one; a second DSN is a second chance to forget `mode=ro`.

## Task 2: Destructive changes

`schema.Change` carries what kind of change it is. Destructive ones are marked
in the render and drive the confirmation. Do not re-derive "is this
destructive" from the statement text — ask the type.

`MigrationConfirm` and `*MigrationRequiresConfirmation{DestructiveStatements}`
already model this on the engine side; the wording here should match, because a
user who sees both should not think they are different things.

## Task 3: The confirmation

`ModalShell` with the statements listed. Requirements that are easy to get
subtly wrong:

- it lists **statements**, not a count
- cancelling writes nothing, asserted against the file rather than the UI
- there is no "don't ask again": a schema change that drops a column is not a
  routine confirmation to train someone out of

## Task 4: No database

`Available: false` with a reason. The panel says the change cannot be checked
against a database yet. An empty `Changes` list must never render as "nothing
will happen" — those are different facts and the second one is a lie.

---

## Verification

Fixture databases built by `internal/storage` at one schema, diffed against a
modified schema. Cases: additive only; a dropped column; a dropped table; a
version mismatch; no database at all.

Then the e2e spec against the seeded fixture project, whose most valuable
assertion is that cancelling the confirmation leaves the file byte-identical.
