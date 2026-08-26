# Story 6: Object-layer spawns

**Epic:** 14 — Tiled map & tileset formats  
**Status:** ✅ Complete  
**Priority:** High — removes the last hardcoded entity

**Depends on:** Stories 1 and 4

## Context

`cmd/ecs-db/run.go` creates the player at (2,2) and the goblin at (15,12) in Go,
under a `// TODO: replace with a proper scene/level loader` that has been there
since Epic 5. Tiled's object layers are that loader: an object carries a
position, a name, a type and its own custom properties, which is exactly an
entity type plus component overrides.

This story reads them and creates entities through `world.EntityService`, which
already validates against the schema — so a spawn naming a type that does not
exist, or overriding a component the type does not allow, is refused by the same
rules that refuse it anywhere else.

Spawning has the same re-import question as tiles, and a harder answer. A tile
is identified by its cell; an **object has no natural identity** beyond a Tiled
object id, and an entity created from one has moved by the time the game is
saved. Creating spawns again on every run would duplicate them; skipping when
any exist is the bootstrap trap Story 3 exists to remove.

## Acceptance Criteria

- [x] An object layer's objects become entities of the type they name
- [x] Object position becomes the entity's `Position`, in tiles rather than
      pixels
- [x] Custom properties become component values, validated by `EntityService`
- [x] An object naming a type the schema does not have is refused by name and
      position, and the rest still spawn
- [x] Running twice does not duplicate spawns, and the rule that prevents it is
      stated rather than inherited from the tile bootstrap
- [x] `ensurePlayerEntity` and `ensureGoblinEntity` are gone, not bypassed
- [x] `ecs-db run` against the migrated map spawns the player and the goblin
      where the map puts them — **met by Story 7**, which migrated the map and
      gave it the object layer. `internal/game/project_test.go` asserts both
      spawns from the shipped files: the Player at (2,2) with 10 hp and the
      Goblin at (15,12) with 5, which is what the deleted `ensure` functions
      created
- [x] `go test ./...` passes

## As Implemented

**Identity: an engine-owned `spawns` table**, keyed `(map, object_id)`, with
`entity_id` on `ON DELETE SET NULL`. Not a component, because every entity type
in a real `schema.json` sets `allowExtraComponents: false` and a `Spawn`
component would have to be declared optional on everything anybody ever spawns.
Not `CASCADE`, because the row is a fact about the *import* and stays true after
the goblin dies — cascading would respawn it next run, which is a level reset
dressed up as a restart.

**Create once. Never update, never delete.** An entity stops being the file's
the moment the game runs. See the implementation plan for the argument, and the
Notes below for where it stops being enough.

Two rules the brief did not ask for and Forge inherits:

- **An object with no class is not a spawn.** Region markers, camera bounds and
  notes are the ordinary contents of an object layer; refusing each of them once
  per start forever is noise, not a diagnostic. To place a spawn, give the object
  a class naming an entity type.
- **A spawnable type must declare `Position`**, and a `Position.*` property is
  refused. The object's coordinates are where the map *shows* the spawn, so a
  property overruling them would put the entity somewhere the author cannot see.

Found by review:

- **A duplicate object id left an orphan and then hid it.** Ids default to zero
  when Tiled's attribute is absent. The second insert collided on the primary
  key and failed the whole load — *having already created an entity that no spawn
  row pointed at*, which the next load duplicated and never mentioned again,
  because the object still looked unspawned. The entity and its record now commit
  in one transaction, and a repeated id is refused by name.
- **`CreateEntity` and the spawn row were two transactions.** Anything between
  them — that collision, a killed process — left the same orphan. `InTx` and
  `CreateEntityInTx` existed for exactly this and were not used.
- **`entity_id` was never read and never asserted.** The column carrying the
  whole `SET NULL` argument was write-only, and the one test touching it checked
  `!Valid`, which a column that is never written satisfies perfectly.
- **`validationLevel: "warning"` created half-built entities silently.** A Goblin
  with no Health and no Sprite, with the two sentences saying so sitting in
  `EntityService.Warnings()`, which is last-call-only. Collected per object now.
- **Pixel→cell truncated instead of flooring**, so an object nudged just off the
  left edge landed *inside* the map at column 0; and nothing was bounds-checked
  at all, so a spawn at (250,-2) on a 20×20 map was stored where the grid cannot
  find it. Both refused now, naming the cell.
- **The tile-object rule only worked for grid-aligned objects.** Subtracting a
  row after dividing is right only on the grid; subtracting a pixel before is
  right either way.
- **Value conversion was too permissive.** `"1"` and `"t"` were booleans, where
  `tiled.Properties.Bool` had already decided the opposite and said why; `NaN`
  and `Inf` were numbers; Go's literal underscores made `"1_000"` into 1000; and
  a JSON column took `"this is not json at all"` verbatim, under a comment
  claiming the insert path would refuse it, which it does not.
- **The map path is a key a human can re-spell.** `LoadMap` cleans it, so a `./`
  prefix is not a second map. Renaming or moving the file still is, and its
  objects spawn again beside the ones already there — recorded on `SyncSpawns`,
  because Forge's MAP mode makes moving a map routine.
- **The "no Player" warning fired with no map configured**, telling the reader to
  add an object to nothing.

And a bug this story found in `schema.PropertyByName`: it matched
case-insensitively and returned only the property, so values were written back
under the map's casing — `maxhp` for a property called `maxHp`, producing a row
with no value for it and a `NOT NULL` failure naming a column nobody had typed.
It returns the canonical key now, as `ComponentByName` always has.

## Notes

- Tiled object coordinates are pixels with the origin at the object's bottom-left
  for tile objects and top-left for rectangles. Getting this wrong puts every
  spawn one tile out, which looks like an off-by-one in the map rather than in
  the reader.
- The identity question is the story's real content. A Tiled object id is stable
  in the file; whether the engine should record it against the entity is the
  decision to make and record.
- **"Never delete" is right for the game and wrong for the editor**, and they
  share this code path. In Epic 15's MAP mode an author will drag a goblin,
  save, and watch the running game not move it; delete one and see it stay;
  fix `Health.maxHp` and see nothing happen. Each is correct by this rule and
  each will read as Forge being broken. What is missing is not a different rule
  but the operation it implies — an explicit re-place that MAP mode calls — and
  the decision about whether that deletes, updates, or spawns beside. That
  belongs to whoever builds MAP mode, and it belongs on this table.
- **The intermediate state is invisible on an existing database.** `ecs-db run`
  keeps working against any `ecs.db` that already has the Player and Goblin rows,
  because `findEntityOfType` finds them. Only a clean database shows the gap, so
  the first person to meet it will be doing something else entirely.
