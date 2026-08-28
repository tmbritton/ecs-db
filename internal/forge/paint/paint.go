// Package paint turns a stroke into the tiles that result.
//
// Everything here is a function from a map value and an operation to the layer
// data that comes out of it: no session, no HTTP, no browser, no pointer. That
// is the whole point of doing it before the pointer surface — the arithmetic of
// what a drag means is provable at a desk, and Story 5 is then left to prove the
// wiring rather than the sums.
//
// What it deliberately does not do is decide *which* tool a gesture is, or
// remember one. The tool, the tile in hand and its rotation are the browser's,
// carried on the request; the server is told them the way it is told everything
// else the view owns. An earlier draft of the story had tool state living here,
// which was written before a stream's fixed subscription made server-rendered
// view state impossible to keep in step.
package paint

import (
	"errors"
	"fmt"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

// A Kind is what a stroke does.
type Kind string

const (
	// Stamp writes the tile in hand into every cell the stroke touched.
	Stamp Kind = "stamp"
	// Fill writes it across the rectangle the stroke spans, both corners
	// included.
	Fill Kind = "fill"
	// Erase writes the empty cell. The tile in hand is ignored: erasing is not
	// stamping a blank, and gid 0 is a cell with nothing in it rather than a
	// tile that happens to look like nothing.
	Erase Kind = "erase"
)

// A Cell is a position in the map, in cells and not pixels.
type Cell struct{ X, Y int }

// An Op is one stroke: everything the server needs to know to apply it, and
// nothing about how the pointer got there.
//
// One operation per request even when a drag covered forty cells, so that it is
// one entry in the session and one patch on the wire rather than forty of each.
type Op struct {
	Kind  Kind
	Layer int
	// From and To are the ends of the stroke. A click has them equal.
	From, To Cell
	// Tile is what is in hand, flags and all — a rotated stamp is the same tile
	// with different flags, not a different tile.
	Tile tiled.Tile
	// Hidden is whether the view is currently drawing this layer. Painting into
	// one it is not is refused: the most common way to lose an hour in a tile
	// editor is painting into the layer you were not looking at.
	Hidden bool
}

// A Result is what an operation came to.
type Result struct {
	// Layer is the layer index the data belongs to.
	Layer int
	// Data is the layer's new contents, or nil when nothing changed.
	Data []uint32
	// Changed reports whether the map is different afterwards. False means the
	// caller writes nothing — stamping the tile a cell already holds must not
	// dirty the file, or the footer stops meaning anything.
	Changed bool
}

// ErrNoChange is not returned. Nothing changing is an ordinary outcome of a
// legal operation, not a refusal, and conflating the two would make the caller
// report "cannot paint there" for painting exactly what was already there.
var errOutside = errors.New("that cell is outside the map")

// Apply works out what a stroke does to a map.
//
// The map is read and never written. The session holds it, and a caller that
// refused the result would otherwise be holding a map that had been changed
// anyway — which is the bug that makes an editor lose work it said it had not
// taken.
func Apply(m *tiled.Map, op Op) (Result, error) {
	if m == nil {
		return Result{}, errors.New("there is no map to paint on")
	}
	if op.Layer < 0 || op.Layer >= len(m.Layers) {
		return Result{}, fmt.Errorf("there is no layer %d to paint into", op.Layer)
	}
	if op.Hidden {
		return Result{}, errors.New("that layer is not drawing, so a stroke would land where you cannot see it")
	}
	switch op.Kind {
	case Stamp, Fill, Erase:
	default:
		return Result{}, fmt.Errorf("%q is not something a stroke can do", op.Kind)
	}

	layer := m.Layers[op.Layer]
	if err := inside(layer, op.From); err != nil {
		return Result{}, err
	}
	if err := inside(layer, op.To); err != nil {
		return Result{}, err
	}

	// Written into a copy, so the caller's map is untouched whatever happens.
	next := make([]uint32, len(layer.Data))
	copy(next, layer.Data)

	// Checked here rather than in the route, and after the bounds checks above,
	// so a stroke that is wrong in more than one way reports the most specific
	// reason. Stamping with nothing in hand is a gesture with no meaning, and
	// refusing it says so rather than quietly erasing what was there.
	if op.Kind != Erase && op.Tile.GID == 0 {
		return Result{}, errors.New("there is no tile in hand to paint with")
	}

	raw := op.Tile.Raw()
	if op.Kind == Erase {
		raw = 0
	}

	changed := false
	for _, c := range cells(op) {
		i := c.Y*layer.Width + c.X
		if i < 0 || i >= len(next) {
			// Not unreachable, though both ends are checked above: a layer
			// off disk can declare a size larger than its data, and a cell
			// inside the declared bounds is then still past the end. The
			// alternative to this check is a panic in the middle of a save.
			return Result{}, errOutside
		}
		if next[i] == raw {
			continue
		}
		next[i] = raw
		changed = true
	}
	if !changed {
		return Result{Layer: op.Layer}, nil
	}
	return Result{Layer: op.Layer, Data: next, Changed: true}, nil
}

// cells is every cell a stroke touches.
//
// A rectangle for all three tools, because a stamp dragged across the map is a
// rectangle one cell tall or wide and a click is a rectangle of one cell. What
// differs between the tools is what gets written, not where.
func cells(op Op) []Cell {
	x0, x1 := order(op.From.X, op.To.X)
	y0, y1 := order(op.From.Y, op.To.Y)
	out := make([]Cell, 0, (x1-x0+1)*(y1-y0+1))
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			out = append(out, Cell{X: x, Y: y})
		}
	}
	return out
}

// order puts a drag's two ends the right way round. A drag up-and-left covers
// the same cells as the drag down-and-right that spans them.
func order(a, b int) (int, int) {
	if a > b {
		return b, a
	}
	return a, b
}

func inside(l tiled.Layer, c Cell) error {
	if c.X < 0 || c.Y < 0 || c.X >= l.Width || c.Y >= l.Height {
		return fmt.Errorf("%w: it is %d wide and %d tall, and that is (%d, %d)",
			errOutside, l.Width, l.Height, c.X, c.Y)
	}
	return nil
}

// RotateCW turns a tile a quarter turn clockwise.
//
// Tiled encodes orientation as three flags rather than an angle, and the four
// quarter turns are a cycle through them: D+H, H+V, D+V, none. Written as the
// cycle rather than derived, because the derivation is a transposition followed
// by a mirror and reads as neither.
//
// RotatedHex is carried through untouched. It is a different axis of rotation
// for a different grid, and folding it into this one is how a hexagonal map
// comes back scrambled.
func RotateCW(t tiled.Tile) tiled.Tile {
	// Closed form over all eight states rather than a case per angle. The
	// four-case version this replaces handled the unmirrored cycle and sent
	// every mirrored state to upright in its default branch — so mirroring the
	// stamp and then turning it silently threw the mirror away. The eight flag
	// combinations are the eight symmetries of a square, and a quarter turn
	// permutes all of them; there is no "anything else" to fall through to.
	//
	// Derived from the renderer's own matrix, not from Tiled's documentation:
	// TestRotateCW_IsWhatTheRendererDraws composes a quarter turn with each of
	// the eight and checks the flags name the transform that results.
	t.FlipD, t.FlipH, t.FlipV = !t.FlipD, !t.FlipV, t.FlipH
	return t
}
