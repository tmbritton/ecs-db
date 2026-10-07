# Story 8 — Context menus: implementation plan

## Verified before planning

- Epic 13's menu is `components.ContextMenu`, rendered in
  `modes.canvasMenu`. `menuAction` posts client viewport coordinates and
  `Server.canvasMenu` holds one open menu per server. Escape and outside-click
  post `/forge/agents/menu?close=1`; no viewport-sized backdrop blocks another
  tab. MAP can use the same primitive and a separate per-server menu record,
  with the same documented cross-tab limitation.
- MAP currently has one tile layer panel row per `tiled.Map.Layers` index, a
  view-only eye (`$hide<N>`), a paint toolbar and a spawn palette. The canvas
  has `paint.js` pointer handling, but its menu can be a Datastar
  `data-on:contextmenu` expression like the AGENTS canvas; no third JS surface.
- `tiled.Document` has `SetLayerData`, `AddObject`, `MoveObject` and
  `RemoveObject`. It has **no rename/reorder/delete layer operations**. Its
  `tileLayers()` walks nested folders in file order, matching `Map.Layers`.
  A move across parents would silently unpack a Tiled folder, so refuse that
  case explicitly rather than using an emitter. Within one parent, swap the
  XML elements without touching their unknown children or rewriting image/
  object layers. A layer deletion must also preserve everything else verbatim.
- `nextobjectid` allocation exists and scans the whole file, so duplication
  can get a safe new id, but `Document.AddObject` writes only the *reading*
  model's fields and would discard polygon/text/template children. Duplicate
  by cloning the original XML subtree, changing id and position, not by
  reconstructing a `tiled.Object` from `Map`.
- The layer eye already toggles browser view state without a file edit.
  A menu's visibility item calls the same signal toggle; a second file-level
  visibility rule would disagree with the eye and re-import semantics.
- `sameOriginOnly` publishes MAP mutations, including menu opens/closes, to
  the event bus. Server `addMapData` renders the working map each time. A menu
  opened on a layer/spawn must validate its target against *that* session map,
  carry the map path and identity, and close before another map/page can act on
  it. Page loads already clear AGENTS' menu; MAP follows the same rule.

## Design decisions

1. **Layer menu:** Rename with an authored name; move up/down only when there
   is an adjacent tile layer under the same XML parent; delete after a confirm
   naming that the next game load can delete every tile the removed layer
   contributed. Indices are checked at action time against the id/name the
   menu opened on, so a concurrent edit cannot reorder the wrong layer.
   Boundary actions are absent, not disabled. The eye item toggles `$hide<N>`
   and closes the menu without touching the file.
2. **Canvas menu:** Offer an eyedropper — pick the topmost *visible* tile's gid
   and orientation at this cell into the stamp signals. Stamp, fill and erase
   already have toolbar buttons, so their duplicates do not appear. An empty
   cell with nothing to pick gets no menu entry; if that makes the menu empty,
   do not open it. Hidden-layer signals accompany the open request.
3. **Spawn menu:** Delete the targeted id; duplicate by a deep XML clone with a
   freshly allocated id and a neighboring in-bounds cell, keeping its class,
   properties and unknown children. A new duplicate is selected via a link or
   server-held selection; no id is ever reused. No entity-type palette menu
   until there is an operation on a *type* which is not placement.
4. **One menu:** A MAP server record holds kind, map path, layer identity or
   object id, and viewport coordinates. Rendering uses
   `components.ContextMenu` with `Autofocus`; Escape, click elsewhere,
   navigation, successful action and refusal close it. Two right-clicks replace
   one record rather than stacking overlays. Only the mode's small inspector/
   list regions patch for a menu; painting still patches the canvas only when
   the map actually changed.

## TDD sequence

1. `internal/tiled/document_test.go`: failing byte-fidelity tests for layer
   rename, same-parent move up/down, delete, boundary/cross-folder refusals;
   for duplicate object preserving unknown XML and allocating a new id. Add
   only the mutators those tests require. Verify `Map()` invalidation and
   `nextobjectid` monotonicity.
2. `internal/forge/server/mapmenu_test.go`: test menu target resolution, stale
   map/layer/id refusal, each layer operation, spawn duplicate/delete, menu
   replacement and close semantics. Use the real map session and inspect its
   TMX/document, not only HTTP 204. Test view-only visibility separately from
   file edits.
3. Templ rendering tests pin `data-testid`s and that every item has a real
   action, correct boundaries, one open menu and no palette/query/live stubs.
   Add `e2e/specs/15-context-menus.spec.js` before wiring handlers: right-click
   layer, Escape/outside click, rename, reorder stacked cells, duplicate/delete
   spawn, confirm layer deletion, and an explicit accessibility block. Break a
   handler and watch the spec fail before trusting it.
4. Run `make test`, both lint tag sets, `make build`, `make build-headless`,
    `make e2e`. Request a fresh-context sub-agent review of the staged diff,
    fix its findings, repeat affected checks, then commit and mark the story
    and roadmap complete before moving to Story 9.

## Review follow-up

- Stable positive Tiled layer IDs, rather than indices, now key the browser's
  visibility and paint selection so reordering cannot retarget a stroke. Maps
  without unique IDs can still be painted or renamed, but cannot reorder or
  delete layers while their identity is ambiguous. Deleting
  the selected layer leaves selection empty until the author chooses another.
- The eyedropper skips fully transparent layers; layer moves carry leading
  comments and processing instructions with their authored element.
- Each menu opening gets a token, including spawn actions; opening attempts
  are ordered before reading the map. A queued action, close, or older
  right-click response cannot affect a newer menu or its map.
