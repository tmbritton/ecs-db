package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
)

func (s *Server) registerMachineEditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /forge/agents/machine", sameOriginOnly(s.handleMachineEdit))
	mux.HandleFunc("POST /forge/agents/save", sameOriginOnly(s.handleMachineSave))
	mux.HandleFunc("POST /forge/agents/discard", sameOriginOnly(s.handleMachineDiscard))
	mux.HandleFunc("POST /forge/agents/reload", sameOriginOnly(s.handleMachineReload))
	mux.HandleFunc("POST /forge/agents/save/overwrite", sameOriginOnly(s.handleMachineOverwrite))
}

// machineSession answers with the session or writes the reason there is none.
func (s *Server) machineSession(w http.ResponseWriter) (*machines.Session, bool) {
	if s.cfg.MachineSession == nil {
		http.Error(w, "no project is open", http.StatusConflict)
		return nil, false
	}
	return s.cfg.MachineSession, true
}

// machinePath resolves the ?machine= parameter to a machine in the resolved set.
//
// Checked against the open set rather than trusted: the parameter arrives from
// the page and names a file, and a handler that passed it through would let a
// crafted request address any path on disk.
func (s *Server) machinePath(sess *machines.Session, want string) (string, error) {
	return matchMachine(sess.Paths(), want)
}

// machineHeld is the same against the wider set: everything the session holds a
// file for, including a machine kept back because it had unsaved work and
// stopped resolving.
//
// Only discard uses it, and deliberately only discard. Renaming or deleting a
// machine that is in no list is an operation with no visible subject; giving up
// its work is the one thing there is left to do with it, and without this the
// work is kept and unreachable.
func (s *Server) machineHeld(sess *machines.Session, want string) (string, error) {
	return matchMachine(sess.Held(), want)
}

func matchMachine(paths []string, want string) (string, error) {
	if want == "" {
		return "", fmt.Errorf("no machine named")
	}
	for _, path := range paths {
		if path == want {
			return path, nil
		}
	}
	// Deliberately worded differently from the session's own refusal. Both
	// layers check, and a shared message made it impossible to tell which one
	// answered — so a test could not show that the handler checks at all.
	return "", fmt.Errorf("%s is not a machine this project has open", want)
}

// handleMachineEdit is the one endpoint every structural change goes through,
// dispatching on which parameter is present — the same shape as the schema and
// ents editors.
func (s *Server) handleMachineEdit(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.machineSession(w)
	if !ok {
		return
	}
	q := r.URL.Query()

	switch {
	case q.Get("add") != "":
		if _, err := sess.Create(q.Get("add"), q.Get("mod")); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	case q.Get("renameID") != "":
		path, err := s.machinePath(sess, q.Get("machine"))
		if err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		if err := sess.RenameID(path, q.Get("renameID")); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	case q.Get("renameFile") != "":
		path, err := s.machinePath(sess, q.Get("machine"))
		if err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		moved, err := sess.RenameFile(path, q.Get("renameFile"))
		if err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		// A page subscribes with the path it is showing, and that URL cannot
		// change afterwards. Without the trail, the stream goes on asking for a
		// file that no longer exists and the editor quietly swaps to whichever
		// machine sorts first.
		s.recordRename(renameMachineKind, path, moved)
	case q.Get("delete") != "":
		path, err := s.machinePath(sess, q.Get("delete"))
		if err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		if err := sess.Delete(path); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		// The report described a file that no longer exists.
		s.ClearSaveReport(path)
	default:
		s.refuseMachineEdit(w, r, fmt.Errorf("no machine operation named"))
		return
	}

	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

// refuseMachineEdit reports why an edit did not happen, in the editor rather
// than only in the log: an edit that vanishes with no explanation teaches you
// that the control is broken.
func (s *Server) refuseMachineEdit(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "machine edit", "err", err)
	s.setEditProblem(err.Error())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMachineSave(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.machineSession(w)
	if !ok {
		return
	}
	results, err := sess.Save()
	if err != nil {
		s.refuseMachineEdit(w, r, err)
		return
	}
	// Every machine gets its own report, because every machine got its own
	// verdict — a single "saved" over a set where one was refused would be the
	// most misleading thing the footer could say.
	for _, result := range results {
		s.ReportSave(result.Path, result.Err)
	}
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMachineDiscard(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.machineSession(w)
	if !ok {
		return
	}
	if machine := r.URL.Query().Get("machine"); machine != "" {
		path, err := s.machineHeld(sess, machine)
		if err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		if err := sess.Discard(path); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		s.ClearSaveReport(path)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := sess.DiscardAll(); err != nil {
		s.refuseMachineEdit(w, r, err)
		return
	}
	for _, path := range sess.Paths() {
		s.ClearSaveReport(path)
	}
	w.WriteHeader(http.StatusNoContent)
}

// machineData fills in the AGENTS half of a mode render.
func (s *Server) machineData(r *http.Request) (selected string, dirty, reformat map[string]bool) {
	sess := s.cfg.MachineSession
	if sess == nil {
		return "", nil, nil
	}
	dirty, reformat = map[string]bool{}, map[string]bool{}
	changes, err := sess.Changes()
	if err != nil {
		// A machine that will not serialise cannot be saved, and reporting it
		// as clean would show the footer as settled over a file that is not.
		slog.Error("computing dirty machines", "err", err)
		for _, path := range sess.Paths() {
			dirty[path] = true
		}
	}
	for _, change := range changes {
		dirty[change.Path] = true
		if change.Reformatting {
			reformat[change.Path] = true
		}
	}
	return selectMachine(sess, s.followRenames(r.URL.Query().Get("machine"), renameMachineKind)), dirty, reformat
}

// selectMachine resolves the URL's ?machine= to one that is open, falling back
// to the first — a stale bookmark or a just-deleted machine should show the
// editor, not a 404.
func selectMachine(sess *machines.Session, want string) string {
	paths := sess.Paths()
	for _, path := range paths {
		if path == want {
			return path
		}
	}
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
}

// machineInspection is what the engine says about the selected machine.
//
// A machine that cannot be inspected is reported as one problem rather than as
// an empty inspection: the zero value reads as "not validated", and a panel
// that showed "not computed" with no reason beside it would be describing the
// machine when the trouble is with Forge.
func (s *Server) machineInspection(path string) machines.Inspection {
	if s.cfg.MachineSession == nil || path == "" {
		return machines.Inspection{}
	}
	got, err := s.cfg.MachineSession.Inspect(path)
	if err != nil {
		slog.Error("inspecting a machine", "path", path, "err", err)
		return machines.Inspection{Errors: []agent.ValidationError{{Message: err.Error()}}}
	}
	return got
}

// strandedSet is which held machines are in no list, for the problem panel.
func strandedSet(sess *machines.Session) map[string]bool {
	stranded := sess.Stranded()
	if len(stranded) == 0 {
		return nil
	}
	out := make(map[string]bool, len(stranded))
	for _, path := range stranded {
		out[path] = true
	}
	return out
}

// machinesFooterFile is what the save footer names on AGENTS.
//
// A count rather than a name once there is more than one, because the footer
// has room for one line and "3 unsaved machines" is the fact that matters; the
// list itself marks which.
func machinesFooterFile(dirty map[string]bool, reformat map[string]bool) string {
	switch {
	case len(dirty) == 0:
		return "behaviors/"
	case len(dirty) == len(reformat):
		// Nobody edited anything: these files were authored in another
		// whitespace style and saving would only restyle them. Calling that
		// "unsaved changes" is how a tool teaches you to ignore the word.
		if len(dirty) == 1 {
			for path := range dirty {
				return filepath.Base(path) + " · would be reformatted"
			}
		}
		return fmt.Sprintf("%d machines would be reformatted", len(dirty))
	case len(dirty) == 1:
		for path := range dirty {
			return filepath.Base(path)
		}
	}
	return fmt.Sprintf("%d unsaved machines", len(dirty))
}

// dirtyNames lists the unsaved machines by filename, for the footer's title.
//
// Sorted, because Go randomises map iteration and the SSE loop suppresses a
// patch by comparing the rendered string. Unsorted, the footer re-rendered
// differently on a random fraction of ticks and was re-sent forever while more
// than one machine was unsaved — the exact waste the suppression exists to
// prevent, and it would have made the browser's event log useless for
// debugging the busier traffic Epic 18 puts on the same stream.
func dirtyNames(dirty map[string]bool) string {
	names := make([]string, 0, len(dirty))
	for path := range dirty {
		names = append(names, filepath.Base(path))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// handleMachineReload takes what is on disk, discarding unsaved work — the
// "take theirs" half of the conflict path, and the way back to a directory that
// changed underneath Forge.
func (s *Server) handleMachineReload(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.machineSession(w)
	if !ok {
		return
	}
	if machine := r.URL.Query().Get("machine"); machine != "" {
		path, err := s.machinePath(sess, machine)
		if err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		if err := sess.ReloadOne(path); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		s.ClearSaveReport(path)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	for _, path := range sess.Paths() {
		s.ClearSaveReport(path)
	}
	if err := sess.ReloadAll(); err != nil {
		s.refuseMachineEdit(w, r, err)
		return
	}
	// Taking what is on disk undoes the renames themselves, so a trail still
	// pointing at the new paths would send every page to a file that is not
	// there — the reasoning forgetRenames already carries for schema.json.
	s.forgetRenames()
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

// handleMachineOverwrite is "keep mine": save without the conflict check.
func (s *Server) handleMachineOverwrite(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.machineSession(w)
	if !ok {
		return
	}
	path, err := s.machinePath(sess, r.URL.Query().Get("machine"))
	if err != nil {
		s.refuseMachineEdit(w, r, err)
		return
	}
	err = sess.SaveOverwriting(path)
	s.ReportSave(path, err)
	w.WriteHeader(http.StatusNoContent)
}
