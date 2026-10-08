# Story 3: Occupant-aware traversal

**Epic:** 16 — Forge: TILES and SPRT modes
**Status:** ✅ Implemented
**Priority:** High — TILES must not author the wrong movement contract

## Confirmed product requirements

- The first map is a simple grid. **No polygon collision authoring** is needed.
- A tile is a grid cell and its artwork, **not** the wall or water itself. A
  wall is a separate entity **referenced by the Tile entity**. A Tile has
  Position and any number of entity references. One River entity may be
  referenced by Tiles at irregular cells. Those referencing Tiles' Positions
  define where Wall/River interactions apply; traversal considers every
  referenced occupant at the destination. An unreferenced runtime entity may
  instead occupy its own Position/OccupiedCells footprint.
- The Tile does **not** own the artwork component. Each referenced entity may
  supply TileVisual art, Passability/Visibility rules, or both. The renderer
  places referenced visuals at the Tile's Position in layer order. Several
  references can contribute visuals or restrictions at one cell.
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

## Earlier code this story replaced

Previously, `Tile.passable` and `SyncTiles` wrote and cached one Boolean per
cell. `PlayerInputHandler`, A*, reachability, and line of sight used it without
a mover or observer. `stepAlongPath` and `moveTowardTarget` could cross an
occupant without rechecking. The old writer, grid Boolean, builtin and tests
were retired with this story.

`SyncSpawns` already imported typed TMX objects at their anchor cell. Now an
optional `OccupiedCells` array extends an entity's footprint with explicit
grid offsets. Live reads reflect moves and deletions without a stale index.

The former `LineOfSight` used the tile Boolean. Sight now reads occupants'
`Visibility.kind` and the observer's independent capabilities, unrelated to
movement. `schema.json` has an `interactions` section for validated categories
and references to schema-declared boolean capability components.

Story 2 made every authored layer tile a database-backed art entity. This
story also changed `Game.Draw` so one occupant with Sprite draws that frame at
each of its occupied cells under one entity ID. A moving river moves both its
restriction and its own visual, without cloning the entity.

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

A referenced occupant's footprint is the set of Positions of Tiles that
reference it. Several Tiles may reference the **same** River ID, giving it an
irregular multi-cell footprint without a polygon or one River per cell.
Moving/relinking Tiles changes both the placed art and that River's occupancy.
`OccupiedCells` offsets remain available for **unreferenced** positioned
runtime entities. An entity referenced by Tiles uses its referencing Tiles'
cells instead of also contributing its own Position/OccupiedCells, avoiding a
phantom second footprint. Off-map placements are refused before import writes.

Implemented component shape: mover capabilities are
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

- [x] One tested traversal predicate compares mover components to **every
      restricting occupant entity** of a destination. All combinations above,
      Tile references, overlapping occupants, bounds and empty-art-cell behavior are explicit;
      a multi-cell mover checks its destination footprint without blocking
      itself.
- [x] Each imported Tile owns Position and a schema-declared collection of
      entity references. A Wall may be referenced by one Tile and one River by
      several irregularly placed Tiles, without cloning either referenced
      entity. Re-import preserves IDs/links and runtime-only components where
      their authored owners remain; a changed reference updates occupancy.
- [x] Decorative Tile entities remain artwork/cell identity, not implicit
      collision blockers. Their linked entities supply restrictions; no
      `tile_type == "wall"` switch or `Tile.passable` shortcut.
- [x] Player input, A*, reachable cells, `computePath`, `stepAlongPath` and
      direct `moveToward` movement either consult the same traversal decision
      at the right time or are explicitly named as an intentional bypass.
- [x] The `inLineOfSight` guard checks sight-blocking **referenced entities** independently;
      the observer's independent components decide how its ray interacts with
      each Visibility category. A flyer or phased entity crossing a wall does not see through
      it by virtue of its movement ability. River cells are traversable only
      for the right mover but transparent to normal sight. A wall can itself
      be seen even when it blocks sight to an entity behind it.
- [x] Passability and Visibility attach to any entity type. Their taxonomy
      labels and component-to-category rules are validated against
      `schema.json` rather than accepted as arbitrary strings, including
      TMX-authored properties and referenced capability component names.
- [x] A linked multi-cell River's visual belongs to the referenced entity and
      draws at every referencing Tile's Position under one River ID. A Tile
      with a separate art-only reference draws that entity's visual too.
      Moving or relinking Tiles updates both picture and traversal. A
      standalone runtime entity with OccupiedCells still draws its own Sprite
      across that footprint. Unrendered restrictions are explicit.
- [x] Replace or retire `setTilePassable` without leaving a builtin that writes
      a value no movement code reads. Update bundled maps, schema, behaviours,
      Tiled tilesets and fixture data together; there is no old map corpus to
      migrate.
- [x] TILES' reference/type template and MAP's placed-Tile reference editing
      have a compatible import contract. No tile Boolean or collision polygon
      authoring. `go test ./...`, tagged builds and browser checks pass.

## Earlier implementation checkpoint (before Tile-reference correction)

Before the Tile-reference correction, the starter map authored 82 separate
Wall objects; it now paints Wall templates that create referenced entities.
Player and Goblin explicitly receive the example Walking component. One River fixture imports an
irregular occupied-cell set, moves it and re-imports it without changing its
ID or detaching runtime components. Schema version 5 persists interaction
rules and example ability declarations; another boolean capability can be
added and referenced without changing engine code. MAP's visual authoring
controls and TILES' art editing remain their later stories.

The direct-Position implementation passed `make test`, both lint tag sets,
both builds and 300 Playwright checks. Those results are a regression baseline;
the corrected Tile-reference model also passes `make test`, tagged renderer
tests, both lint tag sets, both builds and 300 Playwright checks. The
`internal/tilemap` package has 88.0% statement coverage; `internal/schema`
has 92.2%.

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
