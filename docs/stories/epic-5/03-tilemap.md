# Story 3: Tilemap + TileGrid

**Epic:** 5 — Interpreter tick loop & Ebitengine monolith  
**Status:** 🔲 Not started  
**Priority:** High — tilemap is required by player movement (Story 4) and pathfinding (Story 6)

**Depends on:** Story 2 (Ebitengine window + tick loop), Story 1 (Tile component in schema)

## Context

The game needs a world to move through. Tiles are ECS entities (entity type `Tile`, component `Tile`) so they're inspectable through the debugger like any other entity. A small TOML map file describes the layout; a bootstrap function converts it to tile entities in the database at startup.

Two derived structures are built from the tile entities: a static render buffer (drawn once to an `*ebiten.Image`) and an in-memory passability grid (`TileGrid`). The render buffer avoids re-querying the DB every frame for tiles that almost never change. The `TileGrid` serves pathfinding (Story 6) and line-of-sight (Story 7) without hitting the database.

At the end of this story, the window shows a tile map — floor and wall tiles — with nothing moving yet.

## Acceptance Criteria

- [ ] `internal/tilemap/grid.go` — `TileGrid` struct:
  ```go
  type TileGrid struct {
      Width, Height int
      passable      [][]bool
  }
  func NewTileGrid(width, height int) *TileGrid
  func (g *TileGrid) IsPassable(x, y int) bool
  func (g *TileGrid) SetPassable(x, y int, val bool)
  func (g *TileGrid) Rebuild(ctx context.Context, db *sql.DB) error
  ```
  - `Rebuild` queries `SELECT comp_tile.x, comp_tile.y, comp_tile.passable FROM entities JOIN comp_tile ON entities.id = comp_tile.entity_id WHERE entities.entity_type = 'Tile'` and populates `passable`
  - Out-of-bounds `IsPassable` returns `false`
  - `internal/tilemap/grid_test.go` — unit tests for `IsPassable`, `SetPassable`, out-of-bounds
- [ ] `mods/map/level1.toml` — map definition:
  ```toml
  width  = 20
  height = 15

  # Each row is a string of characters: '.' = floor, '#' = wall
  rows = [
    "####################",
    "#..................#",
    "#....###...........#",
    "#..................#",
    "#......#...........#",
    "#......#...........#",
    "#..................#",
    "#...########.......#",
    "#..................#",
    "#..................#",
    "#........###.......#",
    "#..................#",
    "#..................#",
    "#..................#",
    "####################",
  ]
  ```
- [ ] `game.toml` updated with `[map]` section: `path = "mods/map/level1.toml"`
- [ ] Bootstrap function `tilemap.LoadMap(ctx, db, path string, tileSize int) (*TileGrid, error)`:
  - Parses `level1.toml`
  - For each tile character, calls `EntityService.CreateEntity("Tile", ...)` with `Tile` component values (`x`, `y`, `passable`, `tile_type`)
  - Returns a populated `TileGrid`
  - Skips bootstrap if tile entities already exist (idempotent on restart)
- [ ] `internal/renderer/tilemap_renderer.go` — builds a static `*ebiten.Image` from tile entities:
  - Floor tiles: solid grey rectangle
  - Wall tiles: solid dark rectangle
  - Placeholder colors are fine; no sprite sheets required in this story
  - Exposes `TilemapRenderer.Image() *ebiten.Image` and `TilemapRenderer.Invalidate()`
- [ ] `Game.Draw()` draws the tilemap render buffer at offset (0, 0)
- [ ] `Game` struct holds `*tilemap.TileGrid` (populated at startup) and `*renderer.TilemapRenderer`
- [ ] `go test ./...` passes; window shows a tile map

## Notes

- Tile entities are created once and persist in the database across restarts (idempotent bootstrap). Check for existing `Tile` entities before inserting.
- `tileSize` (pixels per tile) comes from `game.toml` `[window].tileSize`. A 32px tile on a 20×15 grid = 640×480 window — a sensible default.
- The `TilemapRenderer` invalidation hook is called by the `setTilePassable` action (Story 7). For now it is stubbed and never called.
- `'.'` → `passable = true`, `tile_type = "floor"`. `'#'` → `passable = false`, `tile_type = "wall"`. Additional characters (e.g. `'d'` for door) can be added later.
- `TileGrid` is passed by pointer into the `Game` struct. In Story 6 it is passed into action/guard handler closures at registration time. Keep the pointer stable — don't replace the struct, mutate it in place via `SetPassable` or `Rebuild`.
