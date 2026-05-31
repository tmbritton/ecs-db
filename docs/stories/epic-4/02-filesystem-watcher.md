# Story 2: Filesystem Watcher for Behavior Hot Reload

**Epic:** 4 — Behavior hot reload  
**Status:** 🔲 Not started  
**Priority:** High — delivers the core developer experience of this epic

**Depends on:** Story 1 (config file provides the list of `behaviors/` directories to watch)

## Context

The config file (Story 1) gives the interpreter an ordered list of mod `behaviors/` directories. This story adds a filesystem watcher that monitors those directories while the game is running. When a `.json` file changes, the watcher re-parses and re-validates the file; if it passes, it atomically swaps the in-memory `MachineDefinition`. If it fails, the old definition stays in memory and a warning is logged — the game keeps running while the modder fixes their typo.

This is implemented as a component of the interpreter (in-process), not a separate goroutine with complex coordination. The watcher fires a callback; the callback loads the file synchronously; the interpreter uses the updated definition on its next tick.

## Acceptance Criteria

- [ ] `internal/agent/watcher.go` — `Watcher` type that watches a list of directories for `*.json` changes
- [ ] Uses `github.com/fsnotify/fsnotify` for cross-platform filesystem events
- [ ] Debounces rapid writes: multiple events on the same file within a short window (e.g., 50 ms) coalesce into one reload
- [ ] On change: re-parses the file with `ParseMachine`, then re-validates with `ValidateMachine` (registries + schema)
- [ ] If parse/validation passes: atomically replaces the `MachineDefinition` in the `Loader`'s map; logs `[hot-reload] machine "<id>" reloaded from <path>`
- [ ] If parse/validation fails: logs `[hot-reload] machine "<id>" reload failed: <error>` and retains the previous in-memory definition
- [ ] `Watcher.Start(ctx context.Context) error` — starts watching; returns when ctx is cancelled
- [ ] `Watcher.Stop()` — signals shutdown cleanly
- [ ] Directories that do not exist at watch time are skipped with a logged warning (consistent with `ScanDir` behaviour)
- [ ] `internal/agent/watcher_test.go` — tests using real temp directories and file writes; no mocks
- [ ] All new code has tests; `go test ./...` passes

## Notes

- `fsnotify` emits `WRITE` and `CREATE` events; editors often save via rename (CREATE of a temp file + rename). Watch both `WRITE` and `RENAME`/`CREATE` on `*.json` files.
- The debounce timer should be per-file, not global — changing two files quickly should reload both.
- `Loader` already holds `machines map[string]*MachineDefinition` and `sources map[string]string`. The watcher calls a new method `Loader.ReloadFile(path, modName string) error` that the watcher invokes; this keeps the swap logic inside `Loader` where the map lives.
- The `Loader.ReloadFile` method should follow the same last-mod-wins logic as `ScanDir`: if the same machine ID exists from a higher-priority mod, the reload of a lower-priority mod's file does not override it (the source map tracks this).
- Tests: write a temp file, start the watcher, modify the file, wait briefly, assert `loader.Get()` returns the updated definition. Test the failure path by writing invalid JSON and asserting the previous definition is retained.
- The watcher is wired into the interpreter startup in a later story (Epic 5); for now, expose the API and test it in isolation.
- `github.com/fsnotify/fsnotify` requires `go get github.com/fsnotify/fsnotify`.
