package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/tmbritton/ecs-db/internal/forge/maps"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
)

func (s *Server) registerMapEditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /forge/map/save", sameOriginOnly(s.handleMapSave))
	mux.HandleFunc("POST /forge/map/save/overwrite", sameOriginOnly(s.handleMapOverwrite))
	mux.HandleFunc("POST /forge/map/discard", sameOriginOnly(s.handleMapDiscard))
	mux.HandleFunc("POST /forge/map/reload", sameOriginOnly(s.handleMapReload))
}

// mapSession answers with the session or writes the reason there is none.
func (s *Server) mapSession(w http.ResponseWriter) (*maps.Session, bool) {
	if s.cfg.MapSession == nil {
		http.Error(w, "no project is open", http.StatusConflict)
		return nil, false
	}
	return s.cfg.MapSession, true
}

// mapPath resolves the ?map= parameter against the maps the session holds,
// falling back to the one the page would be showing.
//
// Checked rather than trusted, for machinePath's reason: the parameter arrives
// from the page and names a file, and a handler that passed it through would
// let a crafted request write to any path on disk. The session checks too, in
// different words, so a test can show which layer answered.
// A request that names no map is refused rather than defaulted. Defaulting is
// what made the footer's Save write the configured map while the confirm dialog
// named the one on screen — the caller has to say which map it means, and every
// caller knows.
func (s *Server) mapPath(sess *maps.Session, want string) (string, error) {
	return matchMap(sess.Paths(), want)
}

// mapHeld is the same against everything the session holds a file for,
// including a map kept back because it had unsaved work and then vanished from
// the project.
//
// Only the operations that can still do something useful with such a map use
// it: writing it back, which recreates the file, and giving up its work. It is
// in no list and reachable from nothing else, so without this the work is kept
// and invisible — which is worse than losing it, because nobody knows to look.
// machines.Session made the same distinction for the same reason.
func (s *Server) mapHeld(sess *maps.Session, want string) (string, error) {
	return matchMap(sess.Held(), want)
}

func matchMap(paths []string, want string) (string, error) {
	if want == "" {
		return "", fmt.Errorf("no map named")
	}
	for _, path := range paths {
		if path == want {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s is not a map this project has open", want)
}

func (s *Server) handleMapSave(w http.ResponseWriter, r *http.Request) {
	s.mapWrite(w, r, func(sess *maps.Session, path string) error { return sess.Save(path) })
}

func (s *Server) handleMapOverwrite(w http.ResponseWriter, r *http.Request) {
	s.mapWrite(w, r, func(sess *maps.Session, path string) error { return sess.SaveOverwriting(path) })
}

// mapWrite runs a save and reports it, the way every other save in Forge does.
func (s *Server) mapWrite(w http.ResponseWriter, r *http.Request, save func(*maps.Session, string) error) {
	sess, ok := s.mapSession(w)
	if !ok {
		return
	}
	path, err := s.mapHeld(sess, r.URL.Query().Get("map"))
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	s.ReportSave(path, save(sess, path))
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMapDiscard(w http.ResponseWriter, r *http.Request) {
	s.mapRestore(w, r, s.mapHeld, func(sess *maps.Session, path string) error { return sess.Discard(path) })
}

func (s *Server) handleMapReload(w http.ResponseWriter, r *http.Request) {
	// Not mapHeld: reload takes what is on disk, and a stranded map has no
	// file left to take.
	s.mapRestore(w, r, s.mapPath, func(sess *maps.Session, path string) error { return sess.Reload(path) })
}

// mapRestore is discard and reload: both throw work away and neither is a save,
// so the save report is cleared rather than replaced. A stale "not saved ·
// changed on disk" left standing over a map that has just been reloaded is a
// message about a state that no longer exists.
func (s *Server) mapRestore(
	w http.ResponseWriter, r *http.Request,
	resolve func(*maps.Session, string) (string, error),
	restore func(*maps.Session, string) error,
) {
	sess, ok := s.mapSession(w)
	if !ok {
		return
	}
	path, err := resolve(sess, r.URL.Query().Get("map"))
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	if err := restore(sess, path); err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	s.ClearSaveReport(path)
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

// refuseMapEdit records why and lets the page stream render it, the same shape
// every other refusal in Forge takes: the reason belongs on the panel where the
// mistake was made, not in a response body nobody renders.
func (s *Server) refuseMapEdit(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "map edit", "err", err)
	s.setEditProblem(err.Error())
	w.WriteHeader(http.StatusNoContent)
}

// addMapData fills in what MAP mode renders, and what every other mode's footer
// needs to know about it.
//
// The dirty set is computed for every mode, not just MAP: the save footer lives
// in the shell and is on screen everywhere, so a footer that only knew about
// unsaved maps while MAP happened to be open is a way to lose work.
func (s *Server) addMapData(data *modes.Data, r *http.Request, slug string) {
	if s.cfg.MapSession == nil {
		return
	}
	// Only where the list is on screen. A map added to the project by Tiled
	// while Forge is running should appear and one deleted should go — but that
	// is a directory listing, and running it on every tick of every mode's
	// stream is a cost that buys nothing on SCHEMA.
	if slug == "map" {
		s.cfg.MapSession.Refresh()
	}
	data.HasMaps = true
	data.Maps = s.cfg.MapSession.Maps()
	data.MapProblems = s.cfg.MapSession.Problems()

	dirty, err := s.cfg.MapSession.Dirty()
	if err == nil && len(dirty) > 0 {
		data.DirtyMaps = make(map[string]bool, len(dirty))
		for _, path := range dirty {
			data.DirtyMaps[path] = true
		}
	}
	data.ConfiguredMap = s.cfg.MapSession.Configured()
	data.SelectedMap = selectMap(data.Maps, r.URL.Query().Get("map"))
}

// selectMap resolves ?map= to a map the project has, falling back to the first
// — which is the configured one, the map the engine actually loads.
//
// Resolved against what exists rather than taken as given, so a stale link or a
// hand-typed path selects something real instead of leaving the mode showing a
// map that is not there.
func selectMap(all []maps.Map, want string) string {
	if len(all) == 0 {
		return ""
	}
	for _, m := range all {
		if m.Path == want {
			return m.Path
		}
	}
	return all[0].Path
}
