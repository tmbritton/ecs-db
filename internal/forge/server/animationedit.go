package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/animations"
	"github.com/tmbritton/ecs-db/internal/forge/templates"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
	"github.com/tmbritton/ecs-db/internal/renderer"
)

func (s *Server) addAnimationData(data *modes.Data, r *http.Request, slug string) {
	sess := s.cfg.AnimationSession
	if sess == nil {
		return
	}
	data.HasAnimations = true
	data.AnimationPath = sess.Path()
	data.AnimationActive = sess.Active()
	data.AnimationLater = sess.Later()
	data.AnimationTileSize = s.cfg.TileSize
	data.AnimationProblem = sess.Problem()
	data.AnimationMissing = sess.Missing()
	data.AnimationStranded = sess.Disappeared()
	dirty, err := sess.Dirty()
	if err != nil {
		slog.Error("computing animation dirty state", "path", sess.Path(), "err", err)
	} else {
		data.AnimationDirty = dirty
	}
	if slug == "sprites" && data.AnimationProblem == "" {
		if err := sess.Read(func(doc *animations.Document) error {
			data.AnimationDefs = doc.Animations()
			data.AnimationBindings = doc.Bindings()
			return nil
		}); err != nil {
			data.AnimationProblem = err.Error()
		} else {
			data.AnimationSelected = s.followRenames(r.URL.Query().Get("animation"), renameAnimationKind)
			if data.AnimationSelected == "" && len(data.AnimationDefs) > 0 {
				data.AnimationSelected = data.AnimationDefs[0].Name
			}
			if data.AnimationSelected != "" {
				data.AnimationPreview = sess.Preview(data.AnimationSelected, s.cfg.TileSize)
				if data.AnimationPreview.Problem == "" {
					for _, binding := range data.AnimationBindings {
						bound := sess.BindingPreviewCandidate(binding.Sheet, data.AnimationPreview.Frames,
							data.AnimationPreview.FPS, data.AnimationPreview.Loop, s.cfg.TileSize)
						if bound.Problem != "" {
							if data.AnimationBindingWarnings == nil {
								data.AnimationBindingWarnings = make(map[string]string)
							}
							data.AnimationBindingWarnings[binding.EntityType] = bound.Problem
						}
					}
				}
			}
		}
	}
}

func (s *Server) unsavedAnimations() int {
	if s.cfg.AnimationSession == nil {
		return 0
	}
	dirty, err := s.cfg.AnimationSession.Dirty()
	if err == nil && dirty {
		return 1
	}
	return 0
}

func (s *Server) animationFooter(data modes.Data) templates.Component {
	other := templates.Elsewhere{
		Schema: s.unsavedSchema(), Machines: s.unsavedMachines(), Maps: s.unsavedMaps(), Tilesets: s.unsavedTilesets(),
	}
	if data.AnimationStranded {
		return templates.AnimationRestoreFooter(data.AnimationPath, data.AnimationDirty, other)
	}
	if data.AnimationPath == "" {
		return templates.TilesetStatusFooter(data.AnimationPath, "animation file unavailable", other)
	}
	if data.AnimationMissing {
		return templates.TilesetStatusFooter(data.AnimationPath, "missing animation file", other)
	}
	if data.AnimationProblem != "" {
		return templates.AnimationProblemFooter(data.AnimationPath, data.AnimationDirty, other)
	}
	return templates.AnimationFooter(data.AnimationPath, filepath.Base(data.AnimationPath), data.AnimationDirty, other)
}

func (s *Server) registerAnimationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /forge/sprites/image", s.handleSpritesImage)
	mux.HandleFunc("POST /forge/sprites/edit/{field}", s.sameOriginOnly(s.handleAnimationField))
	mux.HandleFunc("POST /forge/sprites/create/{kind}", s.sameOriginOnly(s.handleAnimationNew))
	mux.HandleFunc("POST /forge/sprites/save", s.sameOriginOnly(s.handleAnimationSave))
	mux.HandleFunc("POST /forge/sprites/save/overwrite", s.sameOriginOnly(s.handleAnimationOverwrite))
	mux.HandleFunc("POST /forge/sprites/discard", s.sameOriginOnly(s.handleAnimationDiscard))
	mux.HandleFunc("POST /forge/sprites/reload", s.sameOriginOnly(s.handleAnimationReload))
	mux.HandleFunc("POST /forge/sprites/create", s.sameOriginOnly(s.handleAnimationCreate))
}

func (s *Server) handleSpritesImage(w http.ResponseWriter, r *http.Request) {
	sess := s.cfg.AnimationSession
	if sess == nil {
		http.NotFound(w, r)
		return
	}
	sheet := r.URL.Query().Get("sheet")
	named := false
	err := sess.Read(func(doc *animations.Document) error {
		for _, def := range doc.Animations() {
			if def.Sheet == sheet {
				named = true
			}
		}
		for _, binding := range doc.Bindings() {
			if binding.Sheet == sheet {
				named = true
			}
		}
		return nil
	})
	if err != nil || !named || sheet == "" {
		http.NotFound(w, r)
		return
	}
	path, err := sess.AssetImage(sheet)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	mime, ok := assetTypes[strings.ToLower(filepath.Ext(path))]
	if !ok || mime != "image/png" {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

func animationFrames(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	frames := make([]int, 0, len(parts))
	for _, part := range parts {
		frame, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("frames must be comma-separated whole-number columns: %w", err)
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func (s *Server) checkPlayableDraft(sess *animations.Session, sheet string, frames []int, fps float64, loop bool) error {
	view := sess.PreviewCandidate(sheet, frames, fps, loop, s.cfg.TileSize)
	if view.Image != "" && view.Problem != "" {
		return fmt.Errorf("%s", view.Problem)
	}
	return nil // the image may not have been imported yet
}

func animationNamed(doc *animations.Document, name string) (renderer.AnimDef, bool) {
	for _, def := range doc.Animations() {
		if def.Name == name {
			return def, true
		}
	}
	return renderer.AnimDef{}, false
}

func (s *Server) handleAnimationField(w http.ResponseWriter, r *http.Request) {
	sess, _, ok := s.animationFile(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	name, value, field := q.Get("name"), q.Get("value"), r.PathValue("field")
	if !q.Has("value") {
		s.refuseAnimationEdit(w, r, fmt.Errorf("%s edit needs a value", field))
		return
	}
	if field == "sheet" || field == "binding-sheet" {
		if err := sess.ValidateSheet(value); err != nil {
			s.refuseAnimationEdit(w, r, err)
			return
		}
	}
	err := sess.Edit(func(doc *animations.Document) error {
		switch field {
		case "name":
			return doc.RenameAnimation(name, value)
		case "sheet":
			if def, ok := animationNamed(doc, name); ok {
				if err := s.checkPlayableDraft(sess, value, def.Frames, def.FPS, def.Loop); err != nil {
					return err
				}
			}
			return doc.SetSheet(name, value)
		case "frames":
			frames, err := animationFrames(value)
			if err != nil {
				return err
			}
			if def, ok := animationNamed(doc, name); ok {
				if err := s.checkPlayableDraft(sess, def.Sheet, frames, def.FPS, def.Loop); err != nil {
					return err
				}
			}
			return doc.SetFrames(name, frames)
		case "fps":
			fps, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("fps needs a positive number: %w", err)
			}
			return doc.SetFPS(name, fps)
		case "loop":
			loop, err := strconv.ParseBool(value)
			if err != nil || value != "true" && value != "false" {
				return fmt.Errorf("loop takes true or false")
			}
			return doc.SetLoop(name, loop)
		case "binding-name":
			return doc.RenameBinding(name, value)
		case "binding-sheet":
			return doc.SetBindingSheet(name, value)
		default:
			return fmt.Errorf("unknown animation field %q", field)
		}
	})
	if err != nil {
		s.refuseAnimationEdit(w, r, err)
		return
	}
	if field == "name" {
		s.recordRename(renameAnimationKind, name, value)
	}
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAnimationNew(w http.ResponseWriter, r *http.Request) {
	sess, _, ok := s.animationFile(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if err := sess.ValidateSheet(q.Get("sheet")); err != nil {
		s.refuseAnimationEdit(w, r, err)
		return
	}
	err := sess.Edit(func(doc *animations.Document) error {
		switch r.PathValue("kind") {
		case "binding":
			return doc.AddBinding(q.Get("name"), q.Get("sheet"))
		case "animation":
			frames, err := animationFrames(q.Get("frames"))
			if err != nil {
				return err
			}
			fps, err := strconv.ParseFloat(q.Get("fps"), 64)
			if err != nil {
				return fmt.Errorf("fps needs a positive number: %w", err)
			}
			if loop := q.Get("loop"); loop == "true" || loop == "false" {
				if err := s.checkPlayableDraft(sess, q.Get("sheet"), frames, fps, loop == "true"); err != nil {
					return err
				}
				return doc.AddAnimation(q.Get("name"), q.Get("sheet"), frames, fps, loop == "true")
			}
			return fmt.Errorf("loop takes true or false")
		default:
			return fmt.Errorf("unknown sprite creation %q", r.PathValue("kind"))
		}
	})
	if err != nil {
		s.refuseAnimationEdit(w, r, err)
		return
	}
	if r.PathValue("kind") == "animation" {
		s.mu.Lock()
		delete(s.renamedAnimationTo, q.Get("name"))
		s.mu.Unlock()
	}
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) animationFile(w http.ResponseWriter, r *http.Request) (*animations.Session, string, bool) {
	sess := s.cfg.AnimationSession
	if sess == nil {
		http.Error(w, "no animation project is open", http.StatusConflict)
		return nil, "", false
	}
	path := r.URL.Query().Get("file")
	if path == "" || path != sess.Path() {
		s.refuseAnimationEdit(w, r, fmt.Errorf("%q is not the active animation file", path))
		return nil, "", false
	}
	return sess, path, true
}

func (s *Server) handleAnimationSave(w http.ResponseWriter, r *http.Request) {
	s.animationWrite(w, r, (*animations.Session).Save)
}

func (s *Server) handleAnimationOverwrite(w http.ResponseWriter, r *http.Request) {
	s.animationWrite(w, r, (*animations.Session).SaveOverwriting)
}

func (s *Server) animationWrite(w http.ResponseWriter, r *http.Request, action func(*animations.Session) error) {
	sess, path, ok := s.animationFile(w, r)
	if !ok {
		return
	}
	s.ReportSave(path, action(sess))
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAnimationDiscard(w http.ResponseWriter, r *http.Request) {
	s.animationRestore(w, r, (*animations.Session).Discard)
}

func (s *Server) handleAnimationReload(w http.ResponseWriter, r *http.Request) {
	s.animationRestore(w, r, (*animations.Session).Reload)
}

func (s *Server) animationRestore(w http.ResponseWriter, r *http.Request, action func(*animations.Session) error) {
	sess, path, ok := s.animationFile(w, r)
	if !ok {
		return
	}
	if err := action(sess); err != nil {
		s.refuseAnimationEdit(w, r, err)
		return
	}
	s.ClearSaveReport(path)
	s.clearAnimationRenames()
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clearAnimationRenames() {
	s.mu.Lock()
	s.renamedAnimationTo = nil
	s.mu.Unlock()
}

func (s *Server) handleAnimationCreate(w http.ResponseWriter, r *http.Request) {
	sess, _, ok := s.animationFile(w, r)
	if !ok {
		return
	}
	if err := sess.Create(); err != nil {
		s.refuseAnimationEdit(w, r, err)
		return
	}
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) refuseAnimationEdit(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "animation edit", "err", err)
	s.setEditProblem(err.Error())
	w.WriteHeader(http.StatusNoContent)
}
