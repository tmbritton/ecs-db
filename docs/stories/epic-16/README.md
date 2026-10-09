# Epic 16 — Forge: TILES and SPRT modes

Occupant-aware grid traversal and MAP authoring, then tileset metadata and
sprite-sheet slicing on the formats the game actually reads. The roadmap's
three bullets are refined below before touching either asset mode.

## Verified against the code

| Roadmap assumption | What the code does |
|---|---|
| Epic 14 supplies a TSX writer | **False.** `tiled.ParseTileset` reads TSX/TSJ and embedded map tilesets. `tiled.Document` is a lossless writer for **TMX maps only** and refuses a root other than `<map>`. Epic 16 Story 1 added `TilesetDocument` using its `xtree`; the TSX editing session and save route belong to Story 5. |
| TILES edits hot-reload | **False.** `cmd/ecs-db/run.go` watches behaviours, `animations.toml` and the first assets mod's `sprites/` directory. A changed `.tsx` takes effect at the next `ecs-db run`. The mode and save confirmation must say so. |
| Every Collision / Animation / Terrain / Class tab changes the game | **False.** Epic 16 Stories 3–4 retired Boolean `passable`: traversal and sight consult independently configured interactions against the entities referenced by positioned Tiles. Collision polygons, wangsets, terrain definitions and per-tile `<animation>` have no game consumer. `tiled.Tileset` drops those shapes on parse; Story 8 reports them as Tiled-only metadata while the TSX writer preserves their XML. |
| A tileset is always a rectangular sheet | **False.** `Tileset.Collection()` identifies a sparse image collection. A grid of `0..tilecount-1` only describes a sheet. Collection tiles need their own pictures and declared IDs. |
| SPRT can slice an arbitrary sheet | **False.** The renderer crops `image.Rect(column*tileSize, 0, (column+1)*tileSize, tileSize)` and reads column indices from `[[animation]].frames`. Only a one-row horizontal strip of square `window.tileSize` frames is presently playable. |
| All mods' assets are merged | **False.** `cmd/ecs-db/run.go` selects the **first** mod with a nonempty `assets` directory for `animations.toml` and `sprites/`. An editor presenting a later mod's sheet as game-loaded would lie. |
| The sprite file already has a lossless writer | **False.** `renderer.AnimLoader.Load` decodes `[[animation]]` and `[[entity_asset]]` with BurntSushi/toml into runtime structs. Re-encoding only those fields would discard comments, ordering and unknown metadata. SPRT needs an editing session and a preservation decision before Save. |

## Contracts and boundaries

**Movement decision (before TILES authoring):** The first map stays grid-based
and has no collision polygons. Walls are **separate entities referenced by
Tiles**; one water or River entity may be referenced by Tiles at an irregular
set of cells. Each Tile owns Position and any number of entity references. A mover's
ability to enter a cell depends on its components and the components of the
entities occupying that cell, not on `Tile.passable` or a tile class string.
Walking, swimming, flying and phasing therefore have different outcomes at a
wall or water.
Story 1's generic XML writer proving it can change a `passable` property does
not mean TILES should offer a universal passability toggle.

**Confirmed independent interactions:** `Passability` and `Visibility` are
attachable components on **any** entity. They carry taxonomy/enum values,
not universal Boolean fields or hard-coded wall/water type switches. A mover's
**independent components** — e.g. Flying, Swimming or Phased — are evaluated
against the `Passability` taxonomy of **every** destination occupant. Every
restricting occupant must allow that mover. A flyer crosses a wall and river;
a swimmer crosses water; a phased walker crosses a wall. An in-bounds cell
without art is enterable when no occupant denies it. Referencing Tile
Positions define a linked River's occupied cells; an unreferenced runtime
entity may use its own Position/OccupiedCells offsets.

`Visibility` is evaluated separately against the observer's **independent
components**, such as NightVision; those abilities are not values in a
universal vision-mode enum. A person sees over water they cannot traverse. A
phased Kitty Pryde crosses a wall but does **not** see through it merely by
phasing.
Story 3 removed `LineOfSight`'s dependency on `TileGrid.IsPassable`. Schema's
`interactions` section now declares the taxonomy vocabulary and which boolean
capability components permit each category; an unknown category is refused
rather than silently becoming passable or transparent. Visibility
controls sight occlusion here, not renderer drawing. DetectsMagic illustrates
another independent observer capability, but revealing a hidden magical
*target* is a **later detection feature**, not Story 3's occlusion rule. See
[Story 3](03-occupant-traversal.md).

**Rendering decision — Story 2 foundation, corrected in Story 3:** the former importer created
one Tile entity for the topmost nonempty tile per cell, while the renderer
drew every visual layer directly from TMX/TSX. Lower visible tiles were not
entities, and changing a Tile entity did not change the cached artwork. Now
**every nonempty authored layer tile is a Tile entity**, including
hidden layers. The Tile owns Position, references and layer order; its
**referenced entities own visual components**. The renderer resolves those
visuals at Tile positions, with PNGs remaining file-backed assets.
`mapId + layerID + cell` is the stable tile identity;
reordering a layer does not replace its entities. Forge's unsaved working-map
preview may still draw directly from the editor's TMX because that is an
authoring view, not the game world's state.

A linked River can supply a visual to several referencing Tiles without
cloning its entity ID; moving or relinking those Tiles moves both art and
occupancy. An unreferenced
entity with Sprite and OccupiedCells still draws across its own footprint.

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
- TILES shows tile pictures, class and a tile's reference/type template.
  **MAP** paints positioned Tile entities and authors their references to
  separate Wall, River or other entity instances, including references shared
  by several Tiles. Do not offer a global `passable` toggle or polygon
  collision authoring. Tiled tile animation does not control this game's sprite
  animator. Saving a TSX cannot hot-reload the running game's grid.
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
| 2 | [Entity-backed tile layers](02-entity-backed-tiles.md) | Import every authored layer tile as a stable entity and render tile instances from database components, not a static TMX snapshot. |
| 3 | [Occupant-aware traversal](03-occupant-traversal.md) | Replace universal tile `passable` with a predicate over every entity referenced by the destination's positioned Tiles, shared by player movement, A*, reachability and path steps; separate line of sight. |
| 4 | [MAP occupant authoring](04-map-occupant-authoring.md) | Painting a Tile instantiates or links its referenced entities; inspect/edit links and share one River instance among an irregular set of Tiles, without polygons. |
| 5 | [Tileset editing session](05-tileset-session.md) ✅ | Discover referenced external TSX/TSJ, one working value per path, dirty/save/discard/reload/conflict and project-scoped path checks. |
| 6 | [TILES read surface](06-tiles-read-surface.md) ✅ | Sheet grid or sparse collection, enlarged selected tile, truthful authored metadata and source file; no collision promise. |
| 7 | [TILES property editing](07-tiles-editing.md) ✅ | Per-tile class/type and supported art metadata through the TSX session; independently patchable grid and inspector; save takes effect on the next run. |
| 8 | [TILES validation](08-tiles-validation.md) ✅ | Parser, image and metadata refusals on tiles/tilesets; no silently ignored edit or false polygon/animation promise. |
| 9 | [Animation-file session](09-animation-session.md) ✅ | Lossless `animations.toml` editing, first-assets-mod discovery, save/conflict and hot-reload behavior without exposing other mods as loaded. |
| 10 | [SPRT mode](10-sprt-mode.md) ✅ | One-row sheet preview, column-index frame sequencing, `fps`, `loop` and entity-to-sheet binding, within the renderer's bounds. |
| 11 | [Import sprite sheet](11-import-sprite-sheet.md) ✅ | Dialog inside SPRT that picks/copies a project-local image, verifies its dimensions and creates playable frames/binding. Epic 17's generic asset dialogs must not duplicate this flow. |

Each story gets its own acceptance criteria, implementation plan and Playwright
steps (when it has a browser surface) **before** implementation, as in Epics
12–15. TILES and SPRT can then be verified without treating any unimplemented
prototype tab as a working feature.

After Story 11 completes Epic 16, the next implementation target is the
[turn-based point-and-click gameplay demo](../../plan.md#gameplay-milestone-turn-based-point-and-click-demo).
The remaining Forge epics resume after that playable milestone.
