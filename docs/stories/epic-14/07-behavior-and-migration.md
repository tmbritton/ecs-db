# Story 7: Entity-type behaviour at spawn, and the migration

**Epic:** 14 — Tiled map & tileset formats  
**Status:** 🔲 Not started  
**Priority:** High — closes the epic and deletes the last of the bootstrap

**Depends on:** Story 6

## Context

`schema.EntityType` has a `Behavior` field, the architecture doc assumes it, and
no entity type in `schema.json` uses one — so the goblin's machine is started by
hand in `ensureGoblinBehavior`. This story makes the binding real: an entity
spawned as a type that declares a behaviour gets that machine started, and the
hand-written call goes.

It also migrates the one map that exists. `mods/map/level1.toml` becomes
`level1.tmx` with a tileset, a tile layer and an object layer carrying the two
spawns Story 6 now reads, and `game.toml` points at it.

That migration is what makes the epic's claim testable end to end: the goblin
wanders, on a map drawn from a tileset, spawned from an object layer, running a
machine bound in the schema — with nothing about the level in Go.

## Acceptance Criteria

- [ ] An entity type declaring `"behavior"` starts that machine when spawned
- [ ] A type declaring a behaviour that does not resolve is reported at spawn,
      naming the type and the machine
- [ ] `ensureGoblinBehavior` is gone
- [ ] `Goblin` declares `"behavior": "goblin"` in `schema.json`
- [ ] `mods/map/level1.tmx` and a starter tileset replace `level1.toml`, with
      the same 20×15 layout
- [ ] `game.toml` points at the new map
- [ ] `ecs-db run` on a fresh database produces the same game it did before this
      epic: the player at the same place, the goblin wandering, pathfinding and
      line-of-sight working against the same walls
- [ ] Nothing in `cmd/` creates an entity or starts a machine
- [ ] `go test ./...` passes

## Notes

- The end-to-end check is the point. Every story before this one can pass while
  the game is broken; this is the one that says the epic worked.
- The old `.toml` loader can go the moment nothing reads it. Leaving it as a
  second way to load a map is a second thing to keep working.
- Epic 15's MAP mode writes this format. Anything left ambiguous here becomes a
  question Forge has to guess at.
