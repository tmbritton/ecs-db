package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/tmbritton/ecs-db/internal/forge/paint"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

// paintSignals is the view state a stroke needs, as the browser holds it.
//
// All of it comes from the page rather than from the server, because all of it
// is the view's: which tool is chosen, which tile is in hand and how it is
// turned, which layer a stroke lands on, and whether that layer is being drawn.
// Datastar sends the page's signals with every request, so the route is told
// rather than remembering — see modes.MapSignals for why the server holding
// this instead would put it back out of step.
//
// No struct tags: nothing unmarshals into this. signalsFrom picks the fields
// out of a map by hand, because the per-layer visibility flags are named for
// their layer and have no fixed shape to unmarshal into.
type paintSignals struct {
	Tool  string
	Tile  uint32
	Layer int
	// A stable Tiled ID accompanies the old index so a layer reordered by the
	// server remains the one a queued stroke targets. Older direct callers can
	// still name an index without a layerID.
	LayerID    int
	HasLayerID bool
	FlipH      bool
	FlipV      bool
	FlipD      bool
	// Hidden is the per-layer visibility, keyed by modes.LayerHideSignal for
	// pages with layer IDs and by HideSignal for older direct callers.
	Hidden map[string]bool
}

// handlePaint applies one stroke.
//
// One request per stroke, however many cells it covered: a drag arrives as its
// two ends and the trail between them, and becomes one entry in the session and
// one patch on the wire. Forty requests for a forty-cell drag would be forty
// saves' worth of churn for one gesture.
func (s *Server) handlePaint(w http.ResponseWriter, r *http.Request) {
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

	var raw map[string]any
	if err := datastar.ReadSignals(r, &raw); err != nil {
		s.refuseMapEdit(w, r, fmt.Errorf("reading what the page has in hand: %w", err))
		return
	}
	sig := signalsFrom(raw)

	from, err := cellFrom(q, "x", "y")
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}
	// A click sends one end. Defaulted here rather than in paint, where a zero
	// To is the cell (0,0) and not "no second corner" — a caller that meant a
	// click would otherwise paint a rectangle back to the origin.
	to := from
	if q.Has("x2") || q.Has("y2") {
		if to, err = cellFrom(q, "x2", "y2"); err != nil {
			s.refuseMapEdit(w, r, err)
			return
		}
	}

	// The cells the pointer actually went through, when there was a pointer.
	// Absent is a click, or any caller without one; empty is a stroke that says
	// it went nowhere, which is a different thing and is refused rather than
	// quietly falling back to the rectangle.
	trail, err := trailFrom(q)
	if err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}

	op := paint.Op{
		Kind:  paint.Kind(sig.Tool),
		From:  from,
		To:    to,
		Cells: trail,
		Tile:  tiled.Tile{GID: sig.Tile, FlipH: sig.FlipH, FlipV: sig.FlipV, FlipD: sig.FlipD},
		// modes.HideSignal, not a literal: the page names these signals, and a
		// name built separately here would silently stop matching.
	}
	if err := sess.Edit(path, func(d *tiled.Document) error {
		m, err := d.Map()
		if err != nil {
			return err
		}
		layer := sig.Layer
		if sig.HasLayerID && sig.LayerID > 0 {
			layer = -1
			for i, candidate := range m.Layers {
				if candidate.ID == sig.LayerID {
					if layer != -1 {
						return fmt.Errorf("selected tile layer ID %d is ambiguous; choose a layer with a unique ID", sig.LayerID)
					}
					layer = i
				}
			}
			if layer == -1 {
				return fmt.Errorf("selected tile layer %d no longer exists; choose another layer", sig.LayerID)
			}
		}
		op.Layer = layer
		if sig.HasLayerID && sig.LayerID > 0 && layer >= 0 && layer < len(m.Layers) {
			op.Hidden = sig.Hidden[modes.LayerHideSignal(sig.LayerID, layer)]
		} else {
			op.Hidden = sig.Hidden[modes.HideSignal(layer)]
		}
		result, err := paint.Apply(m, op)
		if err != nil {
			return err
		}
		if !result.Changed {
			// Nothing to write, and nothing to say: painting the tile a cell
			// already holds is a legal stroke that happened to change nothing,
			// not a refusal. Writing anyway would dirty the map and teach
			// everyone to ignore the footer.
			return nil
		}
		return d.SetLayerData(result.Layer, result.Data)
	}); err != nil {
		s.refuseMapEdit(w, r, err)
		return
	}

	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

// signalsFrom picks a stroke's view state out of the page's signals.
//
// By hand rather than by struct tag, because the per-layer visibility flags are
// named for their layer — hide0, hide1 — so there is no fixed shape to unmarshal
// them into.
func signalsFrom(raw map[string]any) paintSignals {
	sig := paintSignals{Hidden: map[string]bool{}}
	for k, v := range raw {
		switch k {
		case "tool":
			sig.Tool, _ = v.(string)
		case "tile":
			sig.Tile = uint32(number(v))
		case "layer":
			sig.Layer = int(number(v))
		case "layerID":
			sig.LayerID = int(number(v))
			sig.HasLayerID = true
		case "flipH":
			sig.FlipH, _ = v.(bool)
		case "flipV":
			sig.FlipV, _ = v.(bool)
		case "flipD":
			sig.FlipD, _ = v.(bool)
		default:
			if b, isBool := v.(bool); isBool && len(k) > 4 && k[:4] == "hide" {
				sig.Hidden[k] = b
			}
		}
	}
	return sig
}

// number reads a JSON number, which arrives as a float64 whatever it looked like
// on the way in.
func number(v any) float64 {
	f, _ := v.(float64)
	return f
}

// trailFrom reads the cells a pointer-driven stroke covered: a flat list of
// coordinates, x then y, in the order the pointer reached them.
//
// Flat rather than a JSON array in the body, because the body is the page's
// signals and a stroke's path is not one — it changes with every gesture and
// would then travel with every other request the page makes for the rest of its
// life. The page emits each cell once, so its own strokes can never name more
// cells than the map has; the length of anything else that arrives here is
// bounded by the server's header limit, and every cell in it is checked against
// the layer before a single one is written.
//
// A missing parameter is no trail and no error: a click has none, and neither
// does Fill, which means the rectangle its ends span whatever else it is sent.
func trailFrom(q map[string][]string) ([]paint.Cell, error) {
	vals := q["cells"]
	if len(vals) == 0 {
		return nil, nil
	}
	parts := strings.Split(vals[0], ",")
	if len(parts) == 1 && parts[0] == "" {
		return nil, errors.New("the trail names no cells, so there is nothing to paint")
	}
	if len(parts)%2 != 0 {
		return nil, fmt.Errorf("a trail is pairs of coordinates, and that is %d of them", len(parts))
	}
	out := make([]paint.Cell, 0, len(parts)/2)
	for i := 0; i < len(parts); i += 2 {
		x, err := strconv.Atoi(parts[i])
		if err != nil {
			return nil, fmt.Errorf("which cell (trail, %d): %w", i/2, err)
		}
		y, err := strconv.Atoi(parts[i+1])
		if err != nil {
			return nil, fmt.Errorf("which cell (trail, %d): %w", i/2, err)
		}
		out = append(out, paint.Cell{X: x, Y: y})
	}
	return out, nil
}

func cellFrom(q map[string][]string, xKey, yKey string) (paint.Cell, error) {
	x, err := intParam(q, xKey)
	if err != nil {
		return paint.Cell{}, err
	}
	y, err := intParam(q, yKey)
	if err != nil {
		return paint.Cell{}, err
	}
	return paint.Cell{X: x, Y: y}, nil
}

func intParam(q map[string][]string, key string) (int, error) {
	vals := q[key]
	if len(vals) == 0 {
		return 0, fmt.Errorf("the stroke does not say where it is: no %s", key)
	}
	n, err := strconv.Atoi(vals[0])
	if err != nil {
		return 0, fmt.Errorf("which cell (%s): %w", key, err)
	}
	return n, nil
}
