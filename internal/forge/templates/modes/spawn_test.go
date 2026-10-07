package modes

import (
	"os"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

// Browser selectors have a Go-side owner, so changing a test id fails in
// seconds rather than timing out only after starting Chromium.
func TestSpawnPage_PinsBrowserTestIDs(t *testing.T) {
	obj := tiled.Object{ID: 1, Type: "TestGoblin", X: 16, Y: 16}
	data := Data{
		Schema: schema.DatabaseSchema{EntityTypes: map[string]schema.EntityType{
			"TestGoblin": {RequiredComponents: []string{"Position"}},
			"Marker":     {RequiredComponents: []string{"Health"}},
			"Tile":       {RequiredComponents: []string{"Position"}},
		}},
		Canvas:        mapcanvas.Canvas{TileW: 16, TileH: 16},
		ObjectGroups:  []tiled.ObjectGroup{{Name: "spawns", Objects: []tiled.Object{obj}}},
		SelectedSpawn: &obj,
		SelectedMap:   "level.tmx", MapView: MapView{Path: "level.tmx"},
	}
	markup := render(t, spawnPalette(data)) + render(t, mapCanvas(data)) + render(t, spawnInspector(data))
	for _, id := range []string{
		"spawn-type-TestGoblin", "spawn-type-Marker", "object-group-spawns",
		"spawn-canvas", "spawn-1", "spawn-selected", "spawn-delete",
	} {
		if n := strings.Count(markup, `data-testid="`+id+`"`); n != 1 {
			t.Errorf("data-testid=%q occurs %d times, want 1", id, n)
		}
	}
	if !strings.Contains(markup, "does not declare Position") {
		t.Error("unspawnable type has no visible reason")
	}
	if strings.Contains(markup, `data-testid="spawn-type-Tile"`) {
		t.Error("the tile importer's own type is not a spawn to offer")
	}
	data.SelectedSpawn = nil
	data.MissingSpawn = 1
	markup = render(t, spawnInspector(data))
	for _, id := range []string{"spawn-missing", "spawn-clear-selection"} {
		if !strings.Contains(markup, `data-testid="`+id+`"`) {
			t.Errorf("a deleted selection needs %s", id)
		}
	}
}

func mapSpawnFixture() Data {
	data := mapRegionFixture()
	obj := tiled.Object{ID: 3, Type: "Goblin", X: 16, Y: 0, Properties: tiled.Properties{
		"Health.hp":        {Type: "int", Value: "5"},
		"Sprite.sheet":     {Value: "old.png"},
		"Sprite.animation": {Value: "idle"},
	}}
	data.SelectedSpawn = &obj
	data.ObjectGroups = []tiled.ObjectGroup{{Name: "spawns", Objects: []tiled.Object{obj}}}
	data.Schema = schema.DatabaseSchema{
		Components: map[string]schema.Component{
			"Position": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"x": {Type: schema.PropertyTypeInteger}, "y": {Type: schema.PropertyTypeInteger},
			}},
			"Health": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"hp": {Type: schema.PropertyTypeInteger},
			}},
			"Sprite": {Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
				"sheet": {Type: schema.PropertyTypeString}, "animation": {Type: schema.PropertyTypeString},
			}},
			"Label": {Type: schema.ComponentTypeString},
		},
		EntityTypes: map[string]schema.EntityType{
			"Goblin": {
				Behavior: "wander", RequiredComponents: []string{"Position", "Health"},
				OptionalComponents: []string{"Sprite", "Label"}, ValidationLevel: schema.ValidationStrict,
			},
		},
	}
	data.Machines = []project.Machine{{ID: "wander", Definition: &agent.MachineDefinition{
		ID: "wander", Context: map[string]any{"hp": float64(5)}, ContextOrder: []string{"hp"},
		ContextManifest: map[string]string{"hp": "Health"},
	}}}
	return data
}

func TestSpawnInspector_RendersEngineContractAndPinsBrowserIDs(t *testing.T) {
	got := render(t, spawnInspector(mapSpawnFixture()))
	for _, id := range []string{
		"spawn-selected", "spawn-component-Position", "spawn-component-Health", "spawn-component-Sprite",
		"spawn-field-Health.hp", "spawn-add-component", "spawn-behavior",
		"context-seeds", "spawn-ctx-Health.hp", "spawn-position",
	} {
		if !strings.Contains(got, `data-testid="`+id+`"`) {
			t.Errorf("missing data-testid=%q in inspector", id)
		}
	}
	if !strings.Contains(got, "required by type") || !strings.Contains(got, "ƒ ctx") {
		t.Error("Health must show its lock and context badge")
	}
	if !strings.Contains(got, `href="/forge/ents?type=Goblin"`) || !strings.Contains(got, "schema.json") {
		t.Error("bound behaviour must be attributed to the type and link to ENTS")
	}
	if !strings.Contains(got, "animations.toml") || strings.Contains(got, `data-testid="spawn-field-Sprite.sheet"`) {
		t.Error("Sprite.sheet must be read-only with its startup override explained")
	}
	if strings.Contains(got, `data-testid="spawn-field-Position.x"`) || strings.Contains(got, `data-testid="spawn-detach-Health"`) {
		t.Error("Position is not an editable property and Health cannot be detached")
	}
}

func TestSpawnInspector_WarnsAboutForbiddenContextSeed(t *testing.T) {
	data := mapSpawnFixture()
	data.Schema.EntityTypes["Goblin"] = schema.EntityType{
		Behavior: "wander", RequiredComponents: []string{"Position", "Health"},
		ValidationLevel: schema.ValidationStrict,
	}
	data.Machines[0].Definition.Context["name"] = "goblin"
	data.Machines[0].Definition.ContextOrder = []string{"hp", "name"}
	data.Machines[0].Definition.ContextManifest["name"] = "Sprite"
	got := render(t, spawnInspector(data))
	if !strings.Contains(got, "Sprite") || !strings.Contains(got, "forbids") || !strings.Contains(got, "refuse") {
		t.Error("a bound machine that seeds a forbidden component must warn")
	}
	et := data.Schema.EntityTypes["Goblin"]
	et.ValidationLevel = schema.ValidationWarning
	data.Schema.EntityTypes["Goblin"] = et
	got = render(t, spawnInspector(data))
	if !strings.Contains(got, "forbids") || !strings.Contains(got, "proceeds with a warning") || strings.Contains(got, "engine will refuse") {
		t.Error("warning-level binding is allowed with a warning, not refused")
	}
}

func TestSpawnInspector_ContextBadgeSaysExistingMapComponentWins(t *testing.T) {
	got := render(t, spawnInspector(mapSpawnFixture()))
	if !strings.Contains(got, "seeds missing components only") || !strings.Contains(got, "map value wins") {
		t.Error("a context badge must not imply startup overwrites an existing component")
	}
}

func TestSpawnAddable_OffersOptionalThenExtraButNeverAlreadyAttached(t *testing.T) {
	data := mapSpawnFixture()
	if got := spawnAddable(data); len(got) != 1 || got[0] != "Label" {
		t.Errorf("optional offers %v, want Label alone", got)
	}
	data.Schema.Components["Probe"] = schema.Component{Type: schema.ComponentTypeInteger}
	et := data.Schema.EntityTypes["Goblin"]
	et.AllowExtraComponents = true
	data.Schema.EntityTypes["Goblin"] = et
	got := spawnAddable(data)
	if len(got) != 2 || got[0] != "Label" || got[1] != "Probe" {
		t.Errorf("extras offers %v, want Label and Probe; no Position/Health/Sprite", got)
	}
}

func TestSpawnInspector_OpensTheShippedGoblin(t *testing.T) {
	mapBytes, err := os.ReadFile("../../../../mods/map/level1.tmx")
	if err != nil {
		t.Fatal(err)
	}
	m, err := tiled.Parse(mapBytes, "level1.tmx")
	if err != nil {
		t.Fatal(err)
	}
	schemaBytes, err := os.ReadFile("../../../../schema.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := schema.LoadSchema(schemaBytes)
	if err != nil {
		t.Fatal(err)
	}
	data := mapRegionFixture()
	data.Schema = s
	data.Canvas.TileW, data.Canvas.TileH = m.TileWidth, m.TileHeight
	data.SelectedSpawn = &m.ObjectGroups[0].Objects[1]
	got := render(t, spawnInspector(data))
	for _, want := range []string{
		`Goblin · object 2`, `Health.hp`, `value="5"`,
		`goblin`, `href="/forge/ents?type=Goblin"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the shipped goblin did not show %q", want)
		}
	}
}

func TestSpawnInspector_ShowsWarningsAsWarningsNotErrors(t *testing.T) {
	data := mapSpawnFixture()
	data.SpawnWarnings = []string{`component "Label" is not allowed`}
	got := render(t, spawnInspector(data))
	if !strings.Contains(got, `data-testid="spawn-contract-warning"`) ||
		!strings.Contains(got, `role="status"`) || strings.Contains(got, `data-testid="spawn-context-warning"`) {
		t.Error("a warning-level verdict must not be shown as a hard error")
	}
}

func TestSpawnInspector_OffersReferenceWithAnExplicitTargetInput(t *testing.T) {
	data := mapSpawnFixture()
	data.Schema.Components["Target"] = schema.Component{Type: schema.ComponentTypeEntityRef}
	et := data.Schema.EntityTypes["Goblin"]
	et.OptionalComponents = append(et.OptionalComponents, "Target")
	data.Schema.EntityTypes["Goblin"] = et
	got := render(t, spawnInspector(data))
	if !strings.Contains(got, `data-testid="spawn-target-Target"`) ||
		!strings.Contains(got, `data-testid="spawn-attach-Target"`) {
		t.Error("optional entity reference has no place to supply its target")
	}
	if strings.Contains(got, `<option value="Target">`) {
		t.Error("reference offered as a one-click add without its required target")
	}
}

func TestSpawnInspector_OffersEveryReferenceFieldOfAnObjectBeforeAttach(t *testing.T) {
	data := mapSpawnFixture()
	data.Schema.Components["Owner"] = schema.Component{Type: schema.ComponentTypeObject, Properties: map[string]schema.Property{
		"first": {Type: schema.PropertyTypeEntityRef}, "second": {Type: schema.PropertyTypeEntityRef},
	}}
	et := data.Schema.EntityTypes["Goblin"]
	et.OptionalComponents = append(et.OptionalComponents, "Owner")
	data.Schema.EntityTypes["Goblin"] = et
	got := render(t, spawnInspector(data))
	for _, id := range []string{"spawn-target-Owner.first", "spawn-target-Owner.second", "spawn-attach-Owner"} {
		if !strings.Contains(got, `data-testid="`+id+`"`) {
			t.Errorf("missing explicit target control %s", id)
		}
	}
	if strings.Contains(got, `<option value="Owner">`) {
		t.Error("object with reference fields offered as one-click add")
	}
}

func TestSpawnInspector_CanonicalizesAuthoredComponentCase(t *testing.T) {
	data := mapSpawnFixture()
	obj := *data.SelectedSpawn
	obj.Properties = tiled.Properties{
		"health.hp": {Type: "int", Value: "8"},
	}
	data.SelectedSpawn = &obj
	got := render(t, spawnInspector(data))
	if !strings.Contains(got, `data-testid="spawn-component-Health"`) ||
		!strings.Contains(got, `data-testid="spawn-field-Health.hp"`) ||
		!strings.Contains(got, `value="8"`) ||
		strings.Contains(got, `data-testid="spawn-component-health"`) ||
		strings.Contains(got, "Missing required component") {
		t.Errorf("a valid lower-case TMX property appears as missing/unknown:\n%s", got)
	}
}

func TestSpawnInspector_OffersRepairForMissingRequiredComponents(t *testing.T) {
	data := mapSpawnFixture()
	obj := *data.SelectedSpawn
	obj.Properties = nil
	data.SelectedSpawn = &obj
	got := render(t, spawnInspector(data))
	if !strings.Contains(got, `data-testid="spawn-repair-required"`) ||
		!strings.Contains(got, `repair=required`) {
		t.Error("an object missing required components has no way to repair them")
	}
}
