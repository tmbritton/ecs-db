package server

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/templates"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
	"github.com/tmbritton/ecs-db/internal/forge/tilesets"
	"github.com/tmbritton/ecs-db/internal/forge/tilesetvalidation"
	"github.com/tmbritton/ecs-db/internal/forge/tilesurface"
	"github.com/tmbritton/ecs-db/internal/tiled"
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
			} else {
				if s.cfg.MapSession != nil {
					s.cfg.MapSession.TrackTilesetImages(data.Tileset)
				}
				data.TileSelection, data.TilePage = r.URL.Query().Get("tile"), r.URL.Query().Get("tilepage")
				data.TileView = tilesurface.Build(data.Tileset, data.TileSelection, data.TilePage, func(path string) bool {
					if s.cfg.MapSession == nil || !s.cfg.MapSession.Serves(path) {
						return false
					}
					if _, ok := assetTypes[strings.ToLower(filepath.Ext(path))]; !ok {
						return false
					}
					info, err := os.Stat(path)
					return err == nil && info.Mode().IsRegular()
				})
				for i := range data.TileView.Tiles {
					if image := data.TileView.Tiles[i].Image; image != "" {
						data.TileView.Tiles[i].ImageURL = assetURL(image)
					}
				}
				if selected := data.TileView.Selected; selected != nil && selected.Image != "" {
					selected.ImageURL = assetURL(selected.Image)
				}
				known := make(map[string]bool, len(data.Schema.EntityTypes))
				for name := range data.Schema.EntityTypes {
					known[name] = true
				}
				var ambiguous []uint32
				if s.holdsTileset(data.SelectedTileset) {
					_ = sess.Read(data.SelectedTileset, func(doc *tiled.TilesetDocument) error {
						data.TiledOnly = doc.MetadataFeatures()
						ambiguous = doc.ClassAmbiguities()
						return nil
					})
				}
				data.TileValidation = tilesetvalidation.Check(data.Tileset, data.TileView, known, ambiguous, &data.Schema)
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
	mux.HandleFunc("POST /forge/tiles/tile/class", s.sameOriginOnly(s.handleTilesetTileClass))
	mux.HandleFunc("POST /forge/tiles/tile/property", s.sameOriginOnly(s.handleTilesetTileProperty))
	mux.HandleFunc("POST /forge/tiles/save", s.sameOriginOnly(s.handleTilesetSave))
	mux.HandleFunc("POST /forge/tiles/save/overwrite", s.sameOriginOnly(s.handleTilesetOverwrite))
	mux.HandleFunc("POST /forge/tiles/discard", s.sameOriginOnly(s.handleTilesetDiscard))
	mux.HandleFunc("POST /forge/tiles/reload", s.sameOriginOnly(s.handleTilesetReload))
}

func (s *Server) handleTilesetTileProperty(w http.ResponseWriter, r *http.Request) {
	sess, path, ok := s.tilesetFile(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	id, err := strconv.ParseUint(q.Get("tile"), 10, 32)
	if err == nil && !q.Has("value") {
		err = fmt.Errorf("tile property edit named no value")
	}
	name, kind, value := q.Get("name"), q.Get("type"), q.Get("value")
	if err == nil {
		err = validateTileProperty(name, kind, value)
	}
	if err == nil {
		err = sess.Edit(path, func(doc *tiled.TilesetDocument) error {
			return doc.SetTileProperty(uint32(id), name, tiled.Property{Type: kind, Value: value})
		})
	}
	if err != nil {
		s.refuseTilesetEdit(w, r, err)
		return
	}
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func validateTileProperty(name, kind, value string) error {
	if name == "" {
		return fmt.Errorf("a tile property needs a name")
	}
	if name == tiled.PropPassable {
		return fmt.Errorf("artwork passable is ignored by the game; author movement on referenced entities in MAP")
	}
	switch kind {
	case "string":
		return nil
	case "int":
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("%s takes a whole number: %w", name, err)
		}
	case "float":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("%s takes a finite number", name)
		}
	case "bool":
		if value != "true" && value != "false" {
			return fmt.Errorf("%s takes true or false", name)
		}
	default:
		return fmt.Errorf("supported tile property types are string, int, float and bool; got %q", kind)
	}
	return nil
}

func (s *Server) handleTilesetTileClass(w http.ResponseWriter, r *http.Request) {
	sess, path, ok := s.tilesetFile(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	id, err := strconv.ParseUint(q.Get("tile"), 10, 32)
	if err == nil && q.Has("value") {
		err = sess.Edit(path, func(doc *tiled.TilesetDocument) error {
			return doc.SetTileClass(uint32(id), q.Get("value"))
		})
	} else if err == nil {
		err = fmt.Errorf("tile class edit named no value")
	}
	if err != nil {
		s.refuseTilesetEdit(w, r, err)
		return
	}
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
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
	field := ""
	switch r.URL.Path {
	case "/forge/tiles/tile/class":
		field = "tile-class"
	case "/forge/tiles/tile/property":
		field = "tile-property"
	}
	if field == "" {
		s.setEditProblem(err.Error())
	} else {
		s.setTilesetProblemOn(field, r.URL.Query().Get("file"), r.URL.Query().Get("tile"), err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
}
