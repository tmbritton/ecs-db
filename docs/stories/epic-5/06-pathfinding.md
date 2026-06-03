# Story 6: Pathfinding

**Epic:** 5 — Interpreter tick loop & Ebitengine monolith  
**Status:** 🔲 Not started  
**Priority:** Medium — required for the wandering goblin smoke test (Story 8)

**Depends on:** Story 3 (TileGrid), Story 1 (Path component in schema)

## Context

The wandering goblin needs to navigate from one point to another on the tilemap without walking through walls. This story adds A* pathfinding as a built-in action (`computePath`), a movement action (`stepAlongPath`), and a guard (`pathComplete`). All three operate on the in-memory `TileGrid` rather than the database, keeping the interpreter tick fast.

`computePath` writes its result (a JSON array of tile coordinates) into the entity's `Path` component. Subsequent `stepAlongPath` calls advance the entity one tile per tick, incrementing `current_index`. `pathComplete` checks whether the path is exhausted.

## Acceptance Criteria

- [ ] `internal/tilemap/astar.go` — A* implementation:
  ```go
  // AStar returns the path from (sx,sy) to (tx,ty) as a slice of {X,Y} points,
  // including start and end. Returns nil if no path exists.
  func AStar(grid *TileGrid, sx, sy, tx, ty int) []Point
  
  type Point struct{ X, Y int }
  ```
  - Uses Manhattan distance heuristic
  - Explores 4 neighbours (no diagonal movement)
  - Returns `nil` if start or end is impassable, or no path exists
  - Does not include the start tile in the returned path (movement begins from the next tile)
- [ ] `internal/tilemap/astar_test.go` — unit tests:
  - Straight path with no obstacles
  - Path that must route around a wall
  - No path exists (fully enclosed target) → returns nil
  - Start == end → returns empty slice (no movement needed)
- [ ] `computePath` built-in action registered in the action registry:
  - Params: `target_x int`, `target_y int` (or `target_entity int64` — reads `comp_position` of that entity)
  - Reads `comp_position.x`, `comp_position.y` of the acting entity (as `int`)
  - Calls `AStar(tileGrid, sx, sy, tx, ty)`
  - If path is nil: logs warning `"no path found for entity <id>"`, leaves `comp_path` unchanged
  - If path is found: marshals `[]tilemap.Point` to JSON, calls `WorldWriter.SetComponentValue(entityID, "Path", "waypoints", json)` and `SetComponentValue(entityID, "Path", "current_index", 0)`
  - `tileGrid` is injected via closure at registration time
- [ ] `stepAlongPath` built-in action:
  - No params
  - Reads `comp_path.waypoints` (JSON) and `comp_path.current_index`
  - If `current_index >= len(waypoints)`: no-op (path already complete)
  - Otherwise: reads `waypoints[current_index]`, calls `SetComponentValue` for `comp_position.x` and `comp_position.y`, increments `current_index`
  - Sets `comp_sprite.flip_x` based on direction of movement (moving left → true)
- [ ] `pathComplete` built-in guard:
  - No params
  - Reads `comp_path.waypoints` (JSON) and `comp_path.current_index`
  - Returns `true` if `current_index >= len(waypoints)` or `waypoints` is empty/null
- [ ] `internal/agent/builtin_pathfinding_test.go` — integration tests using a real in-memory SQLite DB and a small TileGrid:
  - `computePath` with valid target → `comp_path.waypoints` contains correct route
  - `computePath` with unreachable target → `comp_path.waypoints` unchanged
  - `stepAlongPath` advances `comp_position` and increments `current_index`
  - `pathComplete` returns false mid-path, true after last step
- [ ] `go test ./...` passes

## Notes

- `Position.x` and `Position.y` are `number` (REAL) in SQLite but are used as integer tile coordinates. Cast with `int(math.Round(v.(float64)))` when reading from `GetComponentValue`.
- `computePath` with `target_entity` param: call `WorldReader.FindEntityByType` or a new `GetEntityPosition` helper that reads `comp_position` for a given entity ID.
- The `TileGrid` pointer is captured in the action closure at registration time. It must be the same pointer held by `Game` so that `SetPassable` mutations (Story 7) are visible to pathfinding without re-registration.
- A* time complexity: O(N log N) where N is the number of tiles. On a 20×15 grid (300 tiles) this is negligible per tick.
- `waypoints` in the JSON excludes the starting tile (the entity is already there). `stepAlongPath` moves to `waypoints[current_index]` and increments. After the last waypoint, `current_index == len(waypoints)` and `pathComplete` returns true.
