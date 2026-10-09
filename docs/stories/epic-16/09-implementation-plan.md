# Story 9 — animation-file editing session: implementation plan ✅ Complete

1. Write failing document tests: unchanged TOML round-trips exactly; one
   animation field and one entity binding edit preserve comments, unknown
   keys, table order and unrelated values. Parse with the renderer-compatible
   TOML decoder and locate writable spans without re-encoding the entire
   file. Enforce the 1×N `tileSize` frame constraint in Story 10's editor,
   not the byte-preserving session.
2. Write session tests for first-assets-mod selection, missing/invalid file
   reporting, shared dirty state, no-op saves, conflict/reload/overwrite,
   discarded work, and symlink/path confinement. Reuse editable.File and the
   existing atomic writer.
3. Wire the session in `cmd/ecs-db/forge.go` from the same `cfg.Mods` order
   `run.go` uses. Add SPRT file-status read surface, active/later-mod labels,
   mode-aware footer and scoped lifecycle routes. Keep the new region IDs
   stable, pin browser test IDs from Go.
4. Run Playwright steps above, break Reload deliberately to prove the spec,
   then run `make test`, tagged/headless builds, both lint tag sets and
   `make e2e`. Get fresh staged review, update roadmap and commit.
