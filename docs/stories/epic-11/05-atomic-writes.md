# Story 5: Atomic writes

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** 🔲 Not started  
**Priority:** High — without it, every save can crash the running game

**Depends on:** Story 2 (something to write)

## Context

The engine watches these files with fsnotify and reloads them the moment they change. A plain `os.WriteFile` truncates and then writes, so there is a window — small, but hit often enough by a watcher that fires on the first write event — where the file on disk is empty or half a JSON document. The engine reads it, fails to parse, and logs an error for a file that was fine a millisecond earlier and fine a millisecond later.

The fix is the standard one: write a temp file in the same directory, fsync it, then `rename` over the target. `rename` within a filesystem is atomic, so a reader sees either the old file or the new one, never a partial. Same directory matters — a rename across filesystems is a copy, and not atomic.

This is small, and it is the kind of small that is skipped and then debugged for a day as "flaky hot reload".

## Acceptance Criteria

- [ ] A write helper: temp file in the target's directory, write, `Sync`, `Close`, `Rename`
- [ ] The temp file is removed if any step fails, leaving the original untouched
- [ ] A failed write never leaves the target truncated, missing, or partially written
- [ ] File mode is preserved when overwriting an existing file — a save must not change permissions
- [ ] The temp name cannot collide between concurrent saves of the same file
- [ ] Tests: successful overwrite; write to a path that does not exist yet; failure mid-write leaves the original intact (inject a failing writer); no temp files left behind in either case
- [ ] A test asserting a reader never observes a partial file: write repeatedly in one goroutine while another reads and parses, and assert every read is either the old or the new document
- [ ] `go test ./...` passes

## Playwright steps

None. This is a filesystem property with no browser surface, and the concurrent
reader test above is a far stronger check than anything a browser could see.

## Notes

- `github.com/natefinch/atomic` is already an indirect dependency. Check whether it does exactly this before writing it by hand — but read its source first: some implementations skip the `fsync`, which is the step that matters for surviving a crash rather than merely a concurrent read.
- The concurrent-reader test is the one worth the effort. A non-atomic implementation passes every other test in the list.
- `Sync` on the temp file, and ideally on the containing directory too, so the rename itself is durable. Directory fsync is a portability wart; if it is skipped, say why rather than leaving it silently absent.
- Don't generalise this into a "file service". It is one function.
