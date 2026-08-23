# Story 6: Object-layer spawns

**Epic:** 14 — Tiled map & tileset formats  
**Status:** 🔲 Not started  
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

- [ ] An object layer's objects become entities of the type they name
- [ ] Object position becomes the entity's `Position`, in tiles rather than
      pixels
- [ ] Custom properties become component values, validated by `EntityService`
- [ ] An object naming a type the schema does not have is refused by name and
      position, and the rest still spawn
- [ ] Running twice does not duplicate spawns, and the rule that prevents it is
      stated rather than inherited from the tile bootstrap
- [ ] `ensurePlayerEntity` and `ensureGoblinEntity` are gone, not bypassed
- [ ] `ecs-db run` against the migrated map spawns the player and the goblin
      where the map puts them
- [ ] `go test ./...` passes

## Notes

- Tiled object coordinates are pixels with the origin at the object's bottom-left
  for tile objects and top-left for rectangles. Getting this wrong puts every
  spawn one tile out, which looks like an off-by-one in the map rather than in
  the reader.
- The identity question is the story's real content. A Tiled object id is stable
  in the file; whether the engine should record it against the entity is the
  decision to make and record.
