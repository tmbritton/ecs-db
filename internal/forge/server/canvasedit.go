package server

// The canvas's edits: move, add, rename, delete, connect, disconnect,
// set-initial, and the right-click menu that offers most of them.
//
// Each one answers 204 and lets the page stream redraw, exactly as every other
// machine edit does. The change arrives on the stream rather than in the
// response — one place that renders the mode, rather than a renderer in every
// handler — and the route publishes on its way out, so "on the stream" means
// within a few milliseconds rather than whenever a timer next came round.

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/chart"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
)

func (s *Server) registerCanvasRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /forge/agents/state", s.sameOriginOnly(s.handleStateEdit))
	mux.HandleFunc("POST /forge/agents/transition", s.sameOriginOnly(s.handleTransitionEdit))
	mux.HandleFunc("POST /forge/agents/action", s.sameOriginOnly(s.handleActionEdit))
	mux.HandleFunc("POST /forge/agents/menu", s.sameOriginOnly(s.handleCanvasMenu))
	mux.HandleFunc("POST /forge/agents/select", s.sameOriginOnly(s.handleSelect))
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

// handleTransitionEdit dispatches on a named operation rather than on which
// parameter is present, which is what the state, schema, ents and machine
// editors do.
//
// Deliberately different, for a reason the others do not have: two of these
// write an *empty* value on purpose. Clearing a target makes a transition
// internal — it runs its actions and changes no state — and clearing a guard
// removes the cond. A dispatch keyed on "which parameter is non-empty" cannot
// tell either of those from a parameter nobody sent.
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

	// connect is the canvas's, not the panel's, and names two states rather
	// than a transition — so it is answered before the ref is parsed.
	if q.Get("op") == "connect" {
		if _, err := sess.AddTransition(path, q.Get("from"), q.Get("to")); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
		s.transitionEdited(w)
		return
	}

	ref, err := transitionRef(q)
	if err != nil {
		s.refuseMachineEdit(w, r, err)
		return
	}
	value := q.Get("value")
	// moved is set by the two operations that relocate the transition, and is
	// where this page's selection has to follow it to.
	var moved *machines.TransitionRef
	// cleared is a delete: the transition that was selected is gone, and every
	// sibling after it moved down one — so the selection cannot stay where it
	// is either. It names nothing rather than naming the survivor that took the
	// index, which a second Delete would then remove.
	cleared := false
	switch op := q.Get("op"); op {
	case "delete":
		err = sess.DeleteTransition(path, ref)
		cleared = true
	case "event":
		var to machines.TransitionRef
		to, err = sess.SetTransitionEvent(path, ref, value)
		moved = &to
	case "target":
		err = sess.SetTransitionTarget(path, ref, value)
	case "guard":
		err = sess.SetTransitionGuard(path, ref, value)
	case "guardparam":
		err = sess.SetGuardParam(path, ref, q.Get("param"), value)
	case "addaction":
		err = sess.AddTransitionAction(path, ref, value)
	case "removeaction":
		var index int
		if index, err = actionIndex(q); err == nil {
			err = sess.RemoveTransitionAction(path, ref, index)
		}
	case "actionparam":
		var index int
		if index, err = actionIndex(q); err == nil {
			err = sess.SetTransitionActionParam(path, ref, index, q.Get("param"), value)
		}
	case "move":
		var by int
		if by, err = strconv.Atoi(q.Get("by")); err != nil {
			err = fmt.Errorf("how far to move: %w", err)
			break
		}
		var to machines.TransitionRef
		to, err = sess.MoveTransition(path, ref, by)
		moved = &to
	default:
		err = fmt.Errorf("%q is not something a transition can do", op)
	}
	if err != nil {
		s.refuseMachineEditOn(w, r, transitionField(q), err)
		return
	}
	switch {
	case cleared:
		s.selectOnThisPage(r, "")
		s.transitionEdited(w)
	case moved != nil && *moved != ref:
		// The chart's edge id is positional, so renaming an event or reordering
		// moves the thing that is selected — and only the session knows where
		// it went, because moving onto an event that already exists appends.
		// Selection is the page's own record, so following it is a patch.
		//
		// Only when it actually moved: renaming an event to the name it already
		// has is a no-op, and moving the selection for it would be a change
		// pushed to every open page for nothing.
		s.selectOnThisPage(r,
			chart.SelEdge+chart.EdgeID(moved.From, moved.Kind, moved.Event, moved.Index))
		s.transitionEdited(w)
	default:
		s.transitionEdited(w)
	}
}

func (s *Server) transitionEdited(w http.ResponseWriter) {
	s.setEditProblem("")
	s.closeCanvasMenu()
	w.WriteHeader(http.StatusNoContent)
}

// transitionRef is which transition an op is about, from the four fields that
// address one.
func transitionRef(q url.Values) (machines.TransitionRef, error) {
	index, err := strconv.Atoi(q.Get("index"))
	if err != nil {
		return machines.TransitionRef{}, fmt.Errorf("which transition: %w", err)
	}
	return machines.TransitionRef{
		From: q.Get("from"), Kind: q.Get("kind"), Event: q.Get("event"), Index: index,
	}, nil
}

func actionIndex(q url.Values) (int, error) {
	index, err := strconv.Atoi(q.Get("action"))
	if err != nil {
		return 0, fmt.Errorf("which action: %w", err)
	}
	return index, nil
}

// transitionField is the panel field an op's refusal belongs against, so a
// duration the engine cannot read is reported where it was typed rather than
// only in the banner at the top of the page.
//
// Three of the ten ops, and empty for the rest. A refused delete or move is
// about the transition rather than about one control, and a refused *parameter*
// goes to the banner exactly as every action parameter's does — the generated
// form has one problem slot per parameter and it carries the registry's
// "required" warning, not the last refusal. Naming a field nothing renders
// would be a claim with nothing behind it.
func transitionField(q url.Values) string {
	switch q.Get("op") {
	case "event":
		return "event"
	case "target":
		return "target"
	case "guard":
		return "guard"
	default:
		return ""
	}
}

// handleSelect records what this page has selected. The chart and the
// inspectors follow on the stream, like every other change.
//
// A route rather than a signal, because the inspector's *content* depends on
// the selection and only the server can render that; a route rather than a link,
// because selecting a node in an editor should not reload the page. See
// pageStates.
func (s *Server) handleSelect(w http.ResponseWriter, r *http.Request) {
	s.setPageSel(pageIDOf(r), r.URL.Query().Get("sel"))
	s.closeCanvasMenu()
	w.WriteHeader(http.StatusNoContent)
}

// selectOnThisPage moves this page's selection, for an edit that moved the
// thing that was selected.
//
// It used to be a redirect: selection lived in the URL, so following it meant
// navigating, and deleting a transition or renaming an event reloaded the whole
// editor. Now the selection is the page's own record and the chart arrives on
// the stream, so the same edit is a patch.
func (s *Server) selectOnThisPage(r *http.Request, sel string) {
	s.setEditProblem("")
	s.closeCanvasMenu()
	s.setPageSel(pageIDOf(r), sel)
}

// handleActionEdit adds, removes and fills in a state's entry and exit actions.
func (s *Server) handleActionEdit(w http.ResponseWriter, r *http.Request) {
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
	state, kind := q.Get("state"), q.Get("kind")

	switch {
	case q.Get("add") != "":
		if err := sess.AddAction(path, state, kind, q.Get("add")); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	case q.Get("remove") != "":
		index, err := strconv.Atoi(q.Get("remove"))
		if err != nil {
			s.refuseMachineEdit(w, r, fmt.Errorf("which action: %w", err))
			return
		}
		if err := sess.RemoveAction(path, state, kind, index); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	case q.Get("param") != "":
		index, err := strconv.Atoi(q.Get("index"))
		if err != nil {
			s.refuseMachineEdit(w, r, fmt.Errorf("which action: %w", err))
			return
		}
		if err := sess.SetActionParam(path, state, kind, index, q.Get("param"), q.Get("value")); err != nil {
			s.refuseMachineEdit(w, r, err)
			return
		}
	default:
		s.refuseMachineEdit(w, r, fmt.Errorf("no action operation named"))
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

// selectedState is the state the canvas selection names, or nil when the
// selection is an edge, is nothing, or names a state that is no longer there.
//
// Resolved off the same definition the chart was built from, so the inspector
// and the canvas cannot be describing different machines.
//
// StateAt is what refuses everything that is not a state, including an edge
// selection: trimming the "state:" prefix off "edge:idle|on|GO|0" leaves it
// unchanged, and no state has a path like that. An explicit prefix check was
// here as well and no mutation could reach it — a second guard over the first
// one's answer reads as a safety net and is not one.
func selectedState(def *agent.MachineDefinition, selection string) *agent.StateNode {
	if def == nil {
		return nil
	}
	node, err := machines.StateAt(def, strings.TrimPrefix(selection, chart.SelState))
	if err != nil {
		return nil
	}
	return node
}
