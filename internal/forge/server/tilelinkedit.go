package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/tilelinks"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

func (s *Server) handleTileLink(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.mapSession(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	path, err := s.mapShown(sess, q.Get("map"))
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	layerID, layerErr := strconv.Atoi(q.Get("layer"))
	objectID, objectErr := strconv.Atoi(q.Get("id"))
	at, cellErr := cellFrom(q, "x", "y")
	if layerErr != nil || objectErr != nil || cellErr != nil || layerID <= 0 || objectID <= 0 {
		s.refuseMapEdit(w, r, fmt.Errorf("link a Tile by a positive layer ID, object ID and integer x,y: %v", cellErr))
		return
	}
	action := q.Get("action")
	if action != "link" && action != "unlink" {
		s.refuseMapEdit(w, r, fmt.Errorf("tile reference action %q is not link or unlink", action))
		return
	}
	rules, err := s.spawnSchema()
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	err = sess.Edit(path, func(d *tiled.Document) error {
		cell := tilelinks.Cell{X: at.X, Y: at.Y}
		if action == "unlink" {
			trial, err := tiled.ParseDocument(d.Bytes(), filepath.Base(path))
			if err != nil {
				return err
			}
			changed, err := tilelinks.Unlink(trial, objectID, layerID, cell)
			if err != nil || !changed {
				return err
			}
			if err := validateMapTileLinks(path, trial); err != nil {
				return err
			}
			_, err = tilelinks.Unlink(d, objectID, layerID, cell)
			return err
		}
		m, err := d.Map()
		if err != nil {
			return err
		}
		count := 0
		var object tiled.Object
		for _, group := range m.ObjectGroups {
			for _, candidate := range group.Objects {
				if candidate.ID == objectID {
					count++
					object = candidate
				}
			}
		}
		if count != 1 || object.Type == "" {
			return fmt.Errorf("object ID %d is missing, duplicated or untyped", objectID)
		}
		verdict, err := tilemap.ValidateSpawn(&rules, m, object)
		if err != nil {
			return err
		}
		if !verdict.Valid() {
			return fmt.Errorf("object %d cannot be linked: %s", objectID, strings.Join(verdict.Errors, "; "))
		}
		if err := tilemap.ValidateSpawnFields(&rules, m, object); err != nil {
			return err
		}
		trial, err := tiled.ParseDocument(d.Bytes(), filepath.Base(path))
		if err != nil {
			return err
		}
		changed, err := tilelinks.Link(trial, objectID, layerID, cell)
		if err != nil || !changed {
			return err
		}
		if err := validateMapTileLinks(path, trial); err != nil {
			return err
		}
		_, err = tilelinks.Link(d, objectID, layerID, cell)
		return err
	})
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

// A candidate link or stroke must be importable before the working TMX changes.
// Only maps with authored TileLinks need external tilesets for this check.
func validateMapTileLinks(path string, candidate *tiled.Document) error {
	m, err := tiled.Parse(candidate.Bytes(), path)
	if err != nil {
		return err
	}
	var ids []int
	for _, group := range m.ObjectGroups {
		for _, obj := range group.Objects {
			for key := range obj.Properties {
				if strings.EqualFold(key, "TileLink.layerID") {
					ids = append(ids, obj.ID)
				}
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if err := m.ResolveTilesets(filepath.Dir(path), os.ReadFile); err != nil {
		return err
	}
	artValidator := tilelinks.NewArtValidator(m)
	for _, id := range ids {
		if err := artValidator.Validate(id); err != nil {
			return err
		}
	}
	return nil
}
