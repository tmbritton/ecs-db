# Story 6 — implementation plan: object-layer spawns

**Story:** [`06-object-layer-spawns.md`](06-object-layer-spawns.md)

## What the code says

**The hardcoded bootstrap is as described.** `ensurePlayerEntity` creates a
Player at (2,2) and `ensureGoblinEntity` a Goblin at (15,12), each under a
`// TODO: prototype bootstrap` from Epic 5. Both look the entity up first and
create only when absent, so the *lookup* half is the part worth keeping.

**Two things downstream need those ids.** `playerID` builds the input handler;
`goblinID` starts the goblin's machine. Both become "find the entity of this
type after the map has spawned", which is the query the ensure functions already
open with.

**Validation is per component, not per property.** `ValidateEntityCreation`
checks that the type exists, that every named component is declared, that every
*required* component is present, and that extras are allowed. Every entity type
in `schema.json` has `allowExtraComponents: false`, and Player and Goblin each
require `Position`, `Health` and `Sprite`. So a spawn must carry all three or be
refused — the map has to say what the entity is, which is the point.

**`tiled.Object` already carries everything.** `ID`, `Name`, `Type` (read from
both the `type` and `class` attributes), `X`/`Y` in pixels, `GID`, `Properties`,
and a `Template` that is deliberately unresolved. The type's own doc comment
already warns that a tile object is positioned by its bottom-left corner and
everything else by its top-left, and names this story as the one that will get
it wrong.

## The identity decision, which is the story's real content

An object has no natural identity: the entity it becomes has moved, taken damage
or died by the time anything re-reads the map. So re-import cannot match by
position the way `SyncTiles` does, and needs something recorded.

**An engine-owned `spawns` table**, alongside `behavior_components`:

```sql
CREATE TABLE IF NOT EXISTS spawns (
    map       TEXT NOT NULL,
    object_id INTEGER NOT NULL,
    entity_id INTEGER REFERENCES entities(id) ON DELETE SET NULL,
    PRIMARY KEY (map, object_id)
)
```

**Not a component.** Every entity type in `schema.json` sets
`allowExtraComponents: false`, so a `Spawn` component would have to be declared
optional on Player, on Goblin and on everything anybody ever spawns — the
engine's bookkeeping written into the author's file, in a place where forgetting
it is a refused spawn. `behavior_components` records per-entity engine state in
an engine table for the same reason.

**`ON DELETE SET NULL`, not `CASCADE`.** The row is a fact about the *import* —
"this object has been spawned" — and stays true after the goblin dies. Cascading
would delete the row and respawn the goblin on the next run, which is a level
reset dressed up as a restart. The null says the spawn happened and its entity
is gone, which is exactly what is true.

**Create once. Never update, never delete.** An entity created from a spawn
stops being the file's the moment the game runs. The file describes where a
world *starts*, not what it currently is, so a spawn moved in the map does not
teleport a goblin that has walked away, and a spawn deleted from the map does not
kill one. That is deliberately not `SyncTiles`' rule — a tile is its cell and has
no history — and the difference is the reason this story exists rather than
reusing that one. Wiping spawned entities to re-place them is a *reset*, which is
a different operation and not one anything asks for yet.

## The rest of the design

**Position.** `x/TileWidth`, `y/TileHeight`, floored. A tile object's `Y` is its
**bottom** edge, so its row is one less. Rectangles and points are top-left.

**Properties become component values, addressed as `Component.property`.** A
Player spawn carries `Health.hp`, `Health.maxHp`, `Sprite.animation`,
`Sprite.sheet`, `Sprite.flip_x`; `Position` comes from the object's own
coordinates and is not written as a property.

A property whose name has no dot is **refused**, naming the object and the
property. An author who writes `hp` meaning `Health.hp` otherwise gets silence,
and silence is the failure this epic keeps finding; Tiled has map-level and
layer-level properties for anything that is genuinely editor metadata.

**Values are converted against the schema, not against Tiled.** Tiled's property
types and this engine's are different vocabularies, and the schema is the one
that decides what a column holds — so `Health.hp` is parsed as an integer
because the schema says `hp` is an integer, whatever Tiled recorded it as.

**One bad spawn does not stop the others.** Each refusal names the object — by
id, by name if it has one, and by cell — and import continues. A map with a typo
in one goblin should load nineteen goblins and one complaint, not nothing.

## What this story cannot finish

Its own last-but-one criterion — "`ecs-db run` against the migrated map spawns
the player and the goblin where the map puts them" — needs a migrated map, and
`mods/map/level1.toml` becomes `level1.tmx` in **Story 7**. Until then the map
the engine loads has no object layer, so:

- `ensurePlayerEntity` and `ensureGoblinEntity` go, as the criteria require, and
  their lookup halves survive as one `findEntityOfType`;
- with the character-format map there is no Player, so no input handler, and no
  Goblin, so no machine — the game draws a map nobody is standing on.

That is the honest intermediate state, and Story 7 closes it in one step. Nothing
in the test suite covers `ecs-db run`'s bootstrap, so nothing breaks; the demo is
what waits.

## Work

1. `storage.EnsureInterpreterTables` gains `spawns`.
2. `internal/tilemap/spawn.go` — `SyncSpawns(ctx, svc, db, mapPath, *tiled.Map)`,
   plus the object→components conversion, all testable.
3. `LoadMap` calls it, or `run.go` does — decided by whether a caller can want
   tiles without spawns. It cannot: they are one file.
4. `run.go` — the two ensures go, `findEntityOfType` replaces their lookups.

## Verification

- `go test ./...`, `-count=2`, `-race`, `make lint` (both tag sets)
- Mutation battery over the spawner and the conversion
- Fresh-context review before committing
- `make build` and `make build-headless`
