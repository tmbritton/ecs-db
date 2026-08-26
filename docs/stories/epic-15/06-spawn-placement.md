# Story 6: Spawn placement

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** 🔲 Not started  
**Priority:** High — the half of the map that is not tiles

**Depends on:** Stories 3 and 5

## Context

Drag an entity type out of the `ENTITY TYPES ◂ schema.json` palette and drop it
on the canvas; a spawn appears there. Epic 14 Story 6 built the engine side of
this and Story 8 made it re-import — an object's *class* is its entity type, its
position is where it sits, and the entity it makes is updated, moved or deleted
to match the file on every load.

The type palette is free: `session.Session` already holds the editable schema
and ENTS already renders its types. What is not free is the id.

**An object id is a spawn's identity.** `spawns` is keyed `(map, object_id)`,
so an id handed to a new spawn that an older one once held does not create an
entity — it retargets the old one, moving a live goblin to wherever the new
object sits and taking its `behavior_components` and `transitions` with it.
Ids come from the map's `nextobjectid` and only ever go up, which is why Story 1
had to preserve that counter before this story could exist.

Two engine rules constrain the placement itself, both from Epic 14 Story 6:
a spawnable type must declare `Position`, and a *tile* object is positioned by
its bottom-left corner while everything else is positioned by its top-left.

## Acceptance Criteria

- [ ] The palette lists the schema's entity types, and dragging one onto the
      canvas creates an object with that class at that cell
- [ ] The new object's id comes from `nextobjectid`, which is then incremented;
      no id is ever reused within a map
- [ ] Position comes from where the object sits, and the object is written so
      that the engine's `spawnCell` puts it back in the cell it was dropped on —
      including the tile-object origin rule, if a spawn is ever a tile object
- [ ] A type that does not declare `Position` is not offered as a spawn, with
      the reason shown — the engine refuses it, and offering it would be Forge
      building a map the engine will not load
- [ ] `Tile` is not offered: it is the entity type the tile importer owns, and a
      hand-placed one is a tile the map does not know it has
- [ ] Dragging a placed spawn moves it, and the move keeps its id, because the
      entity it names must survive the edit
- [ ] Deleting a spawn removes the object, and the mode says plainly that this
      deletes the entity on the next load — Epic 14 Story 8's rule, and it is a
      surprise worth spending a sentence on
- [ ] A spawn is drawn on the canvas as the prototype draws it, and clicking one
      selects it into the inspector via the URL
- [ ] Which object group a new spawn lands in is decided and visible, not
      implicit — a map may have several, and a spawn in the wrong one is
      invisible in the layer panel
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/15-spawn-placement.spec.js`.

- [ ] Dragging `Goblin` from the palette onto a cell places a spawn there
- [ ] The placed spawn survives save and reload, in the same cell
- [ ] Placing two spawns gives them different ids, and deleting one and placing
      another does not reuse the deleted id
- [ ] Dragging a placed spawn moves it and its id is unchanged
- [ ] A type with no `Position` is shown as unspawnable with its reason
- [ ] Clicking a spawn selects it, changes the URL, and survives a reload
- [ ] Saving a map with a new goblin and running the engine against it spawns
      that goblin — the criterion `docs/plan.md` states for this epic

## Notes

- **Do not write a `Position.x` property.** `spawnComponents` refuses any
  property that sets `Position`, deliberately: the object's own coordinates are
  where the map *shows* the spawn, and a property that overruled them would put
  the goblin somewhere the author cannot see.
- **Do not fill in `Sprite.sheet`.** `animations.toml` maps an entity type to its
  sheet and stamps it at start-up, so a spawn that filled it in would quietly
  override that — the reasoning `mods/map/level1.tmx` was built on.
- The engine does not watch maps. "Save, then run" is the loop, and the mode
  should say so rather than repeat the prototype's hot-reload caption.
