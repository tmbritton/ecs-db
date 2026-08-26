# Story 2: Map editing session

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** 🔲 Not started  
**Priority:** High — the server-side spine of the mode

**Depends on:** Story 1

## Context

Epics 12 and 13 both learned that the editing session wants its own story.
`session.Session` holds the one `schema.json`; `machines.Session` holds N
machine files with mod-override order, create, rename, delete and per-file save.
MAP needs the same thing for maps, and maps are the hardest case so far.

A map is not one file. It names tilesets, which are files, which name images,
which are files — and `tiled.ResolveTilesets` reads them from disk relative to
the map. So "the map is dirty" is a question about one document, but "can this
map be drawn" is a question about a small tree of them, and a tileset edited by
Epic 16 underneath an open map is a real sequence.

Forge also does not currently know where any map is. `project.Open` reads
`cfg.Map.Path` to decide whether pathfinding actions exist in the registry, and
then drops it — `Project` has no map field. There is exactly one map path in
`game.toml` and the design shows a tab strip of several.

## Acceptance Criteria

- [ ] `project.Project` carries the configured map path, resolved the same way
      every other path is — by `config.Load`, once, so the engine and Forge
      cannot disagree about which file a config means
- [ ] A `maps` session on Epic 11's `editable.File`: open, working value, dirty
      by comparison against the bytes on disk, save, discard, reload, and the
      `ConflictError` path when the file changed underneath
- [ ] Saving goes through Story 1's writer and `atomicfile`, and reports through
      `savereport` like every other save in Forge
- [ ] Dirty is a comparison and not a flag, which is only true if the writer is
      byte-stable — an edit and its exact reversal leaves the map clean
- [ ] Which maps the project has is answered explicitly: the configured map,
      plus the `.tmx`/`.tmj` files beside it. Creating and opening arbitrary
      maps is Epic 17's dialogs; this story decides what the tab strip lists and
      why, and a project with no map configured is a state the mode renders
      rather than a failure
- [ ] Tilesets resolve when a map is opened, and a map whose tileset will not
      open is a `Problem` with the file and the reason — not a blank canvas
- [ ] A map that fails to parse is still listed, with its reason, for the same
      argument `project.Problem` carries: Forge's job includes showing you the
      broken file
- [ ] Concurrency is the session's, not the handler's — `editable.File` is
      documented as unsafe for concurrent use and this is the value that owns it
- [ ] MAP mode's save footer is wired to it and reports per map, the way AGENTS
      reports per machine
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/15-map-session.spec.js`.

- [ ] The mode opens the project's configured map and names it
- [ ] The footer reads `✓ saved` on a freshly opened map
- [ ] An edit made through any Story 4 route flips the footer to `● unsaved`
- [ ] Discard restores the file and the footer, with no write to disk
- [ ] Save writes the file, the footer goes clean, and a reload keeps it clean
- [ ] A map changed on disk while open reports the conflict and offers both ways
      out, as SCHEMA and AGENTS do
- [ ] A project whose map path names a file that is not there renders the mode
      with the reason, not an empty canvas

## Notes

- **Do not generalise `machines.Session` into a shared one.** They will look
  alike and diverge: machines have mod-override order and maps do not; maps have
  a resolved tileset tree and machines do not. Two clear sessions beat one that
  is mostly `if kind == …`.
- **A tileset is shared state between MAP and TILES.** Epic 16 edits the same
  `.tsx` this session resolved. Whether the resolved tree is re-read on demand
  or held is a decision worth taking here rather than in Epic 16, where it would
  arrive as a stale-palette bug.
- Renaming a map file is a spawn-identity event, not a filesystem one — Epic 14
  Story 9's `mapId` is what makes it survivable. Renaming is Epic 17's; this
  story only has to avoid making it worse.
