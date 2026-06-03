# Story 1: Schema Updates

**Epic:** 5 — Interpreter tick loop & Ebitengine monolith  
**Status:** 🔲 Not started  
**Priority:** High — prerequisite for every other Epic 5 story

**Depends on:** Epic 2 (migration system), Epic 1 (schema loader)

## Context

The existing `schema.json` has `Position` (x, y as `number`) and `Health` already declared, and a `Sprite` component with `imageId` and `frame` fields. The renderer design replaces that `Sprite` shape with `sheet` + `animation` + `flip_x` — the renderer manages the frame counter locally as `AnimState`, so `frame` moves out of the DB.

Three new components are also needed: `Tile` (for the tilemap entities), `Path` (for storing A* pathfinding results), and `Speed` (tiles per tick, used by `stepAlongPath`). The `Tile` entity type must also be declared.

The existing migration system (Epic 2) handles all of this automatically when `schemaVersion` is bumped: it will rebuild the `comp_sprite` table (dropping `imageId`/`frame`, adding `sheet`/`animation`/`flip_x`) and create the three new `comp_*` tables.

## Acceptance Criteria

- [ ] `schema.json` `schemaVersion` bumped to `2`
- [ ] `Sprite` component updated:
  - Remove `imageId` (string), `frame` (integer)
  - Add `sheet` (string), `animation` (string), `flip_x` (boolean, default `false`)
- [ ] `Tile` component added: `x` (integer), `y` (integer), `passable` (boolean), `tile_type` (string)
- [ ] `Path` component added: `waypoints` (string — JSON array of `{x,y}` objects), `current_index` (integer, default `0`)
- [ ] `Speed` component added: `value` (number — tiles per tick, e.g. `1.0`)
- [ ] `Tile` entity type added: `requiredComponents: ["Tile"]`, `allowExtraComponents: false`, `validationLevel: "strict"`
- [ ] `Goblin` entity type updated: `optionalComponents` includes `Path` and `Speed`
- [ ] Running the CLI against an existing database automatically migrates to v2 (comp_sprite rebuilt, three new tables created)
- [ ] `go test ./...` passes

## Notes

- `comp_sprite` rebuild: the migration system's table-rebuild sequence handles dropping columns that SQLite cannot `DROP COLUMN` in older versions. Existing `Goblin` and `Player` entity rows survive; their `comp_sprite` rows are dropped and recreated empty (no data to preserve in `imageId`/`frame` — this is a new DB for the prototype).
- `Path.waypoints` is stored as a JSON string column (`TEXT` in SQLite). Pathfinding actions parse/write it via `encoding/json`.
- `Speed.value` is `number` (maps to `REAL` in SQLite). `1.0` means move one tile per `stepAlongPath` call.
- The `Tile` entity type does not declare a `behavior` — tiles are passive data, not agents.
- `Player` and `Goblin` entity types already exist; only their `optionalComponents` lists change.
