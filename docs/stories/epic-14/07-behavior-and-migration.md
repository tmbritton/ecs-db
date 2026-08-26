# Story 7: Entity-type behaviour at spawn, and the migration

**Epic:** 14 — Tiled map & tileset formats  
**Status:** ✅ Complete  
**Priority:** High — closes the epic and deletes the last of the bootstrap

**Depends on:** Story 6

## Context

`schema.EntityType` has a `Behavior` field, the architecture doc assumes it, and
no entity type in `schema.json` uses one — so the goblin's machine is started by
hand in `ensureGoblinBehavior`. This story makes the binding real: an entity
spawned as a type that declares a behaviour gets that machine started, and the
hand-written call goes.

It also migrates the one map that exists. `mods/map/level1.toml` becomes
`level1.tmx` with a tileset, a tile layer and an object layer carrying the two
spawns Story 6 now reads, and `game.toml` points at it.

That migration is what makes the epic's claim testable end to end: the goblin
wanders, on a map drawn from a tileset, spawned from an object layer, running a
machine bound in the schema — with nothing about the level in Go.

## Acceptance Criteria

- [x] An entity type declaring `"behavior"` starts that machine when spawned
- [x] A type declaring a behaviour that does not resolve is reported at spawn,
      naming the type and the machine
- [x] `ensureGoblinBehavior` is gone
- [x] `Goblin` declares `"behavior": "goblin"` in `schema.json`
- [x] `mods/map/level1.tmx` and a starter tileset replace `level1.toml`, with
      the same 20×15 layout
- [x] `game.toml` points at the new map
- [x] `ecs-db run` on a fresh database produces the same game it did before this
      epic: the player at the same place, the goblin wandering, pathfinding and
      line-of-sight working against the same walls — the world it builds is
      asserted from the shipped files in `internal/game/project_test.go`; the
      picture on screen is the one thing no test here can see
- [x] Nothing in `cmd/` creates an entity or starts a machine
- [x] `go test ./...` passes

## As Implemented

**`game.SyncBehaviors` — a load-time reconcile, not a create hook.** Every entity
whose type declares a `behavior` gets that machine, unless it is already running
for that entity. `behavior_components` is keyed `(entity_id, machine_id)`, so
"has its type's machine" is a question about one row rather than about the
entity being busy.

It is a choice and not a forced one, and the plan first argued it as forced.
`StartAgent` runs the initial state's entry actions, and the goblin's are
`pickRandomTarget` and `computePath` — actions that must be registered against
the map's `TileGrid` before the loader will even accept the file. That grid
comes from `LoadMap`, and `LoadMap` creates the entities, which reads like a
cycle. It is not one: only `SyncTiles` feeds the grid, so building it between
`SyncTiles` and `SyncSpawns` would leave the registry complete before the first
spawn existed. What decided it is what a reconcile buys instead — an existing
database gets its machines, a process killed between creating an entity and
starting its machine recovers, and nothing below has to learn about machines,
which matters because Forge and any script create entities with no registry at
all.

**A machine's context is checked against the entity type before anything
starts.** Found by review, and the most serious thing in the story:
`StartAgent` seeds `def.Context` by attaching components through the raw storage
port, which validates nothing. Binding a machine with a `GoblinStats` context to
`Player` — a type with no optional components and `allowExtraComponents: false`
— gave a Player a `GoblinStats` row, written by the engine, with nothing said.
Re-importing the map does not catch it, because that revalidates what the
*object* declares and this component came from the machine. Every component in
`ContextManifest` now goes to `world.ValidateAttachComponent`, so a strict type
is refused and a lenient one warns and starts.

The hazard is new. Before this story only the hand-coded goblin ever started a
machine, and `Goblin` declares `GoblinStats` optional, so it could not be
reached.

**The migration.** `level1.tmx` + `starter.tsx` + a generated `starter.png`,
checked cell by cell against the file they replace. The two objects reproduce
`ensurePlayerEntity` and `ensureGoblinEntity` exactly: Player at (2,2) with 10
hp, Goblin at (15,12) with 5. `Sprite.sheet` is deliberately empty —
`animations.toml` maps an entity type to its sheet and stamps it at start-up, so
a spawn that filled it in would quietly override that.

`scripts/starter-tileset/main.go` is the generator, checked in beside the PNG,
because a binary nobody can read a diff of should have the thing that made it in
the repository. The two greys are the ones the renderer filled rectangles with
before Story 5.

**`cmd/` no longer creates an entity or starts a machine.** `ensureGoblinBehavior`
is gone, and the goblin lookup with it — its only purpose was starting that
machine. `findEntityOfType(…, "Player")` stays, because the input handler is
bound to one entity and no map file says which one the keyboard drives. That is
the player-input story's to remove.

**The character format went with the file it described**, along with `readTOML`,
`mapDef`, `tilesOf`, `readMap`'s dispatch and `tiled.LooksLike` — whose only
reason to exist was choosing between two formats, and `Parse` refuses a file
that is neither better than a boolean can. That made the renderer's colour
fallback dead in effect: `Drawable()` is false only for a map with no tilesets,
and such a map imports zero cells, so the colours could only ever have painted
an empty picture.

**`internal/game/project_test.go` loads the files the repository ships**, through
`game.toml`, with the arguments `run` passes. Every other test in this epic runs
against a fixture written for it, and each can pass while the game is broken: a
gid off by one, a tileset that does not resolve, a spawn property the schema
refuses, a binding naming a machine nobody shipped. All of those are one file's
contents and no Go change at all.

Found by review, beyond the context check:

- **The built-in default map path still named `level1.toml`**, a file that no
  longer exists, so a run with no `game.toml` failed on a missing map. Removed
  rather than repointed: those defaults are what a project with no config file
  gets, and such a project has no level either. The same struct also defaults
  `tileSize` to 16, which the new map would have refused anyway.
- **Two doc comments had become false.** `Drawable` still described choosing
  between tilesets and colours, and `stateOf` still said the renderer draws
  anything that is not `"wall"` as floor. `checkShape`'s was overclaiming in the
  other direction — it does not refuse a map that imports as nothing, and should
  not: a Tiled map whose tile layer was deleted really does describe no cells,
  and emptying the world is the file-wins rule working.
- **`rebuild` could no longer fail**, so `NewTilemapRenderer`'s error path and
  `Invalidate`'s log line were unreachable. Both gone; neither linter tag set
  can see dead code behind a build tag.
- **`project_test.go` hardcoded what it claimed to read.** It named the map and
  the behaviours directory directly and compared the tile size against a literal
  32, so reverting `[map]` in `game.toml` — the story's own acceptance criterion
  — left it green. It reads `game.toml` now, and takes `Tick` and
  `TickDurationMs` from where `run` takes them rather than from plausible
  stand-ins.
- **`SELECT id … LIMIT`-shaped assertions hid a duplicate.** `QueryRow` takes the
  first of however many rows there are, so a map that spawned the player twice
  satisfied every assertion after it.
- **Nothing said the binding had done anything.** `run` logged only problems, so
  a start-up where every machine started and one where none did looked identical
  — and "the goblin is not moving" is the symptom of both.

## Notes

- The end-to-end check is the point. Every story before this one can pass while
  the game is broken; this is the one that says the epic worked.
- The old `.toml` loader can go the moment nothing reads it. Leaving it as a
  second way to load a map is a second thing to keep working.
- Epic 15's MAP mode writes this format. Anything left ambiguous here becomes a
  question Forge has to guess at.

### Left undone, deliberately

- **Nothing ever stops a machine**, and that covers two edits. Retargeting a
  binding from `goblin` to `orc` leaves the entity running both, writing the same
  components every tick with the last row SQLite returns winning; *removing* a
  binding leaves the old machine running, which is the edit somebody makes when
  they want a monster to stop wandering. Both are silent. The composite key
  exists precisely so an entity may run machines nothing bound it to — a
  `Burning` component starts one — so from here a machine the schema does not
  name cannot be told from a machine something else started on purpose, and
  guessing deletes live interpreter state. Stopping a machine is a
  schema-migration operation and wants asking for.
- **A machine whose states the file no longer has is not repaired at load.** An
  entity whose `current_states` name a state deleted while the game was closed
  takes the `Running` path untouched; `LoadAgent` then drops the ids it cannot
  find and leaves the entity inert with nothing logged. `agent.Reconciler` exists
  for exactly this and is wired only to the hot-reload watcher, so it never sees
  an edit made between runs. `SyncBehaviors` is the place that could — it holds
  every `(entity, machine)` pair and the definitions — and it needs `agent` to
  export the valid-state set that `ReloadFile` computes privately. That is a
  story about the reconciler, not about bindings.
- **`comp_tile.tile_type` now has no consumer.** The renderer took its colours
  from it until this story deleted them; what is left reading it is `SyncTiles`'
  own diff, deciding whether to write it again. It stays because it is the only
  place a map says what a cell *is* rather than what it looks like, and a game
  rule that wants "is this lava" has nowhere else to ask.
- **`schema.ValidateBehaviorRefs` still has no caller.** This story made
  `EntityType.Behavior` real and did not use it, because `SyncBehaviors` asks the
  loader — which accounts for mod override order and for a file that exists and
  does not parse — where that function stats a path. Its caller is Forge's Epic
  12, which validates a schema being edited rather than one being run.
- **`NewTilemapRenderer` has no test and cannot get one here.** `make test` vets
  the `ebitengine` tag set and never runs it; the new nil-map refusal is covered
  by compilation.

### The battery

33 mutations over `internal/game/behaviors.go`, `schema.json`,
`mods/map/level1.tmx`, `mods/map/starter.tsx` and `internal/tiled/tiled.go`.
Three survived the first pass and two were fixed — a nil-registry guard nothing
asked about, and a tileset naming an image that is not there, which nothing could
see because the image is opened only by the tagged renderer. The third is
accepted: removing `ORDER BY entities.id` survives, because SQLite returns rowid
order for that scan anyway. The test written for it says so, rather than leaving
a reader to think it is load-bearing.
