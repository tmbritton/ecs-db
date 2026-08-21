package modes

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

func renderMode(t *testing.T, data Data) string {
	t.Helper()
	var buf bytes.Buffer
	if err := SchemaMode(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// Deliberately non-alphabetical: sorting the list is the most natural-looking
// wrong thing to do here, and it silently undoes Epic 11 Story 2.
func modeFixture() Data {
	return Data{
		HasSession: true,
		Machines:   []string{"wander", "chase"},
		Schema: schema.DatabaseSchema{
			SchemaVersion: 3,
			Components: map[string]schema.Component{
				"Position": {
					Type:          schema.ComponentTypeObject,
					Properties:    map[string]schema.Property{"x": {Type: "number"}, "y": {Type: "number"}},
					PropertyOrder: []string{"y", "x"},
				},
				"Health": {
					Type:          schema.ComponentTypeObject,
					Behavior:      "wander",
					Properties:    map[string]schema.Property{"hp": {Type: "integer"}},
					PropertyOrder: []string{"hp"},
				},
				"Anchor": {Type: schema.ComponentTypeEntityRef},
			},
			ComponentOrder: []string{"Position", "Health", "Anchor"},
			EntityTypes: map[string]schema.EntityType{
				"Player": {RequiredComponents: []string{"Position"}, ValidationLevel: schema.ValidationStrict},
				"Goblin": {OptionalComponents: []string{"Position"}, ValidationLevel: schema.ValidationStrict},
			},
			EntityTypeOrder: []string{"Player", "Goblin"},
		},
	}
}

func TestSchemaMode_RendersComponentsInAuthoredOrder(t *testing.T) {
	got := renderMode(t, modeFixture())
	if i, j := strings.Index(got, "component-Position"), strings.Index(got, "component-Health"); i > j {
		t.Error("components were sorted alphabetically; Position must come first")
	}
	if i, j := strings.Index(got, "component-Health"), strings.Index(got, "component-Anchor"); i > j {
		t.Error("components are not in authored order")
	}
}

func TestSchemaMode_RendersFieldsInAuthoredOrder(t *testing.T) {
	got := renderMode(t, modeFixture())
	if i, j := strings.Index(got, "field-y"), strings.Index(got, "field-x"); i > j {
		t.Error("fields were sorted alphabetically; the fixture authors y before x")
	}
}

// Selection is a URL so it survives a reload and can be pointed at — mode
// switching is a full page load, so a signal would not do.
func TestSchemaMode_Selection(t *testing.T) {
	tests := []struct {
		name     string
		selected string
		want     string
	}{
		{name: "an explicit selection", selected: "Health", want: "Health"},
		{name: "no selection falls back to the first authored", selected: "", want: "Position"},
		{name: "an unknown selection falls back rather than erroring", selected: "Ghost", want: "Position"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := modeFixture()
			data.Selected = tt.selected
			got := renderMode(t, data)
			if !strings.Contains(got, `data-component="`+tt.want+`"`) {
				t.Errorf("editor is not showing %s\n%s", tt.want, got)
			}
		})
	}
}

// Every list entry is a link, so the selection is bookmarkable and the back
// button works — the same reason the mode rail is anchors.
func TestSchemaMode_ListEntriesAreLinks(t *testing.T) {
	got := renderMode(t, modeFixture())
	for _, name := range []string{"Position", "Health", "Anchor"} {
		if !strings.Contains(got, `href="/forge/schema?component=`+name+`"`) {
			t.Errorf("no link to %s", name)
		}
	}
}

// Only object components have fields; the rest become a single value column, so
// a fields table for them would describe nothing.
func TestSchemaMode_FieldsTableOnlyForObjects(t *testing.T) {
	data := modeFixture()

	data.Selected = "Position"
	if got := renderMode(t, data); !strings.Contains(got, `data-testid="fields-table"`) {
		t.Error("an object component has no fields table")
	}

	data.Selected = "Anchor"
	got := renderMode(t, data)
	if strings.Contains(got, `data-testid="fields-table"`) {
		t.Error("an entity-ref component shows a fields table")
	}
	if !strings.Contains(got, `data-testid="no-fields"`) {
		t.Error("nothing explains why there are no fields")
	}
}

// The shape control offers exactly what the engine supports. One it does not
// know would be a component nobody can load.
func TestSchemaMode_ShapeOptionsMatchTheEngine(t *testing.T) {
	got := renderMode(t, modeFixture())
	for _, shape := range ComponentShapes {
		if !strings.Contains(got, `value="`+shape+`"`) {
			t.Errorf("shape %q is not offered", shape)
		}
	}
	for _, notAShape := range []string{"map", "float", "int", "text"} {
		if strings.Contains(got, `value="`+notAShape+`"`) {
			t.Errorf("%q is offered but the engine has no such shape", notAShape)
		}
	}
}

// A component that binds a machine is marked, and the dropdown always offers a
// way to unbind.
func TestSchemaMode_BehaviorBinding(t *testing.T) {
	data := modeFixture()
	data.Selected = "Health"
	got := renderMode(t, data)

	if !strings.Contains(got, "badge-ctx") {
		t.Error("a component binding a machine is not marked in the list")
	}
	for _, machine := range data.Machines {
		if !strings.Contains(got, `value="`+machine+`"`) {
			t.Errorf("machine %q is not offered", machine)
		}
	}
	if !strings.Contains(got, ">none<") {
		t.Error("there is no way to unbind")
	}
}

// Used-by is derived from the file alone, so it is available with no database —
// which matters, because it is the fact you want before deleting something.
func TestSchemaMode_UsedBy(t *testing.T) {
	data := modeFixture()

	data.Selected = "Position"
	got := renderMode(t, data)
	for _, want := range []string{"used-by-Player", "used-by-Goblin"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s is missing from the used-by panel", want)
		}
	}

	data.Selected = "Anchor"
	got = renderMode(t, data)
	if !strings.Contains(got, `data-testid="used-by-none"`) {
		t.Error("an unused component does not say so — that is the state that makes deletion safe")
	}
}

// Deleting drops a SQL column and everything in it, with no undo.
func TestSchemaMode_DestructiveActionsConfirm(t *testing.T) {
	got := renderMode(t, modeFixture())
	for _, id := range []string{"delete-component", "delete-field-x"} {
		i := strings.Index(got, id)
		if i < 0 {
			t.Fatalf("no %s control", id)
		}
		// The confirm has to be on the same element as the action.
		window := got[max(0, i-400):min(len(got), i+400)]
		if !strings.Contains(window, "confirm(") {
			t.Errorf("%s does not confirm before destroying data", id)
		}
	}
}

// A project that failed to open leaves the mode readable rather than 500ing.
func TestSchemaMode_WithNoSession(t *testing.T) {
	got := renderMode(t, Data{})
	if strings.Contains(got, `data-testid="component-list"`) {
		t.Error("a component list rendered with no session")
	}
	if !strings.Contains(got, "No project is open") {
		t.Error("nothing explains why the mode is empty")
	}
}

// A component name can contain anything a JSON key can, and it is put into a
// URL and a JavaScript string.
func TestSchemaMode_EscapesAwkwardNames(t *testing.T) {
	data := Data{
		HasSession: true,
		Schema: schema.DatabaseSchema{
			SchemaVersion: 1,
			Components: map[string]schema.Component{
				`Odd "Name" & <thing>`: {
					Type:          schema.ComponentTypeObject,
					Properties:    map[string]schema.Property{"x": {Type: "number"}},
					PropertyOrder: []string{"x"},
				},
			},
			ComponentOrder: []string{`Odd "Name" & <thing>`},
			EntityTypes:    map[string]schema.EntityType{},
		},
	}
	got := renderMode(t, data)

	// A raw quote inside an attribute would end it early and break the page.
	if strings.Contains(got, `href="/forge/schema?component=Odd "`) {
		t.Errorf("an unescaped quote reached an href:\n%s", got)
	}
	if strings.Contains(got, "<thing>") {
		t.Errorf("an unescaped angle bracket reached the markup:\n%s", got)
	}
}

// Datastar's bind plugin seeds a signal from the first element bound to a path
// and then drives every other element bound to it. A fields table has one type
// dropdown per row, so a shared signal name makes every row display the first
// row's type — a false picture of the schema about to be saved.
func TestSchemaMode_FieldDropdownsDoNotShareASignal(t *testing.T) {
	data := modeFixture()
	data.Schema.Components["Position"] = schema.Component{
		Type: schema.ComponentTypeObject,
		Properties: map[string]schema.Property{
			"x": {Type: "number"},
			"y": {Type: "string"},
		},
		PropertyOrder: []string{"x", "y"},
	}
	data.Selected = "Position"
	got := renderMode(t, data)

	if strings.Contains(got, `data-bind="type"`) {
		t.Errorf("the per-row type dropdowns share one signal:\n%s", got)
	}
	// And each row still shows its own type.
	for _, want := range []string{`value="number" selected`, `value="string" selected`} {
		if !strings.Contains(got, want) {
			t.Errorf("a row is not showing its own type (%s missing)", want)
		}
	}
}
