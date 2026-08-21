# Epic 11 Story 4: Dirty tracking & save/discard — Implementation Plan

**Goal:** know exactly what is unsaved, and be able to undo it.

**Architecture:** dirty is a comparison against an on-disk snapshot, not a flag. Serialisation is byte-stable (Stories 2 and 3), so the comparison is exact and cheap.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/editable/editable.go` | `File[T]`, snapshot/dirty/save/discard |
| Create | `internal/forge/editable/editable_test.go` | State-machine table tests |
| Modify | `internal/forge/project/project.go` | Project-level dirty rollup |

---

## Task 1: The type

```go
// File is one editable file: what is on disk, what is in memory, and whether
// they differ. Generic over the value so schema.DatabaseSchema and
// *agent.MachineDefinition share one implementation rather than two that drift.
type File[T any] struct {
	Path     string
	Current  T
	snapshot []byte              // exactly the bytes on disk when last read or written
	marshal  func(T) ([]byte, error)
	validate func(T) error
}

func (f *File[T]) Dirty() (bool, error)   // marshal Current, compare to snapshot
func (f *File[T]) Save() error            // validate, marshal, atomic write, resnapshot
func (f *File[T]) Discard(unmarshal func([]byte) (T, error)) error
```

Dirty marshals on every call. That is fine at this scale — these files are
kilobytes — and it is what makes edit-then-revert come out clean. If it ever
shows up in a profile, memoise on a change counter; do not switch to a flag.

## Task 2: External modification

At save time, before writing: stat the file and compare mtime and size against
what was recorded at snapshot time. If they differ, re-read and compare content;
if the content differs, refuse and report.

Explicitly not a lock. It catches "you edited this in vim while Forge had it
open", which is the common accident. Say so in the comment so nobody later
mistakes it for a concurrency guarantee.

## Task 3: The tests

Table-driven over a sequence of operations, asserting dirty state after each:

| Sequence | Expected |
|---|---|
| load | clean |
| load, edit | dirty |
| load, edit, revert edit | **clean** |
| load, edit, discard | clean, value restored |
| load, edit, save | clean, file written |
| load, edit-to-invalid, save | refused, still dirty, file unchanged |
| load, external write, edit, save | refused, reported, file unchanged |

The revert case is the one that fails a flag-based implementation. Write it
first and watch it fail against a deliberately flag-based version.

---

## Verification

`go test ./internal/forge/editable/`, plus the mutation above.
