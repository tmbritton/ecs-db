package modes

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/schema"
)

func renderEnts(t *testing.T, data Data) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Render("ents", data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// Deliberately non-alphabetical, like the component fixture: authored order is
// what the file records and what a save has to preserve.
func entsFixture() Data {
	data := modeFixture()
	data.Schema.EntityTypes = map[string]schema.EntityType{
		"Player": {
			RequiredComponents: []string{"Position"},
			OptionalComponents: []string{"Health"},
			ValidationLevel:    schema.ValidationStrict,
		},
		"Goblin": {
			Behavior:           "wander",
			RequiredComponents: []string{"Position", "Health"},
			OptionalComponents: []string{},
			ValidationLevel:    schema.ValidationWarning,
		},
	}
	data.Schema.EntityTypeOrder = []string{"Player", "Goblin"}
	data.Machines = []project.Machine{
		{ID: "wander", Mod: "core", Definition: &agent.MachineDefinition{
			ID:              "wander",
			Context:         map[string]any{"hp": float64(10), "name": "gob"},
			ContextOrder:    []string{"hp", "name"},
			ContextManifest: map[string]string{"hp": "Health", "name": "Sprite"},
		}},
		{ID: "chase", Mod: "core"},
	}
	return data
}

func TestEntsMode_ListsTypesInAuthoredOrder(t *testing.T) {
	got := renderEnts(t, entsFixture())

	player := strings.Index(got, `data-testid="type-Player"`)
	goblin := strings.Index(got, `data-testid="type-Goblin"`)
	if player < 0 || goblin < 0 {
		t.Fatalf("not every type is listed:\n%s", got)
	}
	if player > goblin {
		t.Error("the list was sorted; authored order is what the file records")
	}
}

func TestEntsMode_Selection(t *testing.T) {
	data := entsFixture()
	data.Selected = "Goblin"
	got := renderEnts(t, data)

	// The row itself, not "some row on the page is active": every row carries
	// data-active and one is always true, so a page-wide search for it passes
	// however the highlight is computed.
	row := section(t, got, `data-testid="type-Goblin"`, "</a>")
	if !strings.Contains(row, `data-active="true"`) {
		t.Errorf("the selected type's row is not marked active:\n%s", row)
	}
	other := section(t, got, `data-testid="type-Player"`, "</a>")
	if strings.Contains(other, `data-active="true"`) {
		t.Errorf("an unselected row is marked active:\n%s", other)
	}
	if !strings.Contains(got, `data-type="Goblin"`) {
		t.Error("the editor is not showing the selected type")
	}

	// An unknown selection falls back rather than 404ing a stale bookmark.
	data.Selected = "NoSuchType"
	if !strings.Contains(renderEnts(t, data), `data-type="Player"`) {
		t.Error("an unknown type did not fall back to the first")
	}
}

// Required chips carry the lock and no remove control; optional chips carry
// the remove. That distinction is the contract made visible.
func TestEntsMode_RequiredChipsCannotBeDetached(t *testing.T) {
	data := entsFixture()
	data.Selected = "Player"
	got := renderEnts(t, data)

	required := section(t, got, `data-testid="required-block"`, `data-testid="optional-block"`)
	if !strings.Contains(required, "🔒") {
		t.Error("a required component is not locked")
	}
	if strings.Contains(required, "chip__remove") {
		t.Errorf("a required component offers a remove control:\n%s", required)
	}
	// The way out is demotion, or the lock is a dead end rather than a contract.
	if !strings.Contains(required, `data-testid="demote-Position"`) {
		t.Error("a required component cannot be made optional, so it can never be removed")
	}

	optional := section(t, got, `data-testid="optional-block"`, `data-testid="context-seeds"`)
	if !strings.Contains(optional, "chip__remove") {
		t.Error("an optional component offers no way to remove it")
	}
}

// A component may not be both required and optional, so neither add control
// offers what the type already carries.
func TestEntsMode_AddControlsExcludeWhatIsAlreadyOn(t *testing.T) {
	data := entsFixture()
	data.Selected = "Player" // requires Position, optionally Health
	got := renderEnts(t, data)

	// Bounded at the optional block, or the window spans both dropdowns — which
	// offer an identical option set, so an assertion over both cannot tell them
	// apart.
	add := section(t, got, `data-testid="add-required"`, `data-testid="optional-block"`)
	for _, already := range []string{"Position", "Health"} {
		if strings.Contains(add, `value="`+already+`"`) {
			t.Errorf("%q is already on the type and is still offered", already)
		}
	}
	if !strings.Contains(add, `value="Anchor"`) {
		t.Error("a component not on the type is not offered")
	}
}

// The two add controls are identical but for the parameter their action names.
// Swapping them makes "+ require a component" attach an optional one, and every
// other test in this file and the next passes — the option sets are the same,
// and no test elsewhere connects a rendered control to a route.
func TestEntsMode_AddControlsPostToTheRightSide(t *testing.T) {
	data := entsFixture()
	data.Selected = "Player"
	got := renderEnts(t, data)

	required := section(t, got, `data-testid="add-required"`, `data-testid="optional-block"`)
	if !strings.Contains(required, "require=") {
		t.Errorf("the require control does not post a require:\n%s", required)
	}
	if strings.Contains(required, "optional=") {
		t.Errorf("the require control posts an optional:\n%s", required)
	}

	optional := section(t, got, `data-testid="add-optional"`, `data-testid="context-seeds"`)
	if !strings.Contains(optional, "optional=") {
		t.Errorf("the optional control does not post an optional:\n%s", optional)
	}
	if strings.Contains(optional, "require=") {
		t.Errorf("the optional control posts a require:\n%s", optional)
	}
}

func TestEntsMode_ValidationConsequenceIsStated(t *testing.T) {
	data := entsFixture()

	data.Selected = "Player" // strict
	if got := renderEnts(t, data); !strings.Contains(got, "refused") {
		t.Error("strict does not say what it does")
	}
	data.Selected = "Goblin" // warning
	got := renderEnts(t, data)
	if !strings.Contains(got, "logged") {
		t.Error("warning does not say what it does")
	}
	if strings.Contains(section(t, got, `data-testid="validation-consequence"`, `checkbox`), "refused") {
		t.Error("the warning level claims entities are refused")
	}
}

// The three seeds states that would otherwise render as an unexplained blank.
func TestEntsMode_ContextSeeds(t *testing.T) {
	t.Run("bound and mapped", func(t *testing.T) {
		data := entsFixture()
		data.Selected = "Goblin"
		got := renderEnts(t, data)

		if !strings.Contains(got, `data-testid="seed-hp"`) {
			t.Fatal("the machine's context key is not shown")
		}
		if !strings.Contains(got, "Health") {
			t.Error("the key's component is not shown")
		}
		if !strings.Contains(got, "edit in AGENTS") {
			t.Error("the panel does not say where these are edited")
		}
	})

	t.Run("no machine bound", func(t *testing.T) {
		data := entsFixture()
		data.Selected = "Player"
		got := renderEnts(t, data)

		if !strings.Contains(got, `data-testid="seeds-none"`) {
			t.Error("a type with no machine does not say so")
		}
		if strings.Contains(got, `data-testid="seeds-table"`) {
			t.Error("a type with no machine rendered a seeds table")
		}
	})

	t.Run("bound to a machine that did not resolve", func(t *testing.T) {
		data := entsFixture()
		data.Selected = "Goblin"
		et := data.Schema.EntityTypes["Goblin"]
		et.Behavior = "deleted-machine"
		data.Schema.EntityTypes["Goblin"] = et

		got := renderEnts(t, data)
		if !strings.Contains(got, `data-testid="seeds-missing"`) {
			t.Fatal("a dangling binding renders no explanation")
		}
		if !strings.Contains(got, "deleted-machine") {
			t.Error("the explanation does not name the missing machine")
		}
	})

	// A machine that loaded but did not validate is deliberately NOT a case
	// here: agent.Loader.LoadMachine stores nothing when ValidateMachine
	// reports anything, so such a machine never reaches this package and a test
	// constructing one would be asserting on a value the system cannot produce.
	// TestLoader_DropsAMachineThatDoesNotValidate pins that.

	t.Run("a seeded component that has since been renamed away", func(t *testing.T) {
		data := entsFixture()
		data.Selected = "Goblin"
		// The manifest was computed against schema.json as it was at startup.
		// The session has moved on.
		delete(data.Schema.Components, "Health")

		// The panel alone: the component also names itself on a chip elsewhere
		// on the page, so a page-wide search is about the chip, not the seed.
		panel := renderSeeds(t, data, "Goblin")
		if !strings.Contains(panel, `data-testid="seed-stale"`) {
			t.Fatalf("a mapping to a component that no longer exists is shown as live:\n%s", panel)
		}
		if strings.Contains(panel, "Health") {
			t.Errorf("the panel still names the component that was removed:\n%s", panel)
		}
	})
}

// Read-only means no controls, not controls that are styled to look inert.
// "Looks read-only" and "is read-only" differ, and only one survives a
// stylesheet change.
func TestEntsMode_SeedsPanelHasNoControls(t *testing.T) {
	// The panel on its own, not sliced out of the page: every attempt to bound
	// it by markers caught the delete button that follows it, and the
	// assertion would then have been about the panel's neighbour.
	panel := renderSeeds(t, entsFixture(), "Goblin")

	if !strings.Contains(panel, `data-testid="seed-hp"`) {
		t.Fatal("the panel rendered nothing, so there is nothing to check")
	}
	for _, control := range []string{"<input", "<select", "<button", "data-on:"} {
		if strings.Contains(panel, control) {
			t.Errorf("the seeds panel contains %q; it is meant to be read-only:\n%s", control, panel)
		}
	}
}

func TestEntsMode_ValuesRenderAsTheirJSONTypes(t *testing.T) {
	data := entsFixture()
	data.Selected = "Goblin"
	got := renderEnts(t, data)

	if !strings.Contains(got, ">10<") {
		t.Error("a numeric seed did not render as a number")
	}
	if !strings.Contains(got, `&#34;gob&#34;`) && !strings.Contains(got, `"gob"`) {
		t.Error("a string seed did not render as a quoted string")
	}
}

func TestEntsMode_WithNoSession(t *testing.T) {
	if got := renderEnts(t, Data{}); !strings.Contains(got, "No project is open") {
		t.Errorf("a mode with no session does not say why:\n%s", got)
	}
}

// renderSeeds renders the context-seeds panel on its own. Sliced out of the
// page it is bounded by whatever follows it, and every marker tried caught the
// delete button — so the assertions would have been about its neighbour.
func renderSeeds(t *testing.T, data Data, typeName string) string {
	t.Helper()
	var buf bytes.Buffer
	c := contextSeedsPanel(data, data.Schema.EntityTypes[typeName])
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// section returns the markup between two markers, so an assertion about one
// block cannot be satisfied by another block on the same page.
func section(t *testing.T, s, from, to string) string {
	t.Helper()
	start := strings.Index(s, from)
	if start < 0 {
		t.Fatalf("%s is not in the rendered output", from)
	}
	end := strings.Index(s[start:], to)
	if end < 0 {
		return s[start:]
	}
	return s[start : start+end]
}
