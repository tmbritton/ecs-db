# Story 4: Migration framing

**Epic:** 12 — Forge: SCHEMA & ENTS modes  
**Status:** ✅ Complete  
**Priority:** High — this is where an edit can destroy data

**Depends on:** Story 3

## Context

Editing a schema is not editing a document. The database was built to a shape, and changing that shape means migrating — sometimes losing a column and everything in it. The engine already knows this: it records the `schemaVersion` a database was built with and refuses to start against a mismatch, and its migration runner has a `MigrationConfirm` policy that returns `*MigrationRequiresConfirmation{DestructiveStatements}` rather than running destructive changes silently.

Forge's job is to make that visible *before* the save, not after the game refuses to boot. It has everything it needs: `storage.IntrospectAll` reads the as-built shape from the database, `ToDiffSchema` converts it, and `schema.Diff` produces a phase-ordered list of `Change`s against the file being edited.

This is the story where the read-only rule earns its keep. Forge introspects the running game's database and never writes to it — the migration itself is the engine's to run, at its own startup. Forge only says what will happen.

## Acceptance Criteria

- [x] With a database present, the editor shows the pending changes between it and the schema being edited, from `schema.Diff`
- [x] Destructive changes — dropped columns, dropped tables — are marked as such and visually distinct from additive ones
- [x] Saving a schema with destructive changes requires an explicit confirmation naming what will be lost; the modal lists the statements, not a count
- [x] The confirmation is a real decision: cancelling saves nothing, and there is no "don't ask again"
- [x] With no database, the panel says the change cannot be checked yet rather than showing an empty list that reads as "nothing will happen"
- [x] The database is opened **read-only**, reusing the approach `internal/forge/status` established; a test asserts a write through that connection fails
- [x] A database whose `schema_version` already disagrees with the file is reported as the distinct state it is — the same distinction the engine-status readout makes
- [x] ~~`Change`s render in the phase order `Diff` returns them in~~ — **statements**
      render in the order the *generator* returns them, which is the plan the
      engine runs; see *As Implemented*. The phase order is preserved through it
- [x] `go test ./...` passes `Diff` returns them in; that order is the migration plan, not a display preference

## Playwright steps

`e2e/specs/12-migration-framing.spec.js`. The fixture project seeds a real
database (`e2e/fixtures/seed`), so this can be driven end to end.

- [x] Adding a component shows an additive change and no warning
- [x] Deleting a field shows a destructive change, marked
- [x] Saving with a destructive change opens a confirmation that names the field being dropped
- [x] Cancelling leaves the file unchanged on disk — assert the file, not the UI
- [x] Confirming saves
- [x] With the database removed, the panel says so rather than showing an empty change list
- [x] The dialog is reachable and dismissable by keyboard, and is `aria-modal` with an accessible name

## Notes

- **A count is not a warning.** "3 destructive changes" tells someone nothing about whether to proceed; the statements do.
- Forge cannot run the migration and should not imply it can. The wording is "the engine will do this on next start", not "this will be applied".
- `Diff` takes the as-built `*DomainSchema` plus the new and old file schemas. The "old file" is the snapshot in the editing session — which is exactly what `editable.File` holds, so there is no need to re-read anything.
- Introspecting a WAL database read-only creates `-shm`/`-wal` sidecars and cannot clean them up; that is documented in Epic 10 Story 6 and applies identically here.
- Do not cache the diff. It depends on a database another process is writing, and a stale migration warning is worse than a slow one.

## As Implemented

`internal/forge/migration` is a pure domain package: a database path and two
schemas in, a `Preview` out, no HTTP and no templates. `Check` introspects the
as-built database, diffs it against the schema being edited, and hands the
changes to the engine's **own** generator — so the plan Forge shows and the plan
the engine runs cannot disagree by construction.

Coverage: `migration` 92.5%, `server` 91.0%, `modes` 70.3%.

### The finding: the engine does not run a migration without a version bump

`storage.checkAndMigrate` compares the database's recorded `schema_version`
against `schema.json`'s and **returns early when they match** — before the
migration runner is ever constructed. An author who drops a field and saves
without touching `schemaVersion` gets no migration at all: the column stays, the
game runs against a shape the file no longer describes, and nothing anywhere
says so.

A panel listing five statements the engine will never execute is a correct
answer to the wrong question, so `Preview.WillMigrate()` reports the difference
and the panel says it in words. This is a claim about another package's
behaviour, so it is pinned by two tests that run the engine's own store rather
than by reading its source: `TestEngine_SkipsMigrationWhenTheVersionIsUnchanged`
and `TestEngine_MigratesWhenTheVersionIsBumped`.

### Divergences from the plan

- **`Preview` carries `[]storage.Statement`, not `[]schema.Change`.** The plan
  said to ask the change what kind it is and not to re-derive destructiveness
  from statement text. Both are right, and the conclusion is stronger than the
  plan drew: `schema.Change` has no destructive flag at all, while
  `storage.Statement` already carries `Destructive` **and** a written
  `Description`, assigned by the generator the engine migrates with. Deriving
  destructiveness from `Kind` here would have been a second classifier free to
  drift from the first. One dropped property is four statements, not one change,
  and the four are what the engine runs.
- **`Diff` is called with `nil` as `oldFile`, not the snapshot.** The plan
  specified the snapshot, and `MigrationRunner.Run` passes `nil`. The third
  argument only enables `ChangedEntityType` detection, and entity-type changes
  produce no DDL — so the snapshot would have added changes to the plan that no
  migration ever acts on. The snapshot is still needed, for the version
  comparison `Stale()` makes, which is why it stays in the signature.
- **The generator's `error` statements are `Problems`, not statements.** They
  carry a description and no SQL; listing them as changes would render a blank
  row that reads as safe, when each is a migration that would fail at startup.
  No edit reachable through the editor produces one today, so the split is
  tested through `partition` directly — an unfalsifiable guard is one that
  quietly stops working.
- **`status.ReadOnlyDSN` is exported.** The plan said to reuse the DSN rather
  than write a second one; it was unexported, so reuse needed the export. The
  read-only test now attempts its write through `migration.open` — the function
  `Check` itself calls — rather than through a DSN rebuilt to match, which is
  the difference between testing the property and testing its neighbour.
- **`editable.File.Snapshot()` and `Session.Snapshot()` are new.** The snapshot
  was held as bytes only. It is parsed on demand rather than kept beside them: a
  parallel parsed copy is a second thing that can disagree.
- **The engine-status readout's copy was wrong and is corrected.** It said a
  version mismatch meant the `engine would refuse this database`. It does not —
  `checkAndMigrate` backs the database up and migrates it. That readout sits on
  the same screen as this panel, which would have said the opposite. It now
  reads `engine migrates on next start`. The same claim in `generatedSQLPanel`
  is corrected too.
- **`ModalShell` gained Escape and focus.** The primitive had neither, so every
  dialog in Forge was mouse-only. Escape binds on the backdrop rather than the
  window: events bubble out of the dialog to it, so it fires for Escape inside
  the dialog and nowhere else.
- **The `modes` package had no Datastar bundle guard.** The shell and the
  primitives each had one; the package with the most handlers in it did not.
  Added, and verified by mutation to fail against both the dash form and an
  unregistered plugin name.

### What the review round caught

Six real defects, four of them in the guard the story exists to provide.

- **`/forge/schema/overwrite` wrote destructively with no confirmation.** "Keep
  mine", the answer to a save conflict, is a normal button in the save report,
  and it drops exactly as many columns as an ordinary save. The check had been
  put on one of three save routes. Every route that writes now goes through
  `holdForConfirmation`.
- **`/forge/schema/save/confirm` never checked that anything was held.** It was
  an unconditional save, so a POST to it saved destructively having shown no
  dialog — and, far more likely, the dialog stays on the page until the next
  2-second poll, so its "Save anyway" button still worked *after* Cancel. The
  user cancelled and the tool saved anyway. The hold is now taken and cleared in
  one locked step, which also makes a double-click save once.
- **The confirmation performed the wrong save.** It always called `Save`, even
  when what had been held was an overwrite. `Save` refuses a file that changed
  underneath it, so confirming "keep mine" would have been refused as a
  conflict — the conflict the user had already answered. The held value is now
  the *kind* of save, not a flag.
- **The dialog said the engine would run statements it will skip.** Its copy was
  unconditional while the panel three inches below it rendered "The engine will
  not run any of this" from the same `Preview`. That is the default path, since
  the story's own finding is that authors do not bump `schemaVersion`. The
  dialog now says which case it is in.
- **A save held from another mode was invisible.** The save footer lives in the
  shell and renders on all six modes; the confirmation was rendered only by
  SCHEMA. Pressing Save from ENTS held the save and drew nothing — a button that
  silently did nothing, permanently. The dialog moved to `components` and is now
  a shell-level region, `#save-confirm`, patched like the other four.
- **The save handler failed open.** It gated on `Destructive()`, which is false
  for every unavailable preview — including a locked or corrupt database, where
  a save may destroy plenty. `Preview.Failed` now distinguishes "we looked and
  could not read it" from "there is nothing to look at", and `Holds()` covers
  both. Only a genuinely absent database saves freely.

And two test defects:

- **A dead assertion.** The check that the additive statement carried no
  "destroys data" badge embedded a newline and the template's own indentation in
  its needle, so it could never match. Badging *every* statement passed the
  entire suite. Counted now, and verified by mutation.
- **An unparseable saved file silently disabled the stale warning.** The zero
  schema it fell back to made `Stale()` answer "not stale" — indistinguishable
  from a real comparison. `SnapshotUnknown` reports it as the missing half it is.

The review also found the one thing I could not fix within this story: the
dialog is `aria-modal="true"` with no focus trap, so tabbing past the last
button leaves it. `inert` on the shell body cannot work — that element is in no
patched region, so it can only hold its page-load value, and the dialog always
arrives afterwards on the stream. The honest fix is the native `<dialog>`
element and `showModal()`, which brings a real focus trap with it; that is a
change to a shared primitive used elsewhere and belongs with Epic 17, which
builds the rest of the dialog set. Recorded in `SaveConfirmRegion` rather than
left as an unstated gap.

### Why the confirmation is server-side

The check is in `handleSchemaSave`, not in the button. A confirmation the client
can skip is not a confirmation — and this is the one save in Forge where being
wrong is unrecoverable. Nothing is written when the handler returns; the modal's
own action is what saves. A page load clears the pending confirmation, because a
tab that never asked the question cannot explain the dialog.

### What the tests caught

- **An additive test asserting "some statements" rather than the right one.**
  Feeding the generator the snapshot instead of the edited schema survived it —
  the mutant emitted a statement with no SQL and the test was satisfied. That
  survival is what surfaced the `error` statement kind in the first place.
- **`DestructiveStatements` returning everything survived**, because every
  fixture in the destructive tests happened to be all-destructive. A mixed edit
  — one added column, one dropped table — kills it, and is the realistic case.
- **A confirmation test scoped to the whole page.** "The dialog lists only
  destructive statements" searched the rendered mode, where the panel behind the
  dialog lists every statement by design. The same shape of mistake as three
  earlier stories: asserting on the neighbour of the property.
- **The modal is asserted through the mode-content render the stream pushes**,
  not through a page load — a load clears the confirmation by design, so
  asserting against one would have been asserting against the wrong path.

Every guard was checked against a deliberate defect. In the browser: removing
the Escape binding fails the keyboard test, and saving without the destructive
check fails the hold test. In Go: eleven mutations of `preview.go` and seven of
the save gates in `server.go` — skipping the hold on overwrite, ignoring it on
confirm, confirming as the wrong save kind, gating on `Destructive` instead of
`Holds`, and reading the hold without clearing it — all killed.
