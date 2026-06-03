# Story 8: Goblin Smoke Test

**Epic:** 5 — Interpreter tick loop & Ebitengine monolith  
**Status:** 🔲 Not started  
**Priority:** High — the epic is not done until this works end-to-end

**Depends on:** Stories 1–6 (all prior stories; Story 7 optional)

## Context

All the pieces are in place. This story wires them together into the prototype: a wandering goblin that pathfinds to random targets on the tilemap, a player that walks around, both rendered with sprite animations. Hot-reloading the goblin's behavior JSON visibly changes its behavior without restarting the game.

The goblin machine has two states: `idle` (picks a random passable target and computes a path) and `wandering` (follows the path step by step). When the path is exhausted it returns to `idle`.

## Acceptance Criteria

- [ ] `mods/behaviors/goblin.json` — wandering goblin state machine (XState v4 format):
  - Initial state: `idle`
  - `idle` state:
    - Entry actions: `setAnimation` (`animation: "goblin_idle"`), `pickRandomTarget`, `computePath` (using `target_x`/`target_y` from context, populated by `pickRandomTarget`)
    - After `500ms`: transition to `wandering`
  - `wandering` state:
    - Entry action: `setAnimation` (`animation: "goblin_walk"`)
    - On `TICK`: action `stepAlongPath`
    - Guard `pathComplete` → transition to `idle`
  - Machine has `context`: `speed` (maps to `Speed.value`), so `Speed` component is attached automatically on entity creation
- [ ] `Goblin` entity created at startup if not already present (idempotent):
  - Entity type `"Goblin"`, `comp_position` at tile `(10, 7)` (centre of map), `comp_sprite` with `sheet = "mods/assets/sprites/goblin.png"`, `animation = "goblin_idle"`, `flip_x = false`, `comp_speed` with `value = 1.0`
  - Primary machine `goblin` is activated automatically (entity type has `behavior: "goblin"`)
- [ ] `pickRandomTarget` built-in action extended (or new variant) to write to `target_x`/`target_y` in `comp_*` context components — verify the existing action works with the new tilemap and picks only passable tiles; if it does not check passability, update it to call `tileGrid.IsPassable` before accepting the target
- [ ] `computePath` param handling: action reads `target_x` and `target_y` from the entity's context components (i.e., reads `GetComponentValue(entityID, "Speed", "target_x")` or equivalent context field) rather than requiring literal params in the machine JSON — OR the machine JSON passes literal `target_x`/`target_y` as static params sourced from a context read. Confirm the mechanism and document it in the machine JSON comments.
- [ ] `mods/assets/animations.toml` complete with all four animations from Story 5
- [ ] `mods/assets/sprites/goblin.png` placeholder sprite sheet committed
- [ ] End-to-end behaviour verified manually:
  - Window opens showing the tilemap
  - Goblin starts idle, waits ~500ms, then walks toward a random floor tile
  - On arrival, returns to idle, picks a new target, repeats
  - Player moves in response to arrow keys / WASD
  - Goblin and player do not walk through walls
  - Animations change: goblin shows idle sprite when paused, walk sprite when moving
- [ ] Hot-reload verified manually:
  - While the game is running, edit `mods/behaviors/goblin.json` (e.g., change the `after` delay from `500` to `2000`)
  - Save the file
  - Observe the goblin picking up the new timing without restarting
- [ ] `go test ./...` passes

## Notes

- `pickRandomTarget` needs access to the `TileGrid` to filter passable tiles. If the existing implementation does not check passability, update it to accept a `TileGrid` via closure (same pattern as `computePath`).
- The `computePath` action needs to know the target coordinates. Options: (a) the machine JSON passes `{"target_x": 0, "target_y": 0}` as static params (which `pickRandomTarget` overwrites via component values), or (b) `computePath` reads `target_x`/`target_y` from the goblin's context components. Approach (b) is cleaner and consistent with the "context as component manifest" principle — confirm the context field mapping works before committing to the machine JSON format.
- The 500ms `after` delay is converted to ticks at load time: `500ms / (1000ms / 20Hz) = 10 ticks`. Verify `parseDurationTicks` handles this correctly.
- Both entity bootstraps (Player in Story 4, Goblin here) should use a shared `bootstrap.go` that checks for existing entities before inserting. Keep the bootstrap idempotent so restarting the game does not duplicate entities.
- The smoke test is manual (visual verification). There is no automated end-to-end test for window rendering. The `go test ./...` requirement covers unit and integration tests added in earlier stories.
