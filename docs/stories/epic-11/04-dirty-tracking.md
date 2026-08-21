# Story 4: Dirty tracking & save/discard

**Epic:** 11 — Forge: project model & engine file I/O  
**Status:** ✅ Complete  
**Priority:** High — the save footer is inert until this lands

**Depends on:** Stories 2 and 3

## Context

Story 4 of Epic 10 built a `SaveFooter` primitive with a dirty state, two actions and a filename. Nothing sets any of it. This story gives it a source of truth.

Dirty is a comparison, not a flag. A flag set on every edit says "dirty" after you type a character and delete it again; a comparison against what is actually on disk says what is actually true. Since Stories 2 and 3 make serialisation byte-stable, that comparison is cheap and exact: serialise the in-memory value and compare it to the snapshot taken when the file was read.

That exactness is worth the dependency. A tool that claims unsaved changes when there are none trains you to ignore it, and the one time it matters you will.

Discard is the same comparison run backwards: throw away the in-memory value and reload from the snapshot. It must be as reliable as save, because it is the action people reach for when they have made a mess.

## Acceptance Criteria

- [x] `internal/forge/editable` (or equivalent) tracks, per file: the on-disk snapshot, the current in-memory value, and whether they differ
- [x] Dirty is computed by comparing serialised bytes against the snapshot, not by a mutation flag
- [x] An edit and its exact reversal leaves the file **clean**
- [x] Save writes through Story 5's atomic write, then replaces the snapshot with what was written — so a saved file is immediately clean
- [x] Save refuses to write a value that fails validation, and reports why, without discarding the user's edit
- [x] Discard restores the in-memory value from the snapshot and leaves the file clean
- [x] A file changed on disk by something else since the snapshot is detected on save and reported rather than silently overwritten
- [x] Project-level dirty state: whether *anything* is unsaved, and which files
- [x] Table-driven tests: clean → edit → dirty → discard → clean; clean → edit → save → clean; edit-and-revert → clean; save with an invalid value → refused, still dirty; external modification → reported
- [x] `go test ./...` passes

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

## As Implemented

`internal/forge/editable`. `File[T]` holds a path, the working value, and the exact bytes that were on disk when it was last read or written. `Codec[T]` supplies marshal, unmarshal and validate, so `schema.DatabaseSchema` and `*agent.MachineDefinition` share one implementation rather than two that drift. `Set` answers the project-level question.

Coverage: `editable` 96.1%, `atomicfile` 100%.

### Dirty is a comparison, and the revert case proves it

`Dirty()` marshals the working value and compares it to the snapshot. That is what makes an edit and its exact reversal come out **clean**, which is the one behaviour a flag cannot fake — and the mutation confirms it: replacing the comparison with a sticky flag fails precisely `clean again after reverting the edit` and nothing else in the lifecycle table.

It marshals on every call. Fine at this scale, and if it ever shows up in a profile the fix is to memoise on a change counter, not to switch to a flag.

### The dependency on stories 2 and 3 is real and now tested

An exact comparison only works because the serialisers are byte-stable. `integration_test.go` opens this repo's actual `schema.json` and `behaviors/goblin.json` through the real `schema.Marshal`/`LoadSchema` and `agent.EmitMachine`/`ParseMachine` and asserts they read as **clean on arrival** — if either serialiser stopped being byte-stable, the save footer would light up for a file nobody touched, and this is where that shows up. `TestFile_DirtySurfacesAnUnstableMarshaller` pins the same property from the other direction with a deliberately unstable codec.

That integration test also caught a wrong expectation of mine: I had asserted that `Discard` after a `Save` restores the value the file was *opened* with. It restores the last *saved* value, because the snapshot advances on save — which is what makes a saved file clean. The implementation was right and the test was wrong.

### Design decisions worth knowing later

- **Discard reads the snapshot, not the disk.** It is the action people reach for after making a mess, and it must give back what Forge last knew rather than whatever another process has since written. Mutation-verified.
- **Neither refusal touches `Current`.** A save refused for being invalid, or for a conflict, leaves the edit in place — the user is mid-thought, and losing their work is worse than the invalid state.
- **A file deleted while Forge holds it is not a conflict.** The in-memory content is the only surviving copy, so the save recreates it.
- **The external-modification check is not a lock**, and the comment says so. Nothing stops a write landing between the check and the rename. It catches "you edited this in vim while Forge had it open", which is the accident that actually happens.
- **The check compares content, not mtime.** The plan suggested stat-then-content as an optimisation; at these file sizes the stat is not worth the extra branch, and comparing content means a tool that rewrites a file with identical bytes is correctly not a conflict — which `TestFile_IdenticalExternalWriteIsNotAConflict` pins.

### What the review caught

**A conflict was a permanent dead end.** Once `checkUnchanged` failed, nothing could advance or refresh the snapshot: `Discard` restored the same stale bytes, and `Save` re-read the file and conflicted again, forever. The only escape was to `Open` a second `File` and abandon the first one's working value — precisely the loss this package promises never to inflict, and the save footer would have shown a conflict banner with no button that could resolve it.

There are now two ways out, which are the two answers a person actually has: `Reload` takes what is on disk, and `SaveOverwriting` keeps theirs. `SaveOverwriting` still validates, because writing a file the engine will refuse helps nobody.

Six further mutations survived the original suite and are now caught:

| Mutation | Why it matters |
|---|---|
| advance the snapshot *before* the write succeeds | a failed save would report clean, and the work is one refresh from gone |
| treat any read failure as "deleted while open" | an unreadable file would be blindly overwritten |
| rewrite the file even when nothing changed | wakes the running engine's watcher for a no-op reload |
| `Set.Any` swallowing its error | the save footer would render clean over an unsaveable file |
| report a conflict before a validation failure | the less actionable of the two errors wins |
| an unwrapped write error | the message stops naming the file |

Two of those needed a second attempt at the *test*, not the code. Asserting that an unreadable file produces "some error" passes either way, because the write fails for its own reasons — it has to be a file that is unreadable but still replaceable, so the two behaviours separate. And asserting that a write error "contains the path" passes unwrapped too, because the dependency builds its temp name from the path and mentions it anyway; the assertion has to be on this package's own prefix.

### Smaller fixes from the same review

- `Open` rejects a codec missing `Marshal` or `Unmarshal`, rather than panicking later inside `Dirty`.
- `Codec.Marshal` is documented as needing to be pure: `Dirty` calls it on every render, so a serialiser that normalised in place would corrupt the working value simply by being asked whether it had changed. Both real codecs are pure; nothing enforced it.
- `File` is documented as not safe for concurrent use — Forge is an HTTP server, and the project value owns the file.
- `Discard` is documented as *replacing* `Current`, so a caller holding a pointer `T` from before the call keeps a detached object whose edits go nowhere.
- `Set` is now exercised from an external package, since its unexported-method sealing means only an outside test can show it is usable from outside.

### Verified by mutation

| Mutation | Caught by |
|---|---|
| dirty is a sticky flag rather than a comparison | the revert case |
| the snapshot does not advance after a save | save-then-clean, save-twice |
| no external-modification check | the conflict test |
| save does not validate | the invalid-value test |
| discard reads the disk instead of the snapshot | the snapshot test |
| a plain `os.WriteFile` instead of an atomic one | the concurrent-reader test |
| advance the snapshot before the write succeeds | the failed-save test |
| treat any read failure as deleted | the unreadable-file test |
| rewrite an unchanged file | the no-op-save mtime test |
| `Set.Any` swallows its error | the Any error test |
| conflict reported before validation | the ordering test |
| an unwrapped write error | the error-prefix test |
| remove the `*` filename guard | the star test |
| leave new files at 0600 | the new-file mode test |

The bottom eight all **survived** the original suite.
