package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
	"github.com/tmbritton/ecs-db/internal/forge/maps"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

func (s *Server) registerMapEditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /forge/map/save", s.sameOriginOnly(s.handleMapSave))
	mux.HandleFunc("POST /forge/map/save/overwrite", s.sameOriginOnly(s.handleMapOverwrite))
	mux.HandleFunc("POST /forge/map/discard", s.sameOriginOnly(s.handleMapDiscard))
	mux.HandleFunc("POST /forge/map/reload", s.sameOriginOnly(s.handleMapReload))
	mux.HandleFunc("POST /forge/map/paint", s.sameOriginOnly(s.handlePaint))
	mux.HandleFunc("POST /forge/map/spawn/place", s.sameOriginOnly(s.handleSpawnPlace))
	mux.HandleFunc("POST /forge/map/spawn/move", s.sameOriginOnly(s.handleSpawnMove))
	mux.HandleFunc("POST /forge/map/spawn/delete", s.sameOriginOnly(s.handleSpawnDelete))
	mux.HandleFunc("POST /forge/map/spawn/component", s.sameOriginOnly(s.handleSpawnComponent))
	mux.HandleFunc("POST /forge/map/spawn/property", s.sameOriginOnly(s.handleSpawnProperty))
	mux.HandleFunc("POST /forge/map/spawn/duplicate", s.sameOriginOnly(s.handleSpawnDuplicate))
	mux.HandleFunc("POST /forge/map/layer", s.sameOriginOnly(s.handleMapLayer))
	mux.HandleFunc("POST /forge/map/menu", s.sameOriginOnly(s.handleMapMenu))
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

// mapShown resolves the map a stroke lands on.
//
// Unlike mapPath, an empty ?map= is not a refusal: it means the map the page is
// showing, resolved by selectMap — the same call the page itself made. It has
// to be that same rule rather than merely a reasonable one, because a route
// defaulting differently from the page is exactly how a stroke lands on a file
// nobody was looking at.
//
// Reload and discard keep mapPath and mapHeld, which insist on a name. Both
// throw work away, and defaulting a destructive route to whatever happened to
// be first is a different kind of mistake from refusing one.
func (s *Server) mapShown(sess *maps.Session, want string) (string, error) {
	if want != "" {
		// A named map still has to exist. Falling back here would turn a typo
		// into an edit of the wrong file.
		return matchMap(sess.Paths(), want)
	}
	all := sess.Maps()
	if len(all) == 0 {
		return "", fmt.Errorf("this project has no maps open")
	}
	return selectMap(all, ""), nil
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
	// is a directory listing, and running it on every render of every mode's
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
	// Which map, and nothing else. Zoom, the tile in hand, the active layer and
	// which layers are drawn are signals the browser owns — see modes.MapView
	// for why they had to stop being query parameters.
	data.MapView = modes.MapView{Path: data.SelectedMap}
	if open := s.openMapMenu(); open.Open && open.Path == data.SelectedMap {
		data.MapMenu = open
	}
	// Only where it is drawn. Resolving a map means parsing it and reading its
	// tilesets, and no mode but MAP renders a cell of it.
	if slug != "map" || data.SelectedMap == "" {
		return
	}
	m, err := s.cfg.MapSession.Resolved(data.SelectedMap)
	if err != nil {
		// Already a Problem on the panel above, from the session's own check.
		// A canvas that refused to render would take the map list with it.
		return
	}
	data.Canvas = mapcanvas.Build(m, mapcanvas.Options{AssetURL: assetURL})
	data.ObjectGroups = m.ObjectGroups
	if id, err := strconv.Atoi(r.URL.Query().Get("spawn")); err == nil && id > 0 {
		data.MissingSpawn = id
		data.MapView = modes.MapView{Path: data.SelectedMap, Spawn: id}
		for _, group := range m.ObjectGroups {
			for _, obj := range group.Objects {
				if obj.ID == id && obj.Type != "" {
					selected := obj
					data.SelectedSpawn = &selected
					data.MissingSpawn = 0
					verdict, validationErr := tilemap.ValidateSpawn(&data.Schema, m, selected)
					data.SpawnErrors = verdict.Errors
					data.SpawnWarnings = verdict.Warnings
					if validationErr != nil {
						data.SpawnErrors = append(data.SpawnErrors, validationErr.Error())
					}
				}
			}
		}
	}
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
