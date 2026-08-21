package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/editable"
	"github.com/tmbritton/ecs-db/internal/schema"
)

const fixtureSchema = `{
  "schemaVersion": 3,
  "components": {
    "Position": {
      "type": "object",
      "properties": {
        "x": { "type": "number" },
        "y": { "type": "number" }
      }
    },
    "Health": {
      "type": "object",
      "properties": {
        "hp": { "type": "integer" }
      }
    }
  },
  "entityTypes": {
    "Player": {
      "requiredComponents": ["Position"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    }
  }
}
`

func newSession(t *testing.T) (*Session, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(fixtureSchema), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, path
}

func mustDirty(t *testing.T, s *Session, want bool, when string) {
	t.Helper()
	got, err := s.Dirty()
	if err != nil {
		t.Fatalf("Dirty (%s): %v", when, err)
	}
	if got != want {
		t.Errorf("Dirty = %v %s, want %v", got, when, want)
	}
}

// The lifecycle, and the revert case that separates a comparison from a flag.
func TestSession_Lifecycle(t *testing.T) {
	t.Run("clean on open", func(t *testing.T) {
		s, _ := newSession(t)
		mustDirty(t, s, false, "on open")
	})

	t.Run("dirty after an edit", func(t *testing.T) {
		s, _ := newSession(t)
		mustEdit(t, s, func(d *schema.DatabaseSchema) error {
			d.SchemaVersion = 4
			return nil
		})
		mustDirty(t, s, true, "after an edit")
	})

	t.Run("clean again after reverting", func(t *testing.T) {
		s, _ := newSession(t)
		mustEdit(t, s, func(d *schema.DatabaseSchema) error {
			d.SchemaVersion = 4
			return nil
		})
		mustEdit(t, s, func(d *schema.DatabaseSchema) error {
			d.SchemaVersion = 3
			return nil
		})
		mustDirty(t, s, false, "after reverting")
	})

	t.Run("save writes and returns to clean", func(t *testing.T) {
		s, path := newSession(t)
		mustEdit(t, s, func(d *schema.DatabaseSchema) error {
			d.SchemaVersion = 9
			return nil
		})
		if err := s.Save(); err != nil {
			t.Fatalf("Save: %v", err)
		}
		mustDirty(t, s, false, "after save")

		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading: %v", err)
		}
		if !strings.Contains(string(raw), `"schemaVersion": 9`) {
			t.Errorf("the file was not written:\n%s", raw)
		}
		// And it is still a schema the engine will load.
		if _, err := schema.LoadSchema(raw); err != nil {
			t.Errorf("the saved schema does not load: %v", err)
		}
	})

	t.Run("discard restores and returns to clean", func(t *testing.T) {
		s, _ := newSession(t)
		mustEdit(t, s, func(d *schema.DatabaseSchema) error {
			delete(d.Components, "Health")
			return nil
		})
		mustDirty(t, s, true, "after deleting a component")

		if err := s.Discard(); err != nil {
			t.Fatalf("Discard: %v", err)
		}
		mustDirty(t, s, false, "after discard")

		var names []string
		s.Read(func(d schema.DatabaseSchema) {
			for name := range d.Components {
				names = append(names, name)
			}
		})
		if len(names) != 2 {
			t.Errorf("components = %v, want the deleted one back", names)
		}
	})
}

// A save that would produce a schema the engine refuses must write nothing and
// keep the edit — the user is mid-thought.
func TestSession_SaveRefusesAnInvalidSchema(t *testing.T) {
	s, path := newSession(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	mustEdit(t, s, func(d *schema.DatabaseSchema) error {
		// An entity type requiring a component that does not exist.
		et := d.EntityTypes["Player"]
		et.RequiredComponents = []string{"Nope"}
		d.EntityTypes["Player"] = et
		return nil
	})

	if err := s.Save(); err == nil {
		t.Fatal("Save accepted a schema the engine would refuse")
	}
	mustDirty(t, s, true, "after a refused save")

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(before) != string(after) {
		t.Error("the file was modified despite the refusal")
	}

	// The edit survives, so it can be corrected rather than retyped.
	var required []string
	s.Read(func(d schema.DatabaseSchema) { required = d.EntityTypes["Player"].RequiredComponents })
	if len(required) != 1 || required[0] != "Nope" {
		t.Errorf("the edit was discarded: %v", required)
	}
}

// An edit that fails leaves the session untouched, rather than half-applied.
func TestSession_FailedEditIsNotApplied(t *testing.T) {
	s, _ := newSession(t)
	boom := errors.New("changed my mind")

	err := s.Edit(func(d *schema.DatabaseSchema) error {
		d.SchemaVersion = 99
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Edit error = %v, want the callback's", err)
	}
	mustDirty(t, s, false, "after a failed edit")

	var version int
	s.Read(func(d schema.DatabaseSchema) { version = d.SchemaVersion })
	if version != 3 {
		t.Errorf("SchemaVersion = %d, want the edit rolled back", version)
	}
}

// Both ways out of a conflict, since one of them is always wrong for the
// situation and the user has to choose.
func TestSession_Conflict(t *testing.T) {
	conflicted := func(t *testing.T) (*Session, string) {
		t.Helper()
		s, path := newSession(t)
		mustEdit(t, s, func(d *schema.DatabaseSchema) error {
			d.SchemaVersion = 5
			return nil
		})
		theirs := strings.Replace(fixtureSchema, `"schemaVersion": 3`, `"schemaVersion": 7`, 1)
		if err := os.WriteFile(path, []byte(theirs), 0o600); err != nil {
			t.Fatalf("external write: %v", err)
		}
		var conflict *editable.ConflictError
		if err := s.Save(); !errors.As(err, &conflict) {
			t.Fatalf("expected a conflict, got %v", err)
		}
		return s, path
	}

	t.Run("take theirs", func(t *testing.T) {
		s, _ := conflicted(t)
		if err := s.Reload(); err != nil {
			t.Fatalf("Reload: %v", err)
		}
		var version int
		s.Read(func(d schema.DatabaseSchema) { version = d.SchemaVersion })
		if version != 7 {
			t.Errorf("SchemaVersion = %d, want theirs", version)
		}
		mustDirty(t, s, false, "after Reload")
	})

	t.Run("keep mine", func(t *testing.T) {
		s, path := conflicted(t)
		if err := s.SaveOverwriting(); err != nil {
			t.Fatalf("SaveOverwriting: %v", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading: %v", err)
		}
		if !strings.Contains(string(raw), `"schemaVersion": 5`) {
			t.Errorf("our value was not written:\n%s", raw)
		}
		mustDirty(t, s, false, "after SaveOverwriting")
	})
}

// editable.File is documented as not safe for concurrent use, and the session
// is what owns it. Forge is an HTTP server, so this is not theoretical.
func TestSession_IsSafeForConcurrentUse(t *testing.T) {
	s, _ := newSession(t)

	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch i % 4 {
			case 0:
				_ = s.Edit(func(d *schema.DatabaseSchema) error {
					d.SchemaVersion = 3 + i%3
					return nil
				})
			case 1:
				_, _ = s.Dirty()
			case 2:
				s.Read(func(d schema.DatabaseSchema) { _ = len(d.Components) })
			case 3:
				_ = s.Save()
			}
		}()
	}
	wg.Wait()
}

// Read hands out a value, and a caller must not be able to mutate the session
// through it — the whole point of routing edits through Edit is that they take
// the lock.
func TestSession_ReadDoesNotLeakAMutablePath(t *testing.T) {
	s, _ := newSession(t)

	s.Read(func(d schema.DatabaseSchema) {
		d.SchemaVersion = 999
		delete(d.Components, "Health")
	})

	mustDirty(t, s, false, "after mutating the value Read handed out")

	var version int
	var count int
	s.Read(func(d schema.DatabaseSchema) {
		version = d.SchemaVersion
		count = len(d.Components)
	})
	if version != 3 {
		t.Errorf("SchemaVersion = %d — a Read leaked a mutable path to the session", version)
	}
	if count != 2 {
		t.Errorf("components = %d — a Read leaked a mutable map", count)
	}
}

func TestOpen_MissingFile(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("Open succeeded on a missing file")
	}
}

func TestOpen_UnparseableSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Error("Open accepted an unparseable schema")
	}
}

func mustEdit(t *testing.T, s *Session, fn func(*schema.DatabaseSchema) error) {
	t.Helper()
	if err := s.Edit(fn); err != nil {
		t.Fatalf("Edit: %v", err)
	}
}
