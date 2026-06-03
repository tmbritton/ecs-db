# Epic 5 Renderer Design — Ebitengine Monolith

**Date:** 2026-06-02

---

## Context

Epic 5 introduces the Ebitengine rendering layer and the interpreter tick loop, turning the headless ECS-in-SQLite engine into a runnable game. The architecture doc establishes the core contract: interpreter is the sole writer of world state; renderer is intentionally dumb and reads entities + components from the DB; SQLite WAL mode handles concurrent access within the same process.

This spec fills in the details left open by the arch doc: sprite association, input timing, tilemap, pathfinding, and line-of-sight.

---

## 1. Tick Loop

Ebitengine runs at 60 TPS (`ebiten.SetTPS(60)`). The interpreter fires every 3rd `Update()` call (~20Hz game logic, 60Hz input sampling).

```
const TicksPerSecond = 20  // derive divisor: 60 / TicksPerSecond
```

**`Update()` sequence (every frame):**
1. Sample Ebitengine input; append pressed/held keys to `input_events`
2. Increment `frameCount`
3. If `frameCount % (60 / TicksPerSecond) == 0`: run interpreter tick
   - Drain unconsumed `input_events` → game-specific input handler
   - Drain due `event_queue` rows
   - Deliver `TICK` to all active machines
   - Advance tick counter, bump `world_version`
4. Advance renderer-local animation frame timers (wall-clock, every frame)

**`Draw()`:** queries all entities with `Position` + `Sprite`, composites tile buffer, draws entities.

Ebitengine's built-in accumulator guarantees `Update()` fires at the configured TPS even when `Draw()` runs slow — it calls `Update()` multiple times per rendered frame when catching up. The `% 3` check piggybacks on this. Max input-to-visible latency: ~33ms.

---

## 2. Input Capture

Input is sampled every `Update()` (60Hz). `ebiten.IsKeyPressed` for held keys; `inpututil.IsKeyJustPressed` for one-shot events. Each event appends a row to `input_events`:

```
kind:    "key_pressed" | "key_held" | "mouse_click"
payload: {"key": "ArrowRight"}
```

The interpreter drains all unconsumed rows each tick and dispatches to a registered Go input handler. `input_events` is append-only (audit trail; no schema changes needed).

---

## 3. Sprite Component and Animation System

### Existing Sprite Component (schema.json)

The current `Sprite` component has `imageId` (string) and `frame` (integer). This needs to evolve:

**Updated `Sprite` component:**
```
sheet      string   — sprite sheet path (replaces imageId)
animation  string   — current animation name (NEW)
flip_x     bool     — mirror horizontally (NEW)
```

`frame` is removed from the component — the renderer manages it locally. State machines write `comp_sprite.animation` via a new built-in action `setAnimation(animation: string)`. Same registration pattern as `setTimer`, `log`, etc.

### Animation Definitions

A TOML file in `mods/assets/` (path configured in `game.toml`). Hot-swappable via filesystem watcher — same pattern as the behavior `Loader`. Defines per named animation: source sheet, frame indices, fps, loop bool.

On hot-swap: re-parse and replace in-memory animation map. Entities playing a removed animation log a warning and hold the last valid frame. No reconciliation needed (animations are purely presentational).

### Frame Advancement

The renderer owns `map[int64]*AnimState` keyed by entity ID:

```go
type AnimState struct {
    currentAnim string
    frame       int
    elapsed     float64
}
```

Each `Update()` (60Hz): increment `elapsed` by `1/60`. When `elapsed >= 1/fps`: advance frame, reset `elapsed`. When `comp_sprite.animation` differs from `AnimState.currentAnim`: reset `AnimState`.

### Compositing (deferred)

For Epic 5: one base sprite per entity. Long-term direction: config-driven layer system in `render_layers.toml` per mod — ordered layers with conditions (`has_component`, field values) and effects (tint, alpha). Likely aligns with Epic 8 effects system.

---

## 4. Tilemap

### Tiles as Entities

New component `Tile`:
```
x          int    — tile column
y          int    — tile row
passable   bool
tile_type  string — "floor" | "wall" | "door" | etc.
```

New entity type `Tile` with required component `Tile`. Map dimensions (width × height in tiles) come from `game.toml`. Bootstrap function creates tile entities at startup from a map TOML file.

Tiles are intentionally inspectable through the debugger like any other entity. No neighbor links — for rectangular grids, `(x±1, y±1)` coordinate math gives neighbors in O(1). If portals or one-way passages are needed later, add a `Portal` component as a special case.

### Derived Structures (built at startup from tile entities)

**Render buffer** — `*ebiten.Image` drawn once; invalidated and redrawn when a tile changes.

**Passability grid** — `TileGrid` in `internal/tilemap`:
```go
type TileGrid struct {
    width, height int
    passable      [][]bool
}
func (g *TileGrid) IsPassable(x, y int) bool
func (g *TileGrid) SetPassable(x, y int, val bool)
func (g *TileGrid) Rebuild(db *sql.DB)
```

`TileGrid` lives on the `Game` struct and is passed by pointer into action/guard handler closures at registration time.

### Cache Invalidation

Explicit. Built-in action `setTilePassable(entity_id, passable)` calls `WorldWriter.SetComponentValue` and `grid.SetPassable(x, y, val)` in the same step.

---

## 5. Pathfinding and Line-of-Sight

Both operate on the in-memory `TileGrid`, not the database. `Position.x` and `Position.y` are `number` (float) in the schema; pathfinding casts to `int` for grid index lookup.

### Line-of-Sight Guard

New built-in guard `inLineOfSight(target_entity)`:
- Reads `comp_position` of acting entity and target
- Walks the ray using DDA (Digital Differential Analyzer) — integer grid stepping, no floating-point drift
- Returns false on first impassable tile

### Pathfinding

New built-in action `computePath(target_x, target_y)` (or `target_entity`):
- Reads acting entity's `comp_position` for start
- Runs A* on `TileGrid`
- Writes result to `Path` component: `waypoints` (JSON array of `{x,y}`), `current_index` (int)

Companion action `stepAlongPath`: advances `comp_position` to next waypoint, increments `current_index`.
Companion guard `pathComplete`: returns true when `current_index >= len(waypoints)`.

New component `Path`:
```
waypoints      string   — JSON: [{x,y}, ...]
current_index  int
```

---

## 6. Wandering Goblin + Player Smoke Test

### Map

Small TOML-defined tilemap (~20×15), walls on border, a few internal walls. Bootstrap creates tile entities at startup.

### Goblin (entity type already exists)

Add components: `Path`, `Speed` (new: `value float` — tiles per tick).
Machine (`mods/behaviors/goblin.json`):
- `idle`: entry sets `animation = "goblin_idle"`, picks random passable target, runs `computePath`, transitions to `wandering` (optionally after short `after` timer)
- `wandering`: entry sets `animation = "goblin_walk"`, each `TICK` runs `stepAlongPath`, guard `pathComplete` → back to `idle`

### Player (entity type already exists)

No state machine. Movement handled in the interpreter's game-specific input handler:
- On movement key drained from `input_events`: check `TileGrid`, update `comp_position`
- Set `comp_sprite.animation` (`"player_walk"` / `"player_idle"`) and `flip_x` per direction
- One tile of movement per tick maximum

### Renderer draw pass

Single query: all entities with both `Position` and `Sprite` components. Goblin and player appear automatically.

---

## Schema Changes

| Component  | Status  | Fields                                                              |
|------------|---------|---------------------------------------------------------------------|
| `Position` | exists  | `x number`, `y number` — no change                                 |
| `Health`   | exists  | `hp int`, `maxHp int` — no change                                  |
| `Sprite`   | update  | `sheet string`, `animation string`, `flip_x bool` (drop `imageId`, `frame`) |
| `Tile`     | new     | `x int`, `y int`, `passable bool`, `tile_type string`              |
| `Path`     | new     | `waypoints string` (JSON), `current_index int`                     |
| `Speed`    | new     | `value number`                                                     |

Entity types `Goblin` and `Player` already exist; `Tile` entity type is new.

## New Built-in Actions

| Action            | Params                   | Effect                                          |
|-------------------|--------------------------|-------------------------------------------------|
| `setAnimation`    | `animation`              | Writes `comp_sprite.animation`                  |
| `computePath`     | `target_x`, `target_y`   | Runs A*, writes `comp_path`                     |
| `stepAlongPath`   | —                        | Advances position, increments path index        |
| `setTilePassable` | `entity_id`, `passable`  | Writes component + invalidates grid             |

## New Built-in Guards

| Guard           | Params          | Returns                                      |
|-----------------|-----------------|----------------------------------------------|
| `inLineOfSight` | `target_entity` | DDA ray clear of impassable tiles            |
| `pathComplete`  | —               | `current_index >= len(waypoints)`            |

## Key Files to Create / Modify

| File                                           | Action   | Purpose                                              |
|------------------------------------------------|----------|------------------------------------------------------|
| `internal/tilemap/grid.go`                     | create   | `TileGrid` struct                                    |
| `internal/renderer/renderer.go`                | create   | Ebitengine `Game` struct, `Update`, `Draw`           |
| `internal/renderer/animation.go`               | create   | `AnimState`, frame advancement, animation loader     |
| `internal/agent/` (builtin actions/guards)     | modify   | Add `computePath`, `stepAlongPath`, `setTilePassable`, `setAnimation`, `inLineOfSight`, `pathComplete` |
| `schema.json`                                  | modify   | Update `Sprite`; add `Tile`, `Path`, `Speed`; add `Tile` entity type |
| `mods/behaviors/goblin.json`                   | create   | Wandering goblin state machine                       |
| `mods/assets/animations.toml`                  | create   | Animation definitions (hot-swappable)                |
| `mods/map/level1.toml`                         | create   | Test map                                             |
| `game.toml`                                    | modify   | Add `assets/` path, map path, tile dimensions        |
| `cmd/game/main.go`                             | create   | Entry point wiring `Game`, interpreter, registry     |
