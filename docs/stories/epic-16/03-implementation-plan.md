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

## Concrete data contract for this implementation

- `schema.json` declares `Passability.kind` and `Visibility.kind` as string
  fields and a top-level `interactions` section. For each component, each
  taxonomy category has `allows` (a list of independently attachable **boolean
  capability component names**) and `open` (whether no ability is needed).
  An empty `allows` with `open: false` denies everyone. Missing component means
  no restriction; an unknown category is an error. Validate category names,
  references to declared boolean components, duplicate names and component
  shape when loading the schema; preserve this section through Marshal so a
  Forge schema save cannot erase the game's movement rules. No fixed
  `MovementModes` or `PerceptionModes` field is introduced. Walking, Flying,
  Swimming, Phased and NightVision are **example declarations in the bundled
  game schema**, not capability names hard-coded in the engine. A mod may
  declare and reference a different boolean capability without changing Go.
- A capability is active only when its scalar boolean component's `value` is
  true. The bundled map explicitly authors `Walking.value=true` on its Player
  and Goblin spawns; this is sample game data, not an engine-wide default or a
  hard-coded movement requirement. Flying, Swimming, Phased and NightVision
  are likewise example optional components. A present but false ability does
  not silently grant access. Unknown capability references fail validation.
- A placed Tile must have Position and a `TileReferences` collection of
  entity-ref IDs. The Tile owns placement and cell identity, **not the artwork
  component**. Each referenced entity can own a visual component, an
  interaction component, or both. A referenced Wall/River is a separate
  entity. Resolve artwork and rules at each Tile's
  Position. One Tile can reference several entities, and several Tiles can
  reference the same River instance. Check every referenced restriction.
  Reject dangling references rather than silently treating them as passable.
  A referenced entity's Tile cells replace (do not union with) its own
  Position/OccupiedCells footprint. Moving/relinking Tiles moves occupancy
  and art together; preserve Tile IDs across compatible re-imports. The Tile's
  references are **map-owned**: runtime link changes take effect immediately
  but re-import restores authored links, while unrelated runtime-only
  components remain attached.
- `OccupiedCells` is an array component for an **unreferenced positioned
  entity** whose value is a JSON array of unique
  `{ "x": integer, "y": integer }` offsets. The anchor-only default applies
  when the component is absent; an explicit, nonempty set may place the origin
  outside the footprint. Validate integer type, uniqueness and every
  translated cell's map bounds before writing an authored spawn. A moved
  entity changes all its occupied cells without creating extra entities.
- Traversal and line-of-sight read current Tile Positions and references,
  Position, capability, footprint
  and restriction rows from the database (or the tick's world reader), not a
  startup-only occupancy snapshot. Rendered multi-cell sprites use the same
  footprint projection. Grid bounds remain a map property, not a tile-art
  property. Retire the old `SyncTiles` writer with the Boolean Tile field.

## Initial red–green order (direct-Position foundation)

1. Test schema-validated taxonomy labels and references to existing ability
   components, on both schema.json and TMX-authored properties. Test boolean
   capability components and true/false presence. Then
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
    MAP's legacy-property validation gets a Playwright spec; multi-cell
    editing controls arrive in Story 4 with their steps written **before** code.

## Direct-Position foundation (implemented before authoring correction)

`schema.DatabaseSchema.Interactions` maps arbitrary taxonomy categories to
arbitrary schema-declared boolean ability components and survives schema
Marshal and Forge session cloning. `tilemap.Space` applies the same rule to
every occupied destination cell, excludes the mover itself, and handles sight
occlusion independently. `ReadSpace` queries current Position, footprint and
capability rows; the tick reader exposes the same SQL query port on its live
transaction, so a newly moved wall is visible before commit. A* and
reachability share this predicate. Player input, path computation and stepping,
direct movement and the sight guard use these snapshots. Without a configured
map, direct `moveTowardTarget` remains an explicitly free-space action.
Schemas without Passability, Visibility or OccupiedCells omit those joins and
retain an unrestricted single-cell default. The renderer likewise draws an
anchor-only sprite when a project's schema has no OccupiedCells component.
Spatial spawn preflight checks property values and the entity-type's required
components before committing any layer tiles, even if a Wall author supplied
no spatial properties at all. A present component with an empty stored `kind`
is an error, never the absent-component default.
Passability, Visibility and OccupiedCells use canonical component names in
the schema (because their storage readers are spatial ports); TMX property
spelling resolves case-insensitively through the schema before preflight.
Capability names remain freely schema-declared.
Authored occupants are scoped to the active `mapId` through their spawn
records; runtime-created entities without a spawn record remain visible.
The tick's `$player` lookup and game startup player binding choose the active
map's Player before considering a runtime-created, unowned fallback.
The renderer shows only active-map or unowned runtime sprites; TICK delivery
and due-event consumption leave foreign-map authored entities untouched until
that map becomes active again.

## Tile-reference correction — red–green order before Story 3 can be closed

1. Test a Tile with Position and no references, then a Tile referencing one
   Wall and a Tile referencing two independent blockers. Test several Tiles
   referencing one irregular River, one shared River ID, empty-art cells,
   duplicate and dangling references, and a referenced entity that also has
   Position: only its referencing Tile cells count.
2. Test `TileReferences` persistence and re-import identity through the real
   TMX/TSX paths. Painting a tile with a schema-authored entity-type template
   creates a Tile and its linked occupant; editing art preserves compatible
   identities, removal cleans owned links, and a map-authored shared River can
   be referenced from more than one Tile. Tile References and required
   components validate **before** any partial import commits.
3. Test that moving/relinking a Tile changes both the referenced entities'
   rendered artwork and `Space.CanEnter`/`Space.CanSee` on the next tick,
   including A*, input and paths. `TileVisual` belongs on a referenced entity;
   layer order and Tile Position control where it draws. One River visual
   reused by several Tiles draws repeatedly without cloning the River ID.
   A standalone runtime entity with Position/OccupiedCells still works.
4. Keep the TILES template UI and MAP link editing controls in their later
   authoring stories, with Playwright steps before browser code. Run `make
   test`, both builds/lint sets and `make e2e`, then fresh-context review,
   fixes, commit and push this corrected Story 3.

The sample schema's Walking, Flying, Swimming, Phased and NightVision names are
**examples only**; a test adds Burrowing through schema data and verifies it
without an engine name switch. The starter TMX paints Wall templates instead
of carrying 82 separate Wall objects. A painted irregular River test verifies
one shared referenced River without cloned River instances. Another River
test verifies real TMX import, footprint changes,
runtime component preservation and re-import. Tagged renderer tests verify a
single sprite ID draws at every occupied offset after Position and footprint
edits. The old Tile Boolean, `SyncTiles`, `TileGrid.IsPassable`,
`setTilePassable` and movement-based line of sight are gone.

Both the direct-Position foundation and the corrected Tile-reference importer
passed `make test`, tagged renderer tests, both lint tag sets, both builds
and 300 browser checks. Painted River cells and their shared River entity keep
their IDs on re-import. An unrendered shared entity is rejected before any Tile
is imported. Engine-owned `tile_art_components` records the authored component
set for painted references, distinguishing removed optional fields from
runtime-attached components on re-import. Referenced Sprites and restrictions
both follow their Tile positions without leaking into another map. A shared
visual has one source rectangle: painted cells with distinct artwork must use
per-cell visual references alongside the shared restriction entity; importing
different art through one shared visual fails before writing any Tiles. Duplicate
object IDs involving a TileLink are likewise rejected before import. Coverage:
`internal/tilemap` 88.0%, `internal/schema` 92.2%.
