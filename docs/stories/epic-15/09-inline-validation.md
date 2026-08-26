# Story 9: Inline validation

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** 🔲 Not started  
**Priority:** Medium — closes the epic

**Depends on:** Stories 6 and 7

## Context

Every reason the engine would refuse this map, shown while it is being edited
rather than discovered on the next `ecs-db run`. Epics 12 and 13 both ended with
this story and both were right to: the whole argument for authoring in Forge
instead of a text editor is that Forge knows what the engine will say.

Epic 14 left this epic an explicit inheritance. Story 9 there made spawn identity
a `mapId` map property and recorded what the engine cannot do about it: *"the
engine cannot tell two maps sharing an id from a rename — it sees one map at a
time. Forge can, across a project."* That check exists nowhere else and cannot;
it belongs here.

The refusals are already written, in `internal/tilemap` and `internal/world`,
as sentences meant for a person. Call those, do not restate them.

## Acceptance Criteria

- [ ] A map with no `mapId` is warned about, in the engine's own terms: its
      spawns are filed under its path, and renaming or moving the file will
      spawn them again and leave the originals unreachable
- [ ] **Two maps in one project sharing a `mapId` is an error**, naming both
      files — the check the engine cannot make
- [ ] A duplicate object id within a map is an error naming both objects
- [ ] A spawn whose class is not an entity type is an error
- [ ] A spawn missing a component its type requires is an error, and at
      `validationLevel: "warning"` a warning, matching what the engine will do
- [ ] A property naming no component, or naming `Position`, is reported with the
      engine's message — including the "write it as `Component.name`" hint
- [ ] A gid no tileset holds is reported by cell, distinguishing the three
      cases `tileOf` distinguishes: no tilesets at all, below every first gid,
      and past the end of its own tileset
- [ ] A tileset the map names that will not resolve is reported with the file
      and the reason
- [ ] A layer whose size disagrees with the map's is reported — `checkShape`
      refuses it, and a map that imports as fewer cells than it has deletes the
      difference
- [ ] Every problem is shown against the thing that caused it — a cell, a spawn,
      a layer, the map — not only in a list
- [ ] All problems at once, not the first: a map with three mistakes is fixed in
      one pass or in three, and three is worse
- [ ] Saving a map that will not load is possible and is warned about, because
      the editor is where a broken map gets fixed
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/15-map-validation.spec.js`.

- [ ] A map with no `mapId` shows the warning, and adding one clears it
- [ ] A project with two maps sharing an id names both files
- [ ] A spawn whose class is not an entity type is flagged on the spawn itself
- [ ] Deleting a required component from a spawn flags it and names the
      component
- [ ] A map with an unresolvable gid flags the cell
- [ ] Fixing a problem clears it from the list without a reload
- [ ] Several problems are listed together, not one at a time

## Notes

- **The messages are the engine's.** `tilemap` and `world` already write
  refusals as sentences aimed at an author. Reworded copies drift, and the day
  they drift is the day someone reads two different explanations of one
  failure.
- The duplicate-`mapId` check needs every map in the project parsed, which
  Story 2's discovery already produces. It is the one check here that costs
  something to run, and it is also the one that cannot be made anywhere else.
