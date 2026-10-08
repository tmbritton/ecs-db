# Story 3 — Occupant-aware traversal: implementation plan

## Verified seams before implementation

- Story 2 gives every authored nonempty layer tile an entity and moves tile
  rendering onto the database. Its interim topmost `Tile.passable` projection
  keeps the old game running **only until this story**; it must not become an
  editor contract. Tile art and restricting occupants remain different
  entities even when they share a cell.
- `TileGrid.IsPassable`, `AStar`, `ReachableTiles` and `PlayerInputHandler` do
  not take mover identity. `computePath` calls that A*; `stepAlongPath` writes
  its waypoint without checking a newly arrived blocker, and `moveToward`
  writes Position directly. A change to A* alone leaves other movers bypassing
  the rule.
- `LineOfSight` calls `IsPassable`, conflating optics with movement. It needs
  `Visibility` on sight-affecting occupants and independent observer
  components such as NightVision, unrelated to Flying or Phased.
- `SyncSpawns` maps typed TMX objects to persistent entities keyed by
  `(mapId,objectID)`, but `spawnCell` currently records only their anchor.
  A single river must own many relative cells without being cloned into many
  entities or losing its ID when moved. The same occupied-cell view must
  update when Position/footprint/components change at runtime.
- `Game.Draw` currently renders one Sprite frame at Position. A river with a
  footprint would affect many cells but draw once at its anchor. An occupant
  with a visual component needs a footprint-aware drawing projection over
  its one entity ID; separate Tile entities may supply background art but
  cannot be the only visual source for a region that moves.
- Schema supports array components for OccupiedCells, but **no presence-only
  tag component type**. An initial Walking/Flying/Phased ability should be a
  real declared component, not a magic string or a required array seeded as
  `[]` that silently makes a newly placed Player immobile. Choose and test a
  deliberate starting ability at placement. Validate occupied-cell offsets
  beyond mere JSON syntax before import can commit.
- `schema.Property` has Type, nested Properties and Items, but **no allowed
  values or enum constraint**. Passability/Visibility categories cannot be
  arbitrary strings. Their component-to-category rules must refer to
  components the schema actually declares, not one fixed perception enum.
  Add a schema-declared validated vocabulary/rule representation and test
  JSON/TMX import and persistence. A typo must not silently turn an occupant
  passable or transparent.
- `setTilePassable` changes a Boolean in the grid and `comp_tile`. Once
  traversal uses occupants, it must be replaced or retired, not left writing
  a field no movement check reads. There is no old authored-map corpus to
  migrate; update bundled schema, behaviors and fixtures with the new rule.
- The old single-cell `SyncTiles` API remains only for its legacy tests; retire
  that second writer and its tests as the Boolean Tile field goes away. The
  production importer now uses `SyncLayerTiles` and must keep its stable
  per-layer identity while traversal changes.

## Confirmed domain contract

The mover has independent capability components (Walking, Swimming, Flying,
Phased). `Passability.kind` on any occupant holds a validated taxonomy value,
with schema-declared rules referring to required mover components. Within
map bounds, retrieve **all** entities whose OccupiedCells footprint includes
the destination, independent of art Tile presence. Exclude the mover itself;
for a multi-cell mover, check every destination footprint cell. An absent
Passability is nonblocking. Each occupant carrying it applies its category
rule; all must permit entry and one vetoes. A wall category allows
Flying/Phased, water allows Swimming/Flying. An explicit category accepting
nobody denies every mover. Unknown/malformed values refuse loudly instead of
meaning passable.

OccupiedCells is a set of unique integer offsets relative to Position;
single-cell occupants default to their anchor. Reject malformed, duplicate
and off-map cells before a partial TMX import. Moving or deleting an occupant
updates all of its indexed cells and preserves one entity ID. The occupancy
view may be rebuilt from the database or maintained with an explicit
invalidation contract; a startup-only snapshot is wrong when a wall moves
within a tick. Keep art Tile identity separate from the occupancy index.

`Visibility.kind` on any occupant independently classifies sight occlusion.
The observer's *independent components* determine interactions: NightVision
may overcome darkness but not opacity; Flying or Phased do not grant either.
A phased Kitty Pryde with ordinary sight passes a wall but cannot see through
it; water allows ordinary sight despite blocking a walker. An absent
Visibility does not occlude. Renderer draw visibility is distinct.
DetectsMagic's effects on hidden targets or magical obscurants belong to a
**later detection feature**, not this ray rule. Specify
the guard's end-cell behavior against the existing Bresenham tests: do not
let the target's *own* occlusion hide that target, but let an intervening wall
(including another occupant at the target cell) block sight behind it.

## Red–green order

1. Test schema-validated taxonomy labels and references to existing ability
   components, on both schema.json and TMX-authored properties. Test any
   needed presence-only component representation before relying on one. Then
   table-drive a pure traversal predicate against no occupant, wall, water,
   overlapping wall+water, Walking/Swimming/Flying/Phased components, missing
   capabilities, a mover with its own Passability, multi-cell mover footprints,
   out-of-bounds and a cell without artwork. This is the policy all callers
   must share.
2. Test a one-cell wall and one nonrectangular river represented by a **single
   entity** with relative occupied-cell offsets. Test invalid offsets and
   partial-import refusal, then import from the real TMX object/spawn path.
   Test moving, deleting and re-importing the occupant while preserving its ID
   and runtime-only components.
3. Test A*, reachability, player input and `computePath` against the same map
   with several differently capable movers. Test `stepAlongPath` after a new
   blocker arrives and `moveToward` at an obstruction. Thread mover context
   through the APIs instead of inventing another global passable grid.
4. Test sight across a wall independently from movement across it, including
   a phased mover with ordinary sight and NightVision against darkness versus
   opaque walls. Replace `LineOfSight`'s use of passability with the
   observer-components/occupant-Visibility taxonomy. Retire
   `setTilePassable` and remove the interim Tile Boolean from
   schema/import/grid; update bundled maps, fixtures, action metadata and
   engine integration tests.
5. Test a river's visual component across multiple occupied cells in the
   tagged renderer; moving/changing its footprint must update both the
   rendered cells and the movement decision without cloning the river entity.
   A deliberately unrendered restricting occupant is also a valid case.
6. Run `make test`, both lint tag sets, both builds and `make e2e`; fresh-context
   review of the staged diff, fixes and affected checks before committing.
   MAP's multi-cell editing controls arrive in Story 4 with Playwright steps
   written **before** their code; Story 3 is engine-only.
