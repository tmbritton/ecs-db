package server

import (
	"fmt"
	"net/http"

	"github.com/tmbritton/ecs-db/internal/forge/spawn"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

// editSpawn resolves the project map exactly as the page does and puts all
// failures into MAP's visible error region rather than an ignored response.
func (s *Server) editSpawn(w http.ResponseWriter, r *http.Request, edit func(*tiled.Document) error) {
	sess, ok := s.mapSession(w)
	if !ok {
		return
	}
	path, err := s.mapShown(sess, r.URL.Query().Get("map"))
	if err == nil {
		err = sess.Edit(path, edit)
	}
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSpawnPlace(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if s.cfg.Session == nil {
		s.refuseMapEdit(w, r, fmt.Errorf("there is no schema to choose a spawn type from"))
		return
	}
	group, err := intParam(q, "group")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	position, err := cellFrom(q, "x", "y")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	var current schema.DatabaseSchema
	s.cfg.Session.Read(func(d schema.DatabaseSchema) { current = d })
	s.editSpawn(w, r, func(d *tiled.Document) error {
		_, err := spawn.Place(d, &current, group, q.Get("type"), position.X, position.Y)
		return err
	})
}

func (s *Server) handleSpawnMove(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, err := intParam(q, "id")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	position, err := cellFrom(q, "x", "y")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	s.editSpawn(w, r, func(d *tiled.Document) error {
		return spawn.Move(d, id, position.X, position.Y)
	})
}

func (s *Server) handleSpawnDelete(w http.ResponseWriter, r *http.Request) {
	id, err := intParam(r.URL.Query(), "id")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	s.editSpawn(w, r, func(d *tiled.Document) error { return spawn.Delete(d, id) })
}
