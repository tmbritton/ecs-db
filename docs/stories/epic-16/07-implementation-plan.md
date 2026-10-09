# Story 7 — TILES tile metadata editing: implementation plan ✅ Complete

1. Start with failing session/HTTP tests for `SetTileClass` and
   `SetTileProperty` through `tilesets.Session.Edit`, including implicit sheet
   tile, sparse collection refusal, TSJ/unheld route refusal, clean no-op,
   XML unknown sibling survival and a shared MAP working preview. Reuse the
   existing `editable.File` lifecycle and Story 5 save routes.
2. Split TILES's main content into disjoint head/grid/inspector SSE regions,
   always present even when no project or file can open. Keep `mode-list`
   independent; use the existing one page-level stream.
3. Add labelled class/type and typed-property inputs only on writable TSX
   tiles. POST after a committed change, validate at both route/session and
   document boundaries, and surface errors at the corresponding inspector
   control. URL-selected tile, page and file must survive the push.
4. Add the Playwright steps above and Go-side test-ID pins. Break an edit
   action deliberately and watch the browser spec fail. Run `make test`,
   tagged and headless builds, both lint tag sets and `make e2e`; obtain a
   fresh-context staged review before committing. Update the story and
   roadmap with results.
