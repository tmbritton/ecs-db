# Story 5 — tileset editing session: implementation plan ✅ Complete

## Verified starting seams

- Story 1's `tiled.ParseTilesetDocument` parses TSX into a copy-on-write tree
  plus the engine's tileset reading model. `Bytes()` is exact on no-op;
  `SetTileClass` and `SetTileProperty` are surgical. It rejects `.tsj` and
  embedded tilesets as editing inputs. Story 5 supplies file ownership, not
  another XML writer.
- `forge/editable.File[T]` already implements byte-based Dirty, conflict-safe
  atomic Save, SaveOverwriting, Discard and Reload. `forge/mapfile.Codec` is
  its map adapter; add an equivalent tileset codec rather than teaching the
  generic file about TSX.
- `forge/maps.Session` discovers TMX files, holds their working Documents and
  resolves tilesets afresh in `Preview`/`Resolved` using `os.ReadFile`. It
  currently records external-image allow-list entries there. To show unsaved
  TSX changes in MAP, supply an injected tileset opener that reads TILES'
  working bytes for a held TSX and falls back to disk for other resources,
  without making maps.Session import the TILES session package.
- TILES is still a stub in `modes.Registry`. Existing modes use a path in the
  URL for selected files, a common `editable` session for working bytes, and
  separate independently patchable list/main/footer regions. The shell's
  save footer and SSE event bus need to count TSX changes across modes.

## Boundaries

1. Discover references from MAP's working documents, not from a stale
   startup scan or an arbitrary recursive search. Resolve relative source
   paths against each TMX; check containment against the project root after
   following symlinks. A TSX shared by several maps has exactly one session
   File keyed by its cleaned resolved path. Record each referencing map.
2. Do not drop a dirty File when a source map is removed or an on-disk TSX
   disappears; keep it reachable as stranded work until saved or discarded.
   Non-TSX references are listed with their reason but have no writer. Ensure
   a new TSX from another map appears on Refresh without restarting Forge.
3. `maps.Session` receives an explicit opener from the composition root.
   Maps using TSX resolve the session's working bytes on each preview. Hold
   neither session mutex while calling the other's callback: a TSX edit and
   a map preview must not deadlock. Existing MAP behavior with no tileset
   session uses `os.ReadFile` exactly as before.
4. TILES Story 5's browser surface is a file rail, selected-file summary and
   honest save lifecycle; do not add class/property controls or an image grid
   before Stories 6–8. An edit used for the MAP preview proof should go through
   the session's domain port, with a narrow route exposed to the browser only
   when it is an actual TILES control in the later story.

## Red → green order

1. Test discovery from two maps referencing one external TSX, an unsaved TMX
   source edit, a TSJ, an embedded tileset, a missing TSX, a path outside root
   and a symlink outside root. Write the session codec and discovery with
   fresh temp-project fixtures. Pin no-op bytes and one shared identity.
2. Test Dirty, Save, concurrent external change, Reload, SaveOverwriting,
   Discard and disappearance/reappearance with `editable.File` semantics.
   Validate failure leaves working bytes and snapshot intact. Use one real
   TSX with unknown XML, a sparse collection and two maps.
3. Test MAP preview using the new working-byte opener with two maps, one
   unsaved TILES edit, save and discard. Test no deadlock when both map and TSX
   sessions are active. Wire the constructed sessions in `cmd/ecs-db`.
4. Test TILES file selection/summary, a read-only TSJ, dirty footer and
   cross-mode pending changes in Go before templates/routes. Story 7's actual
   editing controls make the browser dirty/save/conflict tests meaningful; do
   not invent a test-only TSX mutation route here. Add Playwright specs from
   the Story 5 steps above, break an action deliberately, then run the
   full Go, tagged, lint and browser checks. Fresh staged review, fixes,
   commit/push, and update this story and `docs/plan.md` with coverage.
