# Story 7 — implementation plan: entity-type behaviour at spawn, and the migration

**Story:** [`07-behavior-and-migration.md`](07-behavior-and-migration.md)

## What the code says

**The story's claims check out.** `schema.EntityType.Behavior` exists
(`types.go`), no entity type in `schema.json` sets it, and the goblin's machine
is started by `ensureGoblinBehavior` in `cmd/ecs-db/run.go` — the last thing in
`cmd/` that knows an entity by name.

**`schema.ValidateBehaviorRefs` is not the check this story needs.** It stats
`<behaviorsDir>/<name>.json` and still has no caller anywhere. It answers "is
there a file", where the question at start-up is "did the loader end up with
this machine", which accounts for mod override order and for a file that exists
and does not parse. Left alone; Forge's Epic 12 is where a *file* check belongs.

**`ensureGoblinBehavior` is already a reconcile, not a bootstrap.** It counts
`behavior_components` rows for `(entity, 'goblin')` and starts the machine only
when there are none. That shape is right and is the half worth keeping — what is
wrong with it is that the machine id is a Go string literal and the entity is
found by hardcoded type.

**`behavior_components` is keyed `(entity_id, machine_id)`,** so an entity may
run several machines at once. "Has its type's machine" is therefore a question
about one row, not about the entity being busy.

**Entity types are matched exactly.** `ValidateEntityCreation` looks the type up
with `s.EntityTypes[name]` and refuses anything it does not find, so
`entities.entity_type` always holds a name the schema declares, spelled as the
schema spells it. Case folding is not needed here and would invent a second
matching rule for the same string.

## Where the machine gets started, and why not at creation

The obvious home is `EntityService.CreateEntityInTx`: every creator would honour
the binding, and the machine would commit with the entity that needs it.

> **Corrected after review.** This section claimed the placement was forced. It
> is not: only `SyncTiles` feeds the grid, so building it between `SyncTiles`
> and `SyncSpawns` would leave the registry complete before the first spawn
> existed. The reconcile is still the right answer, for the three reasons below
> plus a fourth — a create hook puts an `agent` dependency into `world` or
> `tilemap`, and Forge and any script create entities with no registry at all.
> The shipped doc comment says so; this paragraph is left as it was written.

**It cannot go there, and the reason is an ordering fact rather than a
preference.** `StartAgent` runs the initial state's entry actions, and the
goblin's are `pickRandomTarget` and `computePath` — actions registered by
`builtins.RegisterPathfinding(registry, grid)`, which needs the `TileGrid`.
The grid comes from `LoadMap`, and `LoadMap` is what creates the entities. A
create-time start would run the goblin's first path search against a registry
that does not have pathfinding in it yet.

So the binding is honoured **once per load, over the entities that exist**, after
the map has been read and the registry built — which is exactly where
`ensureGoblinBehavior` already sits, and is the only point in start-up where
both the grid and the loader are real.

Making it a reconcile rather than a create hook buys three things a create hook
does not:

- **An existing database works.** A goblin spawned before its type declared a
  behaviour gets its machine on the next run rather than never.
- **A crash between the two is recoverable.** With the start inside the create
  transaction, a process killed after commit and before start leaves an entity
  the spawn table calls done and no machine will ever be started for.
- **It is the epic's own shape.** `SyncTiles`, `SyncSpawns`, and now
  `SyncBehaviors`: read what the files say, compare against the world, act on
  the difference.

What it does not do, and the doc comment will say so: **it never stops a
machine.** A type whose `behavior` changes from `goblin` to `orc` leaves the
entity running both. Stopping one is a schema-migration question — an entity may
legitimately run machines nothing bound it to, which is what the composite key
is for — and inventing an answer here would delete live interpreter state on the
strength of a guess about an edit.

### Where it lives

`internal/game`, beside `PlayerInputHandler`. It needs `*sql.DB`, the schema,
the loader, the registry and the storage adapters at once, and `game` is the
only existing package that may import all of them: `agent` cannot import
`storage` (storage imports agent), and putting a function that starts state
machines in `storage` names it after the wrong thing.

```go
type BehaviorSync struct {
    DB             *sql.DB
    Schema         schema.DatabaseSchema
    Loader         *agent.Loader
    Registry       *agent.Registry
    Tick           int64
    TickDurationMs int64
}

type BehaviorResult struct {
    Started  int
    Running  int      // already had it; nothing was written
    Problems []string // one sentence per binding that could not be honoured
}

func SyncBehaviors(ctx context.Context, p BehaviorSync) (BehaviorResult, error)
```

**One transaction per entity**, which is `createSpawn`'s reason rather than
`SyncTiles`': entry actions are arbitrary author-supplied code, and one goblin
whose `computePath` fails must not stop the other nineteen from starting.

**A machine that does not resolve is reported once per type**, naming the type
and the machine, not once per entity — twenty goblins should produce one
sentence, and the fault is in the schema, which has one line for it.

## The migration

`mods/map/level1.toml` is 20×15 characters at `window.tileSize = 32`, which is
the 640×480 window exactly. It becomes:

| File | What |
|---|---|
| `mods/map/starter.png` | 64×32, two 32px tiles: floor then wall |
| `mods/map/starter.tsx` | the sheet, with `passable` and a class per tile |
| `mods/map/level1.tmx` | `mapId=level1`, one tile layer, one object layer |

**The image is generated, not drawn.** A checked-in binary nobody can diff wants
to be reproducible, so `scripts/` gets the generator that made it and the
tileset carries the same two greys the renderer used to fill rectangles with —
60 for wall, 180 for floor — so the migrated map looks like the map it replaces
rather than like a new one.

**The two spawns reproduce the deleted `ensure` functions exactly**: Player at
(2,2) with `hp`/`maxHp` 10 and animation `player_idle`, Goblin at (15,12) with 5
and `goblin_idle`. Both are plain rectangle objects, so their `x`/`y` are
top-left and a cell is `x*32`, `y*32`. Every component the type requires is
named, because `allowExtraComponents: false` and `validationLevel: "strict"`
means a spawn missing one is refused.

`Goblin` gains `"behavior": "goblin"` in `schema.json`. No `schemaVersion` bump:
entity types generate no DDL, so there is nothing for a migration to do.

## The character format goes with the file

The story's own note: *"The old `.toml` loader can go the moment nothing reads
it. Leaving it as a second way to load a map is a second thing to keep
working."* With `level1.toml` deleted, nothing reads it.

`readTOML`, `mapDef`, `mapDef.check` and `tilesOf` are deleted, along with
`readMap`'s dispatch — `readTiled` becomes the only reader, and a file that is
not a Tiled map is refused by name. `tilemap` drops its `BurntSushi/toml`
import. `loader_test.go`'s character-format tests go with the code they test;
the ones that are really about `SyncTiles` or about `LoadMap`'s wiring are
rewritten against a Tiled fixture rather than deleted.

`tiled.LooksLike` stays: it is what tells TMX from TMJ, which is still two
formats.

## What `cmd/ecs-db/run.go` ends up as

- `ensureGoblinBehavior` deleted.
- The `findEntityOfType(ctx, db, "Goblin")` lookup deleted with it — the goblin
  id existed only to start its machine, and the schema says that now.
- `findEntityOfType(ctx, db, "Player")` stays. The input handler still names its
  entity by type, and that is the player-input story, not this one.
- `game.SyncBehaviors` called after `loader.ScanDir`, its problems logged.

That leaves nothing in `cmd/` that creates an entity or starts a machine.

## Tests

`internal/game/behaviors_test.go`, against a real in-memory SQLite with the
interpreter tables and a hand-built machine, because that is what
`input_handler_test.go` and `agent`'s integration tests already do:

1. An entity of a bound type gets its machine — `behavior_components` holds the
   row and `current_states` is the machine's initial state.
2. Running it twice starts nothing the second time: `Running`, not `Started`,
   and `updated_at` is unchanged (a re-entry would reset a machine mid-flight).
3. An entity of an unbound type is left alone.
4. A type binding a machine the loader does not have is reported once, naming
   both, and the other type's entities still start.
5. An entity already running a *different* machine still gets its type's one —
   the composite key, not "has any behaviour".
6. One entity's failing entry action does not stop the next entity, and is
   reported.
7. Nothing is half-written when an entry action fails: no `behavior_components`
   row for the entity that failed.

`internal/tilemap`: the character-format tests are replaced; `LoadMap` against a
`.tmx` fixture with tiles and objects is already covered by Stories 4 and 6 and
gains the "a file that is not a Tiled map is refused" case.

**End to end**, which the story says is the point: `ecs-db run` on a fresh
database, on a machine with a display.
