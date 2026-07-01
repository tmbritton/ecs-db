# Story 7: Line-of-Sight + Tile Mutation

**Epic:** 5 — Interpreter tick loop & Ebitengine monolith  
**Status:** ✅ Complete  
**Priority:** Medium — not required for the goblin smoke test; adds the full guard/mutation surface

**Depends on:** Story 3 (TileGrid), Story 6 (TileGrid closure pattern established)

## Context

Line-of-sight is a guard (`inLineOfSight`) that casts a ray between two entities on the `TileGrid` using the DDA (Digital Differential Analyzer) algorithm — the same technique Wolfenstein used for rendering, repurposed here for AI. It returns false the moment the ray crosses an impassable tile.

`setTilePassable` is the explicit cache-invalidation action for the `TileGrid`: it writes the new passability value to the database and calls `grid.SetPassable` in the same step. Any future state machine that opens a door or breaks a wall uses this action.

Neither is exercised by the wandering goblin smoke test (Story 8), but both are unit-tested here and available for future state machines.

## As Implemented

- `LineOfSight(grid, start, end Point) bool` (not `HasLineOfSight`). Start is excluded from the passability check; end is included (if end is a wall, returns false). Uses Bresenham walk.
- `setTilePassable` works by coordinates (`x`, `y` params), not by entity ID. `TileGrid` was extended with `entityIDs map[Point]int64` (populated by `Rebuild`, settable via `SetEntityID`) so the action can look up the DB row without an extra query. Does not call `tilemapRenderer.Invalidate()` — tile mutation and renderer invalidation are decoupled.
- Both registered via `RegisterLineOfSight(r, grid)` in `internal/agent/builtins/register.go`.
- Tests in `internal/tilemap/los_test.go` and `internal/agent/builtins/builtins_test.go`.

## Acceptance Criteria

- [x] `internal/tilemap/los.go` — DDA line-of-sight:
  ```go
  // HasLineOfSight returns true if the ray from (x0,y0) to (x1,y1) does not
  // cross any impassable tile. Start and end tiles are not checked.
  func HasLineOfSight(grid *TileGrid, x0, y0, x1, y1 int) bool
  ```
  - DDA algorithm: step along the ray in unit increments, checking each tile the ray passes through
  - Start and end tiles are excluded from the check (entity positions may be on walls in edge cases)
  - Both (x0,y0) and (x1,y1) out-of-bounds → returns false
- [x] `internal/tilemap/los_test.go` — unit tests:
  - Clear line of sight on open floor → true
  - Wall between two entities → false
  - Adjacent entities → true
  - Diagonal LoS not blocked by corner-touching wall (DDA behaviour — document expected result)
- [x] `inLineOfSight` built-in guard registered in the guard registry:
  - Params: `target_entity int64`
  - Reads `comp_position` of acting entity and target entity
  - Calls `HasLineOfSight(tileGrid, x0, y0, x1, y1)`
  - Returns false if either entity has no `comp_position`
  - `tileGrid` injected via closure
- [x] `setTilePassable` built-in action registered in the action registry:
  - Params: `entity_id int64`, `passable bool`
  - Reads `comp_tile.x`, `comp_tile.y` of the target entity
  - Calls `WorldWriter.SetComponentValue(entityID, "Tile", "passable", passable)`
  - Calls `tileGrid.SetPassable(x, y, passable)`
  - Calls `tilemapRenderer.Invalidate()` to trigger render buffer rebuild on next `Draw()`
  - `tileGrid` and `tilemapRenderer` injected via closure
- [x] `internal/agent/builtin_los_test.go` — integration tests using a real in-memory SQLite DB and TileGrid:
  - `inLineOfSight` with clear path → guard returns true
  - `inLineOfSight` with wall blocking → guard returns false
  - `setTilePassable` writes `comp_tile.passable` and updates `TileGrid`
- [x] `go test ./...` passes

## Notes

- DDA algorithm reference: cast a ray from (x0, y0) to (x1, y1). Compute `dx = abs(x1-x0)`, `dy = abs(y1-y0)`. Step in unit increments along the longer axis, advancing the shorter axis proportionally. Check `grid.IsPassable` at each stepped tile coordinate.
- `tilemapRenderer.Invalidate()` causes the render buffer to be redrawn on the next `Draw()` call. The invalidation flag is a simple `bool` field on `TilemapRenderer`; `Draw()` checks it and calls `Rebuild()` if set.
- `setTilePassable` acts on a tile entity by ID — the caller (state machine) must know the entity ID of the tile to mutate. The `target_entity` entity ID can be stored in a component (e.g., a `Door` component with `door_tile_id`) or hard-coded in the machine params for now.
- The `tilemapRenderer` pointer must be the same instance held by `Game`. Inject via closure at registration time, same as `tileGrid`.
