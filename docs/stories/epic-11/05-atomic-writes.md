# Story 5: Atomic writes

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** ✅ Complete  
**Priority:** High — without it, every save can crash the running game

**Depends on:** Story 2 (something to write)

## Context

The engine watches these files with fsnotify and reloads them the moment they change. A plain `os.WriteFile` truncates and then writes, so there is a window — small, but hit often enough by a watcher that fires on the first write event — where the file on disk is empty or half a JSON document. The engine reads it, fails to parse, and logs an error for a file that was fine a millisecond earlier and fine a millisecond later.

The fix is the standard one: write a temp file in the same directory, fsync it, then `rename` over the target. `rename` within a filesystem is atomic, so a reader sees either the old file or the new one, never a partial. Same directory matters — a rename across filesystems is a copy, and not atomic.

This is small, and it is the kind of small that is skipped and then debugged for a day as "flaky hot reload".

## Acceptance Criteria

- [x] A write helper: temp file in the target's directory, write, `Sync`, `Close`, `Rename`
- [x] The temp file is removed if any step fails, leaving the original untouched
- [x] A failed write never leaves the target truncated, missing, or partially written
- [x] File mode is preserved when overwriting an existing file — a save must not change permissions
- [x] The temp name cannot collide between concurrent saves of the same file
- [x] Tests: successful overwrite; write to a path that does not exist yet; failure mid-write leaves the original intact (inject a failing writer); no temp files left behind in either case
- [x] A test asserting a reader never observes a partial file: write repeatedly in one goroutine while another reads and parses, and assert every read is either the old or the new document
- [x] `go test ./...` passes

## Playwright steps

None. This is a filesystem property with no browser surface, and the concurrent
reader test above is a far stronger check than anything a browser could see.

## Notes

- `github.com/natefinch/atomic` is already an indirect dependency. Check whether it does exactly this before writing it by hand — but read its source first: some implementations skip the `fsync`, which is the step that matters for surviving a crash rather than merely a concurrent read.
- The concurrent-reader test is the one worth the effort. A non-atomic implementation passes every other test in the list.
- `Sync` on the temp file, and ideally on the containing directory too, so the rename itself is durable. Directory fsync is a portability wart; if it is skipped, say why rather than leaving it silently absent.
- Don't generalise this into a "file service". It is one function.

## As Implemented

`internal/forge/atomicfile.Write(path, data)`, a thin wrapper over
`github.com/natefinch/atomic`. Coverage 100%.

### Task 0 said check the dependency first, and it was the right call

`natefinch/atomic` already does everything this story specified: the temp file
is created in the target's own directory, so the rename stays within one
filesystem and is therefore atomic; it is `fsync`ed before the rename, so a
crash cannot leave a zero-length file; the original's mode is carried over; and
the temp is removed if any step fails. Writing that again would have been the
same code with fewer eyes on it.

The wrapper exists for three reasons rather than none: it takes a `[]byte`
instead of an `io.Reader`, it gives the reasoning a home, and it is the single
place to change if the dependency ever stops being suitable.

### Two things it does not do, both deliberate

**No directory `fsync` after the rename**, so the rename is durable only once
the filesystem flushes. That matters for surviving a power cut, not for
surviving a concurrent reader, and an authoring tool that loses the last save in
a crash is an acceptable trade against a portability wart. Recorded rather than
left silently absent, which the plan asked for.

**Temp files are named `<file><random>`** — `goblin.json1234567`. Checked
against the engine: `internal/agent/watcher.go` filters on
`filepath.Ext(event.Name) != ".json"`, and `filepath.Ext` of that name is
`.json1234567`, so the watcher ignores the temp entirely and fires only on the
rename. That is load-bearing, not incidental: a temp file the watcher tried to
interpret would be a machine that appears and vanishes.

### The test that earns its keep, and how it was nearly a coin flip

`TestWrite_ReaderNeverSeesAPartialFile` alternates two documents of very
different lengths while a second goroutine reads and parses. A non-atomic write
passes every other test in the file, so this is the only thing defending the
property.

It originally used a 3.5KB payload, and review **measured it missing the
regression 5 runs in 30** — the truncate-then-write window closed before the
reader could land in it. The payload is now about a megabyte, and the mutation
is caught 10/10 with `-race` and 15/15 without.

The check that the test actually sampled anything went through two versions
too. Requiring one read per write looked rigorous and was simply wrong: the two
goroutines are unsynchronised and under `-race` the reader gets proportionally
fewer turns, so it failed a correct implementation. What it now asserts is that
the reader observed the file in **both** states, which it can only do by reading
across transitions — the property actually wanted, rather than a proxy for it.

### Three more findings from review

- **The mid-write failure path was dead code.** The test aimed `Write` at a
  subdirectory that was never created, so the call quietly *succeeded* and the
  assertion tested nothing — while the story's AC for it was ticked. The
  directory is now created first, which produces a genuine failure at the rename
  stage with the original intact and no temp left behind.
- **A `*` in a filename defeats the watcher-invisibility guarantee.** The base
  name is passed to `os.CreateTemp` as a *pattern*, and the last `*` is where
  the random digits go — so `ma*chine.json` yields `ma1273057549chine.json`,
  whose extension really is `.json`. The engine would try to load a machine that
  then vanishes. `*` is a legal filename character and these names come from the
  user's project, so `Write` now refuses it with an explanation.
- **A file Forge creates was mode 0600.** The dependency copies an existing
  destination's mode but leaves a new file at the temp file's permissions, so a
  new `schema.json` would be unreadable by a game process running as another
  user. New files are now 0644, umask still applying.

Also documented: `Write` replaces a symlink with a regular file, leaving the
link target stale. Inherent to rename-based atomic writes, but worth knowing
before someone symlinks a shared schema between projects.
