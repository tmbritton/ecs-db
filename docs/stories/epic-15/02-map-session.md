# Story 2: Map editing session

**Epic:** 15 — Forge: MAP mode (AUTHORED)  
**Status:** ✅ Complete  
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

- [x] `project.Project` carries the configured map path, resolved the same way
      every other path is — by `config.Load`, once, so the engine and Forge
      cannot disagree about which file a config means
- [x] A `maps` session on Epic 11's `editable.File`: open, working value, dirty
      by comparison against the bytes on disk, save, discard, reload, and the
      `ConflictError` path when the file changed underneath
- [x] Saving goes through Story 1's writer and `atomicfile`, and reports through
      `savereport` like every other save in Forge
- [x] Dirty is a comparison and not a flag, which is only true if the writer is
      byte-stable — an edit and its exact reversal leaves the map clean
- [x] Which maps the project has is answered explicitly: the configured map,
      plus the `.tmx`/`.tmj` files beside it. Creating and opening arbitrary
      maps is Epic 17's dialogs; this story decides what the tab strip lists and
      why, and a project with no map configured is a state the mode renders
      rather than a failure
- [x] Tilesets resolve when a map is opened, and a map whose tileset will not
      open is a `Problem` with the file and the reason — not a blank canvas
- [x] A map that fails to parse is still listed, with its reason, for the same
      argument `project.Problem` carries: Forge's job includes showing you the
      broken file
- [x] Concurrency is the session's, not the handler's — `editable.File` is
      documented as unsafe for concurrent use and this is the value that owns it
- [x] MAP mode's save footer is wired to it and reports per map, the way AGENTS
      reports per machine
- [x] `go test ./...` passes

## Playwright steps

`e2e/specs/15-map-session.spec.js`.

- [x] The mode opens the project's configured map and names it
- [x] The footer reads `✓ saved` on a freshly opened map
- [~] ~~An edit made through any Story 4 route flips the footer to `● unsaved`~~
      — **moved to Story 4.** This story ships no edit route, so there is no way
      to dirty a map from a browser. Proven in Go, where an edit is one call
- [~] ~~Discard restores the file and the footer, with no write to disk~~ — same
      reason; `TestDiscard_RestoresTheLastSaveAndNotWhatIsOnDiskNow` proves the
      part that matters, which is that Discard is not Reload
- [~] ~~Save writes the file, the footer goes clean, and a reload keeps it clean~~
      — same reason
- [x] A map changed on disk while open reports the conflict and offers both ways
      out, as SCHEMA and AGENTS do
- [x] A project whose map path names a file that is not there renders the mode
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

## As Implemented

`internal/forge/maps` is a sibling of `internal/forge/session` and
`internal/forge/machines`, not a generalisation. One
`editable.File[*tiled.Document]` per map, one lock, and every operation checked
against the maps the session actually holds.

**Which maps a project has**: the map `game.toml` names, then the `.tmx` and
`.tmj` files beside it, by name. Its own directory and no other, with no
recursion — Epic 17's Open Map dialog is where "some other file somewhere else"
belongs. The configured map comes first and is **marked**, because a project can
hold five maps and `ecs-db run` reads exactly one; editing any of the others is
real work that changes nothing about the game until the config says so, and a
strip that hid that would be lying by omission.

**A `.tmj` is listed as a problem rather than passed over.** It is a map the
project has, and Story 1's refusal says what to do about it, which is more use
than a file that silently is not there.

**Tilesets are resolved on demand and never held.** `Resolved` parses the
document's current bytes fresh and resolves against the map's directory each
time, so a `.tsx` edited by Epic 16 — or by Tiled in another window — is read
again rather than served from a tree nothing can invalidate. It also keeps
resolution off `Document.Map()`, which is memoised: resolving into that value
would give the resolution a lifetime ending at the next edit, for no reason a
caller could predict.

**The list refreshes before MAP renders.** A map can appear beside the others
without Forge doing anything, and Tiled saving a new level into the same
directory is an ordinary afternoon. A file already open keeps its working value,
so a refresh caused by somebody else's file cannot discard an edit in progress.

**A map that vanishes is let go of; one that vanishes with unsaved work is
kept and reported.** Work held for a map that is in no tab is reachable from
nothing, so losing sight of it is worse than losing it — nobody knows to look.
`Held()` is what the save and discard routes check against, so the message
("saving it writes the file back") names something the UI can actually do.
`machines.Session` made the same distinction for the same reason.

**The footer's "elsewhere" notice became a struct.** It was two positional
parameters across two footers; maps made it three sources and three footers, and
the whole point of it is that switching modes must not make unsaved work read as
saved.

Found by review, and the two that mattered:

- **MAP's Save and Discard acted on the configured map, whatever map was on
  screen.** The actions posted with no `?map=`, and the route defaulted to the
  first map it held. So the confirm dialog named the map you were looking at
  while the request destroyed a different one's work — and reported it as saved.
  The actions name the map now, and the routes refuse a request that names none:
  defaulting is what made this silent rather than a visible refusal.
- **A conflict's "Use theirs" and "Keep mine" posted to `/forge/schema/`
  whatever file the conflict was on.** Older than this story — AGENTS had it too
  — and this story is what makes map conflicts producible. "Use theirs" on a map
  conflict reloaded `schema.json`, discarding unsaved schema work to resolve a
  problem somewhere else entirely. The resolutions are computed per file now,
  from whichever session holds the path, and a path no session claims renders no
  buttons rather than the wrong ones.

Also from review: "This project declares no map" was shown to projects whose
configured map was merely missing — two states that both leave the strip empty
and read completely differently, and the advice for one is useless to the other.
`Refresh` re-resolved every open map's tilesets on every tick of every mode's
stream, measured at 24 ms per 500×500 map; resolution is recorded when a map is
opened and re-checked when its own bytes change, and the refresh itself now runs
only where the strip is rendered. And `maps.Open` could not fail, so its error
return and the `slog.Warn` behind it were unreachable.

## Notes

- **The e2e fixture gained a `[map]`**, which flips `hasMap` and so the
  pathfinding builtins. Three Epic 13 specs asserted the gate was *closed*; they
  now assert it *opens*. Nothing is lost — the closed side is covered in Go four
  times over, where a project can be built without a map — and the browser gains
  a check it did not have. `15-` also joins the serial `stateful` project,
  because it writes into the shared fixture.

### Left undone, deliberately

- **A tileset fixed on disk with the map untouched keeps its problem listed**
  until something changes the map's own bytes. `Resolved` re-reads from disk on
  every call, so the canvas Story 3 draws shows the truth; only the line in the
  problems panel lags. The alternative is re-resolving every open map on every
  stream tick, which is what this replaced.
- **`Read`, `Edit` and `Resolved` have no production caller yet.** They are the
  seams Stories 3 and 4 write through, and each has tests. Story 1 deleted an
  exported accessor for having no caller; the difference is that these are the
  interface this story exists to provide.
- **A `.tmx` symlink in the maps directory is editable**, and `atomicfile`'s
  rename replaces the symlink with a regular file rather than writing through
  it. Inherent to rename-based atomic writes, and already recorded in
  `atomicfile`'s own doc comment for `schema.json`.

### The battery

29 mutations over `internal/forge/maps`, the MAP routes, the footer composition
and `project.MapPath`. 23 caught on the first pass; all six survivors were real
test gaps, the sharpest being that nothing could tell **Discard from Reload** —
the two are identical until somebody edits the file underneath, which is exactly
when confusing them loses whichever the user did not ask for. Second pass: 29 of
29.
