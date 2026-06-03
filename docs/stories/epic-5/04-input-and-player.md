# Story 4: Input Capture + Player Movement

**Epic:** 5 — Interpreter tick loop & Ebitengine monolith  
**Status:** 🔲 Not started  
**Priority:** High — validates the full input pipeline end-to-end

**Depends on:** Story 3 (TileGrid for passability checks), Story 2 (tick loop + input_events table)

## Context

Input capture is a two-step pipeline: the renderer writes raw key events to `input_events` during `Update()`, and the interpreter drains that table at tick time and dispatches to a registered Go handler. This story wires both ends and uses them to move a `Player` entity on the tilemap.

The `Player` entity has no state machine. Movement is driven entirely by the input handler: on each tick, it inspects the drained `input_events` rows for directional keys, checks `TileGrid` passability, and calls `WorldWriter.SetComponentValue` to update `comp_position`. It also sets `comp_sprite.animation` and `flip_x`.

At the end of this story, the player entity is visible on the map (placeholder rectangle or sprite) and moves one tile per tick in response to arrow key / WASD input.

## Acceptance Criteria

- [ ] `Update()` samples Ebitengine input every frame (60 Hz) and appends rows to `input_events`:
  - Uses `ebiten.IsKeyPressed` for held directional keys (ArrowUp, ArrowDown, ArrowLeft, ArrowRight, W, A, S, D)
  - `kind = "key_held"`, `payload = {"key": "ArrowRight"}` (JSON)
  - `consumed = 0` on insert
- [ ] `internal/game/input_handler.go` — `PlayerInputHandler` struct implementing the interpreter's input drain callback:
  - Registered at startup as the game-specific input handler
  - Receives a slice of drained `input_events` rows each tick
  - Collapses multiple `key_held` rows for the same key into one direction decision (last-held wins, or any-held)
  - For each movement direction found: reads `comp_position.x`, `comp_position.y` of the player entity; computes target tile; checks `tileGrid.IsPassable(tx, ty)`; if passable, calls `WorldWriter.SetComponentValue` for `x` and `y`
  - Sets `comp_sprite.animation = "player_walk"` when moving, `"player_idle"` when not
  - Sets `comp_sprite.flip_x = true` when moving left, `false` otherwise
  - Marks rows `consumed = 1` after processing
- [ ] `Player` entity created at startup if not already present (idempotent): entity type `"Player"`, `comp_position` at tile `(2, 2)`, `comp_sprite` with `sheet = ""` (placeholder), `animation = "player_idle"`, `flip_x = false`
- [ ] Player is visible in `Draw()` as a solid colour rectangle at `(comp_position.x * tileSize, comp_position.y * tileSize)` — no sprite sheet required yet
- [ ] Player cannot walk through wall tiles (passable check enforced)
- [ ] One tile of movement per interpreter tick maximum (multiple `key_held` rows collapse to one move)
- [ ] `internal/game/input_handler_test.go` — unit tests:
  - Handler with no input rows → no position change
  - Handler with `key_held ArrowRight` → x incremented by 1 if passable
  - Handler with `key_held ArrowRight` into a wall → position unchanged
- [ ] `go test ./...` passes; player moves on the tilemap

## Notes

- The input handler receives `WorldWriter` (write access to component tables) and `WorldReader` (read access) plus the `*TileGrid`. Wire these at startup via closures or dependency injection — the same pattern used for built-in action/guard handlers.
- `input_events` schema: `id INTEGER PK AUTOINCREMENT`, `tick INTEGER`, `kind TEXT`, `payload TEXT`, `consumed INTEGER DEFAULT 0`, `wall_ms INTEGER`. The drain query is `SELECT id, kind, payload FROM input_events WHERE consumed = 0 ORDER BY id ASC`. Update `consumed = 1` after reading.
- `wall_ms` is populated at insert time with the current Unix milliseconds.
- The player entity's starting position (`2, 2`) should be a passable floor tile. Verify against `level1.toml`.
- `comp_sprite.sheet` is empty in this story — the renderer draws a coloured rectangle for any entity with a missing or empty sheet. Sprite rendering comes in Story 5.
- The `PlayerInputHandler` only processes the `Player` entity. It looks up the player entity ID at startup via `WorldReader.FindEntityByType("Player")` and stores it. Panics loudly (or returns error) if no player entity exists at startup.
