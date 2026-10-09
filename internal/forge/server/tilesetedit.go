package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/templates"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
	"github.com/tmbritton/ecs-db/internal/forge/tilesets"
)

func (s *Server) addTilesetData(data *modes.Data, r *http.Request, slug string) {
	sess := s.cfg.TilesetSession
	if sess == nil {
		return
	}
	data.HasTilesets = true
	if slug == "tiles" {
		// Session.Refresh discovers new maps and references in working TMX.
		sess.Refresh()
		data.Tilesets = sess.Entries()
		want := r.URL.Query().Get("file")
		for _, entry := range data.Tilesets {
			if want == "" || entry.Path == want {
				data.SelectedTileset = entry.Path
				break
			}
		}
		if data.SelectedTileset == "" && len(data.Tilesets) > 0 {
			data.SelectedTileset = data.Tilesets[0].Path
		}
		if data.SelectedTileset != "" {
			var err error
			data.Tileset, err = sess.Describe(data.SelectedTileset)
			if err != nil {
				data.TilesetProblem = err.Error()
			}
		}
	}
	dirty, err := sess.Dirty()
	if err != nil {
		slog.ErrorContext(r.Context(), "computing tileset dirty state", "err", err)
		return
	}
	data.DirtyTilesets = make(map[string]bool, len(dirty))
	for _, path := range dirty {
		data.DirtyTilesets[path] = true
	}
}

func (s *Server) unsavedTilesets() int {
	if s.cfg.TilesetSession == nil {
		return 0
	}
	paths, err := s.cfg.TilesetSession.Dirty()
	if err != nil {
		return 0
	}
	return len(paths)
}

func (s *Server) holdsTileset(path string) bool {
	if s.cfg.TilesetSession == nil {
		return false
	}
	for _, entry := range s.cfg.TilesetSession.Entries() {
		if entry.Path == path && entry.Writable {
			return true
		}
	}
	return false
}

func (s *Server) tilesetFooter(data modes.Data) templates.Component {
	for _, entry := range data.Tilesets {
		if entry.Path != data.SelectedTileset {
			continue
		}
		other := s.unsavedTilesets()
		if entry.Writable && data.DirtyTilesets[entry.Path] {
			other--
		}
		elsewhere := templates.Elsewhere{
			Schema: s.unsavedSchema(), Machines: s.unsavedMachines(), Maps: s.unsavedMaps(), Tilesets: other,
		}
		if entry.Writable {
			return templates.TilesetFooter(entry.Path, filepath.Base(entry.Path), data.DirtyTilesets[entry.Path], elsewhere)
		}
		status := "unavailable"
		if strings.EqualFold(filepath.Ext(entry.Path), ".tsj") {
			status = "read-only"
		}
		return templates.TilesetStatusFooter(entry.Path, status, elsewhere)
	}
	return templates.TilesetStatusFooter("", "no external tilesets", templates.Elsewhere{
		Schema: s.unsavedSchema(), Machines: s.unsavedMachines(), Maps: s.unsavedMaps(), Tilesets: s.unsavedTilesets(),
	})
}

func (s *Server) registerTilesetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /forge/tiles/save", s.sameOriginOnly(s.handleTilesetSave))
	mux.HandleFunc("POST /forge/tiles/save/overwrite", s.sameOriginOnly(s.handleTilesetOverwrite))
	mux.HandleFunc("POST /forge/tiles/discard", s.sameOriginOnly(s.handleTilesetDiscard))
	mux.HandleFunc("POST /forge/tiles/reload", s.sameOriginOnly(s.handleTilesetReload))
}

func (s *Server) tilesetFile(w http.ResponseWriter, r *http.Request) (*tilesets.Session, string, bool) {
	sess := s.cfg.TilesetSession
	if sess == nil {
		http.Error(w, "no tileset project is open", http.StatusConflict)
		return nil, "", false
	}
	path := r.URL.Query().Get("file")
	if !s.holdsTileset(path) {
		s.refuseTilesetEdit(w, r, fmt.Errorf("%q is not a writable project tileset", path))
		return nil, "", false
	}
	return sess, path, true
}

func (s *Server) handleTilesetSave(w http.ResponseWriter, r *http.Request) {
	s.tilesetWrite(w, r, (*tilesets.Session).Save)
}

func (s *Server) handleTilesetOverwrite(w http.ResponseWriter, r *http.Request) {
	s.tilesetWrite(w, r, (*tilesets.Session).SaveOverwriting)
}

func (s *Server) tilesetWrite(w http.ResponseWriter, r *http.Request, action func(*tilesets.Session, string) error) {
	sess, path, ok := s.tilesetFile(w, r)
	if !ok {
		return
	}
	s.ReportSave(path, action(sess, path))
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTilesetDiscard(w http.ResponseWriter, r *http.Request) {
	s.tilesetRestore(w, r, (*tilesets.Session).Discard)
}

func (s *Server) handleTilesetReload(w http.ResponseWriter, r *http.Request) {
	s.tilesetRestore(w, r, (*tilesets.Session).Reload)
}

func (s *Server) tilesetRestore(w http.ResponseWriter, r *http.Request, action func(*tilesets.Session, string) error) {
	sess, path, ok := s.tilesetFile(w, r)
	if !ok {
		return
	}
	if err := action(sess, path); err != nil {
		s.refuseTilesetEdit(w, r, err)
		return
	}
	s.ClearSaveReport(path)
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) refuseTilesetEdit(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "tileset edit", "err", err)
	s.setEditProblem(err.Error())
	w.WriteHeader(http.StatusNoContent)
}
