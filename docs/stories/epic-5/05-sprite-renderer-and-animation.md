# Story 5: Sprite Renderer + Animation System

**Epic:** 5 — Interpreter tick loop & Ebitengine monolith  
**Status:** 🔲 Not started  
**Priority:** High — replaces placeholder rectangles with actual sprites

**Depends on:** Story 4 (player moves; renderer draws entities), Story 1 (updated Sprite component)

## Context

The renderer currently draws coloured rectangles for entities. This story replaces that with sprite sheet rendering: a TOML file in `mods/assets/` defines named animations (which sprite sheet, which frame indices, at what frame rate). The renderer maintains per-entity `AnimState` and advances frame timers at 60 Hz. State machines write `comp_sprite.animation` via the new `setAnimation` built-in action; the renderer reads it and resets the animation when it changes.

Animation definitions are hot-swappable: a filesystem watcher reloads the TOML when saved, exactly as behavior JSON files reload today.

At the end of this story, the player and goblin (once spawned in Story 8) display correct walking and idle animations from sprite sheets.

## Acceptance Criteria

- [ ] `mods/assets/animations.toml` — animation definitions (hot-swappable):
  ```toml
  [[animation]]
  name   = "player_idle"
  sheet  = "mods/assets/sprites/player.png"
  frames = [0]
  fps    = 1
  loop   = true

  [[animation]]
  name   = "player_walk"
  sheet  = "mods/assets/sprites/player.png"
  frames = [1, 2, 3, 4]
  fps    = 8
  loop   = true

  [[animation]]
  name   = "goblin_idle"
  sheet  = "mods/assets/sprites/goblin.png"
  frames = [0]
  fps    = 1
  loop   = true

  [[animation]]
  name   = "goblin_walk"
  sheet  = "mods/assets/sprites/goblin.png"
  frames = [1, 2, 3, 4]
  fps    = 8
  loop   = true
  ```
  - `frames` is a list of column indices into the sprite sheet (single row assumed; each frame is `tileSize × tileSize` pixels)
- [ ] Placeholder sprite sheets: `mods/assets/sprites/player.png` and `mods/assets/sprites/goblin.png` — simple 5-frame sheets (1 idle + 4 walk), hand-drawn or generated (32×160 px at 32px tile size). Committed to the repo.
- [ ] `game.toml` updated with `[assets]` or `[[mods]]` `assets` path pointing to `mods/assets/`
- [ ] `internal/renderer/anim_loader.go` — `AnimLoader`:
  - `Load(path string) (map[string]*AnimDef, error)` — parses TOML, returns map keyed by animation name
  - `AnimDef` struct: `Name string`, `Sheet string`, `Frames []int`, `FPS float64`, `Loop bool`
  - Filesystem watcher reloads on change (same `fsnotify` pattern as behavior `Loader`)
  - On reload: replace the in-memory map atomically; log success or parse error
  - On missing animation name at draw time: log warning once per entity, fall back to solid colour
- [ ] `internal/renderer/anim_state.go` — `AnimState` struct and per-entity tracking:
  ```go
  type AnimState struct {
      CurrentAnim string
      Frame       int
      Elapsed     float64 // seconds since last frame advance
  }
  ```
  - `Game` struct holds `animStates map[int64]*AnimState`
  - Each `Update()` (60 Hz): for each entity in `animStates`, increment `Elapsed` by `1.0/60.0`; when `Elapsed >= 1.0/def.FPS`, advance `Frame` (wrap if `Loop`, clamp to last frame if not), reset `Elapsed`
  - When `comp_sprite.animation` differs from `AnimState.CurrentAnim`: reset `AnimState` to frame 0, elapsed 0
- [ ] `setAnimation` built-in action registered in the action registry:
  - Params: `animation string`
  - Effect: calls `WorldWriter.SetComponentValue(entityID, "Sprite", "animation", params["animation"])`
  - Registered at startup alongside `setTimer`, `log`, etc.
- [ ] `Draw()` renders all entities with both `Position` and `Sprite` components:
  - Query: `SELECT e.id, cp.x, cp.y, cs.sheet, cs.animation, cs.flip_x FROM entities e JOIN comp_position cp ON e.id = cp.entity_id JOIN comp_sprite cs ON e.id = cs.entity_id`
  - For each entity: look up `AnimState`; look up `AnimDef` by `animation` name; load sheet image (cached by path); draw the sub-rect `(Frame * tileSize, 0, tileSize, tileSize)` at `(x * tileSize, y * tileSize)`; apply horizontal flip if `flip_x`
  - Fall back to solid colour rectangle if sheet is empty string, image fails to load, or animation not found
- [ ] Sprite sheet images are cached in `Game` (`map[string]*ebiten.Image`) — loaded once, reused every `Draw()`
- [ ] `go test ./...` passes; player displays sprite animation when moving vs idle

## Notes

- `ebiten.Image` caching: load on first use, cache by sheet path. On animation TOML hot-reload, do NOT evict the image cache — only the animation definitions change.
- Horizontal flip: use `ebiten.DrawImageOptions` with `GeoM.Scale(-1, 1)` and then `GeoM.Translate(tileSize, 0)` to flip in place.
- The `AnimState` map is keyed by entity ID (int64). Entries are created on first draw, removed if the entity is no longer returned by the query (entity deleted).
- Frame index in the sprite sheet: `Frame` is the column index. Sub-rect: `image.Rect(Frame*tileSize, 0, (Frame+1)*tileSize, tileSize)`.
- The query runs every `Draw()`. This is acceptable for the prototype (small entity count). If it becomes a bottleneck, add a dirty flag driven by `world_version`.
