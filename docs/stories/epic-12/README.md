# Epic 12 — Forge: SCHEMA & ENTS modes

Both halves of `schema.json`: components with a live generated-DDL preview, and
entity types with their component contracts.

**This is the first epic where Forge changes anything.** Everything before it
either read files or built the frame; from here a click alters a file the engine
loads. That shifts what "correct" means — a mistake is no longer a wrong pixel,
it is someone's schema.

## Verified before planning

Every claim `docs/plan.md` makes about the engine was checked against the code
rather than trusted. All of them hold:

| Claim | Verified |
|---|---|
| `componentTableBuilder.go` iterates properties unsorted | True — `for propName, prop := range comp.Properties` (`componentTableBuilder.go:21`). Column order changes between calls. |
| `ddlgen.go`'s rebuild path already sorts | True (`buildNewColumns`, `ddlgen.go:311-318`) — so the two paths disagree with each other today. |
| `storage.MigrateComponent` is the exported entry to the real generator | True (`migrate.go:10`), a one-line wrapper over `componentTableSQL`. |
| `ValidateBehaviorRefs` has no caller | True — referenced only from `load_validate_test.go`. |
| `MigrationConfirm` / `MigrationRequiresConfirmation{DestructiveStatements}` exist | True (`migration.go:43-56`). |
| `schema.Diff` and `storage.IntrospectAll` → `ToDiffSchema` exist | True (`diff.go:88`, `introspect.go:174,230`). |
| `"Behavior"` is rejected as a component name | True (`validate.go:257`, case-insensitive). |

One correction to the plan's proposed fix. It says to make the unsorted path
"match" the sorted one. Since Epic 11 Story 2, `Component.PropertyOrder` records
the **authored** order — so the better fix is to emit columns in the order the
author wrote them, which both removes the non-determinism and makes the DDL
preview read like the file it came from. Sorting would be deterministic and
still wrong-looking.

## Two places the design outruns the engine

Both were flagged in the original plan and are still true. They are called out
in the stories that hit them so they are decisions rather than surprises.

- **`array‹entity-ref›` with a junction table does not exist.** Arrays are stored
  as one JSON `TEXT` column (`componentTableBuilder.go:32`). Story 3 renders
  arrays honestly as the column they are, and the junction-table shape is not
  offered. Implementing it is engine work, not Forge's.
- **Spawn counts do not exist.** The usage panel's "spawn count" needs TMX object
  layers, which arrive in Epic 14. Story 6 shows live instance counts from the
  database and says plainly that spawns are not available yet, rather than
  showing a zero that looks like an answer.

## Stories

| # | Story | Delivers |
|---|---|---|
| 1 | [Editing session](01-editing-session.md) | The server-side spine: an editable `schema.json`, save/discard, the wired footer |
| 2 | [SCHEMA mode](02-schema-mode.md) | Component list, version badge, shape, behavior binding, fields table |
| 3 | [Generated-SQL panel](03-generated-sql.md) | Live `CREATE TABLE comp_*` from the real generator, plus the determinism fix |
| 4 | [Migration framing](04-migration-framing.md) | `Diff` against the live database; destructive changes behind a confirmation |
| 5 | [ENTS mode](05-ents-mode.md) | Entity types: behavior, component chips, validation level, context seeds |
| 6 | [Usage panel](06-usage-panel.md) | Used-by chips and live instance counts, degrading honestly |
| 7 | [Inline validation](07-inline-validation.md) | `ValidateSchema` + `ValidateBehaviorRefs` + the ambiguous-context-key warning |

`docs/plan.md` lists six bullets for this epic; this is seven. The extra one is
Story 1: both modes need an editing session, and building it inside whichever
mode happened to land first is how it would end up shaped for that mode only.

## Process

Per `AGENTS.md`: TDD, a fresh-context code review before each commit, and
`make test` + `make e2e` + the linter green. Each story carries a **Playwright
steps** section written before the code — and from this epic on those steps are
not deferrable, because there is finally a UI that changes things.

### One rule specific to this epic

**No mode may write to disk on its own.** Every edit goes through the
`editable.File` from Story 1, so dirty tracking, validation, atomic writes and
the conflict path stay in one place. A mode that calls `os.WriteFile` — or even
`schema.Marshal` — directly has bypassed all four.
