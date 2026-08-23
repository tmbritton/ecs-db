# Story 3: Map re-import semantics

**Epic:** 14 — Tiled map & tileset formats  
**Status:** ✅ Complete  
**Priority:** High — decides whether Forge's MAP mode can work at all

**Depends on:** Story 1

## Context

`LoadMap` is a one-time bootstrap. It counts `Tile` entities and creates none if
any exist, so after the first `ecs-db run` the **database is the source of truth
and the file is decoration**. A map edited in Forge would save successfully and
change nothing — no error, no warning, nothing to notice.

Epic 15 is a map editor. It cannot be built on that.

This story settles it before anything is built on top, which is the same call
Epic 13 Story 1 made about the machine round trip and for the same reason: a
format that cannot be re-read is a format nothing above it can trust.

The shape of the answer is a **diff**, not a reload. Tiles carry state the file
does not own — a door someone opened at runtime is `comp_tile.passable` differing
from the tileset's property, and `setTilePassable` is a registered action that
exists to do exactly that. Truncating and recreating would silently discard it,
and would also churn every entity id, which `comp_tile.entity_id` and the
`TileGrid`'s own index both depend on.

## Acceptance Criteria

- [x] Loading a map whose tiles already exist updates them rather than skipping
- [x] A tile whose file position is unchanged keeps its entity id
- [x] A tile the file no longer has is removed
- [x] A tile the file has and the database does not is created
- [x] Nothing is written when the file and the database already agree, so a
      restart with no edit touches no rows
- [x] The decision is per tile, not per map: one changed cell does not rewrite
      the other 299
- [x] Runtime state a tile carries that the file does not describe is stated one
      way or the other — either preserved with a reason, or overwritten with a
      reason
- [x] A map that fails to parse leaves the database exactly as it was
- [x] `go test ./...` passes

## Notes

- The identity of a tile is its position, not its row in the table. Two tiles
  cannot share a cell, and that is what makes the diff possible.
- Watch the transaction boundary. A partial re-import is a map with a hole in
  it, and the engine reads the grid on the next tick.
- This is the story most likely to want a decision recorded rather than a rule
  invented: "the file wins" and "the database wins" are both defensible for
  `passable`, and only one of them can be true.

## As Implemented

`LoadMap` no longer counts tiles. It parses, refuses a file that is not a map,
and hands the cells to `SyncTiles`, which diffs them against the database per
cell and writes only what differs — in one transaction, or in none at all when
the file and the database already agree.

### The decision, recorded

> **The file wins for what the file describes. The database keeps everything
> else. The entity id survives either way.**

`passable` and `tile_type` are overwritten from the file on every load. The
argument for the other answer is real — a door `setTilePassable` opened is
state someone's game produced — but letting it win means an author who walls off
a corridor in Forge saves, runs, and finds the corridor still open. That is the
failure this story exists to end, moved one field to the left.

What survives is identity: the entity **id**, because `TileGrid` indexes by it
and every row in `transitions` naming a tile names its id; `created_tick` with
it, since the entity row is never rewritten; and **any other component** a tile
picked up at runtime, because the file says nothing about those and so neither
does re-import. That last one is only true because a changed cell is an UPDATE
and not a delete-and-recreate, and it is the practical reason the diff is worth
having over a truncate-and-reload.

Two tiles in one cell is a state the database permits — `comp_tile` is keyed by
`entity_id`, nothing makes `(x, y)` unique — and the engine cannot use, since
`TileGrid.Rebuild` keeps whichever row the query returned last, an order SQLite
does not promise. Re-import keeps the lowest id and deletes the rest, reported
separately from cells the map lost: a database repaired is not the same news as
a map that shrank.

### What the port had to grow

There was no update and no entity delete anywhere in the repo. `world.Tx` gained
`SetComponentValues` (partial, all four component kinds, and a refusal when it
matches no row) and `DeleteEntity`. `EntityService` gained `InTx` and
`CreateEntityInTx`, and `CreateEntity` is now the second wrapped in the first,
so the bulk path validates through the same code the single-entity path does
rather than a copy of it.

### An engine defect found and not fixed here

`comp_*.entity_id` declares `ON DELETE CASCADE`, and `sqlite.go` issues
`PRAGMA foreign_keys = ON` once, on `*sql.DB`. Both `foreign_keys` and
`busy_timeout` are per-connection and `database/sql` is a pool. Measured on a
real store:

```
foreign_keys: conn1=1 conn2=0
busy_timeout: conn1=5000 conn2=0
```

`migration.go` already knows this — it pins a connection before a table rebuild
for exactly this reason — but the knowledge never reached the open path. So the
cascade is enforced on one connection and no other, and `busy_timeout` is 0 on
the rest, which is what turns WAL contention into `SQLITE_BUSY` rather than a
wait. **`DeleteEntity` therefore deletes component rows explicitly** and is
correct whichever way the defect is settled. Fixing it properly is a DSN
`_pragma=`, it changes behaviour for every database the engine opens, and it
wants its own story.

### What the review found

Thirteen findings, two of which were the kind no test written from the story
would have caught:

1. **A misspelled key would have deleted the map.** TOML ignores keys it was not
   asked for, so `rowz = [...]` unmarshals into a `mapDef` of zeroes. Under the
   old bootstrap that was harmless — no rows meant nothing to create, and the
   guard skipped anyway. Under a diff it means the file describes no cells and
   every tile is a cell the file dropped: 300 tiles gone, no error, `ecs-db run`
   rendering black. `mapDef.check` now refuses a file with no size, with a row
   count that disagrees with `height`, or with a row that is not `width` cells
   wide. A file truncated halfway is the same class of problem and is refused
   the same way.
2. **`DeleteEntity` left `behavior_components` behind**, and `tick.go` reads
   that table with no join to `entities` — so a deleted entity's machine would
   keep receiving `TICK`, running actions against components that no longer
   exist, and writing `transitions` rows, quietly, forever. `event_queue` is the
   same: a pending-work queue drained by the tick, not an audit log. Both are
   cleared now. `transitions` alone is left, because it *is* the audit log and
   declares no foreign key for exactly that reason.

The rest, in short: the row index was a byte offset, so one non-ASCII character
shifted every cell after it and pushed the last ones outside the grid — where
`TileGrid` keeps their ids but refuses their passability, so `setTilePassable`
would find an id, write the row, and no-op on the grid; `SetComponentValues`
pointed callers at `HasComponent`, which is on the store, a different pooled
connection that cannot see the transaction's own writes; `InTx` had no `defer`,
so a panic in a caller's closure would have held the transaction, its connection
and the WAL write lock for the life of the process; `CreateEntity` validated
after `BeginTx`, so a busy database masked a `ValidationError`; a present-but-nil
entity-ref target reached a `NOT NULL` column as a raw constraint error; and
`updateColumns` validated field names before sorting them, so the refusal named
whichever bad field the map happened to yield first — the exact nondeterminism
its own comment claimed to have removed.

Three tests asserted less than their names claimed and were rewritten to assert
it: a refusal test checking only that an error occurred, a "no statement" test
that never looked at the database, and `PlanTiles` exported while taking an
unexported parameter type, so nothing outside the package could ever have called
it.

### Verification

- 87/87 mutations caught, 0 survived.
- `go test ./... -race` clean; `golangci-lint` clean.
- Coverage: `tilemap` 95%, `world` 98%, `storage` 87%.
- Loaded the real `mods/map/level1.toml` against the real `schema.json` twice:
  300 tiles created, second pass wrote nothing, clearing the map deleted 300.

### Left for later

- **The read is outside the transaction.** `world.Tx` has no reader, so the
  database is read on the pool and only then written in a transaction. That is
  unreachable from the engine, which loads the map in its composition root
  before the tick loop starts, and Forge never calls this at all — it writes map
  files. It would become reachable if something loaded a map while the game was
  running, and the fix then is a reader on the port, not a retry here.
- `GetCurrentTick` is read once per created entity, on a different connection
  from the transaction that stamps it into `created_tick`. 300 tiles is 300
  round trips and about 15 ms; making it one wants either a tick parameter on
  `CreateEntityInTx` or a tick read on `Tx`.
- The new SQL builders validate identifiers; `insertObjectComponent` still does
  not. That gap predates this story and now sits next to code that closed it.
- `SetComponentValues` is stricter than `InsertComponent` about where a scalar
  or array value lives — deliberate, and now stated in the port contract, but
  the two neighbouring methods still disagree.
