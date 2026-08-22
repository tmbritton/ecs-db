// Package machines is the editing session for a project's behaviour machines.
//
// It is a sibling of internal/forge/session rather than a generalisation of it.
// That one edits schema.json, which is one file that always exists; this one
// edits as many files as the project has machines, and they can be created,
// renamed and deleted while Forge is running. A type that handled "one or many"
// would handle neither clearly.
package machines

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/editable"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/schema"
)

// Config is what a session needs to resolve and validate machines.
type Config struct {
	// Mods are the project's mods in load order. Only those declaring a
	// behaviours directory can hold a machine.
	Mods []project.Mod
	// HasMap mirrors what the running engine registers: cmd/ecs-db/run.go
	// registers the pathfinding and line-of-sight builtins only when a map is
	// configured, so a Forge that validated against a fuller registry would
	// call a machine valid that the engine then refuses to load.
	HasMap bool
	// Schema reads the schema currently being edited, which is not necessarily
	// the one on disk. Machine validation depends on it — a context key has to
	// match exactly one component field — so validating against the saved file
	// would report problems the editor's own state says are already fixed.
	//
	// A function rather than a value because it is read on every validation and
	// the answer changes as the user types.
	Schema func() schema.DatabaseSchema
}

// Session holds one editable file per resolved machine.
type Session struct {
	cfg Config

	mu sync.Mutex
	// Keyed by path, not by id. A machine's id lives inside the file and this
	// package can change it; a map keyed by something the user edits rewrites
	// its own keys mid-operation. A path is stable for everything except a file
	// rename, which is the one place the key moves deliberately.
	files    map[string]*editable.File[*agent.MachineDefinition]
	order    []string // paths, in resolved order
	machines []project.Machine
	problems []project.Problem
}

// codec is a method rather than a function because Validate has to see the
// schema as it stands at save time, not as it stood when the session opened.
func (s *Session) codec() editable.Codec[*agent.MachineDefinition] {
	return editable.Codec[*agent.MachineDefinition]{
		Marshal:   agent.EmitMachine,
		Unmarshal: agent.ParseMachine,
		// The engine's own check, joined into one error because that is the
		// shape a validation hook takes. It returns every failure rather than
		// the first, which is what Story 8 will render against the node that
		// caused each one.
		Validate: s.validateAgainst,
	}
}

// Open resolves the project's machines and opens one editable file for each.
func Open(cfg Config) (*Session, error) {
	if cfg.Schema == nil {
		return nil, fmt.Errorf("machines: a Schema function is required")
	}
	s := &Session{cfg: cfg, files: map[string]*editable.File[*agent.MachineDefinition]{}}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// Reload re-resolves the project's machines and re-opens their files.
//
// Called after every mutation, so Forge's view of what exists stops being the
// snapshot it took at startup. Epic 12 Story 7 had to word its "no machine with
// this id is loaded" warning around that staleness; this is what lets the
// hedge go.
func (s *Session) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reload()
}

// reload rebuilds the resolved set. Caller holds s.mu.
//
// Files already open keep their working value: a re-resolve triggered by
// creating some other machine must not silently discard an edit in progress.
func (s *Session) reload() error {
	resolved, problems := project.ResolveMachines(s.cfg.Mods, s.cfg.Schema(), s.cfg.HasMap)
	s.machines, s.problems = resolved, problems

	kept := make(map[string]*editable.File[*agent.MachineDefinition], len(resolved))
	order := make([]string, 0, len(resolved))
	inSet := make(map[string]bool, len(resolved))
	for _, m := range resolved {
		inSet[m.Path] = true
		order = append(order, m.Path)
		if existing, ok := s.files[m.Path]; ok {
			kept[m.Path] = existing
			continue
		}
		f, err := editable.Open(m.Path, s.codec())
		if err != nil {
			// Reported, not fatal: a machine the loader resolved but that will
			// not open is a broken project, and refusing to start the editor
			// over it removes the only way to fix it.
			s.problems = append(s.problems, project.Problem{Path: m.Path, Err: err})
			continue
		}
		kept[m.Path] = f
	}

	// A machine can leave the resolved set while Forge holds unsaved work on it
	// — shadowed by a later mod, or its file broken outside the editor. Dropping
	// the file would discard that work silently, so it is kept out of the list
	// and reported instead. It is not rendered, because there is nothing
	// coherent to render it as; it is not lost, which is the part that matters.
	for path, f := range s.files {
		if inSet[path] {
			continue
		}
		if dirty, err := f.Dirty(); err != nil || !dirty {
			continue
		}
		kept[path] = f
		s.problems = append(s.problems, project.Problem{
			Path: path,
			Err: fmt.Errorf("has unsaved changes but no longer resolves as a machine; " +
				"discard them or fix the file to get it back"),
		})
	}
	s.files, s.order = kept, order
	return nil
}

// validateAgainst is the engine's own check, against the schema as it stands.
func (s *Session) validateAgainst(def *agent.MachineDefinition) error {
	return agent.ValidateMachineError(def, project.BuildRegistry(s.cfg.HasMap), s.cfg.Schema())
}

// Paths returns the open machines' files in resolved order.
func (s *Session) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.order...)
}

// Machines is the resolved set, carrying each machine's *working* id.
//
// The id, and only the id. The resolved set comes from re-reading the files, so
// an unsaved rename leaves it naming the old one — and the list would show
// "wander" while the editor beside it showed "roam", for the same machine.
//
// Definition stays the resolved one, for two reasons that both bit. It is a
// live pointer into an open editable.File, and agent.ValidateMachine *writes*
// ContextManifest on whatever it validates — so handing it out raced every save
// against every render, which -race caught the moment a test held both at once.
// And it is parse-derived, so it has no ContextManifest at all: swapping it in
// made ENTS report every context seed on every entity type as "no longer in the
// schema", which is the message for a component that has actually been removed.
//
// Path, Mod and Overrides are facts about where the file came from, and an
// unsaved edit does not change any of them.
func (s *Session) Machines() []project.Machine {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]project.Machine, 0, len(s.machines))
	for _, m := range s.machines {
		if f, ok := s.files[m.Path]; ok && f.Current != nil {
			m.ID = f.Current.ID
		}
		out = append(out, m)
	}
	return out
}

// Working hands back a copy of one machine's current value, for a caller that
// needs what is being edited rather than what was last validated.
func (s *Session) Working(path string) (*agent.MachineDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[path]
	if !ok {
		return nil, fmt.Errorf("machines: no open machine at %s", path)
	}
	return clone(f.Current)
}

// Problems is why any file did not resolve.
func (s *Session) Problems() []project.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]project.Problem(nil), s.problems...)
}

// Read runs fn against one machine's working value.
//
// The value handed over is a re-parse of the working value's own bytes, so a
// caller cannot reach the session's state through the pointer — the same
// property session.Read gets from cloning, by the only means available for a
// tree with parent back-pointers.
func (s *Session) Read(path string, fn func(*agent.MachineDefinition)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[path]
	if !ok {
		return fmt.Errorf("machines: no open machine at %s", path)
	}
	copied, err := clone(f.Current)
	if err != nil {
		return err
	}
	fn(copied)
	return nil
}

// Edit applies fn to one machine's working value under the lock.
//
// A callback rather than a pointer, for the reason session.Edit documents:
// Discard and Reload replace the working value wholesale, so a caller holding a
// pointer holds a stale one. An fn that returns an error leaves the session
// untouched.
func (s *Session) Edit(path string, fn func(*agent.MachineDefinition) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.edit(path, fn)
}

func (s *Session) edit(path string, fn func(*agent.MachineDefinition) error) error {
	f, ok := s.files[path]
	if !ok {
		return fmt.Errorf("machines: no open machine at %s", path)
	}
	draft, err := clone(f.Current)
	if err != nil {
		return err
	}
	if err := fn(draft); err != nil {
		return err
	}
	f.Current = draft
	return nil
}

// clone deep-copies a definition by round-tripping it.
//
// StateNode carries a Parent back-pointer, so there is no struct copy that is
// not either shallow or a hand-written walk that would go stale the next time
// the type grows a field. The serialiser is byte-stable and lossless as of
// Epic 13 Story 1, which is what makes this both correct and cheap enough.
func clone(def *agent.MachineDefinition) (*agent.MachineDefinition, error) {
	raw, err := agent.EmitMachine(def)
	if err != nil {
		return nil, fmt.Errorf("machines: copying: %w", err)
	}
	return agent.ParseMachine(raw)
}

// Dirty reports which machines have unsaved changes, in resolved order.
func (s *Session) Dirty() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirty()
}

func (s *Session) dirty() ([]string, error) {
	var out []string
	for _, path := range s.order {
		f, ok := s.files[path]
		if !ok {
			continue
		}
		changed, err := f.Dirty()
		if err != nil {
			// Not swallowed: a machine that will not serialise cannot be saved,
			// and reporting it as clean would render the footer as settled over
			// a file that is anything but.
			return nil, fmt.Errorf("machines: %s: %w", path, err)
		}
		if changed {
			out = append(out, path)
		}
	}
	return out, nil
}

// Change is why a machine counts as unsaved.
type Change struct {
	Path string
	// Reformatting is true when the machine means exactly what the file means
	// and only its layout would move.
	//
	// Epic 13 Story 1 left this for this story to handle. The emitter has one
	// canonical whitespace style, so a machine authored in another one — by
	// Stately, by a formatter, by hand — differs from its own file the instant
	// Forge opens it, with nobody having edited anything. Reporting that as
	// "unsaved changes" is how a tool teaches you to ignore the word "unsaved".
	Reformatting bool
}

// SaveResult is what became of one machine.
type SaveResult struct {
	Path  string
	Err   error // nil when written
	Saved bool
}

// Changes reports every unsaved machine and why.
func (s *Session) Changes() ([]Change, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dirty, err := s.dirty()
	if err != nil {
		return nil, err
	}
	out := make([]Change, 0, len(dirty))
	for _, path := range dirty {
		out = append(out, Change{Path: path, Reformatting: s.reformatOnly(path)})
	}
	return out, nil
}

// reformatOnly compares what the file *means* with what the session holds.
//
// Both sides go through the emitter, so the comparison is of content rather
// than of layout: equal output means the working value says exactly what the
// file says, and the difference between them is the file's own formatting.
// Caller holds s.mu.
func (s *Session) reformatOnly(path string) bool {
	f, ok := s.files[path]
	if !ok {
		return false
	}
	snapshot, err := f.Snapshot()
	if err != nil {
		// The file no longer parses, so there is nothing to compare against and
		// no basis for calling the difference cosmetic.
		return false
	}
	theirs, err := agent.EmitMachine(snapshot)
	if err != nil {
		return false
	}
	ours, err := agent.EmitMachine(f.Current)
	if err != nil {
		return false
	}
	return bytes.Equal(theirs, ours)
}

// Save writes every dirty machine that validates and refuses the rest,
// individually.
//
// Deliberately not all-or-nothing, which is what schema.json's save is. An
// invalid schema.json stops the engine starting, so blocking the whole save
// protects the project; an invalid *machine* is rejected by the loader on its
// own — the game runs, the other machines work, and one entity does nothing. The
// blast radius is one file, so the refusal is one file, and half-built work on
// the canvas cannot hold finished work hostage.
func (s *Session) Save() ([]SaveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dirty, err := s.dirty()
	if err != nil {
		return nil, err
	}
	results := make([]SaveResult, 0, len(dirty))
	for _, path := range dirty {
		if s.reformatOnly(path) {
			// Nobody edited this: it differs from its file in layout only,
			// because it was authored in another whitespace style. Rewriting it
			// would move the mtime of a file the user never touched — which the
			// story forbids in as many words — and put a whole-file diff in
			// their working tree as a side effect of saving something else.
			// SaveOne still reformats it, deliberately, for a caller that asks.
			continue
		}
		results = append(results, s.saveOne(path))
	}
	// Reloading after a save keeps the resolved set matching what is on disk —
	// a machine whose id changed resolves under the new one from here on.
	if err := s.reload(); err != nil {
		return results, err
	}
	return results, nil
}

// saveOne writes a single machine. Caller holds s.mu.
func (s *Session) saveOne(path string) SaveResult {
	f, ok := s.files[path]
	if !ok {
		return SaveResult{Path: path, Err: fmt.Errorf("machines: no open machine at %s", path)}
	}
	// editable.File.Save validates first — see codec — so a machine that would
	// not load is refused here, on its own, without touching the file.
	if err := f.Save(); err != nil {
		return SaveResult{Path: path, Err: err}
	}
	return SaveResult{Path: path, Saved: true}
}

// SaveOne writes one machine, for a caller that wants just this one.
func (s *Session) SaveOne(path string) SaveResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := s.saveOne(path)
	_ = s.reload()
	return result
}

// Discard restores one machine from the last saved state.
//
// It re-resolves afterwards, which matters for exactly one case: a machine held
// only because reload() found unsaved work on a file that had stopped
// resolving. Giving that work up is the whole of what keeps it held, so
// without the re-resolve the session would carry the file — and report it as a
// problem — for the life of the process.
func (s *Session) Discard(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[path]
	if !ok {
		return fmt.Errorf("machines: no open machine at %s", path)
	}
	if err := f.Discard(); err != nil {
		return err
	}
	return s.reload()
}

// DiscardAll restores every machine.
func (s *Session) DiscardAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for _, path := range s.order {
		if f, ok := s.files[path]; ok {
			if err := f.Discard(); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
			}
		}
	}
	return errors.Join(errs...)
}

// Reload takes what is on disk for one machine, discarding unsaved work.
func (s *Session) ReloadOne(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[path]
	if !ok {
		return fmt.Errorf("machines: no open machine at %s", path)
	}
	return f.Reload()
}

// ReloadAll takes what is on disk for every machine, and re-resolves the set.
//
// This is "take theirs" widened to the whole directory, and it is also what a
// caller needs after the files changed underneath Forge — a machine added,
// removed or renamed by something that is not this editor. Discard cannot do
// it: discarding restores each file's own snapshot, which is what Forge last
// read, not what is there now.
func (s *Session) ReloadAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	for path, f := range s.files {
		if err := f.Reload(); err != nil {
			// A file that has gone is not an error here: the re-resolve below
			// drops it, which is the correct outcome for a machine someone
			// deleted outside Forge.
			if !os.IsNotExist(err) && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
			}
			delete(s.files, path)
		}
	}
	if err := s.reload(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// SaveOverwriting is "keep mine" after a conflict.
func (s *Session) SaveOverwriting(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[path]
	if !ok {
		return fmt.Errorf("machines: no open machine at %s", path)
	}
	if err := f.SaveOverwriting(); err != nil {
		return err
	}
	return s.reload()
}
