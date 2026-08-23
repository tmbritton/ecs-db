# Epic 14 Story 3: Map re-import semantics — Implementation Plan

**Goal:** loading a map a second time updates the tiles that changed, creates
the ones that appeared, deletes the ones that went away, and touches nothing
else — in one transaction, with the entity ids intact.

---

## Verified before planning

| Claim | Verified |
|---|---|
| `LoadMap` skips creation when any `Tile` entity exists | True — `loader.go:32` counts `entity_type = 'Tile'` and creates nothing when the count is non-zero. It still rebuilds the grid, so an edited map is not an error, it is a no-op. |
| The identity of a tile is its position | True in intent, **unenforced in the database**: `comp_tile` has `entity_id` as its primary key and no unique index on `(x, y)`. Two tiles can share a cell, and `TileGrid.Rebuild` would keep whichever the query returned last — an order SQLite does not promise. The plan has to say what happens. |
| `setTilePassable` diverges `comp_tile.passable` from what the map declares | True — `builtins/actions.go:313` writes the grid and the row. It is the only runtime writer of a tile. |
| Nothing else writes `comp_tile` | True — `grep` finds the loader, `setTilePassable`, and the renderer's read query. |
| `world.Tx` can update or delete | **False.** The port is `InsertEntity`, `InsertComponent`, `AttachComponent`, `DetachComponent`, `Commit`, `Rollback`. There is no update and no entity delete anywhere in the repo, so both are this story's work. |
| `EntityService.CreateEntity` opens its own transaction | True — one transaction per entity, which is 300 transactions for this map and no atomicity across them. A bulk path is needed. |
| `ON DELETE CASCADE` will clean up a deleted entity's component rows | **False in practice.** See below. |

### The pragma defect this story must not depend on

`comp_*.entity_id` is declared `REFERENCES entities(id) ON DELETE CASCADE`, and
`sqlite.go:96` issues `PRAGMA foreign_keys = ON` — once, on `*sql.DB`. Both
`foreign_keys` and `busy_timeout` are **per-connection** pragmas and
`database/sql` is a pool, so the setting lands on whichever connection was idle
at open time and no other. Measured on a real store:

```
foreign_keys: conn1=1 conn2=0
busy_timeout: conn1=5000 conn2=0
```

`migration.go:144` already knows this — it pins a connection before a table
rebuild for exactly this reason — but the knowledge never made it back to the
open path.

That is an engine defect wider than this story (it also means `busy_timeout` is
0 on most connections, which is what turns WAL contention into `SQLITE_BUSY`
rather than a wait). It is **reported, not fixed here**: the fix is a DSN
`_pragma=` so every pooled connection carries it, it changes behaviour for every
database the engine opens, and it wants its own tests.

What it changes for this story is one design decision: **deleting a tile deletes
its component rows explicitly** rather than trusting a cascade that may or may
not be enforced. That is correct whichever way the defect is settled.

---

## The decision this story exists to record

> **The file wins for what the file describes. The database keeps everything
> else. The entity id survives either way.**

`passable` is the field where both answers are defensible, so both are worth
stating.

**Why the file wins.** The map file is the only place an author can say what the
level *is*, and re-import exists precisely so their edit takes effect. If the
database won, someone who walls off a corridor in Forge would save, run, and see
the corridor still open — which is the exact failure this story was written to
end, moved one field to the left. A door `setTilePassable` opened is state from
one session; the file is the level.

**What survives anyway.** The entity **id**, because it is the one thing that
cannot be recreated: `TileGrid` indexes by it, `comp_tile.entity_id` is the
primary key, and every row in `transitions` naming that tile names the id. So
does `entities.created_tick`, since the entity row is never rewritten.

**What the file says nothing about, re-import says nothing about.** Any other
component attached to a tile entity at runtime is left alone — updating in place
rather than recreating is what makes that true, and it is the practical reason
the diff is worth having over a truncate-and-reload.

**Duplicates.** Two tiles in one cell is a state the database permits and the
engine cannot use. Re-import keeps the **lowest** entity id at that position and
deletes the others: lowest is the earliest created, so the bootstrapped tile
wins over whatever added the second one. Deterministic, and it makes the
invariant the diff depends on true rather than assumed.

---

## Design

### The diff is pure; only applying it needs a database

```go
// package tilemap

type TileState struct {
    Passable bool
    TileType string
}

type Plan struct {
    Create    map[Point]TileState
    Update    map[Point]TileState
    Delete    []int64            // entity ids, ascending
    Keep      map[Point]int64    // position → surviving entity id
    Unchanged int
}

func (p Plan) Empty() bool

func PlanTiles(have map[Point]storedTile, want map[Point]TileState) Plan
```

`PlanTiles` is a function of two maps. Every acceptance criterion about *what*
changes is a table test over it, with no SQLite in sight; the database tests are
then about the transaction, not about the arithmetic.

### Applying it

```go
func SyncTiles(ctx context.Context, svc *world.EntityService, db *sql.DB,
    want map[Point]TileState) (Result, error)

type Result struct{ Created, Updated, Deleted, Unchanged int }
```

- read what is stored (one query, the shape `TileGrid.Rebuild` already uses plus
  `tile_type`),
- plan,
- **return early if the plan is empty** — no transaction is opened, so a restart
  with no edit is a read and nothing else,
- otherwise one transaction for all of it.

### What the port has to grow

Two methods on `world.Tx`, because neither exists:

```go
// SetComponentValues updates the named fields of a component already attached.
// Fields the caller does not name keep their stored value.
SetComponentValues(ctx, entityID int64, compName string, values ComponentValues) error

// DeleteEntity removes an entity and the component rows belonging to it.
DeleteEntity(ctx, entityID int64) error
```

`SetComponentValues` is the plural of the `SetComponentValue` the agent runtime
already has, and partial by the same reasoning. It covers all four component
kinds, not just `object`: a port method that quietly mishandles an array is a
trap for whoever calls it next.

`DeleteEntity` deletes from every component table the schema declares, then the
`entities` row — see the pragma defect above for why it does not lean on the
cascade. `transitions` and `event_queue` are deliberately left: the audit log
outlives the entity, which is why neither has a foreign key.

### And one on the service

```go
func (s *EntityService) InTx(ctx context.Context, fn func(Tx) error) error
func (s *EntityService) CreateEntityInTx(ctx context.Context, tx Tx,
    entityTypeName string, components []EntityComponent) (*Entity, error)
```

`CreateEntity` becomes `InTx(CreateEntityInTx)`, so the validation the sync path
gets is the same code the single-entity path gets rather than a copy of it.
This is what lets `LoadMap` keep its signature: `tilemap` never needs the store.

### `LoadMap` keeps its signature and stops skipping

It still reads the TOML — replacing the format is Story 4 — but its `'.'`/`'#'`
switch now builds a `map[Point]TileState` and hands it to `SyncTiles`. The
`SELECT COUNT(*)` bootstrap guard goes away entirely; "already loaded" stops
being a special case and becomes a plan with nothing in it.

---

## Files

| File | What |
|---|---|
| `internal/world/port.go` | `SetComponentValues`, `DeleteEntity` on `Tx` |
| `internal/world/service.go` | `InTx`, `CreateEntityInTx`; `CreateEntity` rewritten on top |
| `internal/storage/entity.go` | the two new `sqliteTx` methods |
| `internal/tilemap/sync.go` | `TileState`, `Plan`, `PlanTiles`, `SyncTiles`, `storedTiles` |
| `internal/tilemap/loader.go` | builds `want`, calls `SyncTiles`, drops the count guard |
| `internal/tilemap/sync_test.go` | the diff as a table test; the transaction against SQLite |

## How the sharper criteria get tested

- *"one changed cell does not rewrite the other 299"* and *"touches no rows"* are
  asserted with **SQLite triggers**: an `AFTER INSERT/UPDATE/DELETE ON comp_tile`
  writing into a scratch table, then a count. That is a claim about what the
  database was told, not about what the Go code thinks it did — and it catches a
  no-op `UPDATE` setting a column to the value it already had, which counting API
  calls would not.
- *"a partial re-import is a map with a hole in it"* uses a
  `BEFORE INSERT ... RAISE(ABORT)` trigger on one cell, so a real failure lands
  mid-apply and the assertion is that the database is byte-for-byte what it was.
