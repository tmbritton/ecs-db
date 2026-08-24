# Story 4 — implementation plan

## Verified before planning

| Claim | Verified |
|---|---|
| `LoadMap` reads TOML and switches on `'.'`/`'#'` | True — `tilemap/loader.go:94`. |
| `TileGrid` needs no change; `Rebuild` reads `comp_tile.passable` | True — `grid.go:48`. It has never seen a character. |
| Story 3 gives re-import, so this story reuses it | True — `LoadMap` already builds a `map[Point]TileState` and hands it to `SyncTiles`. Only how that map is built changes. |
| Stories 1 and 2 give a parsed map and resolved tilesets | True — `tiled.Parse`, `Map.ResolveTilesets`, `Map.TilesetFor`, `Tileset.Passable`, `TilesetTile.Type`. |
| Multiple tile layers mean two tiles in one cell | True, and `comp_tile` can hold one row per cell. The rule is forced, not chosen. |
| The only caller of `LoadMap` is a composition root | True — `cmd/ecs-db/run.go:78`, and nothing else in the repo. |
| An infinite map would decode to nothing | False — Story 1 already refuses it by name, both serialisations. |
| `Map.Orientation` is validated | **False.** It is parsed and nobody checks it. An isometric map's cells are not this package's grid cells. |
| The two serialisations parse to the same value | **False.** See below. |

### The one that changes the story

`collectXML` (`tiled/tmx.go:217`) appends a map's direct tile layers first and
only then recurses into its layer folders, so a `<group>` written *above* a
plain layer comes out *below* it. `collectJSON` walks its layers in one pass and
gets it right. Measured on the same one-cell map in both serialisations:

```
tmx order: [over under]
tmj order: [under over]
```

That is Story 1's defect, and it is invisible until something depends on layer
order. This story's central decision is a layer-order rule, so it lands here:
the same map exported both ways would disagree about which tile is on top, and
the acceptance criterion is that the answer is the same every run.

Fixed first, as its own change with its own tests, then built on.

## The decisions this story has to state

1. **One cell, one tile: the topmost non-empty tile is the cell's tile.** It
   decides both `passable` and `tile_type`. Not a preference — `comp_tile` is
   keyed by entity and holds one row per cell, so the loader must choose one
   tile out of the stack, and the one the player sees is the one the cell is.
2. **A hidden layer still counts.** Visibility is what the editor shows, not
   what the map is. An author who hides the wall layer to look at the floor
   underneath, saves, and loses every wall has been robbed by a view setting.
   Story 5 honours `Visible` for drawing, which is what it is for.
3. **A tile whose tileset declares no `passable` is passable**, and a tileset
   may declare `passable` itself to change that for every tile in it. A map is a
   floor with obstacles on it: the default that makes a 200-tile decoration set
   usable without 200 declarations is the one that will not be scripted around.
   The asymmetry with an empty cell — impassable, because `TileGrid` has no
   entry for it — is deliberate: nothing to stand on is not the same as a tile
   that says nothing.
4. **`tile_type` is the tile's class, else the tileset's class, else empty.**
   The renderer's `switch` draws anything that is not `"wall"` as floor, so an
   undeclared class draws rather than disappears.
5. **A gid belonging to no tileset is refused by layer and position**, not
   skipped.
6. **A non-orthogonal map is refused.** `Point` is a square grid cell.

## Shape

| Action | File | What |
|---|---|---|
| Modify | `internal/tiled/tmx.go` | Walk a map's and a group's children in document order. |
| Modify | `internal/tiled/tiled_test.go` | Both serialisations agree on layer order, with groups interleaved. |
| Create | `internal/tilemap/tiled.go` | `tilesOfTiled` — the layer rule, the property reads and the refusals. |
| Create | `internal/tilemap/tiled_test.go` | The rule, per decision above. |
| Modify | `internal/tilemap/loader.go` | `LoadMap` sniffs the file and dispatches; the TOML path is untouched. |

`LoadMap` keeps its signature. The TOML reader stays until Story 7 deletes it
along with the file it reads — `game.toml` still points at `level1.toml`, and a
story that changes the loader and migrates the map is two stories.

## Verification

- Unit tests first, then a mutation battery over every guard, in the background.
- A fresh-context review before the commit.
- Real-data smoke: generate the Tiled equivalent of `mods/map/level1.toml`, load
  both against the real `schema.json`, and assert the two `TileGrid`s are
  identical cell for cell. That is a stronger statement than watching the goblin
  wander, and it is what the goblin AC actually means: pathfinding, line of
  sight and reachability all read the grid and nothing else.
