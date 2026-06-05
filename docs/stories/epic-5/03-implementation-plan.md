# Epic 5 Story 3: Tilemap + TileGrid

## Context

Story 3 adds the world tile map to the game. Tiles are ECS entities (`Tile` entity type, `Tile` component) stored in SQLite so they're inspectable via the debugger like any other entity. A TOML map file defines the layout; a bootstrap function converts it to tile entities at startup (idempotent — skips if they already exist).

Two derived structures are built from the DB:
- `TileGrid` — in-memory passability grid for pathfinding (Story 6) and line-of-sight (Story 7)
- `TilemapRenderer` — static `*ebiten.Image` drawn once, avoids re-querying the DB every frame

End state: window shows floor and wall tiles. Nothing moves yet.

**Depends on:** Story 1 (Tile component in schema.json ✅), Story 2 (Ebitengine window + tick loop ✅)

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/tilemap/grid.go` | TileGrid struct and methods |
| Create | `internal/tilemap/grid_test.go` | Unit tests for TileGrid |
| Create | `internal/tilemap/loader.go` | LoadMap bootstrap function |
| Create | `internal/tilemap/loader_test.go` | Integration tests for LoadMap |
| Create | `mods/map/level1.toml` | Map definition file |
| Create | `internal/renderer/tilemap_renderer.go` | Static tile render buffer (`//go:build ebitengine`) |
| Modify | `internal/config/config.go` | Add `MapConfig` struct + `Map` field to `Config`; add default map path |
| Modify | `game.toml` | Add `[map]` section; update `tileSize` to `32` |
| Modify | `internal/renderer/game.go` | Add `*tilemap.TileGrid` + `*TilemapRenderer` fields; update `NewGame`; draw tilemap in `Draw()` |
| Modify | `cmd/game/main.go` | Create `EntityService`, call `LoadMap`, create `TilemapRenderer`, pass both into `NewGame` |

---

## Task 1: TileGrid

### `internal/tilemap/grid.go`

```go
package tilemap

import (
	"context"
	"database/sql"
	"fmt"
)

type TileGrid struct {
	Width, Height int
	passable      [][]bool
}

func NewTileGrid(width, height int) *TileGrid {
	p := make([][]bool, height)
	for i := range p {
		p[i] = make([]bool, width)
	}
	return &TileGrid{Width: width, Height: height, passable: p}
}

func (g *TileGrid) IsPassable(x, y int) bool {
	if x < 0 || y < 0 || x >= g.Width || y >= g.Height {
		return false
	}
	return g.passable[y][x]
}

func (g *TileGrid) SetPassable(x, y int, val bool) {
	if x < 0 || y < 0 || x >= g.Width || y >= g.Height {
		return
	}
	g.passable[y][x] = val
}

func (g *TileGrid) Rebuild(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx,
		`SELECT comp_tile.x, comp_tile.y, comp_tile.passable
		 FROM entities
		 JOIN comp_tile ON entities.id = comp_tile.entity_id
		 WHERE entities.entity_type = 'Tile'`)
	if err != nil {
		return fmt.Errorf("TileGrid.Rebuild: %w", err)
	}
	defer rows.Close()

	for row := range g.passable {
		for col := range g.passable[row] {
			g.passable[row][col] = false
		}
	}
	for rows.Next() {
		var x, y, p int // SQLite stores bool as 0/1
		if err := rows.Scan(&x, &y, &p); err != nil {
			return fmt.Errorf("TileGrid.Rebuild scan: %w", err)
		}
		g.SetPassable(x, y, p != 0)
	}
	return rows.Err()
}
```

### `internal/tilemap/grid_test.go`

```go
package tilemap

import "testing"

func TestNewTileGrid_Dimensions(t *testing.T) {
	g := NewTileGrid(5, 3)
	if g.Width != 5 || g.Height != 3 {
		t.Errorf("got %d×%d, want 5×3", g.Width, g.Height)
	}
}

func TestIsPassable_DefaultsFalse(t *testing.T) {
	g := NewTileGrid(3, 3)
	if g.IsPassable(1, 1) {
		t.Error("new grid cell should be impassable by default")
	}
}

func TestSetPassable_UpdatesCell(t *testing.T) {
	g := NewTileGrid(3, 3)
	g.SetPassable(1, 1, true)
	if !g.IsPassable(1, 1) {
		t.Error("cell should be passable after SetPassable(true)")
	}
}

func TestIsPassable_OutOfBounds(t *testing.T) {
	g := NewTileGrid(3, 3)
	g.SetPassable(0, 0, true)
	cases := [][2]int{{-1, 0}, {0, -1}, {3, 0}, {0, 3}, {-1, -1}}
	for _, c := range cases {
		if g.IsPassable(c[0], c[1]) {
			t.Errorf("IsPassable(%d,%d) = true, want false (out of bounds)", c[0], c[1])
		}
	}
}

func TestSetPassable_OutOfBounds_NoOp(t *testing.T) {
	g := NewTileGrid(3, 3)
	// Should not panic.
	g.SetPassable(-1, 0, true)
	g.SetPassable(0, 3, true)
}
```

Verify: `go test ./internal/tilemap/...` passes.

---

## Task 2: Config + Map Files

### `internal/config/config.go` — add `MapConfig`

Add `MapConfig` struct and `Map` field to `Config`. Also update `Defaults()`.

```go
type MapConfig struct {
	Path string `toml:"path"`
}

type Config struct {
	Database DatabaseConfig `toml:"database"`
	Schema   SchemaConfig   `toml:"schema"`
	Mods     []ModConfig    `toml:"mods"`
	Window   WindowConfig   `toml:"window"`
	Map      MapConfig      `toml:"map"`
}
```

In `Defaults()`, add:
```go
Map: MapConfig{Path: "mods/map/level1.toml"},
```

### `game.toml` — add map section, update tileSize

```toml
[window]
title    = "ECS Demo"
width    = 640
height   = 480
tileSize = 32

[map]
path = "mods/map/level1.toml"
```

### `mods/map/level1.toml`

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

---

## Task 3: LoadMap Bootstrap

### `internal/tilemap/loader.go`

```go
package tilemap

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/tmbritton/ecs-db/internal/world"
)

type mapDef struct {
	Width  int      `toml:"width"`
	Height int      `toml:"height"`
	Rows   []string `toml:"rows"`
}

// LoadMap bootstraps Tile entities from a TOML map file and returns a
// populated TileGrid. Idempotent: skips entity creation if Tile entities
// already exist in the DB.
func LoadMap(ctx context.Context, svc *world.EntityService, db *sql.DB, path string, tileSize int) (*TileGrid, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("LoadMap: reading %q: %w", path, err)
	}
	var def mapDef
	if err := toml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("LoadMap: parsing %q: %w", path, err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&count); err != nil {
		return nil, fmt.Errorf("LoadMap: checking existing tiles: %w", err)
	}

	if count == 0 {
		for y, row := range def.Rows {
			for x, ch := range row {
				var passable bool
				var tileType string
				switch ch {
				case '.':
					passable, tileType = true, "floor"
				case '#':
					passable, tileType = false, "wall"
				default:
					continue
				}
				if _, err := svc.CreateEntity(ctx, "Tile", []world.EntityComponent{
					{Name: "Tile", Values: map[string]interface{}{
						"x": x, "y": y, "passable": passable, "tile_type": tileType,
					}},
				}); err != nil {
					return nil, fmt.Errorf("LoadMap: creating tile (%d,%d): %w", x, y, err)
				}
			}
		}
	}

	grid := NewTileGrid(def.Width, def.Height)
	if err := grid.Rebuild(ctx, db); err != nil {
		return nil, fmt.Errorf("LoadMap: rebuilding grid: %w", err)
	}
	return grid, nil
}
```

### `internal/tilemap/loader_test.go`

Read `internal/schema/types.go` before writing to confirm exact struct field names. The test creates an in-memory DB with an inline Tile schema (never reads schema.json).

```go
package tilemap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/world"
	_ "modernc.org/sqlite"
)

func tileSchema() schema.DatabaseSchema {
	return schema.DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]schema.Component{
			"Tile": {
				Type: "object",
				Properties: map[string]schema.Property{
					"x":         {Type: "integer"},
					"y":         {Type: "integer"},
					"passable":  {Type: "boolean"},
					"tile_type": {Type: "string"},
				},
			},
		},
		EntityTypes: map[string]schema.EntityType{
			"Tile": {
				RequiredComponents:   []string{"Tile"},
				AllowExtraComponents: false,
				ValidationLevel:      "strict",
			},
		},
	}
}

func writeTempMap(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "level.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp map: %v", err)
	}
	return p
}

const threeByThree = `width=3
height=3
rows=["###","#.#","###"]
`

func TestLoadMap_CreatesEntities(t *testing.T) {
	ds := tileSchema()
	store, err := storage.NewSQLiteStore(":memory:", ds, "test")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	grid, err := LoadMap(context.Background(), svc, store.DB(), writeTempMap(t, threeByThree), 32)
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}
	if grid.Width != 3 || grid.Height != 3 {
		t.Errorf("grid = %d×%d, want 3×3", grid.Width, grid.Height)
	}
	if !grid.IsPassable(1, 1) {
		t.Error("center cell should be passable (floor)")
	}
	if grid.IsPassable(0, 0) {
		t.Error("corner cell should be impassable (wall)")
	}
}

func TestLoadMap_Idempotent(t *testing.T) {
	ds := tileSchema()
	store, err := storage.NewSQLiteStore(":memory:", ds, "test")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)

	path := writeTempMap(t, threeByThree)
	for range 2 {
		if _, err := LoadMap(context.Background(), svc, store.DB(), path, 32); err != nil {
			t.Fatalf("LoadMap: %v", err)
		}
	}

	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&count); err != nil {
		t.Fatalf("counting tiles: %v", err)
	}
	if count != 9 {
		t.Errorf("tile count = %d after two calls, want 9", count)
	}
}
```

Verify: `go test ./internal/tilemap/...` passes.

---

## Task 4: TilemapRenderer

### `internal/renderer/tilemap_renderer.go`

```go
//go:build ebitengine

package renderer

import (
	"context"
	"database/sql"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

type TilemapRenderer struct {
	db       *sql.DB
	img      *ebiten.Image
	w, h     int
	tileSize int
}

func NewTilemapRenderer(db *sql.DB, w, h, tileSize int) (*TilemapRenderer, error) {
	r := &TilemapRenderer{db: db, w: w, h: h, tileSize: tileSize}
	if err := r.rebuild(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *TilemapRenderer) Image() *ebiten.Image { return r.img }

func (r *TilemapRenderer) Invalidate() { _ = r.rebuild() }

func (r *TilemapRenderer) rebuild() error {
	img := ebiten.NewImage(r.w, r.h)
	rows, err := r.db.QueryContext(context.Background(),
		`SELECT comp_tile.x, comp_tile.y, comp_tile.tile_type
		 FROM entities
		 JOIN comp_tile ON entities.id = comp_tile.entity_id
		 WHERE entities.entity_type = 'Tile'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var x, y int
		var tileType string
		if err := rows.Scan(&x, &y, &tileType); err != nil {
			return err
		}
		var c color.RGBA
		switch tileType {
		case "wall":
			c = color.RGBA{R: 60, G: 60, B: 60, A: 255}
		default: // floor
			c = color.RGBA{R: 180, G: 180, B: 180, A: 255}
		}
		vector.DrawFilledRect(img,
			float32(x*r.tileSize), float32(y*r.tileSize),
			float32(r.tileSize), float32(r.tileSize),
			c, false)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	r.img = img
	return nil
}
```

---

## Task 5: Wire into Game

### `internal/renderer/game.go` — add grid and renderer fields

Updated `Game` struct:

```go
type Game struct {
	ticker     *Ticker
	frameCount int
	logicalW   int
	logicalH   int
	grid       *tilemap.TileGrid
	tr         *TilemapRenderer
}
```

Updated `NewGame` signature:

```go
func NewGame(db *sql.DB, loader *agent.Loader, registry *agent.Registry, w, h int, grid *tilemap.TileGrid, tr *TilemapRenderer) *Game {
	return &Game{
		ticker:   newTicker(db, loader, registry),
		logicalW: w,
		logicalH: h,
		grid:     grid,
		tr:       tr,
	}
}
```

Updated `Draw()`:

```go
func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.Black)
	if g.tr != nil {
		screen.DrawImage(g.tr.Image(), nil)
	}
}
```

Add import: `"github.com/tmbritton/ecs-db/internal/tilemap"`.

Note: `game_test.go` calls `NewGame` — update that call site to pass `nil, nil` for the new grid and tr parameters.

### `cmd/game/main.go` — bootstrap tilemap in `runGame`

After `EnsureInterpreterTables`, before behavior scanning, add:

```go
svc := world.NewEntityService(store)
svc.SetSchema(dbSchema)

var (
    grid *tilemap.TileGrid
    tr   *renderer.TilemapRenderer
)
if cfg.Map.Path != "" {
    g, err := tilemap.LoadMap(ctx, svc, store.DB(), cfg.Map.Path, cfg.Window.TileSize)
    if err != nil {
        return fmt.Errorf("loading map: %w", err)
    }
    grid = g
    t, err := renderer.NewTilemapRenderer(store.DB(), cfg.Window.Width, cfg.Window.Height, cfg.Window.TileSize)
    if err != nil {
        return fmt.Errorf("building tilemap renderer: %w", err)
    }
    tr = t
}
```

Update the `NewGame` call:

```go
game := renderer.NewGame(store.DB(), loader, registry, cfg.Window.Width, cfg.Window.Height, grid, tr)
```

Add imports:
```go
"github.com/tmbritton/ecs-db/internal/tilemap"
"github.com/tmbritton/ecs-db/internal/world"
```

---

## Verification

```bash
# All tests pass (no ebitengine tag needed for tilemap tests)
go test ./...

# Build succeeds
make build

# Window shows floor (light grey) and wall (dark grey) tiles
make run

# Schema subcommand still works
./bin/game schema validate

# Restart — tile count in DB unchanged (idempotency)
make run
```

After the first `make run`, the DB has tile entities. Confirm idempotency by running again and checking no extra tiles are inserted.

Also: delete `ecs.db` before first run so the DB is fresh (schema version 2 with `comp_tile` table).
