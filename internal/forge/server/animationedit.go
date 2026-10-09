package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/tmbritton/ecs-db/internal/forge/animations"
	"github.com/tmbritton/ecs-db/internal/forge/templates"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
)

func (s *Server) addAnimationData(data *modes.Data, slug string) {
	sess := s.cfg.AnimationSession
	if sess == nil {
		return
	}
	data.HasAnimations = true
	data.AnimationPath = sess.Path()
	data.AnimationActive = sess.Active()
	data.AnimationLater = sess.Later()
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
	mux.HandleFunc("POST /forge/sprites/save", s.sameOriginOnly(s.handleAnimationSave))
	mux.HandleFunc("POST /forge/sprites/save/overwrite", s.sameOriginOnly(s.handleAnimationOverwrite))
	mux.HandleFunc("POST /forge/sprites/discard", s.sameOriginOnly(s.handleAnimationDiscard))
	mux.HandleFunc("POST /forge/sprites/reload", s.sameOriginOnly(s.handleAnimationReload))
	mux.HandleFunc("POST /forge/sprites/create", s.sameOriginOnly(s.handleAnimationCreate))
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
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
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
