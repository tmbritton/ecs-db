package tiled_test

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

// richTMX carries one of everything the reading model drops. If a save can keep
// this file intact it can keep a real Tiled project intact, which is the whole
// claim of the story this belongs to.
const richTMX = `<?xml version="1.0" encoding="UTF-8"?>
<!-- a level somebody wrote by hand -->
<map version="1.10" tiledversion="1.11.0" orientation="orthogonal" renderorder="right-down" width="2" height="2" tilewidth="32" tileheight="32" infinite="0" nextlayerid="9" nextobjectid="42" backgroundcolor="#112233">
 <editorsettings>
  <export target="level.json" format="json"/>
 </editorsettings>
 <properties>
  <property name="mapId" value="rich"/>
 </properties>
 <tileset firstgid="1" source="starter.tsx"/>
 <imagelayer id="7" name="sky" offsetx="4" offsety="-8" repeatx="1">
  <image source="sky.png" width="64" height="64"/>
 </imagelayer>
 <group id="8" name="folder">
  <layer id="1" name="ground" width="2" height="2" offsetx="8" offsety="-4" parallaxx="0.5" tintcolor="#ff0000" class="floors">
   <data encoding="csv">
1,1,
1,1
</data>
  </layer>
 </group>
 <objectgroup id="2" name="spawns" color="#00ff00" draworder="index">
  <object id="7" name="g" type="Goblin" x="32" y="64">
   <polygon points="0,0 8,0 8,8"/>
   <properties>
    <property name="Health.hp" type="int" value="5"/>
   </properties>
  </object>
  <object id="8" name="note" x="0" y="0">
   <text wrap="1">hello</text>
  </object>
 </objectgroup>
</map>
`

func shippedLevel(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../mods/map/level1.tmx")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseDocument_ASaveWithNoEditIsByteIdentical(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  []byte
	}{
		{"the shipped level", nil},
		{"a map full of things the reader drops", []byte(richTMX)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.src
			if src == nil {
				src = shippedLevel(t)
			}
			doc, err := tiled.ParseDocument(src, "level.tmx")
			if err != nil {
				t.Fatalf("ParseDocument: %v", err)
			}
			if got := doc.Bytes(); string(got) != string(src) {
				t.Errorf("a save with no edit changed the file\n got: %q\nwant: %q", got, src)
			}
		})
	}
}

func TestDocument_MapReadsWhatTheDocumentHolds(t *testing.T) {
	doc, err := tiled.ParseDocument(shippedLevel(t), "level1.tmx")
	if err != nil {
		t.Fatal(err)
	}
	m, err := doc.Map()
	if err != nil {
		t.Fatal(err)
	}
	if m.Width != 20 || m.Height != 15 {
		t.Errorf("map is %dx%d, want 20x15", m.Width, m.Height)
	}
	if len(m.Layers) != 1 || m.Layers[0].Name != "ground" {
		t.Errorf("layers: %+v", m.Layers)
	}
	if m.Name != "level1.tmx" {
		t.Errorf("map name is %q", m.Name)
	}
}

func TestParseDocument_RefusesWhatItCannotEdit(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a .tmj", `{"width":2}`, ".tmx"},
		{"not a map at all", `<tileset name="x"/>`, "tiled:"},
		{"an infinite map", `<map version="1.10" width="1" height="1" tilewidth="8" tileheight="8" infinite="1"/>`, "infinite"},
		{"a map with no tile size", `<map version="1.10" width="1" height="1"/>`, "pixels"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tiled.ParseDocument([]byte(tc.src), "x.tmx")
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestDocument_SetLayerDataChangesTheDataAndNothingElse(t *testing.T) {
	doc, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetLayerData(0, []uint32{1, 2, 2, 1}); err != nil {
		t.Fatal(err)
	}
	got := string(doc.Bytes())
	want := strings.Replace(richTMX, "\n1,1,\n1,1\n", "\n1,2,\n2,1\n", 1)
	if got != want {
		t.Errorf("painting a cell changed more than the layer\n got: %q\nwant: %q", got, want)
	}

	m, err := doc.Map()
	if err != nil {
		t.Fatal(err)
	}
	if m.Layers[0].TileAt(1, 0).GID != 2 {
		t.Errorf("the map does not read back what was painted: %v", m.Layers[0].Data)
	}
}

func TestDocument_SetLayerDataKeepsTheEncodingTheFileUsed(t *testing.T) {
	const src = `<map version="1.10" width="2" height="1" tilewidth="8" tileheight="8">
 <layer id="1" name="l" width="2" height="1">
  <data encoding="base64" compression="zlib">eJxjZGBgYGJgYAAAABgABA==</data>
 </layer>
</map>`
	doc, err := tiled.ParseDocument([]byte(src), "b.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetLayerData(0, []uint32{5, 6}); err != nil {
		t.Fatal(err)
	}
	out := string(doc.Bytes())
	if !strings.Contains(out, `encoding="base64" compression="zlib"`) {
		t.Fatalf("the encoding attributes changed: %q", out)
	}
	if strings.Contains(out, "5,6") {
		t.Fatalf("a base64 layer was rewritten as CSV: %q", out)
	}
	back, err := tiled.ParseDocument([]byte(out), "b.tmx")
	if err != nil {
		t.Fatal(err)
	}
	m, err := back.Map()
	if err != nil {
		t.Fatal(err)
	}
	if m.Layers[0].Data[0] != 5 || m.Layers[0].Data[1] != 6 {
		t.Errorf("gids did not survive: %v", m.Layers[0].Data)
	}
}

func TestDocument_SetLayerDataWritesTheElementFormWhenThatIsWhatTheFileUses(t *testing.T) {
	const src = `<map version="1.10" width="2" height="1" tilewidth="8" tileheight="8">
 <layer id="1" name="l" width="2" height="1">
  <data>
   <tile gid="1"/>
   <tile gid="0"/>
  </data>
 </layer>
</map>`
	doc, err := tiled.ParseDocument([]byte(src), "e.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetLayerData(0, []uint32{0, 3}); err != nil {
		t.Fatal(err)
	}
	back, err := tiled.ParseDocument(doc.Bytes(), "e.tmx")
	if err != nil {
		t.Fatalf("the element form did not parse back: %v\n%s", err, doc.Bytes())
	}
	m, err := back.Map()
	if err != nil {
		t.Fatal(err)
	}
	if m.Layers[0].Data[0] != 0 || m.Layers[0].Data[1] != 3 {
		t.Errorf("gids did not survive: %v", m.Layers[0].Data)
	}
}

func TestDocument_SetLayerDataRefusesWhatWouldBreakTheMap(t *testing.T) {
	doc, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		idx  int
		gids []uint32
		want string
	}{
		{"a layer that is not there", 4, []uint32{1, 1, 1, 1}, "layer 4"},
		{"a negative index", -1, []uint32{1, 1, 1, 1}, "layer -1"},
		{"too few cells", 0, []uint32{1, 1, 1}, "3"},
		{"too many cells", 0, []uint32{1, 1, 1, 1, 1}, "5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := doc.SetLayerData(tc.idx, tc.gids)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal %q does not mention %q", err, tc.want)
			}
		})
	}
	if string(doc.Bytes()) != richTMX {
		t.Error("a refused edit changed the document")
	}
}

// The layer index the document takes is the index into Map().Layers, and that
// list is flattened through <group> folders. A document that counted only
// top-level <layer> elements would paint the wrong layer in any map whose
// author had organised it.
func TestDocument_LayerIndexMatchesTheFlattenedMap(t *testing.T) {
	doc, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	m, err := doc.Map()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Layers) != 1 || m.Layers[0].Name != "ground" {
		t.Fatalf("the fixture's only layer is inside a folder: %+v", m.Layers)
	}
	if err := doc.SetLayerData(0, []uint32{2, 2, 2, 2}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc.Bytes()), "\n2,2,\n2,2\n") {
		t.Error("layer 0 is not the layer inside the folder")
	}
}

func TestDocument_ObjectIdsComeFromTheCounterAndNeverGoBack(t *testing.T) {
	doc, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.NextObjectID(); got != 42 {
		t.Fatalf("NextObjectID is %d, want 42", got)
	}
	first, err := doc.AddObject(0, tiled.Object{Type: "Goblin", X: 32, Y: 32})
	if err != nil {
		t.Fatal(err)
	}
	if first != 42 {
		t.Errorf("first new object got id %d, want 42", first)
	}
	if err := doc.RemoveObject(first); err != nil {
		t.Fatal(err)
	}
	second, err := doc.AddObject(0, tiled.Object{Type: "Goblin", X: 64, Y: 64})
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Errorf("a deleted object's id was handed out again: %d", second)
	}
	if got := doc.NextObjectID(); got != 44 {
		t.Errorf("NextObjectID is %d, want 44", got)
	}
	if !strings.Contains(string(doc.Bytes()), `nextobjectid="44"`) {
		t.Error("the counter was not written back to the file")
	}
}

func TestDocument_AMapWithNoCounterGetsOneAboveItsHighestObject(t *testing.T) {
	const src = `<map version="1.10" width="1" height="1" tilewidth="8" tileheight="8">
 <objectgroup id="1" name="spawns">
  <object id="17" type="Goblin" x="0" y="8"/>
 </objectgroup>
</map>`
	doc, err := tiled.ParseDocument([]byte(src), "n.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.NextObjectID(); got != 18 {
		t.Fatalf("NextObjectID is %d, want 18", got)
	}
	id, err := doc.AddObject(0, tiled.Object{Type: "Goblin", X: 0, Y: 8})
	if err != nil {
		t.Fatal(err)
	}
	if id != 18 {
		t.Errorf("got id %d, want 18", id)
	}
	if !strings.Contains(string(doc.Bytes()), `nextobjectid="19"`) {
		t.Errorf("the counter was not added to the map: %s", doc.Bytes())
	}
}

func TestDocument_AnAddedObjectIsASpawnTheEngineCanRead(t *testing.T) {
	doc, err := tiled.ParseDocument(shippedLevel(t), "level1.tmx")
	if err != nil {
		t.Fatal(err)
	}
	id, err := doc.AddObject(0, tiled.Object{
		Name: "watcher",
		Type: "Goblin",
		X:    96, Y: 128,
		Width: 32, Height: 32,
		Properties: tiled.Properties{
			"Health.hp":        {Type: "int", Value: "3"},
			"Sprite.animation": {Type: "string", Value: "goblin_idle"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := doc.Map()
	if err != nil {
		t.Fatal(err)
	}
	var found *tiled.Object
	for i, o := range m.ObjectGroups[0].Objects {
		if o.ID == id {
			found = &m.ObjectGroups[0].Objects[i]
		}
	}
	if found == nil {
		t.Fatalf("the new object is not in the map: %s", doc.Bytes())
	}
	if found.Type != "Goblin" || found.X != 96 || found.Y != 128 {
		t.Errorf("object came back as %+v", *found)
	}
	if got := found.Properties.Get("Health.hp"); got != "3" {
		t.Errorf("Health.hp came back as %q", got)
	}
	if got := found.Properties["Health.hp"].Type; got != "int" {
		t.Errorf("Health.hp is typed %q, want int", got)
	}
	if got := found.Properties["Sprite.animation"].Type; got != "string" {
		t.Errorf("Sprite.animation is typed %q, want string", got)
	}
}

func TestDocument_AddAndRemoveObjectRefuseWhatIsNotThere(t *testing.T) {
	doc, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddObject(3, tiled.Object{Type: "Goblin"}); err == nil {
		t.Error("adding to an object group that is not there was accepted")
	}
	if err := doc.RemoveObject(999); err == nil {
		t.Error("removing an object that is not there was accepted")
	}
	if string(doc.Bytes()) != richTMX {
		t.Error("a refused edit changed the document")
	}
}

func TestDocument_RemovingAnObjectLeavesTheRestOfTheGroupAlone(t *testing.T) {
	doc, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.RemoveObject(7); err != nil {
		t.Fatal(err)
	}
	// The whole document, not a handful of substrings. A contains-check would
	// pass with <editorsettings> dropped, the image layer gone or the
	// indentation collapsed — which is exactly what this story exists to catch.
	want := strings.Replace(richTMX, `  <object id="7" name="g" type="Goblin" x="32" y="64">
   <polygon points="0,0 8,0 8,8"/>
   <properties>
    <property name="Health.hp" type="int" value="5"/>
   </properties>
  </object>
`, "", 1)
	if got := string(doc.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDocument_MoveObjectPreservesIdentityAndUnknownContent(t *testing.T) {
	doc, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	beforeID := doc.NextObjectID()
	if err := doc.MoveObject(7, 16, 32); err != nil {
		t.Fatal(err)
	}
	got := string(doc.Bytes())
	if !strings.Contains(got, `<object id="7" name="g" type="Goblin" x="16" y="32">`) {
		t.Errorf("object coordinates changed incorrectly: %s", got)
	}
	if !strings.Contains(got, `<polygon points="0,0 8,0 8,8"/>`) ||
		!strings.Contains(got, `<property name="Health.hp" type="int" value="5"/>`) {
		t.Errorf("moving lost the object's unknown content: %s", got)
	}
	if doc.NextObjectID() != beforeID {
		t.Errorf("moving an object changed the next id: %d, want %d", doc.NextObjectID(), beforeID)
	}
	m, err := doc.Map()
	if err != nil {
		t.Fatal(err)
	}
	if o := m.ObjectGroups[0].Objects[0]; o.ID != 7 || o.X != 16 || o.Y != 32 {
		t.Errorf("parsed object after move: %+v", o)
	}
}

func TestDocument_MoveObjectRefusesInvalidRequestsWithoutChangingBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   int
		x, y float64
	}{
		{"missing object", 999, 16, 16},
		{"negative x", 7, -1, 16},
		{"past right edge", 7, 1000, 16},
		{"past bottom edge", 7, 16, 1000},
		{"NaN", 7, math.NaN(), 16},
		{"infinity", 7, 16, math.Inf(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
			if err != nil {
				t.Fatal(err)
			}
			before := string(doc.Bytes())
			if err := doc.MoveObject(tc.id, tc.x, tc.y); err == nil {
				t.Fatal("invalid move accepted")
			}
			if got := string(doc.Bytes()); got != before {
				t.Errorf("refused move changed bytes:\n%s", got)
			}
		})
	}
}

func TestDocument_SetObjectPropertyPreservesUnrelatedXML(t *testing.T) {
	doc, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetObjectProperty(7, "Health.hp", tiled.Property{Type: "int", Value: "9"}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(richTMX, `<property name="Health.hp" type="int" value="5"/>`,
		`<property name="Health.hp" type="int" value="9"/>`, 1)
	if got := string(doc.Bytes()); got != want {
		t.Errorf("one property edit touched something else:\n%s", got)
	}
	if err := doc.SetObjectProperty(7, "Health.hp", tiled.Property{Type: "int", Value: "9"}); err != nil {
		t.Fatal(err)
	}
	if got := string(doc.Bytes()); got != want {
		t.Error("setting the same value was not byte-idempotent")
	}
	if err := doc.SetObjectProperty(7, "Sprite.animation", tiled.Property{Value: "idle"}); err != nil {
		t.Fatal(err)
	}
	m, err := doc.Map()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ObjectGroups[0].Objects[0].Properties.Get("Sprite.animation"); got != "idle" {
		t.Errorf("new property read back as %q", got)
	}
	for _, fragment := range []string{
		`<polygon points="0,0 8,0 8,8"/>`,
		`<imagelayer id="7"`, `<object id="8"`, `nextobjectid="42"`,
	} {
		if !strings.Contains(string(doc.Bytes()), fragment) {
			t.Errorf("adding a property discarded %s", fragment)
		}
	}
	if err := doc.SetObjectProperty(7, "Health.hp", tiled.Property{Value: "a name"}); err != nil {
		t.Fatal(err)
	}
	m, err = doc.Map()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ObjectGroups[0].Objects[0].Properties["Health.hp"]; got.Type != "string" || got.Value != "a name" {
		t.Errorf("changing a property from int to string kept its old type: %+v", got)
	}
}

func TestDocument_RemoveObjectComponentLeavesOtherPropertiesAndShape(t *testing.T) {
	doc, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetObjectProperty(7, "Sprite.animation", tiled.Property{Value: "idle"}); err != nil {
		t.Fatal(err)
	}
	if err := doc.RemoveObjectComponent(7, "Health"); err != nil {
		t.Fatal(err)
	}
	m, err := doc.Map()
	if err != nil {
		t.Fatal(err)
	}
	p := m.ObjectGroups[0].Objects[0].Properties
	if p.Has("Health.hp") || p.Get("Sprite.animation") != "idle" {
		t.Errorf("removing Health left %+v", p)
	}
	if !strings.Contains(string(doc.Bytes()), `<polygon points="0,0 8,0 8,8"/>`) {
		t.Error("removing a component discarded the object shape")
	}
	after := string(doc.Bytes())
	if err := doc.RemoveObjectComponent(7, "Health"); err != nil {
		t.Fatal(err)
	}
	if string(doc.Bytes()) != after {
		t.Error("removing an absent component should be a no-op")
	}
}

func TestDocument_PropertyEditsRefuseAmbiguousOrUnknownTargets(t *testing.T) {
	duplicate := strings.Replace(richTMX, `<object id="8" name="note"`, `<object id="7" name="note"`, 1)
	for _, tc := range []struct {
		name string
		src  string
		edit func(*tiled.Document) error
	}{
		{"duplicate set", duplicate, func(d *tiled.Document) error {
			return d.SetObjectProperty(7, "Health.hp", tiled.Property{Type: "int", Value: "1"})
		}},
		{"duplicate remove", duplicate, func(d *tiled.Document) error {
			return d.RemoveObjectComponent(7, "Health")
		}},
		{"missing object", richTMX, func(d *tiled.Document) error {
			return d.SetObjectProperty(99, "Health.hp", tiled.Property{Type: "int", Value: "1"})
		}},
		{"invalid property", richTMX, func(d *tiled.Document) error {
			return d.SetObjectProperty(7, ".hp", tiled.Property{Type: "int", Value: "1"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tiled.ParseDocument([]byte(tc.src), "rich.tmx")
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.edit(d); err == nil {
				t.Fatal("invalid property edit accepted")
			}
			if string(d.Bytes()) != tc.src {
				t.Error("refused edit changed the document")
			}
		})
	}
}

func TestDocument_SetObjectPropertyRefusesNestedValueItCannotPreserve(t *testing.T) {
	src := strings.Replace(richTMX, `<property name="Health.hp" type="int" value="5"/>`,
		`<property name="Health.hp" type="class"><properties><property name="bonus" type="int" value="5"/></properties></property>`, 1)
	d, err := tiled.ParseDocument([]byte(src), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetObjectProperty(7, "Health.hp", tiled.Property{Type: "int", Value: "9"}); err == nil {
		t.Fatal("overwrote an authored nested property without a refusal")
	}
	if string(d.Bytes()) != src {
		t.Error("the refused edit lost a nested property")
	}
}

func TestDocument_SetObjectPropertyPreservesUnmodelledCommentBesideAttribute(t *testing.T) {
	src := strings.Replace(richTMX, `<property name="Health.hp" type="int" value="5"/>`,
		`<property name="Health.hp" type="int" value="5"><!-- human note --></property>`, 1)
	d, err := tiled.ParseDocument([]byte(src), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetObjectProperty(7, "Health.hp", tiled.Property{Type: "int", Value: "9"}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, `value="5"><!-- human note -->`, `value="9"><!-- human note -->`, 1)
	if got := string(d.Bytes()); got != want {
		t.Errorf("a property value edit lost unmodelled content:\n%s", got)
	}
}

func TestDocument_SetObjectPropertyKeepsElementContentSpelling(t *testing.T) {
	src := strings.Replace(richTMX, `<property name="Health.hp" type="int" value="5"/>`,
		`<property name="Health.hp" type="int">5</property>`, 1)
	d, err := tiled.ParseDocument([]byte(src), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetObjectProperty(7, "Health.hp", tiled.Property{Type: "int", Value: "9"}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, `<property name="Health.hp" type="int">5</property>`,
		`<property name="Health.hp" type="int">9</property>`, 1)
	if got := string(d.Bytes()); got != want {
		t.Errorf("element-content property changed format:\n%s", got)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ObjectGroups[0].Objects[0].Properties.Get("Health.hp"); got != "9" {
		t.Errorf("new value reads back as %q", got)
	}
}

func TestDocument_SetObjectTextPropertyEscapesOnceAndReadsTheMeaning(t *testing.T) {
	src := strings.Replace(richTMX, `<property name="Health.hp" type="int" value="5"/>`,
		`<property name="Health.hp">A &amp; B</property>`, 1)
	d, err := tiled.ParseDocument([]byte(src), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ObjectGroups[0].Objects[0].Properties.Get("Health.hp"); got != "A & B" {
		t.Errorf("XML content read as %q, want A & B", got)
	}
	if err := d.SetObjectProperty(7, "Health.hp", tiled.Property{Value: "C & D"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Bytes()), `>C &amp; D</property>`) {
		t.Errorf("edited text was not escaped once: %s", d.Bytes())
	}
	m, err = d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ObjectGroups[0].Objects[0].Properties.Get("Health.hp"); got != "C & D" {
		t.Errorf("edited text read back as %q, want C & D", got)
	}
}

func TestDocument_EmptyAttributeValueDoesNotBecomeItsComment(t *testing.T) {
	src := strings.Replace(richTMX, `<property name="Health.hp" type="int" value="5"/>`,
		`<property name="Sprite.sheet" value=""><!-- artist note --></property>`, 1)
	d, err := tiled.ParseDocument([]byte(src), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ObjectGroups[0].Objects[0].Properties.Get("Sprite.sheet"); got != "" {
		t.Errorf("empty authored value read as %q", got)
	}
}

const layeredTMX = `<?xml version="1.0" encoding="UTF-8"?>
<map width="1" height="1" tilewidth="16" tileheight="16" nextlayerid="5" nextobjectid="8">
 <group id="2" name="folder">
  <layer id="1" name="ground" width="1" height="1"><data encoding="csv">1</data></layer>
  <layer id="3" name="props" width="1" height="1"><data encoding="csv">2</data></layer>
 </group>
 <imagelayer id="5" name="fog"><image source="fog.png"/></imagelayer>
 <layer id="4" name="roof" width="1" height="1"><data encoding="csv">3</data></layer>
 <objectgroup id="6" name="spawns"><object id="7" type="Goblin" x="0" y="0"/></objectgroup>
</map>
`

func TestDocument_LayerEditsKeepUnknownMapContent(t *testing.T) {
	d, err := tiled.ParseDocument([]byte(layeredTMX), "layers.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.RenameLayer(1, "items"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(layeredTMX, `id="3" name="props"`, `id="3" name="items"`, 1)
	if string(d.Bytes()) != want {
		t.Errorf("renaming a layer changed more than its name:\n%s", d.Bytes())
	}
	if err := d.MoveLayer(1, -1); err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if m.Layers[0].Name != "items" || m.Layers[1].Name != "ground" || m.Layers[2].Name != "roof" {
		t.Errorf("file order not the engine's layer order: %+v", m.Layers)
	}
	if err := d.DeleteLayer(0); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(d.Bytes()), `id="3" name="items"`) ||
		!strings.Contains(string(d.Bytes()), `<imagelayer id="5"`) ||
		!strings.Contains(string(d.Bytes()), `<object id="7"`) ||
		!strings.Contains(string(d.Bytes()), `nextlayerid="5"`) {
		t.Errorf("deleting a layer changed unmodeled parts:\n%s", d.Bytes())
	}
}

func TestDocument_CanMoveLayerOnlyWithinItsFolder(t *testing.T) {
	d, err := tiled.ParseDocument([]byte(layeredTMX), "layers.tmx")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		index, direction int
		want             bool
	}{
		{0, -1, false},
		{0, 1, true},
		{1, -1, true},
		{1, 1, false},
		{2, -1, false},
		{2, 1, false},
	} {
		if got := d.CanMoveLayer(tc.index, tc.direction); got != tc.want {
			t.Errorf("CanMoveLayer(%d, %d) = %v, want %v", tc.index, tc.direction, got, tc.want)
		}
	}
}

func TestDocument_MoveLayerKeepsItsAuthoredComment(t *testing.T) {
	src := strings.Replace(layeredTMX, `<layer id="1" name="ground"`,
		`<!-- ground is walkable -->
  <layer id="1" name="ground"`, 1)
	src = strings.Replace(src, `<layer id="3" name="props"`,
		`<!-- props are scenery -->
  <layer id="3" name="props"`, 1)
	if !strings.Contains(src, "<!-- ground is walkable -->") || !strings.Contains(src, "<!-- props are scenery -->") {
		t.Fatal("comment fixture did not include both comments")
	}
	d, err := tiled.ParseDocument([]byte(src), "layers.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.MoveLayer(1, -1); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	if !strings.Contains(got, "<!-- props are scenery -->\n  <layer id=\"3\"") ||
		!strings.Contains(got, "<!-- ground is walkable -->\n  <layer id=\"1\"") ||
		strings.Index(got, "<!-- props are scenery -->") >= strings.Index(got, "<!-- ground is walkable -->") {
		t.Errorf("reordering detached a layer's comment:\n%s", got)
	}
}

func TestDocument_DeleteLayerAlsoRemovesItsAuthoredComment(t *testing.T) {
	src := strings.Replace(layeredTMX, `<layer id="3" name="props"`,
		`<!-- this comment belongs to props -->
  <layer id="3" name="props"`, 1)
	d, err := tiled.ParseDocument([]byte(src), "layers.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteLayer(1); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	if strings.Contains(got, "this comment belongs to props") || strings.Contains(got, `name="props"`) ||
		!strings.Contains(got, `name="ground"`) || !strings.Contains(got, `name="roof"`) {
		t.Errorf("deleting props left its commentary or removed a sibling:\n%s", got)
	}
}

func TestDocument_MoveLayerRefusesWhenLayerIDsCannotPreserveViewState(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"missing id", strings.Replace(layeredTMX, `id="3" name="props"`, `name="props"`, 1)},
		{"duplicate id", strings.Replace(layeredTMX, `id="3" name="props"`, `id="1" name="props"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tiled.ParseDocument([]byte(tc.source), "layers.tmx")
			if err != nil {
				t.Fatal(err)
			}
			if d.CanMoveLayer(1, -1) || d.CanDeleteLayer(0) || d.MoveLayer(1, -1) == nil || d.DeleteLayer(0) == nil {
				t.Error("an index-changing edit with no stable layer identity was accepted")
			}
			if string(d.Bytes()) != tc.source {
				t.Error("a refused move changed the TMX")
			}
		})
	}
}

func TestDocument_LayerEditsRefuseBoundariesAndCrossFolderMoves(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*tiled.Document) error
	}{
		{"missing", func(d *tiled.Document) error { return d.RenameLayer(9, "x") }},
		{"empty name", func(d *tiled.Document) error { return d.RenameLayer(0, "") }},
		{"duplicate name", func(d *tiled.Document) error { return d.RenameLayer(1, "ground") }},
		{"above first", func(d *tiled.Document) error { return d.MoveLayer(0, -1) }},
		{"below last", func(d *tiled.Document) error { return d.MoveLayer(2, 1) }},
		{"across folder", func(d *tiled.Document) error { return d.MoveLayer(1, 1) }},
		{"bad direction", func(d *tiled.Document) error { return d.MoveLayer(0, 2) }},
		{"delete missing", func(d *tiled.Document) error { return d.DeleteLayer(9) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tiled.ParseDocument([]byte(layeredTMX), "layers.tmx")
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.edit(d); err == nil {
				t.Fatal("invalid layer edit accepted")
			}
			if string(d.Bytes()) != layeredTMX {
				t.Error("refused layer edit changed the file")
			}
		})
	}
}

func TestDocument_DuplicateObjectKeepsItsUnmodeledChildrenAndGetsNewID(t *testing.T) {
	d, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
	if err != nil {
		t.Fatal(err)
	}
	id, err := d.DuplicateObject(7, 0, 32)
	if err != nil {
		t.Fatal(err)
	}
	if id != 42 || d.NextObjectID() != 43 {
		t.Fatalf("duplicate id %d, next %d", id, d.NextObjectID())
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.ObjectGroups[0].Objects) != 3 || m.ObjectGroups[0].Objects[2].ID != 42 ||
		m.ObjectGroups[0].Objects[2].X != 0 || m.ObjectGroups[0].Objects[2].Y != 32 {
		t.Errorf("duplicate object in wrong place: %+v", m.ObjectGroups[0].Objects)
	}
	if n := strings.Count(string(d.Bytes()), `<polygon points="0,0 8,0 8,8"/>`); n != 2 {
		t.Errorf("copied %d polygon bodies, want 2", n)
	}
	if n := strings.Count(string(d.Bytes()), `name="Health.hp" type="int" value="5"`); n != 2 {
		t.Errorf("copied %d authored Health properties, want 2", n)
	}
	if err := d.RemoveObject(id); err != nil || d.NextObjectID() != 43 {
		t.Errorf("deleting duplicate recycled id: %v, next %d", err, d.NextObjectID())
	}
}

func TestDocument_DuplicateObjectRefusesBeforeAllocatingAnID(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   int
		x, y float64
	}{
		{"missing", 999, 0, 0},
		{"outside", 7, 9999, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tiled.ParseDocument([]byte(richTMX), "rich.tmx")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.DuplicateObject(tc.id, tc.x, tc.y); err == nil {
				t.Fatal("invalid duplicate was accepted")
			}
			if string(d.Bytes()) != richTMX || d.NextObjectID() != 42 {
				t.Error("a refused duplicate changed the source or advanced the counter")
			}
		})
	}
}

func TestNewDocument_WritesAMapTheEngineCanLoad(t *testing.T) {
	doc, err := tiled.NewDocument("level2.tmx", tiled.NewMapSpec{
		MapID: "level2", Width: 4, Height: 3, TileWidth: 32, TileHeight: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := doc.Map()
	if err != nil {
		t.Fatalf("the map it wrote does not parse: %v\n%s", err, doc.Bytes())
	}
	if m.Width != 4 || m.Height != 3 || m.TileWidth != 32 {
		t.Errorf("map is %dx%d at %dpx", m.Width, m.Height, m.TileWidth)
	}
	if got := m.Properties.Get(tiled.PropMapID); got != "level2" {
		t.Errorf("mapId is %q, want level2", got)
	}
	if len(m.Layers) != 1 || len(m.Layers[0].Data) != 12 {
		t.Errorf("layers: %+v", m.Layers)
	}
	if len(m.ObjectGroups) != 1 {
		t.Errorf("object groups: %+v", m.ObjectGroups)
	}
	// It has to survive the round trip it will immediately be given.
	if _, err := tiled.ParseDocument(doc.Bytes(), "level2.tmx"); err != nil {
		t.Fatalf("re-reading the new map failed: %v", err)
	}
	id, err := doc.AddObject(0, tiled.Object{Type: "Player", X: 0, Y: 32})
	if err != nil {
		t.Fatalf("a new map cannot take a spawn: %v", err)
	}
	if id != 1 {
		t.Errorf("first object of a new map got id %d, want 1", id)
	}
}

func TestNewDocument_RefusesAMapThatWouldBeATrap(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec tiled.NewMapSpec
		want string
	}{
		{"no mapId", tiled.NewMapSpec{Width: 2, Height: 2, TileWidth: 8, TileHeight: 8}, "mapId"},
		{"no size", tiled.NewMapSpec{MapID: "a", TileWidth: 8, TileHeight: 8}, "size"},
		{"no tile size", tiled.NewMapSpec{MapID: "a", Width: 2, Height: 2}, "pixels"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tiled.NewDocument("x.tmx", tc.spec)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A new spawn has to land in the file looking like the ones already in it. This
// caught a real defect: the <properties> block was filled before it was
// attached, so every property — and the closing tag — was written hard against
// the left margin of a file indented three levels in.
func TestDocument_AnAddedObjectIsIndentedLikeTheOnesAlreadyThere(t *testing.T) {
	const src = `<map version="1.10" width="1" height="1" tilewidth="8" tileheight="8" nextobjectid="100">
 <objectgroup id="1" name="spawns">
  <object id="99" type="Player" x="0" y="8"/>
 </objectgroup>
</map>
`
	doc, err := tiled.ParseDocument([]byte(src), "p.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddObject(0, tiled.Object{
		Type: "Goblin", X: 32, Y: 64,
		Properties: tiled.Properties{"Health.hp": {Type: "int", Value: "5"}},
	}); err != nil {
		t.Fatal(err)
	}
	want := `<map version="1.10" width="1" height="1" tilewidth="8" tileheight="8" nextobjectid="101">
 <objectgroup id="1" name="spawns">
  <object id="99" type="Player" x="0" y="8"/>
  <object id="100" type="Goblin" x="32" y="64">
   <properties>
    <property name="Health.hp" type="int" value="5"/>
   </properties>
  </object>
 </objectgroup>
</map>
`
	if got := string(doc.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Indentation is read from the file rather than assumed, so a tab-indented map
// stays tab-indented. Tiled indents with a single space; a hand-written or
// generated map need not.
func TestDocument_AnAddedObjectTakesTheFilesOwnIndentation(t *testing.T) {
	const src = "<map version=\"1.10\" width=\"1\" height=\"1\" tilewidth=\"8\" tileheight=\"8\" nextobjectid=\"100\">\n\t<objectgroup id=\"1\" name=\"spawns\">\n\t\t<object id=\"99\" type=\"Player\" x=\"0\" y=\"8\"/>\n\t</objectgroup>\n</map>\n"
	doc, err := tiled.ParseDocument([]byte(src), "t.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddObject(0, tiled.Object{
		Type:       "Goblin",
		X:          32,
		Y:          64,
		Properties: tiled.Properties{"Health.hp": {Type: "int", Value: "5"}},
	}); err != nil {
		t.Fatal(err)
	}
	out := string(doc.Bytes())
	if !strings.Contains(out, "\n\t\t\t\t<property name=\"Health.hp\"") {
		t.Errorf("the new property is not four tabs in:\n%s", out)
	}
	if strings.Contains(out, "\n ") {
		t.Errorf("a tab-indented map gained space indentation:\n%s", out)
	}
}

// A stroke that paints a whole layer must not reshape the file around it. The
// element form is the one where that is easy to get wrong, because the tiles
// are elements rather than text.
func TestDocument_TheElementFormKeepsTheFilesShape(t *testing.T) {
	const src = `<map version="1.10" width="2" height="1" tilewidth="8" tileheight="8">
 <layer id="1" name="l" width="2" height="1">
  <data>
   <tile gid="1"/>
   <tile gid="0"/>
  </data>
 </layer>
</map>
`
	doc, err := tiled.ParseDocument([]byte(src), "e.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.SetLayerData(0, []uint32{0, 3}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, "   <tile gid=\"1\"/>\n   <tile gid=\"0\"/>\n", "   <tile gid=\"0\"/>\n   <tile gid=\"3\"/>\n", 1)
	if got := string(doc.Bytes()); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestDocument_ASpawnWithNoPropertiesGetsNoPropertiesBlock(t *testing.T) {
	const src = `<map version="1.10" width="1" height="1" tilewidth="8" tileheight="8" nextobjectid="1">
 <objectgroup id="1" name="spawns"/>
</map>
`
	doc, err := tiled.ParseDocument([]byte(src), "p.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddObject(0, tiled.Object{Type: "Goblin", X: 0, Y: 8}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(doc.Bytes()), "properties") {
		t.Errorf("an object with nothing to say got a properties block:\n%s", doc.Bytes())
	}
}

// The counter is bookkeeping and bookkeeping falls behind. Two branches each
// place a spawn: the objects land on different lines and merge cleanly, while
// nextobjectid is one line and git takes one side of it. The map then says 3
// with an object 4 in it — and because spawns is keyed (map, object_id), a
// second object 3 does not create an entity, it moves the one the first object
// made.
func TestDocument_ACounterThatHasFallenBehindDoesNotHandOutAnIdInUse(t *testing.T) {
	const src = `<map version="1.10" width="1" height="1" tilewidth="8" tileheight="8" nextobjectid="2">
 <objectgroup id="1" name="spawns">
  <object id="2" type="Player" x="0" y="8"/>
  <object id="7" type="Goblin" x="0" y="8"/>
 </objectgroup>
</map>
`
	doc, err := tiled.ParseDocument([]byte(src), "stale.tmx")
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.NextObjectID(); got != 8 {
		t.Errorf("NextObjectID is %d, want 8 — one past the highest object in the file", got)
	}
	id, err := doc.AddObject(0, tiled.Object{Type: "Goblin", X: 8, Y: 8})
	if err != nil {
		t.Fatal(err)
	}
	if id != 8 {
		t.Fatalf("got id %d, want 8", id)
	}
	m, err := doc.Map()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, o := range m.ObjectGroups[0].Objects {
		if seen[o.ID] {
			t.Fatalf("two objects share id %d:\n%s", o.ID, doc.Bytes())
		}
		seen[o.ID] = true
	}
}

// The same claim tileLayers makes, for the other flattener: an object group
// inside a layer folder is addressable, and it is addressable at the index
// Map().ObjectGroups gives it.
func TestDocument_ObjectGroupIndexMatchesTheFlattenedMap(t *testing.T) {
	const src = `<map version="1.10" width="1" height="1" tilewidth="8" tileheight="8" nextobjectid="1">
 <group id="9" name="folder">
  <objectgroup id="1" name="spawns"/>
 </group>
</map>
`
	doc, err := tiled.ParseDocument([]byte(src), "f.tmx")
	if err != nil {
		t.Fatal(err)
	}
	m, err := doc.Map()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.ObjectGroups) != 1 || m.ObjectGroups[0].Name != "spawns" {
		t.Fatalf("the fixture's only object group is inside a folder: %+v", m.ObjectGroups)
	}
	if _, err := doc.AddObject(0, tiled.Object{Type: "Goblin", X: 0, Y: 8}); err != nil {
		t.Fatal(err)
	}
	out := string(doc.Bytes())
	if !strings.Contains(out, `<group id="9" name="folder">`) {
		t.Errorf("the folder was unpacked:\n%s", out)
	}
	if !strings.Contains(out, "\n   <object id=\"1\" type=\"Goblin\"") {
		t.Errorf("the spawn did not land inside the folder's group:\n%s", out)
	}
}

func TestDocument_SetLayerDataRefusesALayerItCannotWriteTo(t *testing.T) {
	t.Run("a layer with no data element", func(t *testing.T) {
		// A 0x0 map is the one shape that reaches this: checkCells is satisfied
		// by a layer with no tiles when the layer has no cells.
		doc, err := tiled.ParseDocument([]byte(
			`<map version="1.10" width="0" height="0" tilewidth="8" tileheight="8">`+
				`<layer id="1" name="l" width="0" height="0"/></map>`), "z.tmx")
		if err != nil {
			t.Fatal(err)
		}
		err = doc.SetLayerData(0, nil)
		if err == nil || !strings.Contains(err.Error(), "<data>") {
			t.Errorf("refusal was %v", err)
		}
	})
	t.Run("csv that claims to be compressed", func(t *testing.T) {
		// decodeCSV ignores compression, so such a file reads; the encoder
		// refuses rather than writing something the attributes contradict.
		doc, err := tiled.ParseDocument([]byte(
			`<map version="1.10" width="2" height="1" tilewidth="8" tileheight="8">`+
				`<layer id="1" name="l" width="2" height="1">`+
				`<data encoding="csv" compression="zlib">1,2</data></layer></map>`), "c.tmx")
		if err != nil {
			t.Fatal(err)
		}
		err = doc.SetLayerData(0, []uint32{3, 4})
		if err == nil || !strings.Contains(err.Error(), "zlib") {
			t.Errorf("refusal was %v", err)
		}
	})
}

// A coordinate is written as a number, at every magnitude. Go's 'g' verb
// switches to exponent notation at a million — out of reach for any map this
// engine draws, and the point of a format function is that it does not have a
// range outside which it means something else.
func TestDocument_ALargeCoordinateIsStillWrittenAsANumber(t *testing.T) {
	doc, err := tiled.NewDocument("big.tmx", tiled.NewMapSpec{
		MapID: "big", Width: 1, Height: 1, TileWidth: 32, TileHeight: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddObject(0, tiled.Object{Type: "Goblin", X: 1000000, Y: 2500000.5}); err != nil {
		t.Fatal(err)
	}
	out := string(doc.Bytes())
	if !strings.Contains(out, `x="1000000" y="2500000.5"`) {
		t.Errorf("coordinates were not written as numbers:\n%s", out)
	}
}
