package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/spawn"
	"github.com/tmbritton/ecs-db/internal/forge/tilelinks"
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
		err = sess.Edit(path, func(d *tiled.Document) error {
			m, err := d.Map()
			if err != nil {
				return err
			}
			linked := false
			for _, group := range m.ObjectGroups {
				for _, obj := range group.Objects {
					for name := range obj.Properties {
						linked = linked || strings.EqualFold(name, "TileLink.layerID")
					}
				}
			}
			if !linked {
				return edit(d)
			}
			trial, err := tiled.ParseDocument(d.Bytes(), filepath.Base(path))
			if err != nil {
				return err
			}
			if err := edit(trial); err != nil {
				return err
			}
			if err := validateMapTileLinks(path, trial); err != nil {
				return err
			}
			return edit(d)
		})
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
		return tilelinks.Move(d, id, position.X, position.Y)
	})
}

func (s *Server) handleSpawnDelete(w http.ResponseWriter, r *http.Request) {
	id, err := intParam(r.URL.Query(), "id")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	if r.URL.Query().Has("token") && !s.takeSpawnMenu(w, r, id) {
		return
	}
	if !r.URL.Query().Has("token") {
		// The inspector has no menu token. If it deletes the spawn a menu is
		// about, dismiss only that opening, not a newer one from another tab.
		if menu := s.openMapMenu(); menu.Open && menu.Kind == "spawn" &&
			menu.ObjectID == id && menu.Path == r.URL.Query().Get("map") {
			s.closeMapMenuIf(menu.Token)
		}
	}
	s.editSpawn(w, r, func(d *tiled.Document) error { return spawn.Delete(d, id) })
}

func (s *Server) spawnSchema() (schema.DatabaseSchema, error) {
	if s.cfg.Session == nil {
		return schema.DatabaseSchema{}, fmt.Errorf("there is no schema to validate this spawn against")
	}
	var current schema.DatabaseSchema
	s.cfg.Session.Read(func(d schema.DatabaseSchema) { current = d })
	return current, nil
}

func (s *Server) handleSpawnComponent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, err := intParam(q, "id")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	current, err := s.spawnSchema()
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	if q.Get("repair") == "required" {
		if q.Get("add") != "" || q.Get("detach") != "" {
			s.refuseMapEdit(w, r, fmt.Errorf("repair cannot add or detach another component in the same request"))
			return
		}
		s.editSpawn(w, r, func(d *tiled.Document) error {
			_, err := spawn.RepairRequired(d, &current, id)
			return err
		})
		return
	}
	add, detach := q.Get("add"), q.Get("detach")
	if (add == "") == (detach == "") {
		s.refuseMapEdit(w, r, fmt.Errorf("choose one component to add or detach"))
		return
	}
	s.editSpawn(w, r, func(d *tiled.Document) error {
		if add != "" {
			targets := map[string]string{"target_entity_id": q.Get("target")}
			for key, values := range q {
				if field, ok := strings.CutPrefix(key, "target."); ok && len(values) > 0 {
					targets[field] = values[0]
				}
			}
			_, err := spawn.AddComponentWithTargets(d, &current, id, add, targets)
			return err
		}
		_, err := spawn.DetachComponent(d, &current, id, detach)
		return err
	})
}

func (s *Server) handleSpawnProperty(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, err := intParam(q, "id")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	if !q.Has("value") {
		s.refuseMapEdit(w, r, fmt.Errorf("no value was provided for this field"))
		return
	}
	current, err := s.spawnSchema()
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	s.editSpawn(w, r, func(d *tiled.Document) error {
		_, err := spawn.SetProperty(d, &current, id, q.Get("component"), q.Get("field"), q.Get("value"))
		return err
	})
}
