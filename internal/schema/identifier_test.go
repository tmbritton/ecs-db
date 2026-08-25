package schema

import (
	"fmt"
	"strings"
	"testing"
)

// probeSchema is a valid schema with one component, so a test can make exactly
// one thing wrong with it.
func probeSchema(compName string, props map[string]Property) DatabaseSchema {
	return DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]Component{
			compName: {Type: ComponentTypeObject, Properties: props},
		},
		EntityTypes: map[string]EntityType{
			"Thing": {RequiredComponents: []string{compName}, ValidationLevel: "strict"},
		},
	}
}

// A component becomes a table name and a property becomes a column name, both
// interpolated into DDL. A name that is not an identifier does not fail loudly:
// "two words" builds a column called "two" of type "words INTEGER", and the
// property the schema declared is simply not there.
func TestValidateSchema_RefusesAPropertyNameThatIsNotAnIdentifier(t *testing.T) {
	for _, name := range []string{
		"two words",
		`x" , "extra`,
		"x) --",
		"a-b",
		"has.dot",
		"1leading_digit",
		"",
		"trailing ",
		"emoji🙂",
		"semi;colon",
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateSchema(probeSchema("Probe", map[string]Property{name: {Type: PropertyTypeInteger}}))
			if err == nil {
				t.Fatalf("property %q was accepted", name)
			}
			if !strings.Contains(err.Error(), "Probe") {
				t.Errorf("error = %q, want it to name the component", err)
			}
		})
	}
}

func TestValidateSchema_RefusesAComponentNameThatIsNotAnIdentifier(t *testing.T) {
	for _, name := range []string{"two words", `Pr"obe`, "Pr-obe", "has.dot", "9lives", ""} {
		t.Run(name, func(t *testing.T) {
			s := probeSchema(name, map[string]Property{"x": {Type: PropertyTypeInteger}})
			err := ValidateSchema(s)
			if err == nil {
				t.Fatalf("component %q was accepted", name)
			}
			// %q is how the message renders it, so a name containing a quote
			// appears escaped — compare against the same rendering.
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", name)) {
				t.Errorf("error = %q, want it to name the component", err)
			}
		})
	}
}

// The table is comp_ + lowercase(name) and the column is lowercase(name), so
// two names differing only in case are one name by the time they reach SQL.
// Two components collapse to one table — silently, which is the worst of the
// three — and two properties collapse to a duplicate column, which at least
// fails at CREATE TABLE.
func TestValidateSchema_RefusesNamesThatCollideOnceLowercased(t *testing.T) {
	t.Run("two components", func(t *testing.T) {
		s := DatabaseSchema{
			SchemaVersion: 1,
			Components: map[string]Component{
				"Probe": {Type: ComponentTypeObject, Properties: map[string]Property{"a": {Type: PropertyTypeInteger}}},
				"probe": {Type: ComponentTypeObject, Properties: map[string]Property{"b": {Type: PropertyTypeInteger}}},
			},
			EntityTypes: map[string]EntityType{
				"Thing": {RequiredComponents: []string{"Probe"}, ValidationLevel: "strict"},
			},
		}
		err := ValidateSchema(s)
		if err == nil {
			t.Fatal("two components differing only in case were accepted; they share one table")
		}
		if !strings.Contains(err.Error(), "comp_probe") {
			t.Errorf("error = %q, want it to name the table they share", err)
		}
	})

	t.Run("two properties", func(t *testing.T) {
		err := ValidateSchema(probeSchema("Probe", map[string]Property{
			"Hp": {Type: PropertyTypeInteger},
			"hp": {Type: PropertyTypeString},
		}))
		if err == nil {
			t.Fatal("two properties differing only in case were accepted; they share one column")
		}
		if !strings.Contains(err.Error(), "Probe") || !strings.Contains(err.Error(), "hp") {
			t.Errorf("error = %q, want it to name the component and the column", err)
		}
	})
}

// The names this engine actually uses have to keep working, including the
// mixed-case ones — Position, tile_type, created_tick — since lowercasing is
// what the generator does and not something to refuse.
func TestValidateSchema_AcceptsTheNamesTheEngineUses(t *testing.T) {
	for _, name := range []string{"x", "y", "tile_type", "passable", "hp", "Position", "TileType", "_private", "a1"} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateSchema(probeSchema("Probe", map[string]Property{
				name: {Type: PropertyTypeInteger},
			})); err != nil {
				t.Errorf("property %q was refused: %v", name, err)
			}
		})
	}
}

// A scalar or array component has no properties of its own, so there is nothing
// to check there — but its own name still becomes a table.
func TestValidateSchema_ChecksTheNameOfAComponentWithNoProperties(t *testing.T) {
	s := DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]Component{
			"bad name": {Type: ComponentTypeInteger},
		},
		EntityTypes: map[string]EntityType{
			"Thing": {RequiredComponents: []string{"bad name"}, ValidationLevel: "strict"},
		},
	}
	if err := ValidateSchema(s); err == nil {
		t.Fatal("a scalar component with an unusable name was accepted")
	}
}

// Two bad names, and the message has to name the same one every run. Go's map
// iteration order is deliberately randomised, so without the sort this reports
// whichever the runtime reached first — and a refusal that moves between runs
// is one nobody can write a test against, including this one.
func TestValidateSchema_ReportsTheSameNameEveryRun(t *testing.T) {
	s := DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]Component{
			"Alpha Bad": {Type: ComponentTypeObject, Properties: map[string]Property{"x": {Type: PropertyTypeInteger}}},
			"Zulu Bad":  {Type: ComponentTypeObject, Properties: map[string]Property{"y": {Type: PropertyTypeInteger}}},
		},
		EntityTypes: map[string]EntityType{
			"Thing": {RequiredComponents: []string{"Alpha Bad"}, ValidationLevel: "strict"},
		},
	}
	first := ValidateSchema(s)
	if first == nil {
		t.Fatal("two unusable component names were accepted")
	}
	for i := 0; i < 40; i++ {
		if got := ValidateSchema(s); got.Error() != first.Error() {
			t.Fatalf("run %d reported %q, run 0 reported %q", i, got, first)
		}
	}
	if !strings.Contains(first.Error(), "Alpha Bad") {
		t.Errorf("error = %q, want the first name in sorted order", first)
	}
}

// The same for two bad properties of one component.
func TestValidateSchema_ReportsTheSamePropertyEveryRun(t *testing.T) {
	s := probeSchema("Probe", map[string]Property{
		"alpha bad": {Type: PropertyTypeInteger},
		"zulu bad":  {Type: PropertyTypeInteger},
	})
	first := ValidateSchema(s)
	if first == nil {
		t.Fatal("two unusable property names were accepted")
	}
	for i := 0; i < 40; i++ {
		if got := ValidateSchema(s); got.Error() != first.Error() {
			t.Fatalf("run %d reported %q, run 0 reported %q", i, got, first)
		}
	}
	if !strings.Contains(first.Error(), "alpha bad") {
		t.Errorf("error = %q, want the first name in sorted order", first)
	}
}

// The reserved list is consulted on the lowercased name, because that is the
// column the generator emits. A property called "Select" builds a column called
// "select", and SQLite reads that as the keyword whatever case it was typed in.
func TestValidateSchema_RefusesAReservedNameWhateverCaseItIsTypedIn(t *testing.T) {
	for _, name := range []string{"Select", "SELECT", "sElEcT", "Current_Time", "ENTITY_ID"} {
		t.Run(name, func(t *testing.T) {
			err := ValidateSchema(probeSchema("Probe", map[string]Property{name: {Type: PropertyTypeInteger}}))
			if err == nil {
				t.Fatalf("property %q was accepted; it becomes column %q", name, strings.ToLower(name))
			}
		})
	}
}

// And the three that do not fail loudly are the reason the list exists: the
// table builds, the write succeeds, and the read comes back as the clock.
func TestValidateSchema_RefusesTheKeywordsThatReadBackAsTheClock(t *testing.T) {
	for _, name := range []string{"current_time", "current_date", "current_timestamp"} {
		t.Run(name, func(t *testing.T) {
			err := ValidateSchema(probeSchema("Probe", map[string]Property{name: {Type: PropertyTypeInteger}}))
			if err == nil {
				t.Fatalf("property %q was accepted; it stores a number and reads back a timestamp", name)
			}
			if !strings.Contains(err.Error(), "clock") {
				t.Errorf("error = %q, want it to say the column reads back as the clock", err)
			}
		})
	}
}
