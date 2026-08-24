# Story 7: Pragmas on every connection

**Epic:** 1 — Schema-driven data foundation
**Status:** ✅ Complete
**Priority:** High — a correctness setting that is off almost everywhere

**Depends on:** Story 3, which is where the defect is

## Context

Story 3's acceptance list carries the bullet *"Pragmas at init: WAL,
`synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=ON`"*, ticked. It was
implemented as four `db.Exec` calls against the `*sql.DB` returned by
`sql.Open` (`storage/sqlite.go:88`).

`*sql.DB` is a **pool**, not a connection. Three of those four pragmas are
per-connection state in SQLite, so they land on whichever connection the pool
happened to hand out at open time and on none of the others. Measured on a real
store, holding three connections at once:

| pragma | conn1 | conn2 | conn3 |
|---|---|---|---|
| `foreign_keys` | 1 | **0** | **0** |
| `busy_timeout` | 5000 | **0** | **0** |
| `synchronous` | 1 (NORMAL) | **2 (FULL)** | **2 (FULL)** |
| `journal_mode` | wal | wal | wal |

Only `journal_mode` holds, because WAL is recorded in the database file rather
than on the connection.

What each one costs:

- **`foreign_keys=OFF`** means `ON DELETE CASCADE` — declared on every
  `comp_*.entity_id` and on `behavior_components` — does not fire, and a
  reference to a row that does not exist is accepted. The schema says the
  database enforces referential integrity. On most connections it does not.
- **`busy_timeout=0`** turns ordinary WAL contention into an immediate
  `SQLITE_BUSY` error instead of a wait. The engine writes from the tick loop
  while Forge reads; that is the exact case the timeout exists for.
- **`synchronous=FULL`** is the safe direction, so this one is a performance
  cost rather than a risk — an fsync per commit in the tick loop.

The knowledge was already in the codebase twice. `migration.go` pins a single
connection before a table rebuild for precisely this reason, and
`forge/status/status.go` builds a DSN whose comment explains that the path must
be escaped because a project directory containing `?` would corrupt it. Neither
reached the open path that everything else in the engine goes through.

## Acceptance Criteria

- [x] Every connection the pool opens carries `foreign_keys`, `busy_timeout`
      and `synchronous`, demonstrated on more than one connection at once
- [x] `journal_mode` is still WAL, and the story states why it is not handled
      the same way as the other three
- [x] A database path containing a character that is special in a URI opens the
      file the caller named
- [x] There is one place that knows how to build a connection string for this
      project's database, and both the read-write and read-only callers use it
- [x] Enabling foreign keys for real does not break the existing suite — or, if
      it does, what it caught is reported rather than worked around
- [x] `go test ./...` passes

## Notes

- The driver (`modernc.org/sqlite`) applies `_pragma=` query parameters on every
  connection it opens, and deliberately runs `busy_timeout` first so the others
  cannot fail with `SQLITE_BUSY`. That is the mechanism; `database/sql` offers
  no per-connection hook short of writing a `driver.Connector`.
- Turning on a constraint that has been off is a behaviour change, not only a
  fix. The suite passing is evidence, not proof: the interesting case is a write
  that has always violated a foreign key and never been told.

## As Implemented

`storage.DSN` builds the connection string, and the three per-connection pragmas
ride in it as `_pragma=` parameters. The driver applies those on every
connection it opens, which is the only mechanism that reaches connections the
pool creates later — `database/sql` has no per-connection hook short of writing
a `driver.Connector`, which would re-implement what the driver already does.

`journal_mode` stayed a statement, run once. WAL is recorded in the database
file, so "every connection" is the wrong shape for it: in the DSN it would be a
redundant statement per connection, and a read-only connection cannot set it at
all. Splitting the four is the honest description of what SQLite actually does,
and the split is now the thing the tests assert.

### A second bug came with it

The path is percent-encoded into a `file:` URI rather than concatenated. The
driver truncates a DSN that is not a URI at its first `?`, so a database at
`/srv/save?1/world.sqlite` opened `/srv/save` — creating it, finding no tables,
bootstrapping a second database and reporting success. That was true of the code
this story replaced. A leading `//` is collapsed to one slash for the same
reason at the other end: SQLite would read the first segment as a URI authority
and refuse the file outright.

### One place, not three

`forge/status` already built a correct read-only DSN, and its own comment argued
that "a second DSN written elsewhere is a second chance to forget mode=ro". It
was right, and it could not reach the write path from where it sat. The builder
now lives in `storage`; `status.ReadOnlyDSN` is gone rather than forwarding, and
`storage.OpenReadOnly` is the single function that makes a read connection —
so "Forge never writes to the game database" is a property of one function
instead of every caller passing the right string.

`readPragmas` also gained `query_only(true)`, because `mode=ro` is weaker than
it sounds: it bounds the main database only, and a `mode=ro` connection will
otherwise create temp tables and `ATTACH` a second database and write to *that*.

### The behaviour change, reported rather than worked around

Turning foreign keys on for real is not only a fix, and the plan's risk analysis
got it half right. It reasoned about *outbound* references — an entity's own
`comp_*.entity_id` rows, which cascade, and which `DeleteEntity` already removed
explicitly. It missed the inbound direction, and the review caught it:

> An entity-ref component's `target_entity_id` is declared
> `REFERENCES entities(id)` with **no `ON DELETE` clause**, which SQLite treats
> as a restrict. Deleting an entity that another entity points at is now
> refused.

`DeleteEntity` cannot fix that by construction: it deletes `WHERE entity_id = ?`,
and the holder's row belongs to a different entity. So the refusal stands, and
what changed is that it now says something usable — "deleting entity 1: FOREIGN
KEY constraint failed — it is still referenced by Carrier of entity 2" instead
of error 787 and nothing else.

**This is an improvement, not a regression.** Before, the delete succeeded and
left a dangling reference to an entity that no longer existed; a loud refusal is
the better failure. But *what should happen to a component whose target is
deleted* is a schema question with three defensible answers — cascade the
holder, null the reference, or refuse — and it is not this story's to settle. No
shipped `schema.json` declares an entity-ref, so nothing is broken today; Forge
can author one, so this will come up. It wants its own story.

Related and verified safe: existing dangling rows in a database created before
this change do not become unwritable. SQLite does not re-check a child row whose
foreign-key columns are unmodified, so legacy data still updates and deletes.
Nothing runs `PRAGMA foreign_key_check`, so a pre-existing violation stays quiet.

### The durability trade, stated

`synchronous=NORMAL` now actually applies everywhere, where most connections
were silently running at `FULL`. Under WAL, NORMAL cannot corrupt the database
but **can lose recently committed transactions** to a power cut; FULL loses
nothing and fsyncs every commit. Losing the last few ticks of a session is worth
not fsyncing sixty times a second, which is the choice this engine wants — but
it is a choice, and the defect was hiding it. The story's original framing
("`synchronous=FULL` is the safe direction, so this one is a performance cost
rather than a risk") described the *broken* state and not the fix.

### What else the review found

Ten findings. Three stale comments — in `entity.go`, on the exported
`world.Tx.DeleteEntity` port, and in a test — stated the pragma defect as
current fact, which is the kind of comment that makes the next person write code
relying on it. A `sql.Register` call keyed on `t.Name()` panicked the whole
package under `go test -count=2`, the standard way to hunt a flake. Two tests
turned off foreign keys on a pooled connection and handed it back without
re-enabling, which `migration.go` has always been careful to do. `mode=ro` was
described as a guarantee it does not provide. And a `//` path produced an
invalid URI.

Verified and left alone: the migration table-rebuild path, which pins a
connection and toggles `foreign_keys` off outside the transaction, was already
correct and is now doing real work rather than a no-op; `VACUUM INTO` backups
are unaffected; and the tick loop's own tables (`event_queue`, `input_events`,
`transitions`) declare no foreign keys at all.

### Verification

- 27/27 mutations caught, 0 survived.
- `go test ./...` clean, including `-count=2`; `-race` clean; `golangci-lint`
  clean. `storage` coverage 87.8%.
- The full Forge e2e suite: 192 passing. It reads the game database through the
  DSN this story changed, so it is the end-to-end check that the read path still
  works.

### Left for later

- **What happens to a component whose target entity is deleted.** Cascade, null,
  or refuse — refuse is what the DDL says today, by omission rather than by
  decision. Wants a story, and wants it before anyone authors an entity-ref in
  Forge.
- **`pruneBackups` globs `dbPath + ".bak.v*"`.** A path containing `[`, `*` or
  `?` — the same user-supplied-config hazard this story fixed on the open path —
  makes the glob match nothing or return `ErrBadPattern`, and the error is
  swallowed. Backups then accumulate silently.
- **`DSN("")` opens a private temporary database.** `NewSQLiteStore("")` would
  bootstrap a complete schema into a file that is unlinked on close. `usage.Read`
  guards the empty path; the store does not.
- **Nothing runs `PRAGMA foreign_key_check`.** A database carrying violations
  from before this change stays silently inconsistent. A one-off check at open,
  or a repair command, would surface them.
