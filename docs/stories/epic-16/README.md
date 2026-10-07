# Epic 16 — Forge: TILES and SPRT modes

Tileset metadata authoring and sprite-sheet slicing, on the formats the game
actually reads. The roadmap's three bullets are refined below before touching
either mode.

## Verified against the code

| Roadmap assumption | What the code does |
|---|---|
| Epic 14 supplies a TSX writer | **False.** `tiled.ParseTileset` reads TSX/TSJ and embedded map tilesets. `tiled.Document` is a lossless writer for **TMX maps only** and refuses a root other than `<map>`. Its `xtree` machinery is reusable, but there is no editable TSX document or save route. |
| TILES edits hot-reload | **False.** `cmd/ecs-db/run.go` watches behaviours, `animations.toml` and the first assets mod's `sprites/` directory. A changed `.tsx` takes effect at the next `ecs-db run`. The mode and save confirmation must say so. |
| Every Collision / Animation / Terrain / Class tab changes the game | **False.** `tilemap.stateOf` reads per-tile/tileset `passable` and tile `type`/`class`; it does not read collision polygons, wangsets, terrain definitions or per-tile `<animation>`. `tiled.Tileset` currently drops those shapes on parse. Show only supported effects as editable engine properties. Unsupported metadata may be read and described as editor-only, but never sold as runtime collision or animation. |
| A tileset is always a rectangular sheet | **False.** `Tileset.Collection()` identifies a sparse image collection. A grid of `0..tilecount-1` only describes a sheet. Collection tiles need their own pictures and declared IDs. |
| SPRT can slice an arbitrary sheet | **False.** The renderer crops `image.Rect(column*tileSize, 0, (column+1)*tileSize, tileSize)` and reads column indices from `[[animation]].frames`. Only a one-row horizontal strip of square `window.tileSize` frames is presently playable. |
| All mods' assets are merged | **False.** `cmd/ecs-db/run.go` selects the **first** mod with a nonempty `assets` directory for `animations.toml` and `sprites/`. An editor presenting a later mod's sheet as game-loaded would lie. |
| The sprite file already has a lossless writer | **False.** `renderer.AnimLoader.Load` decodes `[[animation]]` and `[[entity_asset]]` with BurntSushi/toml into runtime structs. Re-encoding only those fields would discard comments, ordering and unknown metadata. SPRT needs an editing session and a preservation decision before Save. |

## Contracts and boundaries

- TSX editing is an external tileset's own file session, not a mutation of the
  TMX that references it. The engine may resolve one TSX from several maps;
  editing it once must update every affected preview. `.tsj` remains readable
  and explicitly read-only until it has its own lossless writer.
- Reuse `internal/tiled`'s copy-on-write XML tree for TSX. Never emit from the
  `Tileset` reading model: it omits per-tile collision object groups, animation,
  wangsets, terrains, editor settings and XML it has never heard of. An edit to
  `passable` or `type` must leave those bytes intact, and the parser must read
  back the edited meaning. Do not convert a TSJ or rewrite an embedded tileset
  in the first story under a TSX-save label.
- TILES may edit the engine's Boolean `passable` and per-tile class/type.
  Polygon collision objects and Tiled tile animation do **not** control this
  game's collision or sprite animator. If visible as metadata, say so. Saving a
  TSX cannot hot-reload `comp_tile` or a running grid.
- SPRT writes `animations.toml` in the first assets mod, with
  `[[animation]] {name,sheet,frames,fps,loop}` and `[[entity_asset]]`
  `{entity_type,sheet}` as its contract. The renderer watches that TOML and the
  selected `sprites/` directory. Sheet file serving/copying is confined to
  that assets tree, unlike MAP's allow-list of tileset images.
- Mode state belongs in Datastar signals and save sessions, as on MAP and
  AGENTS; browser-visible behavior gets a Playwright spec before markup. Each
  browser spec pins its test IDs in Go and checks a request, file or visible
  effect rather than only an attribute.

## Stories, in dependency order

| # | Story | Delivers |
|---|---|---|
| 1 | [Lossless TSX writer](01-tsx-writer.md) | Editable external TSX document with exact no-op round trip and surgical tile-property/class edits; unknown XML survives. |
| 2 | Tileset editing session | Discover referenced external TSX/TSJ, one working value per path, dirty/save/discard/reload/conflict and project-scoped asset serving. |
| 3 | TILES read surface | Sheet grid or sparse collection, enlarged selected tile, truthful authored/inherited metadata and source file; engine-only effects labelled. |
| 4 | TILES property editing | `passable` and class/type authoring through the TSX session; independently patchable grid and inspector; save takes effect on the next run. |
| 5 | TILES validation | Parser, image and property refusals on tiles/tilesets; no silently ignored edit or false collision/animation promise. |
| 6 | Animation-file session | Lossless `animations.toml` editing, first-assets-mod discovery, save/conflict and hot-reload behavior without exposing other mods as loaded. |
| 7 | SPRT mode | One-row sheet preview, column-index frame sequencing, `fps`, `loop` and entity-to-sheet binding, within the renderer's bounds. |
| 8 | Import sprite sheet | Dialog inside SPRT that picks/copies a project-local image, verifies its dimensions and creates playable frames/binding. Epic 17's generic asset dialogs must not duplicate this flow. |

Each story gets its own acceptance criteria, implementation plan and Playwright
steps (when it has a browser surface) **before** implementation, as in Epics
12–15. TILES and SPRT can then be verified without treating any unimplemented
prototype tab as a working feature.
