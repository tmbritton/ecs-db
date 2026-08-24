# Epic 14 — Tiled map & tileset formats

Engine work, and the largest prerequisite in the Forge sequence. Today the map
is a bespoke TOML file of `.` and `#` characters: no tilesets, no layers, no
spawns, no per-tile properties. Forge's MAP and TILES modes have nothing to
write to until this lands, and Epic 15 cannot start.

The decision to adopt Tiled TMX/TSX rather than extend the ASCII format is
`docs/plan.md`'s, taken when the Forge epics were planned. This epic is that
decision carried out, plus the migration of the one map that exists.

## Verified before planning

Every claim `docs/plan.md` makes about the engine here was checked against the
code rather than trusted, the same discipline Epics 12 and 13 used. Seven hold.
One does not, and it moves work from one story to another.

| Claim | Verified |
|---|---|
| The map is a bespoke TOML of `width`, `height` and `rows` of `.` and `#` | True — `tilemap/loader.go:13`, and `mods/map/level1.toml` is 20×15 of exactly that. |
| `LoadMap` skips when any `Tile` entity exists, so the DB becomes the source of truth after first run | True — `loader.go:32` counts `entity_type = 'Tile'` and creates nothing when the count is non-zero. **Note the shape:** it skips *creation* and still rebuilds the grid from the database, so a map edited in Forge does not error, it silently does nothing. |
| The renderer colours by `tile_type` string, `"wall"` → grey 60, everything else → grey 180 | True — `renderer/tilemap_renderer.go:60`. No tileset image is read anywhere. |
| Spawns are hardcoded in Go | True — `ensurePlayerEntity` at (2,2), `ensureGoblinEntity` at (15,12), `ensureGoblinBehavior`, all carrying a `// TODO: replace with a proper scene/level loader`. The plan says `cmd/game/main.go`; they moved to **`cmd/ecs-db/run.go:194–241`** in Epic 10. |
| `schema.json` declares no entity-type `behavior` | True — the field exists on `schema.EntityType` and no type in the file uses it, so the goblin's machine is started by hand. |
| `game.toml` points `[map].path` at `mods/map/level1.toml` | True. |
| `TileGrid` is what pathfinding, line-of-sight and reachability all read | True — `astar.go`, `los.go`, `reach.go` all take a `*TileGrid`. |
| **`TileGrid.Rebuild` derives passability from `'#'`** | **False.** `Rebuild` (`grid.go:48`) reads `comp_tile.passable` from the database and is already property-driven — it has never seen a character. The `'.'`/`'#'` switch is in `LoadMap` (`loader.go:39`), and it goes away with the TOML format rather than needing a story of its own. |

### What that correction changes

The roadmap's third bullet — *"Passability from tile properties: `TileGrid.Rebuild`
stops keying off `'#'`"* — describes work that does not exist. `Rebuild` already
does the right thing. What is actually needed is that the **TSX parser** produce
a per-tile `passable` property and the **loader** write it into `comp_tile`,
which is Stories 2 and 4. That bullet is therefore folded into them rather than
becoming a story, and this epic has seven stories rather than eight.

### The re-import problem is the interesting one

Everything else here is parsing and drawing. The story that decides whether
Forge's MAP mode can work at all is **map re-import**: `LoadMap` today is a
one-time bootstrap, so after the first `ecs-db run` the database is the source
of truth and the file is decoration. Editing a map in Forge would save a file
the engine then ignores — silently, with no error to notice.

That is the same class of problem Epic 13 Story 1 found in the machine round
trip, and it wants the same treatment: settle it early, before anything is built
on top of a format that cannot be re-read.

## Found while building, and not this epic's to fix

**`PRAGMA foreign_keys` and `busy_timeout` are set on one connection out of the
pool.** `sqlite.go` issues them once against `*sql.DB`; both are per-connection
in SQLite and `database/sql` opens more connections on demand. Measured on a
real store:

```
foreign_keys: conn1=1 conn2=0
busy_timeout: conn1=5000 conn2=0
```

So `ON DELETE CASCADE` — declared on every `comp_*.entity_id` and on
`behavior_components` — is enforced on whichever connection happened to be idle
at open time and on no other, and `busy_timeout` is 0 on the rest, which turns
WAL contention into an immediate `SQLITE_BUSY` rather than a wait.
`migration.go` already pins a connection before a table rebuild for exactly this
reason; the knowledge never reached the open path.

Story 3 works around it — `DeleteEntity` deletes component rows explicitly — so
nothing in this epic depends on the cascade. The fix is a DSN `_pragma=` so
every pooled connection carries the setting, it changes behaviour for every
database the engine opens, and it wants its own story with its own tests.

**Fixed in Epic 1 Story 7**, which is where it belonged: Story 3's own
acceptance list already claimed "pragmas at init". The explicit deletes stay,
because they are also what makes `DeleteEntity` correct on a database opened by
something that did not set the pragma.

## Stories

1. TMX/TMJ parser
2. TSX tileset parser
3. Map re-import semantics
4. The loader writes tiles from a parsed map
5. Tileset rendering
6. Object-layer spawns
7. Entity-type `behavior` honoured at spawn, and the migration of `level1`
