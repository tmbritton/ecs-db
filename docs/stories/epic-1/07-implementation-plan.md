# Story 7 — implementation plan

## Verified before planning

| Claim | Verified |
|---|---|
| The pragmas are issued once against `*sql.DB` | True — `storage/sqlite.go:88`, a loop of four `db.Exec` calls. |
| Three of the four are per-connection | True, measured: `foreign_keys` 1/0/0, `busy_timeout` 5000/0/0, `synchronous` 1/2/2 across three simultaneous connections. |
| `journal_mode` survives | True — WAL is recorded in the database file, and read back `wal` on every connection. |
| The driver applies `_pragma=` per connection | True — `applyQueryParams` (`sqlite.go:136`) runs on every `newConn`, and sorts `busy_timeout` first on purpose. |
| A `?` in the path is a hazard | True, and it is **already broken**: the driver truncates a non-`file:` DSN at the first `?`, so `/tmp/x/with?q.db` opens `/tmp/x/with` today. The `file:` form with an escaped path opens the right file. |
| `forge/status` already builds an escaped DSN | True — `dsn()` at `status.go:102`, whose comment warns that "a second DSN written elsewhere is a second chance to forget mode=ro". |
| `internal/forge/status` already imports `internal/storage` | True, so the canonical builder can live in `storage` with no cycle. |
| Anything passes `:memory:` to `NewSQLiteStore` | **No.** `:memory:` appears only in tests that call `sql.Open` directly, so the store's path is always a file. |

## Decisions

1. **The pragmas move into the DSN.** It is the only mechanism that reaches
   every connection the pool opens, including ones opened long after startup.
   The alternative — a custom `driver.Connector` — re-implements what the driver
   already does and adds a type assertion on `driver.ExecerContext` to get
   wrong.
2. **`journal_mode` stays an explicit statement, run once.** It is persistent
   in the database file, so "every connection" is the wrong shape for it: it
   would be a redundant statement per connection, and a read-only connection
   cannot set it at all. Splitting the four is the honest description of what
   SQLite actually does.
3. **The path is percent-encoded into a `file:` URI.** Not concatenated. This
   fixes the truncation bug above as well as carrying the pragmas.
4. **One builder, in `internal/storage`.** `status.dsn` becomes a caller.
   Its own comment already argued for this; it just could not reach the write
   path from where it sits.
5. **Read-only keeps its own pragma set.** A reader needs `busy_timeout` and
   nothing else: `foreign_keys` and `synchronous` describe writes, and putting
   them on a `mode=ro` connection would say something the connection cannot do.

## Shape

| Action | File | What |
|---|---|---|
| Create | `internal/storage/dsn.go` | `DSN`, `ReadOnlyDSN`, and the escaping both share. |
| Create | `internal/storage/dsn_test.go` | Per-connection assertions, path escaping, the pragma split. |
| Modify | `internal/storage/sqlite.go` | Open through `DSN`; keep `journal_mode` as the one statement. |
| Modify | `internal/forge/status/status.go` | Call `storage.ReadOnlyDSN`. |

## The risk worth naming

Turning foreign keys on for real is a behaviour change. Writes that have always
violated a declared reference have never been refused, and now will be. The
suite passing is evidence rather than proof, so the plan is to run it, and to
look specifically at the paths that delete: `DeleteEntity` already removes
component rows explicitly, so the cascade becomes a second mechanism doing the
same work, which is safe but worth confirming rather than assuming.

## Verification

- Unit tests first, then a mutation battery over every guard, in the background.
- A fresh-context review before the commit.
- The whole suite, plus `-race`, plus the e2e suite, because Forge reads the
  same database through the DSN this story changes.
