# Story 9: Inline validation

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** ✅ Complete
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

- [x] A map with no `mapId` is warned about, in the engine's own terms: its
      spawns are filed under its path, and renaming or moving the file will
      spawn them again and leave the originals unreachable
- [x] **Two maps in one project sharing a `mapId` is an error**, naming both
      files — the check the engine cannot make
- [x] A duplicate object id within a map is an error naming both objects
- [x] A spawn whose class is not an entity type is an error
- [x] A spawn missing a component its type requires is an error, and at
      `validationLevel: "warning"` a warning, matching what the engine will do
- [x] A property naming no component, or naming `Position`, is reported with the
      engine's message — including the "write it as `Component.name`" hint
- [x] A gid no tileset holds is reported by cell, distinguishing the three
      cases `tileOf` distinguishes: no tilesets at all, below every first gid,
      and past the end of its own tileset
- [x] A tileset the map names that will not resolve is reported with the file
      and the reason
- [x] A layer whose size disagrees with the map's is reported — `checkShape`
      refuses it, and a map that imports as fewer cells than it has deletes the
      difference
- [x] Missing, nonpositive or duplicate tile-layer IDs mark the affected rows
      and explain why Forge cannot safely reorder or delete those layers
- [x] Every problem is shown against the thing that caused it — a cell, a spawn,
      a layer, the map — not only in a list
- [x] All problems at once, not the first: a map with three mistakes is fixed in
      one pass or in three, and three is worse
- [x] Saving a map that will not load is possible and is warned about, because
      the editor is where a broken map gets fixed
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/15-map-validation.spec.js`.

- [x] A map with no `mapId` shows the warning, and adding one clears it
- [x] A project with two maps sharing an id names both files
- [x] A spawn whose class is not an entity type is flagged on the spawn itself
- [x] A hand-authored spawn missing a required component is flagged and names
      that component. Required components cannot be detached in Forge; repairing
      the spawn clears the mark and the list without a page reload
- [x] A map with an unresolvable gid flags the cell
- [x] Fixing a problem clears it from the list without a reload
- [x] Several problems are listed together, not one at a time

## Notes

- `tiled.Parse` itself refuses a malformed layer shape before a document can
  open. That parser refusal already appears under the filename and offending
  layer name in the MAP project-problems panel. An in-memory map that gets as
  far as the engine's `checkShape` is also covered by an owner-keyed layer
  issue; there is no editable layer row for a file the parser cannot open.
- Preview resolves tilesets independently, retaining valid cells and spawns
  when another tileset is missing. The saved TMX is still allowed to be broken:
  `Save` changes the file, not a running game's map.
- Tile findings follow the engine's topmost non-empty layer rule. Duplicate-ID
  spawn markers cannot be selected or dragged; an ID-only deep link reports
  the ambiguity even if one claimant is untyped, and the TMX mutators refuse
  to move/delete either claimant. Invalid XML characters in a new mapId are
  refused before a working file is changed.
- Layer-ID findings are **Forge edit-safety checks**, not engine load refusals:
  a freshly generated or hand-edited map can still paint, but an index-changing
  edit requires unique positive IDs. Both claimants of a duplicate are marked.
- Verified: `make test`, both lint tag sets, both builds, and `make e2e` (297
  passing browser checks). Four working maps with 50×50 cells validated in
  ~6.1 ms per benchmark iteration. Statement coverage: tilemap 91.6%, tiled
  94.3%, Forge maps 88.8%, mapvalidation 95.5%, server 86.8%, mode templates
  70.3%.

- **The messages are the engine's.** `tilemap` and `world` already write
  refusals as sentences aimed at an author. Reworded copies drift, and the day
  they drift is the day someone reads two different explanations of one
  failure.
- The duplicate-`mapId` check needs every map in the project parsed, which
  Story 2's discovery already produces. It is the one check here that costs
  something to run, and it is also the one that cannot be made anywhere else.
