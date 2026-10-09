package server

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// The component and field editing actions.
//
// Every one goes through Session.Edit, so dirty tracking, validation, the
// atomic write and the conflict path all stay in one place. A mode that reached
// for schema.Marshal or the filesystem itself would have bypassed all four —
// the rule the epic README states.
//
// None of them writes. They change the working value and the page learns on the
// stream it already holds; saving is a separate, deliberate act.

func (s *Server) registerSchemaEditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /forge/schema/version", s.sameOriginOnly(s.handleBumpVersion))
	mux.HandleFunc("POST /forge/schema/component", s.sameOriginOnly(s.handleComponentEdit))
	mux.HandleFunc("POST /forge/schema/shape", s.sameOriginOnly(s.handleShapeEdit))
	mux.HandleFunc("POST /forge/schema/behavior", s.sameOriginOnly(s.handleBehaviorEdit))
	mux.HandleFunc("POST /forge/schema/field", s.sameOriginOnly(s.handleFieldEdit))
}

// sameOriginOnly refuses a cross-site write before the handler looks at
// anything.
//
// As middleware rather than a line in each handler: the check has to happen
// before argument dispatch, or a malformed cross-origin request gets a 400
// about its arguments instead of a 403 about where it came from — which is a
// confusing answer and one bad refactor away from being no answer at all.
func (s *Server) sameOriginOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "cross-origin write refused", http.StatusForbidden)
			return
		}
		// Deferred, not sequential: net/http recovers a handler panic, so a
		// handler that mutated and then panicked would leave the change made
		// and nobody told. That used to heal on the next poll; now it would be
		// permanent.
		defer s.publish(eventTypeFor(r.URL.Path))
		next(w, r)

		// Every mutation route goes through here and nothing else does, which
		// is why the push lives here rather than in the handlers. Twenty-four
		// handlers each remembering to publish is twenty-four chances to
		// forget, and the symptom of forgetting — a control that works but
		// whose result does not appear until you touch something else — is one
		// nobody reports as a missing publish.
		//
		// Unconditionally, including when the handler refused the edit: a
		// refusal sets the problem the panel renders, so it changed the page
		// just as much as a success did.
	}
}

// edit is the shape every editing action takes: guard the origin, find the
// session, apply the change, answer 204 and let the stream carry the result.
func (s *Server) edit(w http.ResponseWriter, r *http.Request, fn func(*schema.DatabaseSchema) error) {
	// The origin check is middleware — see sameOriginOnly — so it has already
	// run by the time anything reaches here.
	sess := s.cfg.Session
	if sess == nil {
		http.Error(w, "no project is open", http.StatusConflict)
		return
	}
	if err := sess.Edit(fn); err != nil {
		// A rejected edit is a message for the user, not a server failure: it
		// means what they asked for is not a legal schema. It is recorded so
		// the mode renders the reason the moment the route publishes — a
		// control that silently does nothing teaches you it is broken.
		//
		// 204 rather than 422: the outcome travels on the stream like every
		// other change, and a 4xx here would surface in the browser console as
		// a failed request for something that is working exactly as intended.
		s.setEditProblem(err.Error())
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.setEditProblem("")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBumpVersion(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, func(d *schema.DatabaseSchema) error {
		d.SchemaVersion++
		return nil
	})
}

func (s *Server) handleComponentEdit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch {
	case q.Has("add"):
		s.edit(w, r, func(d *schema.DatabaseSchema) error { return addComponent(d, q.Get("add")) })
	case q.Has("rename"):
		from, to := q.Get("rename"), q.Get("to")
		s.edit(w, r, func(d *schema.DatabaseSchema) error {
			if err := renameComponent(d, from, to); err != nil {
				return err
			}
			// Recorded inside the edit, so it is only remembered when the
			// rename actually happened.
			s.recordRename(renameComponentKind, from, to)
			return nil
		})
	case q.Has("delete"):
		s.edit(w, r, func(d *schema.DatabaseSchema) error { return deleteComponent(d, q.Get("delete")) })
	default:
		http.Error(w, "component action needs add, rename or delete", http.StatusBadRequest)
	}
}

func (s *Server) handleShapeEdit(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("component")
	shape := signalOrQuery(r, "shape")
	s.edit(w, r, func(d *schema.DatabaseSchema) error { return setShape(d, name, shape) })
}

func (s *Server) handleBehaviorEdit(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("component")
	behavior := signalOrQuery(r, "behavior")
	s.edit(w, r, func(d *schema.DatabaseSchema) error {
		c, ok := d.Components[name]
		if !ok {
			return fmt.Errorf("no component %q", name)
		}
		c.Behavior = behavior
		d.Components[name] = c
		return nil
	})
}

func (s *Server) handleFieldEdit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	component := q.Get("component")
	switch {
	case q.Has("add"):
		s.edit(w, r, func(d *schema.DatabaseSchema) error { return addField(d, component, q.Get("add")) })
	case q.Has("rename"):
		s.edit(w, r, func(d *schema.DatabaseSchema) error {
			return renameField(d, component, q.Get("rename"), q.Get("to"))
		})
	case q.Has("retype"):
		s.edit(w, r, func(d *schema.DatabaseSchema) error {
			return retypeField(d, component, q.Get("retype"), signalOrQuery(r, "type"))
		})
	case q.Has("delete"):
		s.edit(w, r, func(d *schema.DatabaseSchema) error {
			return deleteField(d, component, q.Get("delete"))
		})
	default:
		http.Error(w, "field action needs add, rename, retype or delete", http.StatusBadRequest)
	}
}

// signalOrQuery reads a value a control may send either way. Datastar posts the
// page's signals as a JSON body, but a plain query parameter is easier to drive
// from a test and from a link, so both are accepted.
func signalOrQuery(r *http.Request, name string) string {
	if v := r.URL.Query().Get(name); v != "" {
		return v
	}
	var signals map[string]any
	if err := datastar.ReadSignals(r, &signals); err != nil {
		return ""
	}
	if v, ok := signals[name].(string); ok {
		return v
	}
	return ""
}

// ── the edits themselves, kept out of the handlers so they can be tested
// without an HTTP request ────────────────────────────────────────────────────

// identifier matches what can safely become a SQL name.
//
// The generator concatenates component and field names straight into DDL with
// no quoting — comp_ + strings.ToLower(name) for tables, the lowercased
// property name for columns — so anything that is not an identifier produces
// either a syntax error or, worse, something that parses as something else.
// The storage side has always been this way; this is the first UI that invites
// someone to type the name, so the guard belongs here.
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validComponentName enforces what the engine enforces, at the point of typing
// rather than at the point of saving.
func validComponentName(d *schema.DatabaseSchema, name string, replacing string) error {
	if name == "" {
		return fmt.Errorf("a component needs a name")
	}
	if !identifier.MatchString(name) {
		return fmt.Errorf(
			"%q cannot name a component: names become SQL tables, so they need a letter or underscore followed by letters, digits or underscores", name)
	}
	// The engine's own rule, reused rather than restated.
	if strings.EqualFold(name, "Behavior") {
		return fmt.Errorf("%q is a reserved component name", name)
	}
	for existing := range d.Components {
		if existing == replacing {
			continue
		}
		if existing == name {
			return fmt.Errorf("a component named %q already exists", name)
		}
		// Table names are comp_ + strings.ToLower(name), so Health and health
		// are distinct JSON keys and the same table — the second CREATE TABLE
		// IF NOT EXISTS silently does nothing and they share one. The same
		// hazard validFieldName guards for columns.
		if strings.EqualFold(existing, name) {
			return fmt.Errorf(
				"%q collides with %q: component names become lowercased SQL tables", name, existing)
		}
	}
	return nil
}

func addComponent(d *schema.DatabaseSchema, name string) error {
	name = uniqueName(name, func(n string) bool { _, ok := d.Components[n]; return ok })
	if err := validComponentName(d, name, ""); err != nil {
		return err
	}
	if d.Components == nil {
		d.Components = map[string]schema.Component{}
	}
	// A new component starts as an object with one field: an object with no
	// properties fails validation, so an empty one could never be saved.
	d.Components[name] = schema.Component{
		Type:          schema.ComponentTypeObject,
		Properties:    map[string]schema.Property{"value": {Type: schema.PropertyTypeNumber}},
		PropertyOrder: []string{"value"},
	}
	// Appended to the recorded order, so it lands where jsonorder.Apply says it
	// will rather than being sorted in among the author's arrangement.
	d.ComponentOrder = append(d.ComponentOrder, name)
	return nil
}

func renameComponent(d *schema.DatabaseSchema, from, to string) error {
	if from == to {
		return nil
	}
	comp, ok := d.Components[from]
	if !ok {
		return fmt.Errorf("no component %q", from)
	}
	if err := validComponentName(d, to, from); err != nil {
		return err
	}
	delete(d.Components, from)
	d.Components[to] = comp
	// Renamed in place in the order, so the component keeps its position rather
	// than jumping to the end.
	for i, n := range d.ComponentOrder {
		if n == from {
			d.ComponentOrder[i] = to
		}
	}
	// And every entity type that referenced it, or the schema stops validating
	// the moment it is renamed.
	for name, et := range d.EntityTypes {
		et.RequiredComponents = replaceIn(et.RequiredComponents, from, to)
		et.OptionalComponents = replaceIn(et.OptionalComponents, from, to)
		d.EntityTypes[name] = et
	}
	return nil
}

func deleteComponent(d *schema.DatabaseSchema, name string) error {
	if _, ok := d.Components[name]; !ok {
		return fmt.Errorf("no component %q", name)
	}
	if len(d.Components) == 1 {
		return fmt.Errorf(
			"%q is the last component, and a schema with none cannot be saved", name)
	}
	delete(d.Components, name)
	// Left in ComponentOrder deliberately: jsonorder.Apply skips recorded keys
	// that are gone, so a component deleted and re-added keeps its old place.
	//
	// typeName, not name: ranging over EntityTypes with `name` shadowed the
	// component being deleted, so this stripped each entity type's own name
	// from its own component lists and left the dangling reference behind.
	for typeName, et := range d.EntityTypes {
		et.RequiredComponents = removeFrom(et.RequiredComponents, name)
		et.OptionalComponents = removeFrom(et.OptionalComponents, name)
		d.EntityTypes[typeName] = et
	}
	return nil
}

func setShape(d *schema.DatabaseSchema, name, shape string) error {
	comp, ok := d.Components[name]
	if !ok {
		return fmt.Errorf("no component %q", name)
	}
	if !validShape(shape) {
		return fmt.Errorf("%q is not a shape the engine supports", shape)
	}
	comp.Type = shape
	switch shape {
	case schema.ComponentTypeObject:
		comp.Items = nil
		if len(comp.Properties) == 0 {
			comp.Properties = map[string]schema.Property{"value": {Type: schema.PropertyTypeNumber}}
			comp.PropertyOrder = []string{"value"}
		}
	case schema.ComponentTypeArray:
		// Cleared, not kept. An earlier version left the properties in place on
		// the theory that Marshal omits them for a non-object shape — it does
		// not, and deliberately so: writeComponent emits properties and items
		// independently because a component carrying both must round-trip.
		// Keeping them therefore wrote dead properties into the user's file
		// beside the items, permanently.
		comp.Properties, comp.PropertyOrder = nil, nil
		if comp.Items == nil {
			comp.Items = &schema.Property{Type: schema.PropertyTypeString}
		}
	default:
		// A scalar or entity-ref component becomes a single value column and
		// has neither.
		comp.Properties, comp.PropertyOrder = nil, nil
		comp.Items = nil
	}
	d.Components[name] = comp
	return nil
}

func validShape(shape string) bool {
	for _, s := range []string{
		schema.ComponentTypeObject, schema.ComponentTypeEntityRef, schema.ComponentTypeArray,
		schema.ComponentTypeString, schema.ComponentTypeInteger,
		schema.ComponentTypeNumber, schema.ComponentTypeBoolean,
	} {
		if shape == s {
			return true
		}
	}
	return false
}

// validFieldName rejects what the generator cannot express.
//
// Column names are lowercased (strings.ToLower in componentTableBuilder), so
// maxHp and maxhp are distinct JSON keys and the same SQL column. Catching it
// here beats a confusing duplicate-column error out of SQLite later.
func validFieldName(comp schema.Component, name, replacing string) error {
	if name == "" {
		return fmt.Errorf("a field needs a name")
	}
	if !identifier.MatchString(name) {
		return fmt.Errorf(
			"%q cannot name a field: names become SQL columns, so they need a letter or underscore followed by letters, digits or underscores", name)
	}
	for existing := range comp.Properties {
		if existing == replacing {
			continue
		}
		if existing == name {
			return fmt.Errorf("a field named %q already exists", name)
		}
		if strings.EqualFold(existing, name) {
			return fmt.Errorf(
				"%q collides with %q: field names become lowercased SQL columns", name, existing)
		}
	}
	return nil
}

func addField(d *schema.DatabaseSchema, component, name string) error {
	comp, ok := d.Components[component]
	if !ok {
		return fmt.Errorf("no component %q", component)
	}
	// Uniquified against exact duplicates only, so clicking "add" twice yields
	// two fields. A case collision is a different thing and must be reported:
	// treating it as "taken" would silently rename the user's HP to HP2 rather
	// than telling them it collides with hp.
	name = uniqueName(name, func(n string) bool {
		_, exists := comp.Properties[n]
		return exists
	})
	if err := validFieldName(comp, name, ""); err != nil {
		return err
	}
	if comp.Properties == nil {
		comp.Properties = map[string]schema.Property{}
	}
	comp.Properties[name] = schema.Property{Type: schema.PropertyTypeNumber}
	comp.PropertyOrder = append(comp.PropertyOrder, name)
	d.Components[component] = comp
	return nil
}

func renameField(d *schema.DatabaseSchema, component, from, to string) error {
	if from == to {
		return nil
	}
	comp, ok := d.Components[component]
	if !ok {
		return fmt.Errorf("no component %q", component)
	}
	prop, ok := comp.Properties[from]
	if !ok {
		return fmt.Errorf("no field %q on %q", from, component)
	}
	if err := validFieldName(comp, to, from); err != nil {
		return err
	}
	delete(comp.Properties, from)
	comp.Properties[to] = prop
	for i, n := range comp.PropertyOrder {
		if n == from {
			comp.PropertyOrder[i] = to
		}
	}
	d.Components[component] = comp
	return nil
}

func retypeField(d *schema.DatabaseSchema, component, name, typ string) error {
	comp, ok := d.Components[component]
	if !ok {
		return fmt.Errorf("no component %q", component)
	}
	prop, ok := comp.Properties[name]
	if !ok {
		return fmt.Errorf("no field %q on %q", name, component)
	}
	prop.Type = typ
	// An object or array property needs its own shape filled in, or the schema
	// stops validating the moment the type is chosen.
	switch typ {
	case schema.PropertyTypeObject:
		if len(prop.Properties) == 0 {
			prop.Properties = map[string]schema.Property{"value": {Type: schema.PropertyTypeNumber}}
			prop.PropertyOrder = []string{"value"}
		}
	case schema.PropertyTypeArray:
		if prop.Items == nil {
			prop.Items = &schema.Property{Type: schema.PropertyTypeString}
		}
	}
	if err := prop.Validate(); err != nil {
		return fmt.Errorf("field %q: %w", name, err)
	}
	comp.Properties[name] = prop
	d.Components[component] = comp
	return nil
}

func deleteField(d *schema.DatabaseSchema, component, name string) error {
	comp, ok := d.Components[component]
	if !ok {
		return fmt.Errorf("no component %q", component)
	}
	if _, ok := comp.Properties[name]; !ok {
		return fmt.Errorf("no field %q on %q", name, component)
	}
	if len(comp.Properties) == 1 && comp.Type == schema.ComponentTypeObject {
		return fmt.Errorf(
			"%q is the last field on %q, and an object component with no fields cannot be saved", name, component)
	}
	delete(comp.Properties, name)
	d.Components[component] = comp
	return nil
}

// uniqueName appends a number until the name is free, so clicking "add" twice
// produces two components rather than one error.
func uniqueName(base string, taken func(string) bool) string {
	if !taken(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s%d", base, i)
		if !taken(candidate) {
			return candidate
		}
	}
}

func replaceIn(list []string, from, to string) []string {
	for i, v := range list {
		if v == from {
			list[i] = to
		}
	}
	return list
}

func (s *Server) setEditProblem(msg string) { s.setEditProblemOn("", msg) }

// setEditProblemOn records the refusal and which field it was about, together —
// one write, because a field left over from an earlier refusal would make the
// next one point at the wrong control.
func (s *Server) setEditProblemOn(field, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.editProblem = msg
	s.editProblemField = field
	s.editProblemFile, s.editProblemTile = "", ""
}

func (s *Server) setTilesetProblemOn(field, file, tile, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.editProblem, s.editProblemField = msg, field
	s.editProblemFile, s.editProblemTile = file, tile
}

func (s *Server) lastEditProblemTarget() (msg, field, file, tile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.editProblem, s.editProblemField, s.editProblemFile, s.editProblemTile
}

// lastEditProblem is why the last edit was refused and which control it was
// about, together — the pair setEditProblemOn writes under one lock, read back
// under one lock. Two acquisitions could pair one refusal's message with the
// next one's field, which is the same defect the write side avoids.
func (s *Server) lastEditProblem() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.editProblem, s.editProblemField
}
