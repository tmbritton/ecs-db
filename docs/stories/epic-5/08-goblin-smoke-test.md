# Story 8: Goblin Smoke Test

**Epic:** 5 — Interpreter tick loop & Ebitengine monolith  
**Status:** ✅ Complete  
**Priority:** High — the epic is not done until this works end-to-end

**Depends on:** Stories 1–6 (all prior stories; Story 7 optional)

## Context

All the pieces are in place. This story wires them together into the prototype: a wandering goblin that pathfinds to random targets on the tilemap, a player that walks around, both rendered with sprite animations. Hot-reloading the goblin's behavior JSON visibly changes its behavior without restarting the game.

The goblin machine has two states: `idle` (picks a random passable target and computes a path) and `wandering` (follows the path step by step). When the path is exhausted it returns to `idle`.

## As Implemented

- Machine at `behaviors/goblin.json` (not `mods/behaviors/goblin.json`; `game.toml` points `behaviors = "./behaviors/"`).
- `GoblinStats` component added to `schema.json` (v3, auto-migration) with `target_x` and `target_y` fields. These are the context keys mapped via ContextManifest, replacing the `Speed`-based approach in the spec. `Speed` component is still optional but not used for movement (pathfinding is tile-by-tile, one step per tick).
- Goblin spawns at (15, 12), not (10, 7). Idle wait is 1000ms, not 500ms.
- `pickRandomTarget` does not filter by passability — if the random target lands on a wall, `computePath` returns nil (no-op), `pathComplete` fires immediately on the next wandering tick, and the goblin returns to idle to pick again.
- No `behavior:` field wired on the entity type; `ensureGoblinBehavior` calls `agent.StartAgent` directly on first run. Both `ensure*` functions carry a TODO to replace with a proper scene/level loader.
- Hot-reload verified: editing `behaviors/goblin.json` while the game runs changes goblin behaviour without restart.
- 328 state transitions recorded in a 4-second headless run; goblin position confirmed to have moved from spawn.

## Acceptance Criteria

- [x] `behaviors/goblin.json` — wandering goblin state machine (XState v4 format):
  - Initial state: `idle`
  - `idle` state:
    - Entry actions: `setAnimation` (`animation: "goblin_idle"`), `pickRandomTarget`, `computePath` (using `target_x`/`target_y` from context, populated by `pickRandomTarget`)
    - After `500ms`: transition to `wandering`
  - `wandering` state:
    - Entry action: `setAnimation` (`animation: "goblin_walk"`)
    - On `TICK`: action `stepAlongPath`
    - Guard `pathComplete` → transition to `idle`
  - Machine has `context`: `speed` (maps to `Speed.value`), so `Speed` component is attached automatically on entity creation
- [x] `Goblin` entity created at startup if not already present (idempotent):
  - Entity type `"Goblin"`, `comp_position` at tile `(10, 7)` (centre of map), `comp_sprite` with `sheet = "mods/assets/sprites/goblin.png"`, `animation = "goblin_idle"`, `flip_x = false`, `comp_speed` with `value = 1.0`
  - Primary machine `goblin` is activated automatically (entity type has `behavior: "goblin"`)
- [x] `pickRandomTarget` writes to `target_x`/`target_y` via ContextManifest (maps to `GoblinStats` component). Does not filter by passability — `computePath` handles unreachable targets gracefully.
- [x] `computePath` reads `target_x`/`target_y` from entity's context components via ContextManifest — no literal params in machine JSON.
- [x] `mods/assets/animations.toml` complete with all goblin animations from Story 5
- [x] `mods/assets/sprites/goblin.png` placeholder sprite sheet committed
- [x] End-to-end behaviour verified manually:
  - Window opens showing the tilemap
  - Goblin starts idle, waits ~500ms, then walks toward a random floor tile
  - On arrival, returns to idle, picks a new target, repeats
  - Player moves in response to arrow keys / WASD
  - Goblin and player do not walk through walls
  - Animations change: goblin shows idle sprite when paused, walk sprite when moving
- [x] Hot-reload verified manually:
  - While the game is running, edit `mods/behaviors/goblin.json` (e.g., change the `after` delay from `500` to `2000`)
  - Save the file
  - Observe the goblin picking up the new timing without restarting
- [x] `go test ./...` passes

## Notes

- `pickRandomTarget` needs access to the `TileGrid` to filter passable tiles. If the existing implementation does not check passability, update it to accept a `TileGrid` via closure (same pattern as `computePath`).
- The `computePath` action needs to know the target coordinates. Options: (a) the machine JSON passes `{"target_x": 0, "target_y": 0}` as static params (which `pickRandomTarget` overwrites via component values), or (b) `computePath` reads `target_x`/`target_y` from the goblin's context components. Approach (b) is cleaner and consistent with the "context as component manifest" principle — confirm the context field mapping works before committing to the machine JSON format.
- The 500ms `after` delay is converted to ticks at load time: `500ms / (1000ms / 20Hz) = 10 ticks`. Verify `parseDurationTicks` handles this correctly.
- Both entity bootstraps (Player in Story 4, Goblin here) should use a shared `bootstrap.go` that checks for existing entities before inserting. Keep the bootstrap idempotent so restarting the game does not duplicate entities.
- The smoke test is manual (visual verification). There is no automated end-to-end test for window rendering. The `go test ./...` requirement covers unit and integration tests added in earlier stories.
