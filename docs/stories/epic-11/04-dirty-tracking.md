# Story 4: Dirty tracking & save/discard

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** 🔲 Not started  
**Priority:** High — the save footer is inert until this lands

**Depends on:** Stories 2 and 3

## Context

Story 4 of Epic 10 built a `SaveFooter` primitive with a dirty state, two actions and a filename. Nothing sets any of it. This story gives it a source of truth.

Dirty is a comparison, not a flag. A flag set on every edit says "dirty" after you type a character and delete it again; a comparison against what is actually on disk says what is actually true. Since Stories 2 and 3 make serialisation byte-stable, that comparison is cheap and exact: serialise the in-memory value and compare it to the snapshot taken when the file was read.

That exactness is worth the dependency. A tool that claims unsaved changes when there are none trains you to ignore it, and the one time it matters you will.

Discard is the same comparison run backwards: throw away the in-memory value and reload from the snapshot. It must be as reliable as save, because it is the action people reach for when they have made a mess.

## Acceptance Criteria

- [ ] `internal/forge/editable` (or equivalent) tracks, per file: the on-disk snapshot, the current in-memory value, and whether they differ
- [ ] Dirty is computed by comparing serialised bytes against the snapshot, not by a mutation flag
- [ ] An edit and its exact reversal leaves the file **clean**
- [ ] Save writes through Story 5's atomic write, then replaces the snapshot with what was written — so a saved file is immediately clean
- [ ] Save refuses to write a value that fails validation, and reports why, without discarding the user's edit
- [ ] Discard restores the in-memory value from the snapshot and leaves the file clean
- [ ] A file changed on disk by something else since the snapshot is detected on save and reported rather than silently overwritten
- [ ] Project-level dirty state: whether *anything* is unsaved, and which files
- [ ] Table-driven tests: clean → edit → dirty → discard → clean; clean → edit → save → clean; edit-and-revert → clean; save with an invalid value → refused, still dirty; external modification → reported
- [ ] `go test ./...` passes

## Playwright steps

`e2e/specs/11-dirty-tracking.spec.js`, once Epic 12 puts an editable field on
screen. Until then this story has no browser surface of its own and the spec is
deferred rather than faked — a spec that drives no UI would be theatre.

What that spec must cover when it lands, recorded now so it is not reinvented:

- [ ] The save footer is clean on load and names the file
- [ ] Editing a field marks it dirty **without a page reload** — the patch arrives on the page-level SSE stream
- [ ] Editing back to the original value returns it to clean; a mutation flag passes every other test and fails this one
- [ ] Save writes and the footer returns to clean
- [ ] Discard restores the field's displayed value and the footer returns to clean
- [ ] Both buttons are disabled when clean — nothing to commit, nothing to throw away
- [ ] An invalid value shows why and leaves the edit in place

## Notes

- **The revert-to-clean case is the one that tells you the implementation is right.** Every naive dirty flag passes the others. Write it first.
- Comparing serialised bytes means dirty depends on Stories 2 and 3 being byte-stable. That is the point, but it means a serialisation regression shows up here as spurious dirtiness — worth a comment where the comparison lives so the next reader knows where to look.
- The external-modification check needs something cheaper than re-reading and comparing on every keystroke. Compare mtime and size at save time; a full content compare only if they differ. This is not a locking scheme and should not pretend to be — it catches the common accident, not a determined race.
- Do not put dirty state in a package-level singleton. It belongs to the project value.
- The engine's own fsnotify watcher will see Forge's saves. That is the intended loop, and Story 6 surfaces the result.
