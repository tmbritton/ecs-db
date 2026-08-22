package machines

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/atomicfile"
	"github.com/tmbritton/ecs-db/internal/forge/project"
)

// identifier is what may name a machine or a file.
//
// Deliberately narrow. The id becomes part of a filename and the target of
// every behaviour binding, and the filename is passed to os.CreateTemp as a
// pattern by the atomic writer — see atomicfile.Write on why '*' in particular
// is a trap. Refusing anything exotic up front is cheaper than discovering
// which characters survive which layer.
func validName(name string) error {
	if name == "" {
		return fmt.Errorf("a machine id cannot be empty")
	}
	// One rule rather than two. An earlier version also checked filepath.Base
	// and a set of forbidden characters, which the allowlist below already
	// subsumes — untestable duplication that reads as a second line of defence
	// and is not one. What it did *not* cover is a name made entirely of dots:
	// filepath.Base("..") is "..", so that check passed it through.
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
		case (r == '-' || r == '.') && i > 0:
			// Legal inside a name, never at the start: a leading dot makes a
			// hidden file, and "." and ".." are directories.
		default:
			return fmt.Errorf(
				"%q cannot be used: a machine id is also a filename, so it must start with a "+
					"letter, digit or underscore and hold only those plus dash and dot", name)
		}
	}
	if last := name[len(name)-1]; last == '.' || last == '-' {
		return fmt.Errorf("%q cannot be used: a machine id must not end in a dot or a dash", name)
	}
	// Long enough for any real name, short enough that the atomic writer's
	// temp file — the name plus random digits — stays inside NAME_MAX. Failing
	// here beats failing deep inside the write with a raw "file name too long".
	if len(name) > 96 {
		return fmt.Errorf("a machine id must be 96 characters or fewer, got %d", len(name))
	}
	return nil
}

// ModsThatCanHold lists the mods a new machine could be created in.
func (s *Session) ModsThatCanHold() []project.Mod {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.modsThatCanHold()
}

// modsThatCanHold is the same for callers that already hold the lock.
//
// Split because the exported one was the only method here that took no lock:
// safe today, since cfg.Mods never changes, and a loaded gun — Create calls it
// while holding s.mu, so adding the lock every sibling has would have
// deadlocked on the next person to notice the inconsistency.
func (s *Session) modsThatCanHold() []project.Mod {
	var out []project.Mod
	for _, mod := range s.cfg.Mods {
		if mod.Behaviors != "" {
			out = append(out, mod)
		}
	}
	return out
}

// Create writes a new machine into the named mod's behaviours directory.
//
// The mod is named rather than defaulted whenever there is a choice: which mod
// a machine belongs to decides which one can override it later, and picking
// silently makes that decision on the user's behalf in a place they will not
// think to look.
func (s *Session) Create(id, modName string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validName(id); err != nil {
		return "", err
	}
	// Two files declaring one id is already a project.Problem, reported at
	// load. Forge must not be able to author one.
	//
	// Checked against the files rather than the resolved set: a file that
	// declares an id and fails to validate is absent from the set, so the
	// resolved-set check alone let Forge create a second file with that id —
	// and the duplicate appeared the moment someone fixed the broken one.
	if owner := s.fileDeclaring(id); owner != "" {
		return "", fmt.Errorf("machine %q is already declared by %s", id, owner)
	}

	candidates := s.modsThatCanHold()
	if len(candidates) == 0 {
		return "", fmt.Errorf("no mod in this project declares a behaviors directory")
	}
	mod, err := pickMod(candidates, modName)
	if err != nil {
		return "", err
	}

	path := filepath.Join(mod.Behaviors, id+".json")
	if _, err := os.Stat(path); err == nil {
		// No id claims it, so it did not load — a file that is already broken.
		// Overwriting it would destroy whatever is wrong with it, which is the
		// thing the user needs to see.
		return "", fmt.Errorf("%s already exists but declares no usable machine; open or remove it first", path)
	}

	// An id, an initial state, and one state. A machine with no states does not
	// validate, so an empty one could never be saved and would exist only to be
	// broken.
	def := &agent.MachineDefinition{
		ID:         id,
		Initial:    "idle",
		States:     map[string]*agent.StateNode{"idle": {ID: id + ".idle"}},
		StateOrder: []string{"idle"},
	}
	raw, err := agent.EmitMachine(def)
	if err != nil {
		return "", fmt.Errorf("machines: %w", err)
	}
	if err := atomicfile.Write(path, raw); err != nil {
		return "", fmt.Errorf("machines: %w", err)
	}
	if err := s.reload(); err != nil {
		return "", err
	}
	return path, nil
}

// fileDeclaring finds any behaviour file that declares an id, whether or not it
// loaded. Caller holds s.mu.
//
// The resolved set only knows about machines that parsed *and* validated. This
// reads the id out of every file in every behaviours directory, because a
// half-built machine still owns its name — the engine's duplicate-id check
// reads the files too.
func (s *Session) fileDeclaring(id string) string {
	for _, mod := range s.modsThatCanHold() {
		entries, err := os.ReadDir(mod.Behaviors)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			path := filepath.Join(mod.Behaviors, e.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var declared struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &declared); err != nil {
				continue
			}
			if declared.ID == id {
				return path
			}
		}
	}
	return ""
}

// pickMod resolves the caller's choice, refusing to guess when there is one to
// make.
func pickMod(candidates []project.Mod, name string) (project.Mod, error) {
	if name == "" {
		if len(candidates) == 1 {
			return candidates[0], nil
		}
		names := make([]string, 0, len(candidates))
		for _, m := range candidates {
			names = append(names, m.Name)
		}
		return project.Mod{}, fmt.Errorf(
			"this project has more than one mod that could hold a machine (%s); name the one to use",
			strings.Join(names, ", "))
	}
	for _, m := range candidates {
		if m.Name == name {
			return m, nil
		}
	}
	return project.Mod{}, fmt.Errorf("no mod named %q declares a behaviors directory", name)
}

// RenameID changes the id *inside* the file.
//
// This is the identity every behaviour binding resolves through, so it is the
// rename that can break things — and the caller is told what it will break
// before it happens rather than discovering it in Epic 12's inline validation
// afterwards. The file is left where it is: nothing resolves through a
// filename, and moving it as a side effect would be Forge editing the user's
// working tree over a change they did not ask for.
func (s *Session) RenameID(path, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validName(to); err != nil {
		return err
	}
	for _, m := range s.machines {
		if m.ID == to && m.Path != path {
			return fmt.Errorf("machine %q already exists, in %s", to, m.Path)
		}
	}
	return s.edit(path, func(def *agent.MachineDefinition) error {
		if def.ID == to {
			return nil
		}
		// State ids are derived as "<machine>.<state>" when the file does not
		// declare them, and the emitter omits exactly those. Rewriting them
		// keeps that true, so a rename does not add an "id" to every state.
		renameDerivedStateIDs(def.States, def.ID, to)
		def.ID = to
		return nil
	})
}

func renameDerivedStateIDs(states map[string]*agent.StateNode, from, to string) {
	for name, node := range states {
		if node == nil {
			continue
		}
		if node.ID == from+"."+name {
			node.ID = to + "." + name
		}
		renameDerivedStateIDs(node.Children, from, to)
	}
}

// RenameFile moves the file. Nothing resolves through a filename, so nothing
// can break — it exists because a user who has just renamed a machine expects
// the file to follow, and doing it silently would be a decision made for them.
func (s *Session) RenameFile(path, to string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	base := strings.TrimSuffix(to, ".json")
	if err := validName(base); err != nil {
		return "", err
	}
	f, ok := s.files[path]
	if !ok {
		return "", fmt.Errorf("machines: no open machine at %s", path)
	}
	target := filepath.Join(filepath.Dir(path), base+".json")
	if target == path {
		return path, nil
	}
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("%s already exists", target)
	}

	// Unsaved work first: renaming a file whose editor state has not been
	// written would move the old bytes and leave the edit pointing at a path
	// that no longer exists.
	dirty, err := f.Dirty()
	if err != nil {
		return "", err
	}
	if dirty {
		return "", fmt.Errorf("save or discard the changes to %s before renaming it", filepath.Base(path))
	}
	if err := os.Rename(path, target); err != nil {
		return "", fmt.Errorf("machines: renaming: %w", err)
	}
	// The key moves with the file — this is the one operation where it does.
	delete(s.files, path)
	if err := s.reload(); err != nil {
		return "", err
	}
	return target, nil
}

// Delete removes a machine's file.
func (s *Session) Delete(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.files[path]
	if !ok {
		return fmt.Errorf("machines: no open machine at %s", path)
	}
	// RenameFile refuses while there is unsaved work; so must this, and for a
	// stronger reason — a rename that lost an edit could at least be undone
	// from the file, and a delete cannot.
	dirty, err := f.Dirty()
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("save or discard the changes to %s before deleting it", filepath.Base(path))
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("machines: deleting: %w", err)
	}
	delete(s.files, path)
	return s.reload()
}
