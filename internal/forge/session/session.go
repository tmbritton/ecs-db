// Package session owns the one editable schema.json a Forge project has.
//
// schema.json has two halves and Forge shows them in two modes, but it is one
// file. If SCHEMA and ENTS each held their own copy, saving one would clobber
// the other's unsaved work — and the bug would only appear when someone edited
// both before saving, which is the normal way to add a component and
// immediately use it. So there is one session, and both modes are views onto it.
//
// It also outlives a request. Mode switching is a full page load (Epic 10
// Story 5), so an unsaved edit only survives it by living on the server.
package session

import (
	"fmt"
	"sync"

	"github.com/tmbritton/ecs-db/internal/forge/editable"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// Session is the editable schema plus the lock that makes it usable from an
// HTTP server. editable.File is documented as not safe for concurrent use, and
// this is the value that owns it.
type Session struct {
	mu   sync.Mutex
	file *editable.File[schema.DatabaseSchema]
}

// Open reads schema.json and starts a session on it.
func Open(path string) (*Session, error) {
	f, err := editable.Open(path, editable.Codec[schema.DatabaseSchema]{
		Marshal:   schema.Marshal,
		Unmarshal: schema.LoadSchema,
		Validate:  schema.ValidateSchema,
	})
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	return &Session{file: f}, nil
}

// Path is the file being edited.
func (s *Session) Path() string { return s.file.Path }

// Edit applies fn to the working value under the lock.
//
// A callback rather than a pointer, deliberately. editable.File.Current is
// replaced wholesale by Discard and Reload, so a caller holding a pointer holds
// a stale one — a hazard found in Epic 11 Story 4's review. Routing every
// mutation through here also means no caller can forget the lock.
//
// An fn that returns an error leaves the session untouched: the edit is applied
// to a copy and only adopted on success, so a handler that validates halfway
// through cannot leave the schema half-changed.
func (s *Session) Edit(fn func(*schema.DatabaseSchema) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft := clone(s.file.Current)
	if err := fn(&draft); err != nil {
		return err
	}
	s.file.Current = draft
	return nil
}

// Read runs fn against the working value.
//
// The value is a deep copy, so a caller cannot reach the session through it.
// That matters more than it looks: DatabaseSchema is a struct, but its
// Components and EntityTypes are maps, so a plain struct copy would still share
// them and `delete(d.Components, …)` inside a Read would silently mutate the
// session without taking the lock.
func (s *Session) Read(fn func(schema.DatabaseSchema)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(clone(s.file.Current))
}

// Snapshot hands back the schema as last saved, deep-copied like Read.
//
// The migration preview needs it: the database was built against the saved
// file, so "has the database fallen behind the file" and "does this unsaved
// edit change the database" are different questions with different answers.
func (s *Session) Snapshot() (schema.DatabaseSchema, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.file.Snapshot()
	if err != nil {
		return schema.DatabaseSchema{}, err
	}
	return clone(snap), nil
}

// Dirty reports whether the working value differs from the file on disk.
func (s *Session) Dirty() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Dirty()
}

// Save validates, writes atomically, and advances the snapshot. It refuses an
// invalid schema and a file that changed underneath, without discarding the
// working value either way.
func (s *Session) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Save()
}

// SaveOverwriting is Save without the conflict check: "keep mine".
func (s *Session) SaveOverwriting() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.SaveOverwriting()
}

// Discard restores the working value from the last saved state.
func (s *Session) Discard() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Discard()
}

// Reload takes what is on disk, discarding unsaved work: "take theirs".
func (s *Session) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Reload()
}

// clone deep-copies a schema, including the order slices, so nothing handed out
// shares memory with the session.
//
// Every copy here starts from the value and overrides only the reference
// fields, rather than building a fresh struct literal. That is deliberate and
// load-bearing: Edit *adopts* the clone, so a field this function forgets is a
// field silently erased from the user's file. Starting from the value means a
// field added to schema.Component later is carried by default — and if it is a
// reference type, the failure is an alias (a leak, caught by the mutable-path
// tests) rather than a deletion (a loss, caught by nothing). Fail toward the
// lesser harm.
//
// TestClone_CoversEveryField is the tripwire for exactly that: it pins the
// field list of each type and fails when one is added.
//
// It is here rather than on schema.DatabaseSchema because this is the only
// caller. If a second one appears, move it — a half-copy written twice is how
// the aliasing bug this prevents comes back.
func clone(s schema.DatabaseSchema) schema.DatabaseSchema {
	out := s
	out.ComponentOrder = cloneStrings(s.ComponentOrder)
	out.EntityTypeOrder = cloneStrings(s.EntityTypeOrder)
	out.Components = nil
	out.EntityTypes = nil
	if s.Components != nil {
		out.Components = make(map[string]schema.Component, len(s.Components))
		for name, c := range s.Components {
			out.Components[name] = cloneComponent(c)
		}
	}
	if s.EntityTypes != nil {
		out.EntityTypes = make(map[string]schema.EntityType, len(s.EntityTypes))
		for name, et := range s.EntityTypes {
			et.RequiredComponents = cloneStrings(et.RequiredComponents)
			et.OptionalComponents = cloneStrings(et.OptionalComponents)
			out.EntityTypes[name] = et
		}
	}
	return out
}

func cloneComponent(c schema.Component) schema.Component {
	out := c
	out.PropertyOrder = cloneStrings(c.PropertyOrder)
	out.Properties = nil
	out.Items = nil
	if c.Properties != nil {
		out.Properties = make(map[string]schema.Property, len(c.Properties))
		for name, p := range c.Properties {
			out.Properties[name] = cloneProperty(p)
		}
	}
	if c.Items != nil {
		items := cloneProperty(*c.Items)
		out.Items = &items
	}
	return out
}

// cloneProperty recurses: a property can nest objects and arrays without limit.
func cloneProperty(p schema.Property) schema.Property {
	out := p
	out.PropertyOrder = cloneStrings(p.PropertyOrder)
	out.Properties = nil
	out.Items = nil
	if p.Properties != nil {
		out.Properties = make(map[string]schema.Property, len(p.Properties))
		for name, nested := range p.Properties {
			out.Properties[name] = cloneProperty(nested)
		}
	}
	if p.Items != nil {
		items := cloneProperty(*p.Items)
		out.Items = &items
	}
	return out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
