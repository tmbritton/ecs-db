package server

// The canvas's edits: move, add, rename, delete, connect, disconnect,
// set-initial, and the right-click menu that offers most of them.
//
// Each one answers 204 and lets the page stream redraw, exactly as every other
// machine edit does. That means a change is on screen within one tick rather
// than in the response — the cost of having one place that renders the mode
// rather than a renderer in every handler.

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
)

func (s *Server) registerCanvasRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /forge/agents/state", sameOriginOnly(s.handleStateEdit))
	mux.HandleFunc("POST /forge/agents/transition", sameOriginOnly(s.handleTransitionEdit))
	mux.HandleFunc("POST /forge/agents/menu", sameOriginOnly(s.handleCanvasMenu))
}

// handleStateEdit dispatches on which parameter is present, the same shape as
// the schema, ents and machine editors.
func (s *Server) handleStateEdit(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.machineSession(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	path, err := s.machinePath(sess, q.Get("machine"))
	if err != nil {
		s.refuseMachineEdit(w, r, err)
		return
	}

	switch {
	case q.Get("move") != "":
		// A delta, not a position. The client knows where it dragged the box to
		// on screen; only the session knows the offsets between that and the
		// coordinate the file records — and it reads and writes both under one
		// lock, which is what stops two drags arriving together losing one.
		dx, dy, err := floats(q.Get("dx"), q.Get("dy"))
		if err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		if err := sess.MoveBy(path, q.Get("move"), dx, dy); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	case q.Get("add") != "":
		x, y, err := floats(q.Get("x"), q.Get("y"))
		if err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		if _, err := sess.AddState(path, x, y); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	case q.Get("rename") != "":
		if err := sess.RenameState(path, q.Get("rename"), q.Get("to")); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	case q.Get("delete") != "":
		if err := sess.DeleteState(path, q.Get("delete")); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	case q.Get("initial") != "":
		if err := sess.SetInitial(path, q.Get("initial")); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	default:
		s.refuseMachineEdit(w, r, fmt.Errorf("no state operation named"))
		return
	}
	s.setEditProblem("")
	s.closeCanvasMenu()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTransitionEdit(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.machineSession(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	path, err := s.machinePath(sess, q.Get("machine"))
	if err != nil {
		s.refuseMachineEdit(w, r, err)
		return
	}

	switch {
	case q.Get("connect") != "":
		if _, err := sess.AddTransition(path, q.Get("connect"), q.Get("to")); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	case q.Get("delete") != "":
		// By its parts. The chart's edge id joins source, kind, event and index
		// with bars, and a state name or an event name may contain one, so
		// splitting it back apart is guesswork.
		index, err := strconv.Atoi(q.Get("index"))
		if err != nil {
			s.refuseMachineEdit(w, r, fmt.Errorf("which transition: %w", err))
			return
		}
		if err := sess.DeleteTransition(path, q.Get("delete"), q.Get("kind"), q.Get("event"), index); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	default:
		s.refuseMachineEdit(w, r, fmt.Errorf("no transition operation named"))
		return
	}
	s.setEditProblem("")
	s.closeCanvasMenu()
	w.WriteHeader(http.StatusNoContent)
}

// handleCanvasMenu opens or closes the right-click menu.
//
// Server-rendered, and held per server rather than per page — the same
// limitation the edit-problem banner has carried since Story 2, for the same
// reason: there is one session and the modes render from it. Two browsers on
// one Forge would see each other's menu.
func (s *Server) handleCanvasMenu(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("close") != "" || q.Get("target") == "" {
		s.closeCanvasMenu()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	x, y, err := floats(q.Get("x"), q.Get("y"))
	if err != nil {
		s.refuseMachineEdit(w, r, err)
		return
	}
	s.setCanvasMenu(CanvasMenu{
		Kind:     q.Get("target"),
		State:    q.Get("state"),
		Edge:     q.Get("edge"),
		Event:    q.Get("event"),
		Index:    q.Get("index"),
		EdgeKind: q.Get("kind"),
		From:     q.Get("from"),
		X:        x,
		Y:        y,
		Open:     true,
	})
	w.WriteHeader(http.StatusNoContent)
}

// floats parses a pair of coordinates, refusing anything that is not one.
//
// Both together, because a handler that took a good x and a bad y would move a
// node sideways to nowhere.
func floats(a, b string) (float64, float64, error) {
	x, err := coordinate(a)
	if err != nil {
		return 0, 0, err
	}
	y, err := coordinate(b)
	if err != nil {
		return 0, 0, err
	}
	return x, y, nil
}

// coordinate parses one, refusing the values that are numbers to ParseFloat and
// not to anything downstream: NaN and ±Inf marshal into no JSON at all, so they
// surfaced as "json: unsupported value: +Inf" in the banner from deep inside
// the writer.
func coordinate(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("a coordinate: %w", err)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("a coordinate has to be a finite number, not %q", s)
	}
	return v, nil
}

// CanvasMenu is the right-click menu the statechart has open.
//
// One type for both kinds of target, because a menu is one thing on screen and
// the alternative is two nearly-identical structs and a rule about which is
// live. Kind says which fields mean anything.
type CanvasMenu = modes.CanvasMenu

func (s *Server) setCanvasMenu(m CanvasMenu) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.canvasMenu = m
}

// closeCanvasMenu is called by every edit, not only by the close control: a
// menu left open over a machine that has just changed describes something that
// may no longer be there.
func (s *Server) closeCanvasMenu() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.canvasMenu = CanvasMenu{}
}

func (s *Server) openCanvasMenu() CanvasMenu {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.canvasMenu
}

// canvasDeleteWarning is what deleting the state a menu is about would break.
//
// Named rather than counted: "2 transitions will dangle" does not tell you
// whether the two that matter are among them. Computed here because only the
// session can answer it, and only for the menu that is open — it is a walk of
// the machine, and every render would otherwise pay for it.
func (s *Server) canvasDeleteWarning(machine string, m CanvasMenu) string {
	if !m.Open || m.Kind != "state" || s.cfg.MachineSession == nil || machine == "" {
		return ""
	}
	dangling := s.cfg.MachineSession.TransitionsTargeting(machine, m.State)
	if len(dangling) == 0 {
		return "Delete state " + m.State + "? Nothing transitions into it."
	}
	return "Delete state " + m.State + "? " + strings.Join(dangling, ", ") +
		" will be left pointing at a state that does not exist."
}
