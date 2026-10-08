package server

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
	"github.com/tmbritton/ecs-db/internal/forge/tilelinks"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

var errNoVisibleTile = errors.New("no visible tile at this cell")

// Opening attempts are ordered at receipt, before reading the map. Otherwise
// an old right-click that takes longer to resolve can overwrite or close the
// menu opened by the next right-click.
func (s *Server) beginMapMenuOpen() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mapMenuSeq++
	return s.mapMenuSeq
}

func (s *Server) finishMapMenuOpen(attempt uint64, menu modes.MapMenu) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if attempt != s.mapMenuSeq {
		return false
	}
	if menu.Open {
		menu.Token = strconv.FormatUint(attempt, 10)
	}
	s.mapMenu = menu
	return true
}

// A full page load invalidates even an open request still reading the map.
func (s *Server) closeMapMenu() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mapMenuSeq++
	s.mapMenu = modes.MapMenu{}
}

func (s *Server) closeMapMenuIf(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token != "" && s.mapMenu.Token != token {
		return
	}
	// A close from an older menu may dismiss what is still on screen, but must
	// not invalidate a newer opening request already in flight.
	if token == "" || token == strconv.FormatUint(s.mapMenuSeq, 10) {
		s.mapMenuSeq++
	}
	s.mapMenu = modes.MapMenu{}
}

func (s *Server) openMapMenu() modes.MapMenu {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mapMenu
}

// takeMapMenu consumes exactly the opening that authorized an edit. The lock
// covers the identity check and removal together: a close or replacement can
// win first, but cannot slip in after an action was authorized and before the
// server consumes that authorization. A later opening is never closed by the
// old action's response.
func (s *Server) takeMapMenu(token string) (modes.MapMenu, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" || !s.mapMenu.Open || s.mapMenu.Token != token ||
		token != strconv.FormatUint(s.mapMenuSeq, 10) {
		return modes.MapMenu{}, false
	}
	menu := s.mapMenu
	s.mapMenuSeq++
	s.mapMenu = modes.MapMenu{}
	return menu, true
}

func (s *Server) handleMapMenu(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("close") != "" {
		s.closeMapMenuIf(q.Get("token"))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	attempt := s.beginMapMenuOpen()
	sess, ok := s.mapSession(w)
	if !ok {
		s.finishMapMenuOpen(attempt, modes.MapMenu{})
		return
	}
	path, err := s.mapShown(sess, q.Get("map"))
	if err != nil {
		s.refuseMapMenu(w, r, attempt, err)
		return
	}
	x, y, err := floats(q.Get("x"), q.Get("y"))
	if err != nil {
		s.refuseMapMenu(w, r, attempt, err)
		return
	}
	menu := modes.MapMenu{Open: true, Path: path, Kind: q.Get("target"), X: x, Y: y}
	err = sess.Read(path, func(d *tiled.Document) error {
		m, err := d.Map()
		if err != nil {
			return err
		}
		switch menu.Kind {
		case "layer":
			index, err := intParam(q, "layer")
			if err != nil {
				return err
			}
			if index < 0 || index >= len(m.Layers) {
				return fmt.Errorf("there is no tile layer %d", index)
			}
			menu.Layer, menu.LayerID, menu.LayerName = index, m.Layers[index].ID, m.Layers[index].Name
			menu.ViewID = mapcanvas.UniqueLayerID(m.Layers, index)
			menu.CanMoveUp = d.CanMoveLayer(index, -1)
			menu.CanMoveDown = d.CanMoveLayer(index, 1)
			menu.CanDelete = d.CanDeleteLayer(index)
		case "spawn":
			id, err := intParam(q, "id")
			if err != nil {
				return err
			}
			found := 0
			for _, group := range m.ObjectGroups {
				for _, obj := range group.Objects {
					if obj.ID == id && obj.Type != "" {
						found++
					}
				}
			}
			if found != 1 {
				return fmt.Errorf("spawn object %d is missing or ambiguous", id)
			}
			menu.ObjectID = id
		case "canvas":
			cell, err := cellFrom(q, "cellx", "celly")
			if err != nil {
				return err
			}
			if cell.X < 0 || cell.Y < 0 || cell.X >= m.Width || cell.Y >= m.Height {
				return fmt.Errorf("cell %d,%d is outside this map", cell.X, cell.Y)
			}
			var raw map[string]any
			if err := datastar.ReadSignals(r, &raw); err != nil {
				return fmt.Errorf("reading visible layers: %w", err)
			}
			hidden := signalsFrom(raw).Hidden
			for i := len(m.Layers) - 1; i >= 0; i-- {
				layer := m.Layers[i]
				hide, present := hidden[modes.LayerHideSignal(mapcanvas.UniqueLayerID(m.Layers, i), i)]
				if layer.Opacity == 0 || (present && hide) || (!present && !layer.Visible) {
					continue
				}
				if tile := layer.TileAt(cell.X, cell.Y); tile.GID != 0 {
					menu.Tile, menu.CellX, menu.CellY = tile, cell.X, cell.Y
					return nil
				}
			}
			return errNoVisibleTile
		default:
			return fmt.Errorf("there is no %q map menu", menu.Kind)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errNoVisibleTile) {
			s.finishMapMenuOpen(attempt, modes.MapMenu{})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.refuseMapMenu(w, r, attempt, err)
		return
	}
	s.finishMapMenuOpen(attempt, menu)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) refuseMapMenu(w http.ResponseWriter, r *http.Request, attempt uint64, err error) {
	if s.finishMapMenuOpen(attempt, modes.MapMenu{}) {
		s.refuseMapEdit(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMapLayer(w http.ResponseWriter, r *http.Request) {
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
	index, err := intParam(q, "layer")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	wantID, err := intParam(q, "id")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	menu, ok := s.takeMapMenu(q.Get("token"))
	if !ok {
		s.refuseMapEdit(w, r, fmt.Errorf("the layer menu changed or closed; choose the layer again"))
		return
	}
	err = sess.Edit(path, func(d *tiled.Document) error {
		m, err := d.Map()
		if err != nil {
			return err
		}
		if index < 0 || index >= len(m.Layers) || m.Layers[index].ID != wantID {
			return fmt.Errorf("tile layer %d changed since this menu opened; choose it again", index)
		}
		if menu.Kind != "layer" || menu.Path != path || menu.Layer != index ||
			menu.LayerID != wantID || menu.LayerName != m.Layers[index].Name {
			return fmt.Errorf("tile layer %d or its menu changed or closed; choose it again", index)
		}
		switch q.Get("op") {
		case "rename":
			return d.RenameLayer(index, q.Get("name"))
		case "up":
			return d.MoveLayer(index, -1)
		case "down":
			return d.MoveLayer(index, 1)
		case "delete":
			trial, err := tiled.ParseDocument(d.Bytes(), filepath.Base(path))
			if err != nil {
				return err
			}
			if err := tilelinks.DeleteLayer(trial, index); err != nil {
				return err
			}
			if err := validateMapTileLinks(path, trial); err != nil {
				return err
			}
			return tilelinks.DeleteLayer(d, index)
		default:
			return fmt.Errorf("unknown tile layer operation %q", q.Get("op"))
		}
	})
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSpawnDuplicate(w http.ResponseWriter, r *http.Request) {
	id, err := intParam(r.URL.Query(), "id")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	if !s.takeSpawnMenu(w, r, id) {
		return
	}
	s.editSpawn(w, r, func(d *tiled.Document) error {
		m, err := d.Map()
		if err != nil {
			return err
		}
		var original *tiled.Object
		for _, group := range m.ObjectGroups {
			for i := range group.Objects {
				if group.Objects[i].ID == id && group.Objects[i].Type != "" {
					if original != nil {
						return fmt.Errorf("spawn %d is ambiguous", id)
					}
					obj := group.Objects[i]
					original = &obj
				}
			}
		}
		if original == nil || m.TileWidth <= 0 || m.TileHeight <= 0 {
			return fmt.Errorf("spawn %d is not in a map with valid tile dimensions", id)
		}
		cellX := int(math.Floor(original.X / float64(m.TileWidth)))
		cellY := int(math.Floor(original.Y / float64(m.TileHeight)))
		if original.GID != 0 {
			cellY = int(math.Floor((original.Y - 1) / float64(m.TileHeight)))
		}
		x, y := cellX, cellY
		switch {
		case x+1 < m.Width:
			x++
		case x-1 >= 0:
			x--
		case y+1 < m.Height:
			y++
		case y-1 >= 0:
			y--
		default:
			return fmt.Errorf("this map has no neighboring cell for a duplicate spawn")
		}
		px, py := float64(x*m.TileWidth), float64(y*m.TileHeight)
		if original.GID != 0 {
			py += float64(m.TileHeight)
		}
		_, err = d.DuplicateObject(id, px, py)
		return err
	})
}

// takeSpawnMenu validates the opening before the edit. Only a menu action uses
// this; the inspector's delete button continues to use its existing route
// without an opening token.
func (s *Server) takeSpawnMenu(w http.ResponseWriter, r *http.Request, id int) bool {
	menu, ok := s.takeMapMenu(r.URL.Query().Get("token"))
	if !ok || menu.Kind != "spawn" || menu.ObjectID != id || menu.Path != r.URL.Query().Get("map") {
		s.refuseMapEdit(w, r, fmt.Errorf("the spawn menu changed or closed; choose the spawn again"))
		return false
	}
	return true
}
