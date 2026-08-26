# Story 9: A map says which map it is

**Epic:** 14 — Tiled map & tileset formats
**Status:** ✅ Complete
**Priority:** High — renaming a level duplicates its world, and Forge is about to
make renaming a menu item

**Depends on:** Story 8, which made the consequence worse

## Context

`spawns` is keyed `(map, object_id)`, and `map` is the path the engine was
configured with. A path is not an identity. Re-spell it, rename the file, move
the mods directory, and it is a different map:

```
as configured=1  with a ./ prefix=1  absolute=1
total entities: 3
```

Story 8 cleaned the path, which settles `./` and `a/../b` and nothing else. It
also made the rest worse. Under the old create-once rule a renamed map left
duplicate entities; now the duplicates are **unreachable**, because deletion is
scoped to the map being loaded and the old rows are filed under a name nothing
will ever load again:

```
a.tmx renamed to a-renamed.tmx: {Created:1}; entities=3
delete every object from the renamed map: {Deleted:1}; entities=2
```

The author's only gesture — removing the object — cannot reach them. Nothing in
the engine can.

This matters now because Epic 15 puts MAP mode in Forge, where renaming a level
is a menu item and reorganising `mods/` is an afternoon.

## The fix: identity travels in the file

A Tiled map can carry custom map-level properties, and this reader already parses
them. A map declares which map it is:

```xml
<properties>
  <property name="mapId" value="level1"/>
</properties>
```

`spawns.map` becomes that id when the map has one, and the cleaned path when it
does not. Rename the file, move it, spell it differently: same id, same spawns.

**Adoption.** A database keyed by path already exists — every one built before
this story. The first load of a map that has newly gained an id re-keys its rows
from the path to the id, or adding the id would be another rename and spawn the
world twice. Where both exist, the id-keyed rows are the live ones and the
path-keyed entities are duplicates of them, so they go.

**No id is still allowed, and said out loud.** Requiring one would refuse every
map that exists. A map without one is warned about once per load, naming what it
costs, because a silent trap that only springs on rename is exactly the shape of
thing this epic keeps having to go back and fix.

## What this cannot fix

Two maps that share an id share their spawns — copy a `.tmx`, keep the property,
and loading the second updates the first's entities and deletes the ones it does
not have. The engine sees one map at a time and cannot tell that from a rename.
Forge can, across a project, and that is where the check belongs.

## Acceptance Criteria

- [x] A map with a `mapId` keys its spawns by it, not by its path
- [x] Renaming or moving a map with an id changes nothing about its world
- [x] A map with no id keys by its cleaned path, as before
- [x] A map that gains an id adopts the rows it had under its path
- [x] Adoption prefers the id-keyed rows and removes the duplicates
- [x] A map with no id says so, once, naming what it costs
- [x] `go test ./...` passes

## As Implemented

Built as designed. Two things the battery found:

**A map whose id happens to be its path adopted itself.** Adoption reads the rows
under the old key and the new one and treats an object in both as a duplicate to
delete — and when the two keys are the same string, every object is its own
duplicate. The guard against it was there; nothing tested it, and the first test
written for it passed against the bug, because self-adoption deletes the entity
and the ordinary create path immediately makes another. One goes in, one comes
out, and the count is unchanged. What it destroys is the entity's *identity* —
the goblin a machine and every row in `transitions` were pointing at — so that
is what the test asserts.

**`ForgetSpawn`'s key in the delete loop is unobservable**, for the same reason
the call itself was in Story 8: `DeleteEntity` cascades and takes the row before
`ForgetSpawn` is reached. Recorded rather than tested, alongside the other one.

## Notes

- **Forge's MAP mode should write a `mapId` into every map it creates**, so a map
  authored in the editor never has the path-keyed fallback at all. That is Epic
  15's, and the warning here is what makes the gap visible until then.
- **Two maps sharing an id share their spawns**, and the engine cannot tell that
  from a rename because it sees one map at a time. Forge can, across a project.
