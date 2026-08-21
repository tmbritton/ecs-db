// Package editable tracks what is unsaved.
//
// Dirty is a comparison, not a flag. A flag set on every edit says "unsaved"
// after you type a character and delete it again; a comparison against what is
// actually on disk says what is actually true. A tool that claims unsaved
// changes when there are none trains you to ignore it, and the one time it
// matters you will.
//
// The comparison is exact and cheap because the serialisers are byte-stable —
// see schema.Marshal and agent.EmitMachine. That is a real dependency in both
// directions: if a serialiser ever stops being stable, it shows up here as a
// file that is permanently dirty for no visible reason.
package editable

import (
	"bytes"
	"fmt"
	"os"

	"github.com/tmbritton/ecs-db/internal/forge/atomicfile"
)

// Codec is everything File needs to know about the value it holds. Passing
// these in rather than constraining T keeps schema.DatabaseSchema and
// *agent.MachineDefinition on one implementation instead of two that drift.
type Codec[T any] struct {
	// Marshal must not mutate its argument. Dirty calls it on every render, so
	// a serialiser that reordered or normalised in place would corrupt the
	// working value simply by being asked whether it had changed.
	Marshal   func(T) ([]byte, error)
	Unmarshal func([]byte) (T, error)
	Validate  func(T) error // may be nil
}

// File is one editable file: what is on disk, what is in memory, and whether
// they differ.
//
// Not safe for concurrent use. Forge is an HTTP server, so the value that owns
// a File — the project — is responsible for serialising access to it. Dirty
// marshals Current, which races with a handler mutating it.
type File[T any] struct {
	Path string

	// Current is the working value. Callers edit it directly; nothing needs to
	// be told that an edit happened, because nothing is counting edits.
	Current T

	codec    Codec[T]
	snapshot []byte // exactly the bytes on disk when last read or written
}

// ConflictError reports that the file changed underneath us since it was read.
type ConflictError struct {
	Path string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s changed on disk since it was opened", e.Path)
}

// Resolutions names the two ways out of a conflict, so a caller rendering this
// error knows what to offer.
func (e *ConflictError) Resolutions() string {
	return "Reload to take what is on disk, or SaveOverwriting to keep yours"
}

// Open reads a file and takes the snapshot everything else compares against.
func Open[T any](path string, codec Codec[T]) (*File[T], error) {
	// A nil codec function is a nil-func panic deep inside Dirty or Save
	// otherwise, which reaches the user as a 500 rather than a message.
	if codec.Marshal == nil || codec.Unmarshal == nil {
		return nil, fmt.Errorf("editable: %s: codec needs both Marshal and Unmarshal", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("editable: reading %s: %w", path, err)
	}
	value, err := codec.Unmarshal(raw)
	if err != nil {
		return nil, fmt.Errorf("editable: parsing %s: %w", path, err)
	}
	return &File[T]{Path: path, Current: value, codec: codec, snapshot: raw}, nil
}

// Dirty reports whether the working value differs from what is on disk.
//
// It marshals on every call, which is fine at this scale — these files are
// kilobytes — and is what makes an edit and its exact reversal come out clean.
// If it ever shows up in a profile, memoise on a change counter; do not switch
// to a flag, which is the thing this is deliberately not.
func (f *File[T]) Dirty() (bool, error) {
	current, err := f.codec.Marshal(f.Current)
	if err != nil {
		return false, fmt.Errorf("editable: serialising %s: %w", f.Path, err)
	}
	return !bytes.Equal(current, f.snapshot), nil
}

// Save validates, writes atomically, and advances the snapshot to what was
// written — so a saved file is immediately clean.
//
// It refuses if the file changed on disk since it was opened, and refuses if
// the value does not validate. Neither refusal touches Current: the caller may
// be halfway through an edit, and losing their work is worse than the invalid
// state they are in.
func (f *File[T]) Save() error { return f.save(true) }

func (f *File[T]) save(checkConflict bool) error {
	// Validation first, deliberately: "this is not valid" is about what the
	// user just typed, and is more actionable than "someone else changed the
	// file" when both are true.
	if f.codec.Validate != nil {
		if err := f.codec.Validate(f.Current); err != nil {
			return fmt.Errorf("editable: %s is not valid: %w", f.Path, err)
		}
	}
	if checkConflict {
		if err := f.checkUnchanged(); err != nil {
			return err
		}
	}

	data, err := f.codec.Marshal(f.Current)
	if err != nil {
		return fmt.Errorf("editable: serialising %s: %w", f.Path, err)
	}

	// Nothing to write. Skipping matters beyond economy: the engine watches
	// these files, and rewriting identical bytes wakes it for a reload that
	// changes nothing.
	if bytes.Equal(data, f.snapshot) {
		return nil
	}

	if err := atomicfile.Write(f.Path, data); err != nil {
		return err
	}
	// Only after the write succeeded. Advancing first would report a failed
	// save as clean, and the user's work would be one refresh from gone.
	f.snapshot = data
	return nil
}

// Discard restores the working value from the snapshot.
//
// From the snapshot, not from disk: discard is the action people reach for
// after making a mess, and it must give them back what Forge last knew rather
// than whatever is there now. To take what is on disk instead, use Reload.
//
// Current is replaced, not updated in place. A caller holding a pointer T from
// before the call keeps a detached object whose edits go nowhere, so re-read
// Current after discarding.
func (f *File[T]) Discard() error {
	value, err := f.codec.Unmarshal(f.snapshot)
	if err != nil {
		return fmt.Errorf("editable: restoring %s: %w", f.Path, err)
	}
	f.Current = value
	return nil
}

// Reload takes what is on disk, replacing both the snapshot and the working
// value. This is one of the two ways out of a ConflictError — the other is
// SaveOverwriting.
//
// Without it a conflict is a dead end: Discard restores the same stale
// snapshot, and Save re-reads the file and conflicts again, so the only escape
// is to Open a second File and abandon the first one's working value. That is
// exactly the loss this package promises not to inflict.
//
// It discards unsaved work by design, which is what "take theirs" means, so a
// caller should confirm before offering it.
func (f *File[T]) Reload() error {
	raw, err := os.ReadFile(f.Path)
	if err != nil {
		return fmt.Errorf("editable: reading %s: %w", f.Path, err)
	}
	value, err := f.codec.Unmarshal(raw)
	if err != nil {
		return fmt.Errorf("editable: parsing %s: %w", f.Path, err)
	}
	f.Current = value
	f.snapshot = raw
	return nil
}

// SaveOverwriting is Save without the external-modification check: "keep mine".
// It still validates, because writing a file the engine will refuse helps
// nobody.
func (f *File[T]) SaveOverwriting() error { return f.save(false) }

// checkUnchanged refuses to overwrite an edit made outside Forge.
//
// This is not a lock and should not be mistaken for one — nothing stops a write
// landing between this check and the rename. It catches "you edited this in vim
// while Forge had it open", which is the accident that actually happens.
func (f *File[T]) checkUnchanged() error {
	onDisk, err := os.ReadFile(f.Path)
	if err != nil {
		if os.IsNotExist(err) {
			// Deleted while open. Writing it back is the useful behaviour:
			// the content Forge holds is the only surviving copy.
			return nil
		}
		return fmt.Errorf("editable: checking %s: %w", f.Path, err)
	}
	if !bytes.Equal(onDisk, f.snapshot) {
		return &ConflictError{Path: f.Path}
	}
	return nil
}

// Set answers the project-level question: is anything unsaved, and what.
type Set struct {
	files []interface{ dirtyPath() (string, bool, error) }
}

func NewSet() *Set { return &Set{} }

// Add registers a file with the set. It takes the concrete *File[T] through a
// tiny interface so one Set can hold files of different value types.
func (s *Set) Add(f interface{ dirtyPath() (string, bool, error) }) {
	s.files = append(s.files, f)
}

func (f *File[T]) dirtyPath() (string, bool, error) {
	dirty, err := f.Dirty()
	return f.Path, dirty, err
}

// Dirty returns the paths of every file with unsaved changes, in the order
// they were added.
func (s *Set) Dirty() ([]string, error) {
	var dirty []string
	for _, f := range s.files {
		path, isDirty, err := f.dirtyPath()
		if err != nil {
			return nil, err
		}
		if isDirty {
			dirty = append(dirty, path)
		}
	}
	return dirty, nil
}

// Any reports whether anything at all is unsaved — what the save footer's
// enabled state hangs on.
//
// The error is returned, not dropped: a serialisation failure that came back as
// "nothing unsaved" would render the footer as clean over an unsaveable file.
func (s *Set) Any() (bool, error) {
	dirty, err := s.Dirty()
	if err != nil {
		return false, err
	}
	return len(dirty) > 0, nil
}
