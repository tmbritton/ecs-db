package server

import (
	"fmt"
	"net/http"
	"strconv"

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
	FlipH bool
	FlipV bool
	FlipD bool
	// Hidden is the per-layer visibility, keyed by modes.HideSignal.
	Hidden map[string]bool
}

// handlePaint applies one stroke.
//
// One request per stroke, however many cells it covered: a drag arrives as two
// corners and becomes one entry in the session and one patch on the wire. Forty
// requests for a forty-cell drag would be forty saves' worth of churn for one
// gesture.
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

	op := paint.Op{
		Kind:  paint.Kind(sig.Tool),
		Layer: sig.Layer,
		From:  from,
		To:    to,
		Tile:  tiled.Tile{GID: sig.Tile, FlipH: sig.FlipH, FlipV: sig.FlipV, FlipD: sig.FlipD},
		// modes.HideSignal, not a literal: the page names these signals, and a
		// name built separately here would silently stop matching.
		Hidden: sig.Hidden[modes.HideSignal(sig.Layer)],
	}
	if err := sess.Edit(path, func(d *tiled.Document) error {
		m, err := d.Map()
		if err != nil {
			return err
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
