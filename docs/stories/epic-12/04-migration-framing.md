# Story 4: Migration framing

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** 🔲 Not started  
**Priority:** High — this is where an edit can destroy data

**Depends on:** Story 3

## Context

Editing a schema is not editing a document. The database was built to a shape, and changing that shape means migrating — sometimes losing a column and everything in it. The engine already knows this: it records the `schemaVersion` a database was built with and refuses to start against a mismatch, and its migration runner has a `MigrationConfirm` policy that returns `*MigrationRequiresConfirmation{DestructiveStatements}` rather than running destructive changes silently.

Forge's job is to make that visible *before* the save, not after the game refuses to boot. It has everything it needs: `storage.IntrospectAll` reads the as-built shape from the database, `ToDiffSchema` converts it, and `schema.Diff` produces a phase-ordered list of `Change`s against the file being edited.

This is the story where the read-only rule earns its keep. Forge introspects the running game's database and never writes to it — the migration itself is the engine's to run, at its own startup. Forge only says what will happen.

## Acceptance Criteria

- [ ] With a database present, the editor shows the pending changes between it and the schema being edited, from `schema.Diff`
- [ ] Destructive changes — dropped columns, dropped tables — are marked as such and visually distinct from additive ones
- [ ] Saving a schema with destructive changes requires an explicit confirmation naming what will be lost; the modal lists the statements, not a count
- [ ] The confirmation is a real decision: cancelling saves nothing, and there is no "don't ask again"
- [ ] With no database, the panel says the change cannot be checked yet rather than showing an empty list that reads as "nothing will happen"
- [ ] The database is opened **read-only**, reusing the approach `internal/forge/status` established; a test asserts a write through that connection fails
- [ ] A database whose `schema_version` already disagrees with the file is reported as the distinct state it is — the same distinction the engine-status readout makes
- [ ] `Change`s render in the phase order `Diff` returns them in; that order is the migration plan, not a display preference
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/12-migration-framing.spec.js`. The fixture project seeds a real
database (`e2e/fixtures/seed`), so this can be driven end to end.

- [ ] Adding a component shows an additive change and no warning
- [ ] Deleting a field shows a destructive change, marked
- [ ] Saving with a destructive change opens a confirmation that names the field being dropped
- [ ] Cancelling leaves the file unchanged on disk — assert the file, not the UI
- [ ] Confirming saves
- [ ] With the database removed, the panel says so rather than showing an empty change list
- [ ] The dialog is reachable and dismissable by keyboard, and is `aria-modal` with an accessible name

## Notes

- **A count is not a warning.** "3 destructive changes" tells someone nothing about whether to proceed; the statements do.
- Forge cannot run the migration and should not imply it can. The wording is "the engine will do this on next start", not "this will be applied".
- `Diff` takes the as-built `*DomainSchema` plus the new and old file schemas. The "old file" is the snapshot in the editing session — which is exactly what `editable.File` holds, so there is no need to re-read anything.
- Introspecting a WAL database read-only creates `-shm`/`-wal` sidecars and cannot clean them up; that is documented in Epic 10 Story 6 and applies identically here.
- Do not cache the diff. It depends on a database another process is writing, and a stale migration warning is worse than a slow one.
