# Epic 11 Story 5: Atomic writes — Implementation Plan

**Goal:** the engine's watcher never sees a half-written file.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/atomicfile/write.go` | `Write(path string, data []byte) error` |
| Create | `internal/forge/atomicfile/write_test.go` | Failure and concurrency tests |

---

## Task 0: Check the existing dependency first

`github.com/natefinch/atomic` is already in `go.mod` (indirect). Read its source
before writing anything. If it fsyncs the temp file before rename and creates
the temp in the target directory, use it and delete this package. If it does
not, write ours and record *why* in the doc comment — "we wrote our own" with no
reason is how a dependency gets re-added later.

## Task 1: The function

```go
func Write(path string, data []byte, perm os.FileMode) error
```

1. `os.CreateTemp(filepath.Dir(path), ".forge-*.tmp")` — same directory, so the
   rename stays within one filesystem and is therefore atomic.
2. Write, `Sync`, `Close`.
3. `os.Chmod` to the existing file's mode if it exists, else `perm`.
4. `os.Rename(tmp, path)`.
5. `defer os.Remove(tmp)` — a no-op after a successful rename, cleanup otherwise.

Directory fsync after the rename makes the rename itself crash-durable. It is a
portability wart and arguably beyond what an authoring tool needs; whichever way
it goes, say so in the comment rather than leaving it silently absent.

## Task 2: The test that matters

```go
// A non-atomic write passes every other test in this file. This is the one
// that fails it: hammer the file from a writer goroutine while a reader
// goroutine parses it, and assert every read is a complete document.
func TestWrite_ReaderNeverSeesAPartialFile(t *testing.T)
```

Alternate between two documents of noticeably different lengths, so a torn write
is structurally invalid rather than coincidentally parseable. Run for a fixed
number of iterations, not a duration, so it is deterministic.

**Verify it bites**: swap in `os.WriteFile` and confirm the test fails.

## Task 3: Failure paths

Inject a writer that fails partway (wrap the temp file, or fill the disk with a
size limit) and assert: the original is byte-identical afterwards, and no
`.forge-*.tmp` remains in the directory.

---

## Verification

`go test ./internal/forge/atomicfile/ -race -count=5` — the concurrency test
deserves repetition and the race detector.
