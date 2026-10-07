package tiled_test

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

// A TSX with everything the engine's reading model intentionally drops. The
// untouched bytes, not the parsed Tileset, are the contract the writer keeps.
const richTSX = `<?xml version="1.0" encoding="UTF-8"?>
<!-- an artist's tileset -->
<tileset version="1.10" name="artist" class="Floor" tilewidth="16" tileheight="16" tilecount="4" columns="2" spacing="0" margin="0">
 <editorsettings><export target="layout.json" format="json"/></editorsettings>
 <properties><property name="passable" type="bool" value="true"/></properties>
 <image source="tiles.png" width="32" height="32"/>
 <tile id="1" type="wall" probability="0.7">
  <properties>
   <property name="passable" type="bool" value="false"/>
   <property name="artistNote" value="keep this"/>
  </properties>
  <objectgroup id="3"><object id="4" x="0" y="0"><polygon points="0,0 8,0 8,8"/></object></objectgroup>
  <animation><frame tileid="2" duration="80"/></animation>
 </tile>
 <wangsets><wangset name="seam" type="corner"/></wangsets>
 <terraintypes><terrain name="grass"/></terraintypes>
</tileset>
`

const collectionTSXDocument = `<?xml version="1.0"?>
<tileset name="props" tilewidth="16" tileheight="16" tilecount="2" columns="0">
 <tile id="0"><image source="barrel.png" width="16" height="16"/></tile>
 <tile id="3" class="crate"><image source="crate.png" width="16" height="16"/></tile>
</tileset>
`

func TestParseTilesetDocument_RoundTripsUnknownTSXAndCollectionExactly(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"rich sheet", richTSX},
		{"sparse image collection", collectionTSXDocument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tiled.ParseTilesetDocument([]byte(tc.src), "tiles.tsx", "project/tiles")
			if err != nil {
				t.Fatal(err)
			}
			if got := string(d.Bytes()); got != tc.src {
				t.Errorf("unchanged tileset altered authored bytes:\n%s", got)
			}
			set, err := d.Tileset()
			if err != nil || set.Name == "" {
				t.Errorf("writer has no usable reading model: %+v, %v", set, err)
			}
		})
	}
}

func TestParseTilesetDocument_RefusesUnsavableFormats(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"JSON", `{"name":"props","tilewidth":16,"tileheight":16,"tilecount":1,"columns":1}`, ".tsx"},
		{"broken XML", `<tileset name="unfinished">`, "XML"},
		{"wrong root", `<map width="1" height="1"/>`, "<tileset>"},
		{"invalid tileset", `<tileset name="bad" tilewidth="0" tileheight="16" tilecount="1" columns="1"/>`, "size"},
		{"duplicate tile id", `<tileset name="bad" tilewidth="16" tileheight="16" tilecount="2" columns="0"><tile id="0"/><tile id="0"/></tileset>`, "two tiles both claim id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tiled.ParseTilesetDocument([]byte(tc.src), "tiles.tsx", "."); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseTilesetDocument_EmbeddedTilesetIsNotAnExternalTSXFile(t *testing.T) {
	if _, err := tiled.ParseTilesetDocument([]byte(richTSX), "level.tmx", "project/maps"); err == nil ||
		!strings.Contains(err.Error(), ".tsx") {
		t.Errorf("embedded tileset was offered an external TSX writer: %v", err)
	}
}

func TestTilesetDocument_SetTilePropertyKeepsUnknownXMLAndReadsBack(t *testing.T) {
	for _, tc := range []struct{ name, src, before, after string }{
		{"attribute", richTSX, `name="passable" type="bool" value="false"`, `name="passable" type="bool" value="true"`},
		{
			"element content", strings.Replace(richTSX, `<property name="passable" type="bool" value="false"/>`,
				`<property name="passable" type="bool">false</property>`, 1),
			`<property name="passable" type="bool">false</property>`, `<property name="passable" type="bool">true</property>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tiled.ParseTilesetDocument([]byte(tc.src), "artist.tsx", "project/tiles")
			if err != nil {
				t.Fatal(err)
			}
			if err := d.SetTileProperty(1, tiled.PropPassable, tiled.Property{Type: "bool", Value: "true"}); err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(tc.src, tc.before, tc.after, 1)
			if got := string(d.Bytes()); got != want {
				t.Errorf("changing one tile property rewrote unknown TSX:\n%s", got)
			}
			set, err := d.Tileset()
			if err != nil {
				t.Fatal(err)
			}
			if passable, setByTile := set.Passable(1); !passable || !setByTile {
				t.Errorf("the engine's reading model did not see the edit: %v, %v", passable, setByTile)
			}
			if err := d.SetTileProperty(1, tiled.PropPassable, tiled.Property{Type: "bool", Value: "true"}); err != nil || string(d.Bytes()) != want {
				t.Errorf("repeating the same edit wrote new bytes: %v", err)
			}
		})
	}
}

func TestTilesetDocument_ChildEditPreservesAuthoredTagQuotingAndEntities(t *testing.T) {
	src := strings.Replace(richTSX, `version="1.10" name="artist"`, `version='1.10' name='ar&#116;ist'`, 1)
	src = strings.Replace(src, `tile id="1" type="wall" probability="0.7"`, `tile id='1' type='wall' probability='0.7'`, 1)
	d, err := tiled.ParseTilesetDocument([]byte(src), "artist.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileProperty(1, tiled.PropPassable, tiled.Property{Type: "bool", Value: "true"}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, `name="passable" type="bool" value="false"`, `name="passable" type="bool" value="true"`, 1)
	if got := string(d.Bytes()); got != want {
		t.Errorf("child edit respelled TSX tags it did not touch:\n%s", got)
	}
}

func TestTilesetDocument_ChangedPropertyKeepsOtherAttributesVerbatim(t *testing.T) {
	src := strings.Replace(richTSX, `<property name="passable" type="bool" value="false"/>`,
		`<property name='passable' type='bool' value='false' artist='a&#38;b'/>`, 1)
	d, err := tiled.ParseTilesetDocument([]byte(src), "artist.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileProperty(1, tiled.PropPassable, tiled.Property{Type: "bool", Value: "true"}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, `value='false'`, `value='true'`, 1)
	if got := string(d.Bytes()); got != want {
		t.Errorf("editing one value respelled the other attributes on the same tag:\n%s", got)
	}
}

func TestTilesetDocument_EditInsideSingleQuotedAttributeEscapesApostrophe(t *testing.T) {
	src := strings.Replace(richTSX, `tile id="1" type="wall"`, `tile id='1' type='wall'`, 1)
	d, err := tiled.ParseTilesetDocument([]byte(src), "artist.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileClass(1, "it's a wall"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, `type='wall'`, `type='it&apos;s a wall'`, 1)
	if got := string(d.Bytes()); got != want {
		t.Errorf("attribute quote escaped or respelled incorrectly:\n%s", got)
	}
	set, err := d.Tileset()
	if err != nil || set.Tiles[1].Type != "it's a wall" {
		t.Errorf("escaped class does not read back: %+v, %v", set, err)
	}
}

func TestTilesetDocument_SameElementContentValueDoesNotReencodeEntities(t *testing.T) {
	src := strings.Replace(richTSX, `<property name="artistNote" value="keep this"/>`,
		`<property name="artistNote">a&#38;b</property>`, 1)
	d, err := tiled.ParseTilesetDocument([]byte(src), "artist.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileProperty(1, "artistNote", tiled.Property{Type: "string", Value: "a&b"}); err != nil || string(d.Bytes()) != src {
		t.Errorf("no-op re-escaped author-selected entity spelling: %v", err)
	}
}

func TestTilesetDocument_DualClassAttributesCannotSilentlyKeepTheOldClass(t *testing.T) {
	for _, attrs := range []string{`type="wall" class="other"`, `class="other" type="wall"`} {
		src := strings.Replace(richTSX, `type="wall" probability="0.7"`, attrs+` probability="0.7"`, 1)
		for _, next := range []string{"door", ""} {
			d, err := tiled.ParseTilesetDocument([]byte(src), "artist.tsx", "project/tiles")
			if err != nil {
				t.Fatal(err)
			}
			if err := d.SetTileClass(1, next); err == nil || string(d.Bytes()) != src {
				t.Errorf("dual-class %q accepted %q without making the readback unambiguous: %v", attrs, next, err)
			}
		}
	}
}

func TestTilesetDocument_AuthoredTilePastSheetEndCannotBeEdited(t *testing.T) {
	src := strings.Replace(richTSX, `<wangsets>`, `<tile id="5" type="phantom"/><wangsets>`, 1)
	for _, edit := range []struct {
		name  string
		apply func(*tiled.TilesetDocument) error
	}{
		{"property", func(d *tiled.TilesetDocument) error { return d.SetTileProperty(5, "note", tiled.Property{Value: "x"}) }},
		{"class", func(d *tiled.TilesetDocument) error { return d.SetTileClass(5, "new") }},
		{"clear class", func(d *tiled.TilesetDocument) error { return d.SetTileClass(5, "") }},
	} {
		t.Run(edit.name, func(t *testing.T) {
			d, err := tiled.ParseTilesetDocument([]byte(src), "artist.tsx", "project/tiles")
			if err != nil {
				t.Fatal(err)
			}
			if err := edit.apply(d); err == nil || string(d.Bytes()) != src {
				t.Errorf("metadata beyond tilecount edited: %v", err)
			}
		})
	}
}

func TestTilesetDocument_SetTilePropertyCreatesAnImplicitSheetTileInFileOrder(t *testing.T) {
	d, err := tiled.ParseTilesetDocument([]byte(richTSX), "artist.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileProperty(2, tiled.PropPassable, tiled.Property{Type: "bool", Value: "false"}); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	for _, fragment := range []string{
		`<tile id="2">`, `name="passable" type="bool" value="false"`,
		`<objectgroup id="3">`, `<animation><frame tileid="2" duration="80"/></animation>`, `<wangsets>`,
	} {
		if !strings.Contains(got, fragment) {
			t.Errorf("new tile discarded %s", fragment)
		}
	}
	if strings.Index(got, `<tile id="1"`) >= strings.Index(got, `<tile id="2"`) ||
		strings.Index(got, `<tile id="2"`) >= strings.Index(got, `<wangsets>`) {
		t.Errorf("new tile not among its siblings in ID order:\n%s", got)
	}
	set, err := d.Tileset()
	if err != nil {
		t.Fatal(err)
	}
	if passable, present := set.Passable(2); passable || !present {
		t.Errorf("new implicit tile did not read as impassable: %v, %v", passable, present)
	}
	after := got
	if err := d.SetTileProperty(4, tiled.PropPassable, tiled.Property{Type: "bool", Value: "false"}); err == nil || string(d.Bytes()) != after {
		t.Errorf("out-of-range tile edit changed the sheet: %v", err)
	}
}

func TestTilesetDocument_CollectionEditsOnlyItsDeclaredSparseIDs(t *testing.T) {
	d, err := tiled.ParseTilesetDocument([]byte(collectionTSXDocument), "props.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileProperty(3, "loot", tiled.Property{Type: "int", Value: "2"}); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); !strings.Contains(got, `id="3" class="crate"`) ||
		!strings.Contains(got, `source="crate.png"`) || !strings.Contains(got, `name="loot" type="int" value="2"`) {
		t.Errorf("editing a collection tile lost its picture/class: %s", got)
	}
	_, crate, _ := strings.Cut(string(d.Bytes()), `<tile id="3"`)
	if strings.Index(crate, `<properties>`) > strings.Index(crate, `<image source="crate.png"`) {
		t.Error("a new tile property block was written after the tile's image")
	}
	set, err := d.Tileset()
	if err != nil || set.Tiles[3].Properties.Get("loot") != "2" {
		t.Errorf("collection property not read back: %+v, %v", set, err)
	}
	before := string(d.Bytes())
	if err := d.SetTileProperty(1, "loot", tiled.Property{Type: "int", Value: "2"}); err == nil || string(d.Bytes()) != before {
		t.Errorf("collection invented undeclared tile 1: %v", err)
	}
}

func TestTilesetDocument_LocalIDMatchesNoncanonicalSourceSpelling(t *testing.T) {
	src := strings.Replace(collectionTSXDocument, `<tile id="3" class="crate"`, `<tile id="03" class="crate"`, 1)
	d, err := tiled.ParseTilesetDocument([]byte(src), "props.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileClass(3, "chest"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Bytes()), `<tile id="03" class="chest"`) {
		t.Errorf("writer did not target the authored local ID or preserve its spelling: %s", d.Bytes())
	}
}

func TestTilesetDocument_SetTileClassKeepsAuthoredSpellingAndTilesetClass(t *testing.T) {
	for _, tc := range []struct {
		name, src, before, after string
		id                       uint32
		class                    string
	}{
		{"type spelling", richTSX, `tile id="1" type="wall"`, `tile id="1" type="door"`, 1, "door"},
		{"class spelling", collectionTSXDocument, `tile id="3" class="crate"`, `tile id="3" class="chest"`, 3, "chest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tiled.ParseTilesetDocument([]byte(tc.src), "tiles.tsx", "project/tiles")
			if err != nil {
				t.Fatal(err)
			}
			if err := d.SetTileClass(tc.id, tc.class); err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(tc.src, tc.before, tc.after, 1)
			if got := string(d.Bytes()); got != want {
				t.Errorf("class edit changed more than the authored value:\n%s", got)
			}
			set, err := d.Tileset()
			if err != nil || set.Tiles[tc.id].Type != tc.class {
				t.Errorf("engine does not read new class: %+v, %v", set, err)
			}
			if tc.id == 1 && set.Class != "Floor" {
				t.Errorf("tile edit changed tileset-level class to %q", set.Class)
			}
			if err := d.SetTileClass(tc.id, tc.class); err != nil || string(d.Bytes()) != want {
				t.Errorf("same class edit rewrote bytes: %v", err)
			}
		})
	}
}

func TestTilesetDocument_SetTileClassCreatesAnImplicitSheetTile(t *testing.T) {
	d, err := tiled.ParseTilesetDocument([]byte(richTSX), "artist.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileClass(0, "grass"); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	if !strings.Contains(got, `<tile id="0" type="grass"/>`) ||
		strings.Index(got, `<tile id="0"`) > strings.Index(got, `<tile id="1"`) {
		t.Errorf("implicit tile not inserted before its siblings:\n%s", got)
	}
	set, err := d.Tileset()
	if err != nil || set.Tiles[0].Type != "grass" {
		t.Errorf("class readback = %+v, %v", set, err)
	}
}

func TestTilesetDocument_EmptyClassOnImplicitTileIsAnExactNoOp(t *testing.T) {
	d, err := tiled.ParseTilesetDocument([]byte(richTSX), "artist.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileClass(0, ""); err != nil || string(d.Bytes()) != richTSX {
		t.Errorf("clearing a class no sheet tile declared wrote metadata: %v", err)
	}
}

func TestTilesetDocument_SetTileClassRefusesMissingOrUnserializableTargets(t *testing.T) {
	for _, tc := range []struct {
		name, src, class string
		id               uint32
	}{
		{"missing collection id", collectionTSXDocument, "crate", 1},
		{"missing collection id with empty class", collectionTSXDocument, "", 1},
		{"past sheet end", richTSX, "grass", 4},
		{"invalid XML value", richTSX, "bad\x00class", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tiled.ParseTilesetDocument([]byte(tc.src), "tiles.tsx", "project/tiles")
			if err != nil {
				t.Fatal(err)
			}
			if err := d.SetTileClass(tc.id, tc.class); err == nil || string(d.Bytes()) != tc.src {
				t.Errorf("refused class edit wrote TSX: %v", err)
			}
		})
	}
}

func TestTilesetDocument_ClearingAClassRestoresTilesetClassFallback(t *testing.T) {
	d, err := tiled.ParseTilesetDocument([]byte(richTSX), "artist.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileClass(1, ""); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(richTSX, `tile id="1" type="wall"`, `tile id="1"`, 1)
	if got := string(d.Bytes()); got != want {
		t.Errorf("clearing the tile class rewrote other metadata:\n%s", got)
	}
	set, err := d.Tileset()
	if err != nil || set.Tiles[1].Type != "" || set.Class != "Floor" {
		t.Errorf("tile's fallback class is wrong: %+v, %v", set, err)
	}
}

func TestTilesetDocument_AddsFirstTileToAnOtherwiseEmptySheet(t *testing.T) {
	const src = `<tileset name="sheet" tilewidth="8" tileheight="8" tilecount="1" columns="1"><image source="tiles.png" width="8" height="8"/></tileset>`
	d, err := tiled.ParseTilesetDocument([]byte(src), "sheet.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileClass(0, "floor"); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); !strings.Contains(got, `<tile id="0" type="floor"/>`) ||
		strings.Index(got, `<tile id="0"`) < strings.Index(got, `<image source=`) {
		t.Errorf("new tile metadata was not appended after the sheet image: %s", got)
	}
}

func TestTilesetDocument_RefusesUnsafeTilePropertyBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		id   uint32
		prop string
		val  tiled.Property
	}{
		{"empty name", richTSX, 2, "", tiled.Property{Value: "value"}},
		{"collection gap", collectionTSXDocument, 1, "loot", tiled.Property{Value: "2"}},
		{"duplicate property", strings.Replace(richTSX, `<property name="artistNote" value="keep this"/>`, `<property name="passable" value="oops"/>`, 1), 1, tiled.PropPassable, tiled.Property{Type: "bool", Value: "true"}},
		{"nested value", strings.Replace(richTSX, `<property name="passable" type="bool" value="false"/>`, `<property name="passable" type="class"><properties><property name="reason" value="drawn"/></properties></property>`, 1), 1, tiled.PropPassable, tiled.Property{Type: "bool", Value: "true"}},
		{"invalid XML value", richTSX, 2, "artistNote", tiled.Property{Value: "bad\x00text"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tiled.ParseTilesetDocument([]byte(tc.src), "tiles.tsx", "project/tiles")
			if err != nil {
				t.Fatal(err)
			}
			if err := d.SetTileProperty(tc.id, tc.prop, tc.val); err == nil {
				t.Fatal("unsafe TSX edit accepted")
			}
			if got := string(d.Bytes()); got != tc.src {
				t.Errorf("refused edit changed tileset bytes:\n%s", got)
			}
		})
	}
}

func TestTilesetDocument_DuplicatePropertyBlocksCannotHideAnAcceptedEdit(t *testing.T) {
	src := strings.Replace(richTSX, `  </properties>
  <objectgroup`, `  </properties>
  <properties><property name="passable" type="bool" value="true"/></properties>
  <objectgroup`, 1)
	if src == richTSX {
		t.Fatal("fixture did not acquire a second tile property block")
	}
	d, err := tiled.ParseTilesetDocument([]byte(src), "artist.tsx", "project/tiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTileProperty(1, tiled.PropPassable, tiled.Property{Type: "bool", Value: "false"}); err == nil ||
		string(d.Bytes()) != src {
		t.Errorf("edited only the first of two conflicting property blocks: %v", err)
	}
}
