package modes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
	"github.com/tmbritton/ecs-db/internal/forge/usage"
	"github.com/tmbritton/ecs-db/internal/jsonorder"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

// ComponentShapes are the shapes the engine actually supports, in the order
// they are offered. Taken from internal/schema's constants rather than typed
// out again: a shape offered here that the engine does not know would be a
// component nobody can load.
var ComponentShapes = []string{
	schema.ComponentTypeObject,
	schema.ComponentTypeEntityRef,
	schema.ComponentTypeArray,
	schema.ComponentTypeString,
	schema.ComponentTypeInteger,
	schema.ComponentTypeNumber,
	schema.ComponentTypeBoolean,
}

// PropertyTypes are the field types an object component's properties may take.
var PropertyTypes = []string{
	schema.PropertyTypeString,
	schema.PropertyTypeInteger,
	schema.PropertyTypeNumber,
	schema.PropertyTypeBoolean,
	schema.PropertyTypeObject,
	schema.PropertyTypeArray,
}

// componentNames returns the components in authored order.
//
// Never sorted, and never map order. Position, Health and Sprite sit together
// in the file because they belong together, and Epic 11 Story 2 went to some
// trouble to keep it that way — sorting the list here is the most natural
// looking thing to do and would silently undo all of it.
func componentNames(s schema.DatabaseSchema) []string {
	return jsonorder.Apply(s.ComponentOrder, s.Components)
}

// ComponentNames is componentNames for callers outside this package. The
// server needs it to resolve which component a panel is showing, and resolving
// it a second way is how the count and the panel come to disagree about which
// component they are about.
func ComponentNames(s schema.DatabaseSchema) []string { return componentNames(s) }

// propertyNames returns one component's fields in authored order.
func propertyNames(c schema.Component) []string {
	return jsonorder.Apply(c.PropertyOrder, c.Properties)
}

// selectComponent resolves the URL's ?component= to one that exists.
//
// An unknown or absent name falls back to the first in authored order rather
// than erroring: a schema always has one, and a 404 for a stale bookmark or a
// just-deleted component would be unhelpful.
func selectComponent(s schema.DatabaseSchema, want string) string {
	if _, ok := s.Components[want]; ok {
		return want
	}
	if names := componentNames(s); len(names) > 0 {
		return names[0]
	}
	return ""
}

// isObject reports whether a component has fields at all. Only object
// components produce one column per property; the rest are a single value
// column, so a fields table for them would describe nothing.
func isObject(c schema.Component) bool { return c.Type == schema.ComponentTypeObject }

// usedBy lists the entity types that declare a component, required or optional.
//
// Delegated rather than reimplemented: the panel that answers "is this safe to
// delete" must give the same answer in both modes, and two implementations of
// one question is how they come to differ.
func usedBy(s schema.DatabaseSchema, component string) []string {
	return usage.UsedBy(s, component)
}

// action builds a Datastar expression posting to an editing endpoint with the
// arguments as query parameters.
//
// Query parameters rather than signals, deliberately: which component a button
// acts on is a property of the button, not of the page, and putting it in a
// signal would mean every control on the page carrying every other control's
// target.
func action(path string, args ...string) string {
	var b strings.Builder
	b.WriteString(path)
	for i := 0; i+1 < len(args); i += 2 {
		if i == 0 {
			b.WriteString("?")
		} else {
			b.WriteString("&")
		}
		fmt.Fprintf(&b, "%s=%s", args[i], urlValue(args[i+1]))
	}
	return "@post('" + b.String() + "')"
}

// urlValue escapes a value for a query string. Component names are
// user-supplied and may contain anything a JSON key may.
func urlValue(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~':
			b.WriteRune(r)
		default:
			for _, c := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		}
	}
	return b.String()
}

// valueAction posts an action carrying the value of the control that fired it.
//
// Not a signal, deliberately. Datastar actions post the page's *signals*, and a
// plain HTML name attribute contributes nothing — the lesson Epic 10 Story 4
// learned the hard way. But binding these to signals would need a unique signal
// per control, because a fields table has one rename input and one type
// dropdown per row, and a shared name would make every row write to the same
// place. `evt` is in scope in any Datastar expression, so the value comes from
// the control that changed and there is no namespace to manage.
func valueAction(path string, param string, args ...string) string {
	base := action(path, args...)
	// action() returns @post('…'); splice the value in before the closing quote.
	inner := strings.TrimSuffix(strings.TrimPrefix(base, "@post('"), "')")
	sep := "?"
	if strings.Contains(inner, "?") {
		sep = "&"
	}
	return "@post('" + inner + sep + param + "=' + encodeURIComponent(evt.target.value))"
}

// componentHref links to a component, so selection survives a reload and can be
// pointed at. Mode switching is a full page load, so a signal would not do.
func componentHref(name string) string {
	return "/forge/schema?component=" + urlValue(name)
}

// The templ files in this package cannot import templ itself — its generated
// code declares the same identifier — so the helpers it needs are aliased here,
// as templates/component.go does for the shell.
type Component = schema.Component

// EntityType is aliased for the same reason Component is: a .templ file in
// this package cannot import schema without colliding with templ's codegen.
type EntityType = schema.EntityType

// Statement is the engine's own DDL statement type; the migration panel
// renders these rather than re-deriving what a change means.
type Statement = storage.Statement

func URL(s string) templ.SafeURL { return templ.URL(s) }

func kv(class string, on bool) templ.KeyValue[string, bool] { return templ.KV(class, on) }

func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int) string { return strconv.Itoa(n) }

// confirmAction guards a destructive action behind a browser confirmation.
// Deleting a component or a field drops a SQL column and everything in it, and
// there is no undo.
func confirmAction(question, act string) string {
	return "confirm(" + jsString(question) + ") && " + act
}

// jsString quotes a string for a JavaScript expression. The text is built from
// user-supplied component and field names.
func jsString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "''"
	}
	return string(b)
}

func shapeOptions() []components.Option {
	out := make([]components.Option, 0, len(ComponentShapes))
	for _, shape := range ComponentShapes {
		out = append(out, components.Option{Value: shape, Label: shape})
	}
	return out
}

func typeOptions() []components.Option {
	out := make([]components.Option, 0, len(PropertyTypes))
	for _, t := range PropertyTypes {
		out = append(out, components.Option{Value: t, Label: t})
	}
	return out
}

// machineOptions offers the project's machines plus an explicit "none", so
// unbinding is a choice rather than the absence of one.
//
// A machine that shadows an earlier mod's is labelled with the mod that won,
// because the ID alone cannot tell you which file you are binding to.
//
// bound is the value currently on the field. When it names a machine that no
// longer exists it is offered anyway, at the end: resetting the dropdown to
// "none" would be an edit the user did not make, to a field they did not
// touch, discovered whenever they next looked. Story 7 reports it as the error
// it is.
func machineOptions(machines []project.Machine, bound string) []components.Option {
	out := []components.Option{{Value: "", Label: "none"}}
	found := false
	for _, m := range machines {
		label := m.ID
		if m.Overrides {
			label = m.ID + " (" + m.Mod + ")"
		}
		out = append(out, components.Option{Value: m.ID, Label: label})
		if m.ID == bound {
			found = true
		}
	}
	if bound != "" && !found {
		out = append(out, components.Option{Value: bound, Label: bound + " — missing"})
	}
	return out
}

// machineByID finds a resolved machine by the ID an entity type names.
func machineByID(machines []project.Machine, id string) (project.Machine, bool) {
	for _, m := range machines {
		if m.ID == id {
			return m, true
		}
	}
	return project.Machine{}, false
}

// generatedSQL returns the DDL the interpreter will run for a component.
//
// storage.MigrateComponent is the exported entry to the generator the engine
// itself uses, so the preview and the migration cannot disagree by
// construction. A second implementation here would drift, and a preview that
// drifts is worse than none because it is believed.
func generatedSQL(comp schema.Component, name string) (string, error) {
	return storage.MigrateComponent(name, comp)
}

// isArray reports whether the DDL note about JSON storage applies.
func isArray(c schema.Component) bool { return c.Type == schema.ComponentTypeArray }
