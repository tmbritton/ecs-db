# Story 3: Occupant-aware traversal

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** 🔲 Planned; follows entity-backed tile rendering
**Priority:** High — TILES must not author the wrong movement contract

## Confirmed product requirements

- The first map is a simple grid. **No polygon collision authoring** is needed.
- A tile is a grid cell and its artwork, **not** the wall or water itself. A
  wall is a separate entity placed at a cell; water or a river is an entity
  occupying a set of cells. Traversal depends on the moving entity's
  components and the components of **every relevant entity occupying the
  destination**.
- A flying entity can cross a wall or river, a swimming entity can cross water,
  and a phasing/ghost entity can cross a wall. A normal walker does not gain
  those abilities because a tile has a single `passable` flag.
- `Passability` and `Visibility` are **independent components** attachable to
  any entity, with taxonomy/enum values rather than universal Boolean flags.
  Another entity's independent capabilities — Flying, Phased and NightVision,
  for example — determine its own result. Capabilities are components,
  **not values of one fixed movement/vision enum**. A person sees
  across a river they cannot traverse; a phased Kitty Pryde crosses a wall
  she still cannot see through.
- TILES must author only a rule the engine really uses. Story 1's TSX writer
  tested changing an existing `passable` property as a byte-fidelity example;
  it does not commit the editor to offering that old Boolean as gameplay.

## Current code that must change together

`schema.json` declares a `Tile` component with `x`, `y`, `passable` and
`tile_type`. `tilemap.stateOf` reads an optional per-tile/tileset `passable`
property, defaults to true and `SyncTiles` writes that Boolean into
`comp_tile`. `TileGrid.Rebuild` caches it. `PlayerInputHandler`, `AStar` and
`ReachableTiles` call `TileGrid.IsPassable` without a moving entity. The
`computePath` action calls that A*, and `stepAlongPath` writes Position without
checking the next tile again. `moveToward` also changes Position directly.
`setTilePassable` mutates the Boolean in both grid and database.

`SyncSpawns` already imports typed TMX objects as entities, but `spawnCell`
uses only the object's anchor cell. No component or index records that one
river occupies multiple cells. The new occupancy model must account for
movement/removal of those entities at runtime, not merely at map import.

`LineOfSight` uses the **same** tile Boolean. It cannot remain the sight rule:
a flyer crossing a wall does not thereby see through it. The observer's
independent components (for example NightVision) must be evaluated against a
`Visibility` taxonomy on occupants, independently of movement. The current
schema supports string and array fields but has **no enum constraint** or
declared category-to-capability rule, so Story 3 needs an explicit validated
vocabulary and interaction policy.

Story 2 first makes every authored layer tile an entity and renders it from
database component state. `Game.Draw` still renders an occupant with Sprite
only at its Position; a moving multi-cell river would otherwise move its
*restriction* while leaving its picture behind. If the occupant has a visual
component, render that **one entity across its footprint**, without cloning
its entity ID. Separate Tile entities may provide background artwork, but
they must not be the only picture of a river that can move.

## Initial rule — confirmed

The mover has independent capability components (Walking, Swimming, Flying,
Phased), not a single shared `MovementModes` enum. An occupant's
`Passability` component declares a validated interaction category (for
example wall-like solid or water-like liquid); **any entity** can carry it,
including a creature or door. Schema-declared rules compare that category
with the mover's actual components. Entities without Passability do not
restrict movement. For every destination cell, gather its occupying entities;
the mover may enter only if **each restricting occupant** permits it. One
blocker vetoes entry. Exclude the moving entity itself from the restrictions;
for a multi-cell mover, check every cell in its destination footprint. The
rule never switches on `tile_type` or an entity's class name and never reads a
Boolean on the art tile.

| Occupying entity | Walk | Swim | Fly | Phase |
|---|---:|---:|---:|---:|
| No restricting occupant | yes | yes | yes | yes |
| Wall | no | no | yes | yes |
| Water/river | no | yes | yes | no |
| Wall **and** water overlapping | no | no | yes | no |

A walker with Phase crosses a wall but not water; a swimmer crosses water; a
flyer crosses both. Out-of-bounds remains blocked. An **in-bounds cell with no
art Tile entity is enterable** if no occupying entity restricts the mover:
art does not decide movement.

An occupant needs a grid footprint. A single wall occupies its Position cell.
For water/river, use an `OccupiedCells` component listing cell offsets
relative to its Position, so **one entity** occupies an irregular set of cells
without polygons. Offsets are unique integer grid coordinates; `(0,0)` is the
default for a single-cell entity. TMX custom properties can store this array;
the MAP editor in Story 4 may paint that set of cells directly. Moving the entity
moves its footprint without changing its identity. Off-map cells are refused
before an import writes anything.

Initial component shape proposed for code and tests: mover capabilities are
separate components such as Flying, Swimming and Phased; `Passability.kind` on
**any** occupant and `Visibility.kind` on **any** sight-affecting occupant are
schema-validated taxonomy fields. A component-to-taxonomy policy declares
what Flying or Phased permits for a solid, and what NightVision or another
observer ability permits for darkness or opacity. `OccupiedCells` lists
offsets for a multi-cell occupant. A missing Passability means nonblocking;
a missing Visibility does not occlude sight. Unknown categories or ability
references are loud errors, not implicit passable/transparent defaults. A wall
can admit flying/phasing but block ordinary sight; water can admit
swimming/flying and allow ordinary sight. No vision ability is assumed from a
movement ability.

Here `Visibility` is the occupant's effect on a sight ray, **not** whether its
sprite is drawn. NightVision is an observer component that may alter a
darkness-category sight interaction; it should not make opaque walls clear.
DetectsMagic's effect on hidden magical targets or magical obscurants belongs
to a **later detection feature**; Story 3 must not guess either meaning from a
generic visibility Boolean.

## Acceptance criteria

- [ ] One tested traversal predicate compares mover components to **every
      restricting occupant entity** of a destination. All combinations above,
      overlapping occupants, bounds and empty-art-cell behavior are explicit;
      a multi-cell mover checks its destination footprint without blocking
      itself.
- [ ] TMX object-layer import can create one wall or water entity with its
      authored traversal restriction and occupancy footprint; deleting,
      moving or re-importing it updates the occupied-cell index without
      retargeting another entity. The same occupant is indexed once in every
      cell it covers. Runtime-only components survive re-import.
- [ ] Decorative Tile entities remain artwork/cell identity, not implicit
      collision blockers; no `tile_type == "wall"` switch or `Tile.passable`
      shortcut.
- [ ] Player input, A*, reachable cells, `computePath`, `stepAlongPath` and
      direct `moveToward` movement either consult the same traversal decision
      at the right time or are explicitly named as an intentional bypass.
- [ ] `LineOfSight` checks sight-blocking **occupant entities** independently;
      the observer's independent components decide how its ray interacts with
      each Visibility category. A flyer or phased entity crossing a wall does not see through
      it by virtue of its movement ability. River cells are traversable only
      for the right mover but transparent to normal sight. A wall can itself
      be seen even when it blocks sight to an entity behind it.
- [ ] Passability and Visibility attach to any entity type. Their taxonomy
      labels and component-to-category rules are validated against
      `schema.json` rather than accepted as arbitrary strings, including
      TMX-authored properties and referenced capability component names.
- [ ] A multi-cell occupant with visual data draws across its occupied cells
      as one entity; changing Position or footprint moves both its traversal
      effect and its picture. Invisible occupants are explicit, not an
      accidental result of the renderer only drawing at the anchor.
- [ ] Replace or retire `setTilePassable` without leaving a builtin that writes
      a value no movement code reads. Update bundled maps, schema, behaviours,
      Tiled tilesets and fixture data together; there is no old map corpus to
      migrate.
- [ ] TILES shows tile art/type metadata, while MAP's object/occupant editor
      owns wall and water traversal/footprints. `go test ./...`, the tagged
      build and renderer smoke tests pass.

## Test-first sequence

Write the traversal matrix as table-driven tests over a mover and the entities
occupying one cell, including two overlapping restrictions and no occupant.
Test an irregular set of occupied grid cells on a river and an anchor-cell wall;
then import those actual entities from a TMX object group. Test A*,
reachability, player keys and path stepping against **the same small map**
with a walker, swimmer, flyer and phasing mover. Test sight through and movement
across a wall independently. Finally test moving/removing a multi-cell entity
and re-import of authored versus runtime-only components, including a tagged
renderer test of its visual footprint. MAP's occupant editing and TILES' read
surface get their own Playwright steps later.
