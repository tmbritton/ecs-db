package editable

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// doc is a stand-in for a schema or a machine: something with a serialised form
// and a notion of validity.
type doc struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func marshalDoc(d doc) ([]byte, error) { return json.Marshal(d) }

func unmarshalDoc(raw []byte) (doc, error) {
	var d doc
	err := json.Unmarshal(raw, &d)
	return d, err
}

var errInvalid = errors.New("count must not be negative")

func validateDoc(d doc) error {
	if d.Count < 0 {
		return errInvalid
	}
	return nil
}

func newFile(t *testing.T, initial doc) (*File[doc], string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.json")
	raw, err := marshalDoc(initial)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	f, err := Open(path, Codec[doc]{
		Marshal:   marshalDoc,
		Unmarshal: unmarshalDoc,
		Validate:  validateDoc,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return f, path
}

func mustDirty(t *testing.T, f *File[doc], want bool, when string) {
	t.Helper()
	got, err := f.Dirty()
	if err != nil {
		t.Fatalf("Dirty (%s): %v", when, err)
	}
	if got != want {
		t.Errorf("Dirty = %v %s, want %v", got, when, want)
	}
}

// The whole state machine in one table. The revert case is the one that tells
// you the implementation is a comparison and not a flag: every naive dirty flag
// passes all the others.
func TestFile_DirtyLifecycle(t *testing.T) {
	t.Run("clean on open", func(t *testing.T) {
		f, _ := newFile(t, doc{Name: "a", Count: 1})
		mustDirty(t, f, false, "on open")
	})

	t.Run("dirty after an edit", func(t *testing.T) {
		f, _ := newFile(t, doc{Name: "a", Count: 1})
		f.Current.Count = 2
		mustDirty(t, f, true, "after an edit")
	})

	t.Run("clean again after reverting the edit", func(t *testing.T) {
		f, _ := newFile(t, doc{Name: "a", Count: 1})
		f.Current.Count = 2
		mustDirty(t, f, true, "after an edit")

		f.Current.Count = 1
		// A flag set on mutation says "dirty" here. A comparison against what
		// is actually on disk says what is actually true.
		mustDirty(t, f, false, "after reverting the edit")
	})

	t.Run("clean after discard, with the value restored", func(t *testing.T) {
		f, _ := newFile(t, doc{Name: "a", Count: 1})
		f.Current = doc{Name: "wrecked", Count: 99}

		if err := f.Discard(); err != nil {
			t.Fatalf("Discard: %v", err)
		}
		mustDirty(t, f, false, "after discard")
		if f.Current != (doc{Name: "a", Count: 1}) {
			t.Errorf("Current = %+v after discard, want the on-disk value", f.Current)
		}
	})

	t.Run("clean after save, with the file written", func(t *testing.T) {
		f, path := newFile(t, doc{Name: "a", Count: 1})
		f.Current.Count = 7

		if err := f.Save(); err != nil {
			t.Fatalf("Save: %v", err)
		}
		mustDirty(t, f, false, "after save")

		onDisk, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading: %v", err)
		}
		if !strings.Contains(string(onDisk), `"count":7`) {
			t.Errorf("the file was not written: %s", onDisk)
		}
	})
}

// Refusing to write an invalid value must not throw away the edit — the user is
// mid-thought, and losing their work is worse than the invalid state.
func TestFile_SaveRefusesAnInvalidValue(t *testing.T) {
	f, path := newFile(t, doc{Name: "a", Count: 1})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	f.Current.Count = -1
	err = f.Save()
	if err == nil {
		t.Fatal("Save accepted an invalid value")
	}
	if !errors.Is(err, errInvalid) {
		t.Errorf("error = %v, want it to wrap the validation failure", err)
	}

	mustDirty(t, f, true, "after a refused save")
	if f.Current.Count != -1 {
		t.Errorf("the edit was discarded: %+v", f.Current)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("the file was modified despite the refusal:\n before: %s\n after:  %s", before, after)
	}
}

// Someone editing the same file in another window must not have their work
// silently overwritten.
func TestFile_SaveDetectsAnExternalModification(t *testing.T) {
	f, path := newFile(t, doc{Name: "a", Count: 1})
	f.Current.Count = 2

	// Something else writes the file. The sleep is to guarantee a different
	// mtime on filesystems with coarse timestamps; the content check below is
	// what actually decides.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte(`{"name":"theirs","count":42}`), 0o600); err != nil {
		t.Fatalf("external write: %v", err)
	}

	err := f.Save()
	if err == nil {
		t.Fatal("Save overwrote a file that had changed underneath it")
	}
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want a *ConflictError the UI can act on", err)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if !strings.Contains(string(onDisk), "theirs") {
		t.Errorf("the other edit was overwritten: %s", onDisk)
	}
	if f.Current.Count != 2 {
		t.Errorf("our edit was discarded: %+v", f.Current)
	}
}

// A file rewritten with identical bytes is not a conflict — the common case is
// a tool that rewrites on save without changing anything.
func TestFile_IdenticalExternalWriteIsNotAConflict(t *testing.T) {
	f, path := newFile(t, doc{Name: "a", Count: 1})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	f.Current.Count = 5
	if err := f.Save(); err != nil {
		t.Errorf("Save refused despite identical content: %v", err)
	}
}

// Saving twice must work: the snapshot has to advance to what was written.
func TestFile_SaveTwice(t *testing.T) {
	f, _ := newFile(t, doc{Name: "a", Count: 1})

	f.Current.Count = 2
	if err := f.Save(); err != nil {
		t.Fatalf("first save: %v", err)
	}
	f.Current.Count = 3
	if err := f.Save(); err != nil {
		t.Fatalf("second save: %v — the snapshot did not advance", err)
	}
	mustDirty(t, f, false, "after two saves")
}

// Discard must not need the file to still be there: it restores from the
// snapshot taken when the file was read, which is the point.
func TestFile_DiscardUsesTheSnapshotNotTheDisk(t *testing.T) {
	f, path := newFile(t, doc{Name: "a", Count: 1})
	f.Current.Count = 9

	if err := os.WriteFile(path, []byte(`{"name":"changed","count":100}`), 0o600); err != nil {
		t.Fatalf("external write: %v", err)
	}
	if err := f.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if f.Current != (doc{Name: "a", Count: 1}) {
		t.Errorf("Current = %+v, want the snapshot taken at open", f.Current)
	}
}

func TestOpen_MissingFile(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "nope.json"), Codec[doc]{
		Marshal: marshalDoc, Unmarshal: unmarshalDoc, Validate: validateDoc,
	})
	if err == nil {
		t.Error("Open succeeded on a missing file")
	}
}

// A project-wide answer to "is anything unsaved", and which files.
func TestSet_TracksSeveralFiles(t *testing.T) {
	a, _ := newFile(t, doc{Name: "a", Count: 1})
	b, _ := newFile(t, doc{Name: "b", Count: 1})

	set := NewSet()
	set.Add(a)
	set.Add(b)

	dirty, err := set.Dirty()
	if err != nil {
		t.Fatalf("Set.Dirty: %v", err)
	}
	if len(dirty) != 0 {
		t.Errorf("dirty = %v on open, want none", dirty)
	}

	b.Current.Count = 2
	dirty, err = set.Dirty()
	if err != nil {
		t.Fatalf("Set.Dirty: %v", err)
	}
	if len(dirty) != 1 || dirty[0] != b.Path {
		t.Errorf("dirty = %v, want just %s", dirty, b.Path)
	}

	if err := b.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	dirty, err = set.Dirty()
	if err != nil {
		t.Fatalf("Set.Dirty: %v", err)
	}
	if len(dirty) != 0 {
		t.Errorf("dirty = %v after saving, want none", dirty)
	}
}

// Dirty depends on the marshaller being byte-stable, which is what stories 2
// and 3 delivered. If a serialiser ever stops being stable, this is where it
// shows up — as a file that is permanently dirty for no visible reason.
func TestFile_DirtySurfacesAnUnstableMarshaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(path, []byte(`{"name":"a","count":1}`), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	var calls int
	f, err := Open(path, Codec[doc]{
		Marshal: func(d doc) ([]byte, error) {
			calls++
			// Deliberately unstable: a different byte string every call.
			return []byte(fmt.Sprintf(`{"name":%q,"count":%d,"n":%d}`, d.Name, d.Count, calls)), nil
		},
		Unmarshal: unmarshalDoc,
		Validate:  validateDoc,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	dirty, err := f.Dirty()
	if err != nil {
		t.Fatalf("Dirty: %v", err)
	}
	if !dirty {
		t.Error("an unstable marshaller should read as dirty, not clean")
	}
}

// The error paths. Each is a message someone will read while something has
// already gone wrong, so they need to name the file and the cause.
func TestFile_ErrorPaths(t *testing.T) {
	t.Run("conflict names the file", func(t *testing.T) {
		err := &ConflictError{Path: "/tmp/thing.json"}
		if !strings.Contains(err.Error(), "/tmp/thing.json") {
			t.Errorf("Error() = %q, does not name the file", err.Error())
		}
	})

	t.Run("open rejects a file it cannot parse", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
			t.Fatalf("seeding: %v", err)
		}
		_, err := Open(path, Codec[doc]{Marshal: marshalDoc, Unmarshal: unmarshalDoc})
		if err == nil {
			t.Fatal("Open accepted an unparseable file")
		}
		if !strings.Contains(err.Error(), path) {
			t.Errorf("error = %v, does not name the file", err)
		}
	})

	t.Run("a marshal failure surfaces from Dirty and Save", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "doc.json")
		if err := os.WriteFile(path, []byte(`{"name":"a","count":1}`), 0o600); err != nil {
			t.Fatalf("seeding: %v", err)
		}
		boom := errors.New("cannot serialise")
		f, err := Open(path, Codec[doc]{
			Unmarshal: unmarshalDoc,
			Marshal:   func(doc) ([]byte, error) { return nil, boom },
		})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if _, err := f.Dirty(); !errors.Is(err, boom) {
			t.Errorf("Dirty error = %v, want it to wrap the marshal failure", err)
		}
		if err := f.Save(); !errors.Is(err, boom) {
			t.Errorf("Save error = %v, want it to wrap the marshal failure", err)
		}
	})

	t.Run("discard reports a snapshot it cannot parse", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "doc.json")
		if err := os.WriteFile(path, []byte(`{"name":"a","count":1}`), 0o600); err != nil {
			t.Fatalf("seeding: %v", err)
		}
		var fail bool
		f, err := Open(path, Codec[doc]{
			Marshal: marshalDoc,
			Unmarshal: func(raw []byte) (doc, error) {
				if fail {
					return doc{}, errors.New("cannot parse")
				}
				return unmarshalDoc(raw)
			},
		})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		fail = true
		if err := f.Discard(); err == nil {
			t.Error("Discard succeeded despite an unparseable snapshot")
		}
	})

	// A file deleted while Forge holds it is not a conflict: the content in
	// memory is the only surviving copy, and writing it back is the useful
	// thing to do.
	t.Run("saving a file deleted underneath us recreates it", func(t *testing.T) {
		f, path := newFile(t, doc{Name: "a", Count: 1})
		f.Current.Count = 5
		if err := os.Remove(path); err != nil {
			t.Fatalf("removing: %v", err)
		}
		if err := f.Save(); err != nil {
			t.Fatalf("Save after deletion: %v", err)
		}
		if !strings.Contains(read(t, path), `"count":5`) {
			t.Error("the file was not recreated with the working value")
		}
	})
}

func TestSet_Any(t *testing.T) {
	a, _ := newFile(t, doc{Name: "a", Count: 1})
	set := NewSet()
	set.Add(a)

	if any, err := set.Any(); err != nil || any {
		t.Errorf("Any = %v (err %v) on open, want false", any, err)
	}
	a.Current.Count = 2
	if any, err := set.Any(); err != nil || !any {
		t.Errorf("Any = %v (err %v) after an edit, want true", any, err)
	}
}

// A failure anywhere in the set must surface, not be silently skipped.
func TestSet_PropagatesAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(path, []byte(`{"name":"a","count":1}`), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	boom := errors.New("cannot serialise")
	f, err := Open(path, Codec[doc]{
		Unmarshal: unmarshalDoc,
		Marshal:   func(doc) ([]byte, error) { return nil, boom },
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	set := NewSet()
	set.Add(f)
	if _, err := set.Dirty(); !errors.Is(err, boom) {
		t.Errorf("Set.Dirty error = %v, want it to wrap the failure", err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(raw)
}

// A conflict must be recoverable. Without a way to refresh the snapshot it is a
// dead end: Discard restores the same stale bytes and Save conflicts again, so
// the only escape is to abandon the File and everything in it.
func TestFile_ConflictIsRecoverable(t *testing.T) {
	conflicted := func(t *testing.T) (*File[doc], string) {
		t.Helper()
		f, path := newFile(t, doc{Name: "a", Count: 1})
		f.Current.Count = 2
		if err := os.WriteFile(path, []byte(`{"name":"theirs","count":42}`), 0o600); err != nil {
			t.Fatalf("external write: %v", err)
		}
		var conflict *ConflictError
		if err := f.Save(); !errors.As(err, &conflict) {
			t.Fatalf("expected a conflict, got %v", err)
		}
		return f, path
	}

	t.Run("take theirs, with Reload", func(t *testing.T) {
		f, _ := conflicted(t)
		if err := f.Reload(); err != nil {
			t.Fatalf("Reload: %v", err)
		}
		if f.Current != (doc{Name: "theirs", Count: 42}) {
			t.Errorf("Current = %+v, want what is on disk", f.Current)
		}
		mustDirty(t, f, false, "after Reload")

		// And saving works again.
		f.Current.Count = 43
		if err := f.Save(); err != nil {
			t.Errorf("Save after Reload: %v", err)
		}
	})

	t.Run("keep mine, with SaveOverwriting", func(t *testing.T) {
		f, path := conflicted(t)
		if err := f.SaveOverwriting(); err != nil {
			t.Fatalf("SaveOverwriting: %v", err)
		}
		if !strings.Contains(read(t, path), `"count":2`) {
			t.Errorf("our value was not written: %s", read(t, path))
		}
		mustDirty(t, f, false, "after SaveOverwriting")

		// And the conflict is resolved, not merely bypassed once.
		f.Current.Count = 3
		if err := f.Save(); err != nil {
			t.Errorf("Save after SaveOverwriting: %v", err)
		}
	})

	t.Run("SaveOverwriting still validates", func(t *testing.T) {
		f, path := conflicted(t)
		before := read(t, path)
		f.Current.Count = -1
		if err := f.SaveOverwriting(); !errors.Is(err, errInvalid) {
			t.Errorf("error = %v, want the validation failure", err)
		}
		if read(t, path) != before {
			t.Error("an invalid value was written")
		}
	})
}

// The snapshot must advance only after the write succeeds. Advancing first
// would report a failed save as clean, and the work would be one refresh from
// gone.
func TestFile_FailedSaveStaysDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.json")
	if err := os.WriteFile(path, []byte(`{"name":"a","count":1}`), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	f, err := Open(path, Codec[doc]{Marshal: marshalDoc, Unmarshal: unmarshalDoc})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	f.Current.Count = 2

	// Make the write fail: a read-only directory means the temp file cannot be
	// created, which happens after validation and marshalling.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := f.Save(); err == nil {
		t.Fatal("Save succeeded despite an unwritable directory")
	}
	mustDirty(t, f, true, "after a failed save")
}

// Only ENOENT means "deleted while open". Any other read failure is a real
// problem and must not be treated as permission to overwrite.
func TestFile_UnreadableFileIsNotTreatedAsDeleted(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	f, path := newFile(t, doc{Name: "a", Count: 1})
	f.Current.Count = 2

	// Unreadable but still replaceable: os.ReadFile fails with EACCES while the
	// rename would succeed. That separates the two behaviours — treating any
	// read failure as "deleted while open" would silently overwrite here, and a
	// test that only checks Save returned *some* error cannot tell the
	// difference, because the write fails for its own reasons.
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	err := f.Save()
	if err == nil {
		t.Fatal("Save overwrote a file it could not read")
	}
	if !strings.Contains(err.Error(), "checking") {
		t.Errorf("error = %v, want it to come from the pre-write check", err)
	}
	var conflict *ConflictError
	if errors.As(err, &conflict) {
		t.Errorf("reported as a conflict rather than a read failure: %v", err)
	}
}

// The engine watches these files. Rewriting identical bytes would wake it for a
// reload that changes nothing.
func TestFile_SavingAnUnchangedFileDoesNotTouchIt(t *testing.T) {
	f, path := newFile(t, doc{Name: "a", Count: 1})

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("a no-op save rewrote the file, which wakes the engine's watcher")
	}
}

// A validation failure is about what the user just typed and is the more
// actionable of the two when a file is both invalid and conflicted.
func TestFile_ValidationIsReportedBeforeConflict(t *testing.T) {
	f, path := newFile(t, doc{Name: "a", Count: 1})
	f.Current.Count = -1
	if err := os.WriteFile(path, []byte(`{"name":"theirs","count":42}`), 0o600); err != nil {
		t.Fatalf("external write: %v", err)
	}

	err := f.Save()
	if !errors.Is(err, errInvalid) {
		t.Errorf("error = %v, want the validation failure first", err)
	}
}

func TestOpen_RejectsAnIncompleteCodec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(path, []byte(`{"name":"a"}`), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	for _, tt := range []struct {
		name  string
		codec Codec[doc]
	}{
		{name: "no marshal", codec: Codec[doc]{Unmarshal: unmarshalDoc}},
		{name: "no unmarshal", codec: Codec[doc]{Marshal: marshalDoc}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Open(path, tt.codec); err == nil {
				t.Error("Open accepted an incomplete codec; it would panic later")
			}
		})
	}
}

// Set.Any is what the save footer's enabled state hangs on, so a failure must
// not come back as "nothing unsaved".
func TestSet_AnyPropagatesAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(path, []byte(`{"name":"a","count":1}`), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	boom := errors.New("cannot serialise")
	f, err := Open(path, Codec[doc]{
		Unmarshal: unmarshalDoc,
		Marshal:   func(doc) ([]byte, error) { return nil, boom },
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	set := NewSet()
	set.Add(f)
	if _, err := set.Any(); !errors.Is(err, boom) {
		t.Errorf("Set.Any error = %v, want it to wrap the failure", err)
	}
}
