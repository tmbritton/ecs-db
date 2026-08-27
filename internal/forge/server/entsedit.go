package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// The entity-type editing actions.
//
// The same shape as the component ones next door, deliberately: both halves
// edit one file through one session, and two idioms for that would be one too
// many. Nothing here writes — the working value changes, the page learns on the
// stream it already holds, and saving stays a separate act.

func (s *Server) registerEntsEditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /forge/ents/type", s.sameOriginOnly(s.handleTypeEdit))
	mux.HandleFunc("POST /forge/ents/behavior", s.sameOriginOnly(s.handleTypeBehaviorEdit))
	mux.HandleFunc("POST /forge/ents/validation", s.sameOriginOnly(s.handleValidationEdit))
	mux.HandleFunc("POST /forge/ents/extras", s.sameOriginOnly(s.handleExtrasEdit))
	mux.HandleFunc("POST /forge/ents/component", s.sameOriginOnly(s.handleTypeComponentEdit))
}

func (s *Server) handleTypeEdit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch {
	case q.Has("add"):
		s.edit(w, r, func(d *schema.DatabaseSchema) error { return addEntityType(d, q.Get("add")) })
	case q.Has("rename"):
		from, to := q.Get("rename"), q.Get("to")
		s.edit(w, r, func(d *schema.DatabaseSchema) error {
			if err := renameEntityType(d, from, to); err != nil {
				return err
			}
			s.recordRename(renameTypeKind, from, to)
			return nil
		})
	case q.Has("delete"):
		s.edit(w, r, func(d *schema.DatabaseSchema) error { return deleteEntityType(d, q.Get("delete")) })
	default:
		http.Error(w, "entity type action needs add, rename or delete", http.StatusBadRequest)
	}
}

func (s *Server) handleTypeBehaviorEdit(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("type")
	behavior := signalOrQuery(r, "behavior")
	s.edit(w, r, func(d *schema.DatabaseSchema) error {
		return updateEntityType(d, name, func(et *schema.EntityType) error {
			return setBehavior(et, behavior)
		})
	})
}

func (s *Server) handleValidationEdit(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("type")
	level := signalOrQuery(r, "level")
	s.edit(w, r, func(d *schema.DatabaseSchema) error {
		return updateEntityType(d, name, func(et *schema.EntityType) error {
			return setValidationLevel(et, level)
		})
	})
}

// handleExtrasEdit toggles rather than taking a value.
//
// A checkbox posts nothing when it is cleared — there is no signal bound to it,
// for the same reason the fields table binds none — so the server flips what it
// holds. The page renders from the session either way, so the two cannot drift.
func (s *Server) handleExtrasEdit(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("type")
	s.edit(w, r, func(d *schema.DatabaseSchema) error {
		return updateEntityType(d, name, func(et *schema.EntityType) error {
			et.AllowExtraComponents = !et.AllowExtraComponents
			return nil
		})
	})
}

func (s *Server) handleTypeComponentEdit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := q.Get("type")
	// Dispatched on the query alone. valueAction puts the chosen value in the
	// query string, so that is where it arrives; asking for a signal as well
	// would be worse than redundant, because datastar.ReadSignals drains the
	// request body — a second read gets nothing, so any branch entered by a
	// signal check would then read the value back as empty and answer 204
	// having changed nothing.
	switch {
	case q.Has("require"):
		s.editComponentOn(w, r, name, q.Get("require"), attachRequired)
	case q.Has("optional"):
		s.editComponentOn(w, r, name, q.Get("optional"), attachOptional)
	case q.Has("detach"):
		s.editComponentOn(w, r, name, q.Get("detach"), detachComponent)
	case q.Has("promote"):
		s.editComponentOn(w, r, name, q.Get("promote"), attachRequired)
	case q.Has("demote"):
		s.editComponentOn(w, r, name, q.Get("demote"), attachOptional)
	default:
		http.Error(w, "component action needs require, optional, detach, promote or demote",
			http.StatusBadRequest)
	}
}

func (s *Server) editComponentOn(
	w http.ResponseWriter, r *http.Request,
	typeName, component string,
	fn func(*schema.DatabaseSchema, *schema.EntityType, string) error,
) {
	s.edit(w, r, func(d *schema.DatabaseSchema) error {
		// The add dropdowns start on a placeholder, and choosing it is not a
		// choice. Answering with an error would put a message on screen for
		// someone who did nothing.
		if component == "" {
			return nil
		}
		return updateEntityType(d, typeName, func(et *schema.EntityType) error {
			return fn(d, et, component)
		})
	})
}

// ── the edits themselves ─────────────────────────────────────────────────────

// updateEntityType applies a change to one type and writes it back.
//
// EntityType is a value in a map, so a caller that took a copy, changed it and
// forgot to assign it back would have edited nothing at all — silently, which
// is the failure this exists to make impossible.
func updateEntityType(d *schema.DatabaseSchema, name string, fn func(*schema.EntityType) error) error {
	et, ok := d.EntityTypes[name]
	if !ok {
		return fmt.Errorf("no entity type %q", name)
	}
	if err := fn(&et); err != nil {
		return err
	}
	d.EntityTypes[name] = et
	return nil
}

// validEntityTypeName enforces what the engine enforces, at the point of typing.
//
// Type names are not SQL identifiers — they are stored as a value in
// entities.entity_type — so the rule is weaker than a component's. What still
// matters is that two types cannot share a name.
func validEntityTypeName(d *schema.DatabaseSchema, name, replacing string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("an entity type needs a name")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("%q has leading or trailing spaces", name)
	}
	for existing := range d.EntityTypes {
		if existing == replacing {
			continue
		}
		if existing == name {
			return fmt.Errorf("an entity type named %q already exists", name)
		}
	}
	return nil
}

func addEntityType(d *schema.DatabaseSchema, name string) error {
	name = uniqueName(name, func(n string) bool { _, ok := d.EntityTypes[n]; return ok })
	if err := validEntityTypeName(d, name, ""); err != nil {
		return err
	}
	if d.EntityTypes == nil {
		d.EntityTypes = map[string]schema.EntityType{}
	}
	// Non-nil slices, not nil ones: EntityType has omitempty only on Behavior,
	// so a nil requiredComponents serialises as `null` where the format says
	// `[]`. This is the mode that produces a type with neither.
	d.EntityTypes[name] = schema.EntityType{
		RequiredComponents: []string{},
		OptionalComponents: []string{},
		ValidationLevel:    schema.ValidationStrict,
	}
	d.EntityTypeOrder = append(d.EntityTypeOrder, name)
	return nil
}

func renameEntityType(d *schema.DatabaseSchema, from, to string) error {
	et, ok := d.EntityTypes[from]
	if !ok {
		return fmt.Errorf("no entity type %q", from)
	}
	if from == to {
		return nil
	}
	if err := validEntityTypeName(d, to, from); err != nil {
		return err
	}
	delete(d.EntityTypes, from)
	d.EntityTypes[to] = et
	d.EntityTypeOrder = replaceIn(d.EntityTypeOrder, from, to)
	return nil
}

func deleteEntityType(d *schema.DatabaseSchema, name string) error {
	if _, ok := d.EntityTypes[name]; !ok {
		return fmt.Errorf("no entity type %q", name)
	}
	// The engine requires at least one. Refused here rather than at save, so
	// the reason appears beside the button that caused it instead of arriving
	// later as a failed save — the same guard deleteComponent makes.
	if len(d.EntityTypes) == 1 {
		return fmt.Errorf(
			"%q is the last entity type, and a schema with none cannot be saved", name)
	}
	delete(d.EntityTypes, name)
	// Left in EntityTypeOrder deliberately, as deleteComponent leaves the
	// component: jsonorder.Apply skips recorded keys that are gone, so a type
	// deleted and re-added keeps its place instead of moving to the end.
	return nil
}

// setBehavior binds a machine by id.
//
// Whether the id resolves is deliberately not checked: a type may name a
// machine that no longer exists, and refusing it here would silently discard a
// binding the user did not ask to change. schema.ValidateBehaviorRefs is what
// reports it, in Story 7.
//
// What is checked is the shape of the id, because the value becomes a file
// path when the engine goes looking for it. The rule is the engine's own, from
// ValidateBehaviorRefs.
func setBehavior(et *schema.EntityType, behavior string) error {
	if strings.ContainsAny(behavior, `/\`) || behavior == ".." {
		return fmt.Errorf("%q cannot name a machine: an id is a name, not a path", behavior)
	}
	et.Behavior = behavior
	return nil
}

func setValidationLevel(et *schema.EntityType, level string) error {
	for _, l := range []schema.ValidationLevel{schema.ValidationStrict, schema.ValidationWarning} {
		if string(l) == level {
			et.ValidationLevel = l
			return nil
		}
	}
	return fmt.Errorf("%q is not a validation level the engine knows", level)
}

// attachRequired puts a component in the required list, taking it out of the
// optional one if it is there — a component cannot be both, and the engine's
// validator treats that as a contradiction rather than a preference.
func attachRequired(d *schema.DatabaseSchema, et *schema.EntityType, component string) error {
	if err := componentExists(d, component); err != nil {
		return err
	}
	et.OptionalComponents = removeFrom(et.OptionalComponents, component)
	if !contains(et.RequiredComponents, component) {
		et.RequiredComponents = append(et.RequiredComponents, component)
	}
	return nil
}

func attachOptional(d *schema.DatabaseSchema, et *schema.EntityType, component string) error {
	if err := componentExists(d, component); err != nil {
		return err
	}
	et.RequiredComponents = removeFrom(et.RequiredComponents, component)
	if !contains(et.OptionalComponents, component) {
		et.OptionalComponents = append(et.OptionalComponents, component)
	}
	return nil
}

// detachComponent removes an optional component. Required ones are not
// detachable — the lock on the chip is the contract, and demoting to optional
// is the way out.
func detachComponent(_ *schema.DatabaseSchema, et *schema.EntityType, component string) error {
	if contains(et.RequiredComponents, component) {
		return fmt.Errorf(
			"%q is required by this type; make it optional first if you want to remove it", component)
	}
	if !contains(et.OptionalComponents, component) {
		return fmt.Errorf("%q is not on this type", component)
	}
	et.OptionalComponents = removeFrom(et.OptionalComponents, component)
	return nil
}

func componentExists(d *schema.DatabaseSchema, component string) error {
	if _, ok := d.Components[component]; !ok {
		return fmt.Errorf("no component %q — an entity type can only name components the schema declares", component)
	}
	return nil
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// removeFrom returns the list without one entry, preserving order.
//
// It allocates rather than filtering in place. The previous version wrote back
// through `list[:0]`, which shares the caller's backing array — so a caller
// holding the original saw it rewritten underneath — and returned nil for a nil
// input, which is how `optionalComponents` would have serialised as `null`
// instead of `[]`.
func removeFrom(list []string, drop string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v != drop {
			out = append(out, v)
		}
	}
	return out
}
