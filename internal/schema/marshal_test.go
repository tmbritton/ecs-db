package schema

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The test. schema.json is a file a human wrote, kept in version control and
// read in diffs; a writer that reorders or reformats it has damaged something
// someone cared about. Everything else here is supporting detail.
func TestMarshal_RoundTripsTheRepoSchemaByteForByte(t *testing.T) {
	// Both schemas in the repo, so the guarantee covers the e2e fixture too —
	// which is where a dropped entityType behavior would have bitten first.
	for _, path := range []string{"../../schema.json", "../../e2e/fixtures/project/schema.json"} {
		t.Run(path, func(t *testing.T) {
			original, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("not readable: %v", err)
			}

			loaded, err := LoadSchema(original)
			if err != nil {
				t.Fatalf("LoadSchema: %v", err)
			}
			got, err := Marshal(loaded)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			if !bytes.Equal(got, original) {
				t.Errorf("round trip is not byte-stable\n--- want ---\n%s\n--- got ---\n%s\n%s",
					original, got, firstDifference(original, got))
			}
		})
	}
}

// Entity-type fields are a fixed set and are written in a fixed order, so a
// schema that authored them differently is normalised on first save. That is a
// deliberate, one-time, semantically-null diff — unlike component order, which
// carries grouping intent and is preserved. Pinned here so the difference
// between the two is a decision rather than an accident.
func TestMarshal_NormalisesEntityTypeFieldOrder(t *testing.T) {
	src := `{"schemaVersion":1,
		"components":{"C":{"type":"object","properties":{"x":{"type":"number"}}}},
		"entityTypes":{"T":{
			"validationLevel":"warning",
			"allowExtraComponents":true,
			"requiredComponents":["C"]
		}}}`

	loaded, err := LoadSchema([]byte(src))
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	out, err := Marshal(loaded)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got := string(out)
	order := []string{"requiredComponents", "optionalComponents", "allowExtraComponents", "validationLevel"}
	prev := -1
	for _, field := range order {
		at := strings.Index(got, field)
		if at < 0 {
			t.Fatalf("output is missing %q:\n%s", field, got)
		}
		if at < prev {
			t.Errorf("%q is out of the canonical order:\n%s", field, got)
		}
		prev = at
	}

	// Normalised, not lost.
	back, err := LoadSchema(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if back.EntityTypes["T"].ValidationLevel != ValidationWarning || !back.EntityTypes["T"].AllowExtraComponents {
		t.Errorf("reordering changed the values: %#v", back.EntityTypes["T"])
	}
}

// firstDifference points at the byte where two documents diverge, because a
// whole-file dump of two 60-line JSON documents is unreadable.
func firstDifference(want, got []byte) string {
	wl := strings.Split(string(want), "\n")
	gl := strings.Split(string(got), "\n")
	for i := range max(len(wl), len(gl)) {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			return "first difference at line " + itoa(i+1) + ":\n  want: " + quote(w) + "\n  got:  " + quote(g)
		}
	}
	return "(documents differ only in trailing bytes)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func quote(s string) string { return "\"" + s + "\"" }

// Authored order carries intent — Position, Health and Sprite are grouped
// because they belong together, not because P precedes H. Go maps have no
// order, so a naive writer emits alphabetically and moves every component.
func TestMarshal_PreservesAuthoredOrder(t *testing.T) {
	src := []byte(`{
  "schemaVersion": 1,
  "components": {
    "Zebra": {
      "type": "object",
      "properties": {
        "zz": { "type": "number" },
        "aa": { "type": "number" }
      }
    },
    "Apple": {
      "type": "object",
      "properties": {
        "x": { "type": "number" }
      }
    }
  },
  "entityTypes": {
    "Zulu": {
      "requiredComponents": ["Zebra"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    },
    "Alpha": {
      "requiredComponents": ["Apple"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    }
  }
}
`)
	loaded, err := LoadSchema(src)
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	got, err := Marshal(loaded)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Errorf("authored order was not preserved\n--- want ---\n%s\n--- got ---\n%s", src, got)
	}

	// Explicitly: alphabetical would have reversed all three of these.
	s := string(got)
	if strings.Index(s, `"Zebra"`) > strings.Index(s, `"Apple"`) {
		t.Error("components were sorted alphabetically")
	}
	if strings.Index(s, `"zz"`) > strings.Index(s, `"aa"`) {
		t.Error("properties were sorted alphabetically")
	}
	if strings.Index(s, `"Zulu"`) > strings.Index(s, `"Alpha"`) {
		t.Error("entity types were sorted alphabetically")
	}
}

// A schema built in code has no recorded order. It must still emit the same
// bytes every time — map iteration order leaking into a file would make every
// save a spurious diff.
func TestMarshal_DeterministicWithoutRecordedOrder(t *testing.T) {
	s := DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]Component{
			"Zebra":  {Type: "object", Properties: map[string]Property{"zz": {Type: "number"}, "aa": {Type: "number"}}},
			"Apple":  {Type: "object", Properties: map[string]Property{"x": {Type: "number"}}},
			"Mango":  {Type: "object", Properties: map[string]Property{"m": {Type: "number"}}},
			"Cherry": {Type: "object", Properties: map[string]Property{"c": {Type: "number"}}},
		},
		EntityTypes: map[string]EntityType{
			"Zulu":  {RequiredComponents: []string{"Zebra"}, ValidationLevel: ValidationStrict},
			"Alpha": {RequiredComponents: []string{"Apple"}, ValidationLevel: ValidationStrict},
		},
	}

	first, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for range 20 {
		again, err := Marshal(s)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("Marshal is not deterministic\n--- first ---\n%s\n--- again ---\n%s", first, again)
		}
	}

	// Unrecorded keys sort, so the result is predictable rather than arbitrary.
	got := string(first)
	if strings.Index(got, `"Apple"`) > strings.Index(got, `"Cherry"`) {
		t.Error("unrecorded components are not sorted")
	}
}

// A component added through the UI has no recorded position. It must land
// somewhere predictable rather than wherever a map iteration put it.
func TestMarshal_NewKeysAppendInSortedOrder(t *testing.T) {
	loaded, err := LoadSchema([]byte(`{
  "schemaVersion": 1,
  "components": {
    "Zebra": {"type": "object", "properties": {"z": {"type": "number"}}},
    "Apple": {"type": "object", "properties": {"a": {"type": "number"}}}
  },
  "entityTypes": {"Thing": {"requiredComponents": ["Zebra"]}}
}`))
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}

	loaded.Components["Mango"] = Component{Type: "object", Properties: map[string]Property{"m": {Type: "number"}}}
	loaded.Components["Banana"] = Component{Type: "object", Properties: map[string]Property{"b": {Type: "number"}}}

	got, err := Marshal(loaded)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(got)

	// Recorded order first, unchanged.
	if strings.Index(s, `"Zebra"`) > strings.Index(s, `"Apple"`) {
		t.Error("recorded order was disturbed by the additions")
	}
	// Then the new ones, sorted among themselves, after the recorded ones.
	if strings.Index(s, `"Apple"`) > strings.Index(s, `"Banana"`) {
		t.Error("new components were interleaved with recorded ones")
	}
	if strings.Index(s, `"Banana"`) > strings.Index(s, `"Mango"`) {
		t.Error("new components are not sorted among themselves")
	}
}

// A deleted component must not reappear because its name is still in the
// recorded order.
func TestMarshal_DroppedKeysDoNotReappear(t *testing.T) {
	loaded, err := LoadSchema([]byte(`{
  "schemaVersion": 1,
  "components": {
    "Keep": {"type": "object", "properties": {"k": {"type": "number"}}},
    "Drop": {"type": "object", "properties": {"d": {"type": "number"}}}
  },
  "entityTypes": {"Thing": {"requiredComponents": ["Keep"]}}
}`))
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	delete(loaded.Components, "Drop")

	got, err := Marshal(loaded)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(got), "Drop") {
		t.Errorf("a deleted component was re-emitted:\n%s", got)
	}
}

// EntityType has no omitempty on its slices, so a nil one marshals as null
// through encoding/json. null is not a list of component names, and the next
// load would have to special-case it.
func TestMarshal_NilSlicesEmitAsEmptyArrays(t *testing.T) {
	s := DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]Component{
			"Position": {Type: "object", Properties: map[string]Property{"x": {Type: "number"}}},
		},
		EntityTypes: map[string]EntityType{
			"Bare": {ValidationLevel: ValidationStrict}, // both slices nil
		},
	}
	got, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(got), "null") {
		t.Errorf("a nil slice was emitted as null:\n%s", got)
	}
	for _, want := range []string{`"requiredComponents": []`, `"optionalComponents": []`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("output does not contain %s:\n%s", want, got)
		}
	}
}

// Whatever Marshal writes, LoadSchema must read back — including the shapes the
// repo's own schema.json does not happen to use.
func TestMarshal_RoundTripsEveryComponentShape(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "object with nested properties",
			src: `{"schemaVersion":1,"components":{
				"C":{"type":"object","properties":{"inner":{"type":"object","properties":{"deep":{"type":"string"}}}}}
			},"entityTypes":{"T":{"requiredComponents":["C"]}}}`,
		},
		{
			name: "array with items",
			src: `{"schemaVersion":1,"components":{
				"C":{"type":"array","items":{"type":"string"}}
			},"entityTypes":{"T":{"requiredComponents":["C"]}}}`,
		},
		{
			name: "entity-ref",
			src: `{"schemaVersion":1,"components":{
				"C":{"type":"entity-ref"}
			},"entityTypes":{"T":{"requiredComponents":["C"]}}}`,
		},
		{
			name: "component with a behavior",
			src: `{"schemaVersion":1,"components":{
				"C":{"type":"object","behavior":"walker","properties":{"x":{"type":"number"}}}
			},"entityTypes":{"T":{"requiredComponents":["C"]}}}`,
		},
		{
			name: "entity type bound to a behavior",
			src: `{"schemaVersion":1,"components":{
				"C":{"type":"object","properties":{"x":{"type":"number"}}}
			},"entityTypes":{"T":{"behavior":"wander","requiredComponents":["C"]}}}`,
		},
		{
			name: "property of type array with items",
			src: `{"schemaVersion":1,"components":{
				"C":{"type":"object","properties":{"tags":{"type":"array","items":{"type":"string"}}}}
			},"entityTypes":{"T":{"requiredComponents":["C"]}}}`,
		},
		{
			name: "array of objects",
			src: `{"schemaVersion":1,"components":{
				"C":{"type":"array","items":{"type":"object","properties":{"x":{"type":"number"}}}}
			},"entityTypes":{"T":{"requiredComponents":["C"]}}}`,
		},
		{
			name: "deeply nested object",
			src: `{"schemaVersion":1,"components":{
				"C":{"type":"object","properties":{
					"a":{"type":"object","properties":{
						"b":{"type":"object","properties":{"c":{"type":"string"}}}
					}}
				}}
			},"entityTypes":{"T":{"requiredComponents":["C"]}}}`,
		},
		{
			name: "entity type with every field set",
			src: `{"schemaVersion":1,"components":{
				"C":{"type":"object","properties":{"x":{"type":"number"}}}
			},"entityTypes":{"T":{
				"requiredComponents":["C"],"optionalComponents":["C"],
				"allowExtraComponents":true,"validationLevel":"warning"
			}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := LoadSchema([]byte(tt.src))
			if err != nil {
				t.Fatalf("LoadSchema: %v", err)
			}
			out, err := Marshal(first)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			second, err := LoadSchema(out)
			if err != nil {
				t.Fatalf("re-loading marshalled output failed: %v\n%s", err, out)
			}

			// The assertion that matters, and the one this test originally
			// lacked: the schema that comes back must equal the one that went
			// in. Checking only that the output re-parses and is idempotent
			// passes for any field Marshal drops entirely — which is exactly
			// how a dropped entityType behavior shipped.
			// A round trip does change one thing on purpose: a nil slice comes
			// back as an empty one, because Marshal never writes null. Those
			// mean the same thing, so the comparison is against normalised
			// copies rather than pretending the difference is not there.
			normaliseEntityTypes(first.EntityTypes)
			normaliseEntityTypes(second.EntityTypes)

			if !reflect.DeepEqual(first.Components, second.Components) {
				t.Errorf("components did not survive the round trip\n want: %#v\n got:  %#v\n--- output ---\n%s",
					first.Components, second.Components, out)
			}
			if !reflect.DeepEqual(first.EntityTypes, second.EntityTypes) {
				t.Errorf("entity types did not survive the round trip\n want: %#v\n got:  %#v\n--- output ---\n%s",
					first.EntityTypes, second.EntityTypes, out)
			}
			if first.SchemaVersion != second.SchemaVersion {
				t.Errorf("schemaVersion = %d, want %d", second.SchemaVersion, first.SchemaVersion)
			}

			// Marshalling twice must be a fixed point, which is what makes the
			// second save of an unchanged file a no-op diff.
			again, err := Marshal(second)
			if err != nil {
				t.Fatalf("Marshal (second pass): %v", err)
			}
			if !bytes.Equal(out, again) {
				t.Errorf("marshal is not idempotent\n--- first ---\n%s\n--- second ---\n%s", out, again)
			}
		})
	}
}

// Renaming one component should touch the lines for that component and nothing
// else. This is the property the story is named for.
func TestMarshal_RenamingOneComponentTouchesOnlyThatComponent(t *testing.T) {
	original, err := os.ReadFile("../../schema.json")
	if err != nil {
		t.Skipf("schema.json not readable: %v", err)
	}
	loaded, err := LoadSchema(original)
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}

	speed := loaded.Components["Speed"]
	delete(loaded.Components, "Speed")
	loaded.Components["Velocity"] = speed
	for i, name := range loaded.ComponentOrder {
		if name == "Speed" {
			loaded.ComponentOrder[i] = "Velocity"
		}
	}
	for name, et := range loaded.EntityTypes {
		for i, c := range et.OptionalComponents {
			if c == "Speed" {
				et.OptionalComponents[i] = "Velocity"
			}
		}
		loaded.EntityTypes[name] = et
	}

	got, err := Marshal(loaded)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	changed := changedLines(string(original), string(got))
	// "Speed" appears as a component name and in one entity type's optional
	// list: two lines, and nothing else may move.
	if changed > 2 {
		t.Errorf("renaming one component changed %d lines, want at most 2:\n%s",
			changed, firstDifference(original, got))
	}
}

// normaliseEntityTypes makes nil and empty slices compare equal, which is the
// one difference a round trip legitimately introduces.
func normaliseEntityTypes(types map[string]EntityType) {
	for name, et := range types {
		if et.RequiredComponents == nil {
			et.RequiredComponents = []string{}
		}
		if et.OptionalComponents == nil {
			et.OptionalComponents = []string{}
		}
		types[name] = et
	}
}

func changedLines(a, b string) int {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	n := 0
	for i := range max(len(al), len(bl)) {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			n++
		}
	}
	return n
}

// Names are written into the file by hand, so they must be escaped like any
// other JSON string. None of this was pinned: replacing jsonString's body with
// naive quote-concatenation left the whole suite green.
func TestMarshal_EscapesNames(t *testing.T) {
	s := DatabaseSchema{
		SchemaVersion: 1,
		Components: map[string]Component{
			`Quote"Name`:    {Type: "object", Properties: map[string]Property{`field"q`: {Type: "number"}}},
			`Back\slash`:    {Type: "object", Properties: map[string]Property{`tab	here`: {Type: "number"}}},
			"Ünïcøde":       {Type: "object", Properties: map[string]Property{"naïve": {Type: "string"}}},
			"Angle<&>Brack": {Type: "object", Properties: map[string]Property{"lt<gt": {Type: "boolean"}}},
		},
		EntityTypes: map[string]EntityType{
			`Type"With\Escapes`: {RequiredComponents: []string{`Quote"Name`}, ValidationLevel: ValidationStrict},
		},
	}

	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Valid JSON first — a naive quoter produces something that is not.
	var any map[string]any
	if err := json.Unmarshal(out, &any); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	// And it must load back with every name intact.
	back, err := LoadSchema(out)
	if err != nil {
		t.Fatalf("re-loading escaped output: %v\n%s", err, out)
	}
	for name := range s.Components {
		if _, ok := back.Components[name]; !ok {
			t.Errorf("component %q did not survive escaping", name)
		}
	}

	// HTML escaping stays off: \u003c is semantically identical to < and
	// unreadable in a diff.
	if strings.Contains(string(out), `\u003c`) {
		t.Errorf("HTML escaping is on; < was written as \\u003c:\n%s", out)
	}
	if !strings.Contains(string(out), "<") {
		t.Errorf("expected a literal < in the output:\n%s", out)
	}
}

// A schema section written as JSON null loaded fine before order tracking
// existed. Reading key order out of the raw bytes must not change that: a
// "properties": null component is valid and would otherwise stop the engine.
func TestLoadSchema_AcceptsNullSections(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr bool
	}{
		{
			name: "null properties on a non-object component",
			src:  `{"schemaVersion":1,"components":{"C":{"type":"string","properties":null}},"entityTypes":{"T":{"requiredComponents":["C"]}}}`,
		},
		{
			// Loads, then fails validation with a message about the schema
			// rather than a parse error about key order.
			name:    "null components",
			src:     `{"schemaVersion":1,"components":null,"entityTypes":{"T":{"requiredComponents":["C"]}}}`,
			wantErr: false,
		},
		{
			name:    "null entityTypes",
			src:     `{"schemaVersion":1,"components":{"C":{"type":"string"}},"entityTypes":null}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadSchema([]byte(tt.src))
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadSchema error = %v, wantErr = %v", err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "order") {
				t.Errorf("failed while reading key order, which is not a schema problem: %v", err)
			}
		})
	}
}

// Nothing rejects a component carrying both properties and items — the
// unmarshaller checks that an object has properties and an array has items,
// never that the other is absent. Writing only one of them produced a file the
// loader then refused, which is a corrupt schema.json on disk.
func TestMarshal_ComponentWithBothPropertiesAndItems(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "array component that also carries properties",
			src: `{"schemaVersion":1,"components":{
				"C":{"type":"array","items":{"type":"string"},"properties":{"x":{"type":"number"}}}
			},"entityTypes":{"T":{"requiredComponents":["C"]}}}`,
		},
		{
			name: "object component that also carries items",
			src: `{"schemaVersion":1,"components":{
				"C":{"type":"object","properties":{"x":{"type":"number"}},"items":{"type":"string"}}
			},"entityTypes":{"T":{"requiredComponents":["C"]}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := LoadSchema([]byte(tt.src))
			if err != nil {
				t.Fatalf("LoadSchema: %v", err)
			}
			out, err := Marshal(first)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			second, err := LoadSchema(out)
			if err != nil {
				t.Fatalf("Marshal wrote a file that no longer loads: %v\n%s", err, out)
			}
			if !reflect.DeepEqual(first.Components, second.Components) {
				t.Errorf("a field was dropped\n want: %#v\n got:  %#v\n--- output ---\n%s",
					first.Components, second.Components, out)
			}
		})
	}
}
