package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
	"github.com/tmbritton/ecs-db/internal/forge/validation"
	"github.com/tmbritton/ecs-db/internal/schema"
)

func TestSchemaMode_RendersAComponentsOwnProblems(t *testing.T) {
	data := modeFixture()
	data.Selected = "Health"
	data.Validation = validation.Report{
		Partial: true,
		Problems: []validation.Problem{
			{
				Owner:    validation.Owner{Kind: validation.OwnerComponent, Name: "Health"},
				Message:  "SQL compatibility: component \"Health\" uses type \"quaternion\"",
				Blocking: true,
			},
			{
				Owner:   validation.Owner{Kind: validation.OwnerComponent, Name: "Position"},
				Message: "a problem belonging to a different component",
			},
		},
	}

	got := renderMode(t, data)
	editor := section(t, got, `data-testid="component-editor"`, `data-testid="generated-sql"`)
	if !strings.Contains(editor, "quaternion") {
		t.Errorf("the selected component's problem is missing:\n%s", editor)
	}
	if strings.Contains(editor, "a problem belonging to a different component") {
		t.Errorf("another component's problem rendered in this editor:\n%s", editor)
	}
	// The note is a fact about the check rather than about this component, so
	// it renders once for the page — asserted here on the whole document, and
	// asserted as *once* by the test-id pin.
	if !strings.Contains(got, "stops at the first failure") {
		t.Errorf("a partial report must say so, or the list implies a completeness\n"+
			"it does not have:\n%s", got)
	}
	if n := strings.Count(got, "stops at the first failure"); n != 1 {
		t.Errorf("the caveat renders %d times; two elements sharing one test id is a\n"+
			"strict-mode violation in the browser suite", n)
	}
}

func TestSchemaMode_HangsABindingProblemOnTheBehaviorControl(t *testing.T) {
	data := modeFixture()
	data.Selected = "Health"
	id := components.ProblemsID("component", "Health", validation.FieldBehavior)
	data.Validation = validation.Report{Problems: []validation.Problem{{
		Owner:    validation.Owner{Kind: validation.OwnerComponent, Name: "Health"},
		Field:    validation.FieldBehavior,
		Message:  "behavior machine file not found",
		Blocking: true,
	}}}

	got := renderMode(t, data)
	if !strings.Contains(got, `id="`+id+`"`) {
		t.Fatalf("no message list with the expected id %q:\n%s", id, got)
	}
	if !strings.Contains(got, `aria-describedby="`+id+`"`) {
		t.Errorf("the behavior control does not point at its message — the message is\n"+
			"beside the field for a sighted reader and nowhere for anyone else:\n%s", got)
	}
	if !strings.Contains(got, `aria-invalid="true"`) {
		t.Errorf("a blocking problem must mark the control invalid:\n%s", got)
	}
}

func TestSchemaMode_HangsAnAmbiguityWarningOnTheFieldRow(t *testing.T) {
	data := modeFixture()
	data.Selected = "Health"
	id := components.ProblemsID("component", "Health", validation.PropertyField("hp"))
	data.Validation = validation.Report{Problems: []validation.Problem{{
		Owner:   validation.Owner{Kind: validation.OwnerComponent, Name: "Health"},
		Field:   validation.PropertyField("hp"),
		Message: "machine \"wander\" seeds context key \"hp\"",
	}}}

	got := renderMode(t, data)
	row := section(t, got, `data-testid="field-hp"`, "</tr>")
	if !strings.Contains(row, "seeds context key") {
		t.Errorf("the warning is not on the row that caused it:\n%s", row)
	}
	if !strings.Contains(row, `aria-describedby="`+id+`"`) {
		t.Errorf("the field input does not point at its warning:\n%s", row)
	}
	// A warning is not an invalid value. The engine accepts this schema.
	if strings.Contains(row, `aria-invalid="true"`) {
		t.Errorf("a warning must not mark the field invalid:\n%s", row)
	}
}

func TestSchemaMode_SaysNothingWhenNothingIsWrong(t *testing.T) {
	data := modeFixture()
	data.Selected = "Health"
	got := renderMode(t, data)
	for _, unwanted := range []string{`class="problems"`, "aria-invalid", "stops at the first failure"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("a sound schema still renders %q:\n%s", unwanted, got)
		}
	}
}

func TestEntsMode_RendersAnEntityTypesProblems(t *testing.T) {
	data := modeFixture()
	data.Selected = "Player"
	id := components.ProblemsID("type", "Player", validation.FieldBehavior)
	data.Validation = validation.Report{Problems: []validation.Problem{
		{
			Owner:    validation.Owner{Kind: validation.OwnerEntityType, Name: "Player"},
			Message:  "cross-reference: entityType \"Player\" references undeclared component \"Nope\"",
			Blocking: true,
		},
		{
			Owner:   validation.Owner{Kind: validation.OwnerEntityType, Name: "Player"},
			Field:   validation.FieldBehavior,
			Message: "the file is there, but no machine with this id loaded from it",
		},
		{
			Owner:   validation.Owner{Kind: validation.OwnerEntityType, Name: "Goblin"},
			Message: "a problem belonging to a different entity type",
		},
	}}

	got := renderEnts(t, data)
	editor := section(t, got, `data-testid="type-editor"`, `data-testid="required-block"`)
	if !strings.Contains(editor, "undeclared component") {
		t.Errorf("the type's own problem is missing:\n%s", editor)
	}
	if strings.Contains(editor, "a problem belonging to a different entity type") {
		t.Errorf("another type's problem rendered in this editor:\n%s", editor)
	}
	if !strings.Contains(editor, `aria-describedby="`+id+`"`) {
		t.Errorf("the behavior control does not point at its message:\n%s", editor)
	}
}

// A fact about the file, not about anything in it. It shows whichever tab is
// open, because which tab is open does not change it.
// The state the story exists to prevent: Save is disabled, the footer says how
// many problems there are, and the page shows none of them because they belong
// to rows nobody has clicked.
func TestBothModes_MarkTheListRowsThatCarryProblems(t *testing.T) {
	data := modeFixture()
	data.Selected = "Health" // deliberately not the one with the problem
	data.Validation = validation.Report{Problems: []validation.Problem{
		{
			Owner:    validation.Owner{Kind: validation.OwnerComponent, Name: "Anchor"},
			Message:  "SQL compatibility: component \"Anchor\" uses type \"quaternion\"",
			Blocking: true,
		},
		{
			Owner:   validation.Owner{Kind: validation.OwnerComponent, Name: "Position"},
			Field:   validation.PropertyField("x"),
			Message: "a warning about a field",
		},
	}}

	got := renderMode(t, data)
	list := section(t, got, `data-testid="component-list"`, `data-testid="schema-empty"`)

	anchor := section(t, list, `data-testid="component-Anchor"`, "</a>")
	if !strings.Contains(anchor, `data-problem="true"`) {
		t.Errorf("the row carrying the reason Save is disabled is unmarked:\n%s", anchor)
	}
	if !strings.Contains(anchor, "badge-problem--error") {
		t.Errorf("a blocking problem is marked the same as a warning:\n%s", anchor)
	}
	// A warning is marked too, but not as an error — the difference is what
	// tells someone which row is stopping the save.
	position := section(t, list, `data-testid="component-Position"`, "</a>")
	if !strings.Contains(position, `data-problem="true"`) {
		t.Errorf("a row carrying a warning is unmarked:\n%s", position)
	}
	if strings.Contains(position, "badge-problem--error") {
		t.Errorf("a warning is marked as stopping the save:\n%s", position)
	}
	// And a sound row carries nothing.
	health := section(t, list, `data-testid="component-Health"`, "</a>")
	if !strings.Contains(health, `data-problem="false"`) || strings.Contains(health, "badge-problem") {
		t.Errorf("a sound row is marked:\n%s", health)
	}
}

func TestEntsMode_MarksTheTypeRowsThatCarryProblems(t *testing.T) {
	data := modeFixture()
	data.Selected = "Player"
	data.Validation = validation.Report{Problems: []validation.Problem{{
		Owner:    validation.Owner{Kind: validation.OwnerEntityType, Name: "Goblin"},
		Message:  "cross-reference: entityType \"Goblin\" references undeclared component \"Nope\"",
		Blocking: true,
	}}}

	list := section(t, renderEnts(t, data), `data-testid="type-list"`, `data-testid="type-editor"`)
	goblin := section(t, list, `data-testid="type-Goblin"`, "</a>")
	if !strings.Contains(goblin, `data-problem="true"`) || !strings.Contains(goblin, "badge-problem--error") {
		t.Errorf("the unselected broken type is unmarked:\n%s", goblin)
	}
	player := section(t, list, `data-testid="type-Player"`, "</a>")
	if strings.Contains(player, "badge-problem") {
		t.Errorf("the sound type is marked:\n%s", player)
	}
}

// Nothing for the selected owner may be invisible. A field problem whose row is
// never drawn — a hand-edited non-object component that kept its properties —
// falls back to the top of the editor rather than being silently dropped.
func TestSchemaMode_ShowsAProblemWhoseControlIsNotOnThePage(t *testing.T) {
	data := modeFixture()
	data.Selected = "Anchor" // an entity-ref: no fields table at all
	data.Validation = validation.Report{Problems: []validation.Problem{{
		Owner:   validation.Owner{Kind: validation.OwnerComponent, Name: "Anchor"},
		Field:   validation.PropertyField("hp"),
		Message: "a warning about a field this component does not render",
	}}}

	editor := section(t, renderMode(t, data), `data-testid="component-editor"`, `data-testid="generated-sql"`)
	if !strings.Contains(editor, "does not render") {
		t.Errorf("a problem attached to a control that is not on the page vanished:\n%s", editor)
	}
}

// The counterpart: a problem that a control *does* show must not be repeated at
// the top of the editor.
func TestSchemaMode_DoesNotRepeatAPlacedProblem(t *testing.T) {
	data := modeFixture()
	data.Selected = "Health"
	data.Validation = validation.Report{Problems: []validation.Problem{{
		Owner:   validation.Owner{Kind: validation.OwnerComponent, Name: "Health"},
		Field:   validation.PropertyField("hp"),
		Message: "the ambiguity warning",
	}}}

	editor := section(t, renderMode(t, data), `data-testid="component-editor"`, `data-testid="generated-sql"`)
	if n := strings.Count(editor, "the ambiguity warning"); n != 1 {
		t.Errorf("want the message once, on the row that caused it; got %d times:\n%s", n, editor)
	}
	if strings.Contains(editor, `data-testid="component-problems"`) {
		t.Errorf("a problem the fields table shows was also hoisted to the top:\n%s", editor)
	}
}

func TestBothModes_RenderFileWideProblems(t *testing.T) {
	report := validation.Report{
		Partial:  true,
		Problems: []validation.Problem{{Message: "structure: schemaVersion: must be >= 1, got 0", Blocking: true}},
	}
	for _, tc := range []struct {
		name   string
		render func(Data) string
	}{
		{"SCHEMA", func(d Data) string { return renderMode(t, d) }},
		{"ENTS", func(d Data) string { return renderEnts(t, d) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := modeFixture()
			data.Validation = report
			got := tc.render(data)
			block := section(t, got, `data-testid="file-problems"`, "</div>")
			if !strings.Contains(block, "schemaVersion") {
				t.Errorf("the file-wide problem is missing:\n%s", got)
			}
		})
	}
}

// The one that would have shipped: a schema so broken there is nothing selected
// to hang the message on. Rendering the panel inside the editor would have made
// it invisible in exactly the state that produces it.
func TestSchemaMode_ReportsAFileWideProblemWithNothingSelected(t *testing.T) {
	data := Data{
		HasSession: true,
		Schema:     schema.DatabaseSchema{SchemaVersion: 3},
		Validation: validation.Report{
			Problems: []validation.Problem{{Message: "components: at least one component must be declared", Blocking: true}},
		},
	}
	got := renderMode(t, data)
	if !strings.Contains(got, `data-testid="schema-empty"`) {
		t.Fatalf("expected the empty state:\n%s", got)
	}
	if !strings.Contains(got, "at least one component") {
		t.Errorf("the reason there is nothing to edit is not on the page:\n%s", got)
	}
}

// The seeds panel used to send the reader to the log. It now defers to the
// behavior field, which carries the reason — so the two must not both claim to
// be the place the answer is.
func TestEntsMode_SeedsPanelDefersToTheBehaviorField(t *testing.T) {
	data := modeFixture()
	data.Selected = "Player"
	et := data.Schema.EntityTypes["Player"]
	et.Behavior = "ghost"
	data.Schema.EntityTypes["Player"] = et
	data.Machines = []project.Machine{{ID: "wander", Mod: "core"}}

	seeds := section(t, renderEnts(t, data), `data-testid="context-seeds"`, `data-testid="delete-type"`)
	if strings.Contains(seeds, "The log says which") {
		t.Errorf("the seeds panel still sends the reader to the log for something the\n"+
			"Behavior field now says on screen:\n%s", seeds)
	}
	if !strings.Contains(seeds, "Behavior field") {
		t.Errorf("the seeds panel does not say where the reason is:\n%s", seeds)
	}
}
