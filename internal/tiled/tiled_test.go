package tiled_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

// A 3×2 map, one tile layer, CSV. Small enough to assert every cell.
const tmxCSV = `<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" renderorder="right-down"
     width="3" height="2" tilewidth="16" tileheight="16" infinite="0">
 <properties>
  <property name="author" value="tom"/>
  <property name="level" type="int" value="1"/>
  <property name="gravity" type="float" value="9.5"/>
  <property name="dark" type="bool" value="true"/>
 </properties>
 <tileset firstgid="1" source="dungeon.tsx"/>
 <layer id="1" name="floor" width="3" height="2">
  <data encoding="csv">
1,2,3,
4,5,0
</data>
 </layer>
</map>
`

// The same map as TMJ.
const tmjCSV = `{
  "version": "1.10", "orientation": "orthogonal", "renderorder": "right-down",
  "width": 3, "height": 2, "tilewidth": 16, "tileheight": 16, "infinite": false,
  "properties": [
    {"name": "author", "type": "string", "value": "tom"},
    {"name": "level", "type": "int", "value": 1},
    {"name": "gravity", "type": "float", "value": 9.5},
    {"name": "dark", "type": "bool", "value": true}
  ],
  "tilesets": [{"firstgid": 1, "source": "dungeon.tsx"}],
  "layers": [
    {"id": 1, "name": "floor", "type": "tilelayer", "width": 3, "height": 2,
     "visible": true, "opacity": 1, "data": [1,2,3,4,5,0]}
  ]
}
`

// tmxFull and tmjFull are the same map written both ways, carrying one of
// everything this package models — so comparing them whole is a real check
// rather than a check of the six fields somebody thought of.
const tmxFull = `<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" renderorder="right-down"
     width="3" height="2" tilewidth="16" tileheight="16" infinite="0">
 <properties>
  <property name="author" value="tom"/>
  <property name="level" type="int" value="1"/>
  <property name="gravity" type="float" value="9"/>
  <property name="dark" type="bool" value="true"/>
 </properties>
 <tileset firstgid="1" source="dungeon.tsx"/>
 <tileset firstgid="65" source="props.tsx"/>
 <layer id="1" name="floor" width="3" height="2" opacity="0.5">
  <properties><property name="depth" type="int" value="0"/></properties>
  <data encoding="csv">1,2,3,4,5,0</data>
 </layer>
 <layer id="2" name="walls" width="3" height="2" visible="0">
  <data encoding="csv">0,0,0,0,0,66</data>
 </layer>
 <objectgroup id="3" name="spawns">
  <properties><property name="note" value="start here"/></properties>
  <object id="7" name="hero" type="Player" x="32" y="48" width="16" height="16" rotation="90">
   <properties><property name="hp" type="int" value="10"/></properties>
  </object>
  <object id="8" name="ghost" type="Goblin" x="0" y="16" visible="0"/>
 </objectgroup>
</map>
`

const tmjFull = `{
  "type": "map", "version": "1.10", "orientation": "orthogonal", "renderorder": "right-down",
  "width": 3, "height": 2, "tilewidth": 16, "tileheight": 16, "infinite": false,
  "properties": [
    {"name": "author", "type": "string", "value": "tom"},
    {"name": "level", "type": "int", "value": 1},
    {"name": "gravity", "type": "float", "value": 9.0},
    {"name": "dark", "type": "bool", "value": true}
  ],
  "tilesets": [{"firstgid": 1, "source": "dungeon.tsx"}, {"firstgid": 65, "source": "props.tsx"}],
  "layers": [
    {"id": 1, "name": "floor", "type": "tilelayer", "width": 3, "height": 2,
     "visible": true, "opacity": 0.5, "data": [1,2,3,4,5,0],
     "properties": [{"name": "depth", "type": "int", "value": 0}]},
    {"id": 2, "name": "walls", "type": "tilelayer", "width": 3, "height": 2,
     "visible": false, "opacity": 1, "data": [0,0,0,0,0,66]},
    {"id": 3, "name": "spawns", "type": "objectgroup", "visible": true, "opacity": 1,
     "properties": [{"name": "note", "type": "string", "value": "start here"}],
     "objects": [
       {"id": 7, "name": "hero", "type": "Player", "x": 32, "y": 48,
        "width": 16, "height": 16, "rotation": 90, "visible": true,
        "properties": [{"name": "hp", "type": "int", "value": 10}]},
       {"id": 8, "name": "ghost", "type": "Goblin", "x": 0, "y": 16,
        "width": 0, "height": 0, "rotation": 0, "visible": false}
     ]}
  ]
}
`

func parse(t *testing.T, src string) *tiled.Map {
	t.Helper()
	m, err := tiled.Parse([]byte(src), "level1")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}

// ── the two serialisations are one value ──────────────────────────────────────

// The whole point of parsing both: nothing above this package learns which was
// on disk, so a project can hold either and every later story is written once.
//
// Compared whole, not field by field. The first version of this test checked
// six fields and missed that .tmj dropped every embedded tileset and typed a
// whole-numbered float differently — both invisible to a comparison that only
// looks where you thought to look.
func TestParse_TMXAndTMJAgree(t *testing.T) {
	x, j := parse(t, tmxFull), parse(t, tmjFull)

	if !reflect.DeepEqual(x, j) {
		t.Errorf("the two serialisations of one map differ:\n tmx: %s\n tmj: %s",
			spew(x), spew(j))
	}
}

// spew is a comparable rendering, so a failure says which field differs rather
// than printing two pointers.
func spew(m *tiled.Map) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%dx%d tile %dx%d %s/%s infinite=%v props=%v",
		m.Width, m.Height, m.TileWidth, m.TileHeight, m.Orientation, m.RenderOrder,
		m.Infinite, m.Properties)
	for _, ts := range m.Tilesets {
		fmt.Fprintf(&b, "\n  tileset first=%d src=%q embedded=%d bytes", ts.FirstGID, ts.Source, len(ts.Embedded))
	}
	for _, l := range m.Layers {
		fmt.Fprintf(&b, "\n  layer %q %dx%d vis=%v op=%v data=%v props=%v",
			l.Name, l.Width, l.Height, l.Visible, l.Opacity, l.Data, l.Properties)
	}
	for _, g := range m.ObjectGroups {
		fmt.Fprintf(&b, "\n  group %q vis=%v props=%v", g.Name, g.Visible, g.Properties)
		for _, o := range g.Objects {
			fmt.Fprintf(&b, "\n    obj %+v", o)
		}
	}
	return b.String()
}

func equalData(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── the map itself ────────────────────────────────────────────────────────────

func TestParse_ReadsTheMapsShape(t *testing.T) {
	m := parse(t, tmxCSV)

	if m.Width != 3 || m.Height != 2 {
		t.Errorf("size = %dx%d, want 3x2", m.Width, m.Height)
	}
	if m.TileWidth != 16 || m.TileHeight != 16 {
		t.Errorf("tile size = %dx%d, want 16x16", m.TileWidth, m.TileHeight)
	}
	if m.Orientation != "orthogonal" {
		t.Errorf("orientation = %q", m.Orientation)
	}
}

// Row-major and exactly width×height, which is what every later story indexes
// on. A layer that is short is a map with a hole in it.
func TestParse_LayerDataIsFlatAndRowMajor(t *testing.T) {
	layer := parse(t, tmxCSV).Layers[0]

	if len(layer.Data) != 6 {
		t.Fatalf("data has %d cells, want 6", len(layer.Data))
	}
	for i, want := range []uint32{1, 2, 3, 4, 5, 0} {
		if layer.Data[i] != want {
			t.Errorf("cell %d = %d, want %d", i, layer.Data[i], want)
		}
	}
	// (2,1) is the last cell of the second row.
	if got := layer.TileAt(2, 1); got.GID != 0 {
		t.Errorf("TileAt(2,1) = %d, want the empty cell", got.GID)
	}
	if got := layer.TileAt(0, 1); got.GID != 4 {
		t.Errorf("TileAt(0,1) = %d, want 4", got.GID)
	}
}

func TestParse_KeepsLayerOrderAndVisibility(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <layer name="under" width="1" height="1"><data encoding="csv">1</data></layer>
 <layer name="over" width="1" height="1" visible="0"><data encoding="csv">2</data></layer>
</map>`)

	if len(m.Layers) != 2 {
		t.Fatalf("%d layers", len(m.Layers))
	}
	if m.Layers[0].Name != "under" || m.Layers[1].Name != "over" {
		t.Errorf("layers out of file order: %q, %q", m.Layers[0].Name, m.Layers[1].Name)
	}
	// A layer with no visible attribute is visible; Tiled only writes it when false.
	if !m.Layers[0].Visible {
		t.Error("a layer with no visible attribute is hidden")
	}
	if m.Layers[1].Visible {
		t.Error("a layer Tiled marked hidden is visible")
	}
}

// ── the flags ─────────────────────────────────────────────────────────────────

// Tiled 1.9 uses the top *four* bits, not three: the fourth is a hexagonal 120°
// rotation. Masking with 0x1FFFFFFF leaves it in the id and produces a tile
// index 268 million too high.
func TestParse_SeparatesFlipFlagsFromTheID(t *testing.T) {
	const (
		h    = 0x80000000
		v    = 0x40000000
		d    = 0x20000000
		hex  = 0x10000000
		gid5 = 5
	)
	m := parse(t, `{
	  "width": 4, "height": 1, "tilewidth": 8, "tileheight": 8,
	  "layers": [{"name": "l", "type": "tilelayer", "width": 4, "height": 1,
	              "data": [`+
		itoa(gid5|h)+`,`+itoa(gid5|v|d)+`,`+itoa(gid5|hex)+`,`+itoa(gid5)+`]}]
	}`)
	layer := m.Layers[0]

	for x, want := range []tiled.Tile{
		{GID: 5, FlipH: true},
		{GID: 5, FlipV: true, FlipD: true},
		{GID: 5, RotatedHex: true},
		{GID: 5},
	} {
		if got := layer.TileAt(x, 0); got != want {
			t.Errorf("cell %d = %+v, want %+v", x, got, want)
		}
	}
	// And the raw value is still there, so nothing is lost.
	if layer.Data[0] != gid5|h {
		t.Errorf("the raw gid was masked in place: %#x", layer.Data[0])
	}
}

// ── encodings ─────────────────────────────────────────────────────────────────

// Four encodings, one answer. Tiled writes whichever the project is configured
// for, and a map that only loads under one of them is a map that breaks when
// somebody changes a preference.
func TestParse_DecodesEveryEncodingToTheSameData(t *testing.T) {
	// One gid with the top bits set, so the test can tell a byte order or a
	// truncated width from a correct decode. Four small numbers cannot.
	want := []uint32{1, 2, 3, 0x80000004}
	for _, tc := range []struct{ name, src string }{
		{"csv", tmxWith(`<data encoding="csv">1,2,3,2147483652</data>`)},
		{"base64", tmxWith(`<data encoding="base64">` + b64(want, "") + `</data>`)},
		{"base64+zlib", tmxWith(`<data encoding="base64" compression="zlib">` + b64(want, "zlib") + `</data>`)},
		{"base64+gzip", tmxWith(`<data encoding="base64" compression="gzip">` + b64(want, "gzip") + `</data>`)},
		{"xml tile elements", tmxWith(`<data><tile gid="1"/><tile gid="2"/><tile gid="3"/><tile gid="2147483652"/></data>`)},
	} {
		m, err := tiled.Parse([]byte(tc.src), "level1")
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !equalData(m.Layers[0].Data, want) {
			t.Errorf("%s: data = %v, want %v", tc.name, m.Layers[0].Data, want)
		}
	}
}

// TMJ writes base64 as a string and CSV as an array of numbers.
func TestParse_DecodesTMJBase64(t *testing.T) {
	m := parse(t, `{
	  "width": 4, "height": 1, "tilewidth": 8, "tileheight": 8,
	  "layers": [{"name": "l", "type": "tilelayer", "width": 4, "height": 1,
	              "encoding": "base64", "compression": "zlib",
	              "data": "`+b64([]uint32{1, 2, 3, 4}, "zlib")+`"}]
	}`)
	if !equalData(m.Layers[0].Data, []uint32{1, 2, 3, 4}) {
		t.Errorf("data = %v", m.Layers[0].Data)
	}
}

// ── properties ────────────────────────────────────────────────────────────────

func TestParse_TypesPropertiesAsTiledTypesThem(t *testing.T) {
	for _, m := range []*tiled.Map{parse(t, tmxCSV), parse(t, tmjCSV)} {
		if got := m.Properties.Get("author"); got != "tom" {
			t.Errorf("author = %q", got)
		}
		if got, ok := m.Properties.Int("level"); !ok || got != 1 {
			t.Errorf("level = %v, %v", got, ok)
		}
		if got, ok := m.Properties.Float("gravity"); !ok || got != 9.5 {
			t.Errorf("gravity = %v, %v", got, ok)
		}
		if got, ok := m.Properties.Bool("dark"); !ok || !got {
			t.Errorf("dark = %v, %v", got, ok)
		}
		// A property nobody declared is absent rather than a zero value that
		// reads as a decision.
		if _, ok := m.Properties.Int("missing"); ok {
			t.Error("a property that is not there was found")
		}
	}
}

// A type this package does not model survives as a string rather than being
// dropped — the argument Epic 13 Story 1 made about unknown machine fields.
func TestParse_KeepsAPropertyTypeItDoesNotModel(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <properties><property name="tint" type="color" value="#ff00ff"/></properties>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	if got := m.Properties.Get("tint"); got != "#ff00ff" {
		t.Errorf("a colour property was dropped: %q", got)
	}
}

// ── what later stories need kept ──────────────────────────────────────────────

// Story 6 reads these. A parser that dropped them would have to be reopened.
func TestParse_KeepsObjectGroups(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="2" height="2" tilewidth="16" tileheight="16">
 <layer name="l" width="2" height="2"><data encoding="csv">0,0,0,0</data></layer>
 <objectgroup id="2" name="spawns">
  <object id="7" name="hero" type="Player" x="32" y="48">
   <properties><property name="hp" type="int" value="10"/></properties>
  </object>
 </objectgroup>
</map>`)

	if len(m.ObjectGroups) != 1 {
		t.Fatalf("%d object groups", len(m.ObjectGroups))
	}
	group := m.ObjectGroups[0]
	if group.Name != "spawns" || len(group.Objects) != 1 {
		t.Fatalf("group = %+v", group)
	}
	obj := group.Objects[0]
	if obj.ID != 7 || obj.Name != "hero" || obj.Type != "Player" {
		t.Errorf("object = %+v", obj)
	}
	if obj.X != 32 || obj.Y != 48 {
		t.Errorf("object at (%v,%v), want (32,48)", obj.X, obj.Y)
	}
	if got, ok := obj.Properties.Int("hp"); !ok || got != 10 {
		t.Errorf("object property hp = %v, %v", got, ok)
	}
}

// Not resolved here: that needs the TSX parser and a filesystem, and both would
// make this package untestable from a string.
func TestParse_KeepsTilesetReferencesUnresolved(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <tileset firstgid="1" source="a.tsx"/>
 <tileset firstgid="65" source="b.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	if len(m.Tilesets) != 2 {
		t.Fatalf("%d tilesets", len(m.Tilesets))
	}
	if m.Tilesets[0].FirstGID != 1 || m.Tilesets[0].Source != "a.tsx" {
		t.Errorf("first = %+v", m.Tilesets[0])
	}
	if m.Tilesets[1].FirstGID != 65 || m.Tilesets[1].Source != "b.tsx" {
		t.Errorf("second = %+v", m.Tilesets[1])
	}
}

// ── refusals ──────────────────────────────────────────────────────────────────

func TestParse_RefusesWhatItCannotRead(t *testing.T) {
	for name, src := range map[string]string{
		"not a map at all":                      `just some text`,
		"json that is not a map":                `{"hello": "world"}`,
		"xml that is not a map":                 `<?xml version="1.0"?><tileset name="x"/>`,
		"a layer shorter than the map it is in": tmxWith(`<data encoding="csv">1,2</data>`),
		"a layer longer than the map it is in":  tmxWith(`<data encoding="csv">1,2,3,4,5</data>`),
		"an encoding nothing decodes":           tmxWith(`<data encoding="runlength">1,2,3,4</data>`),
		"a compression that needs a dependency": tmxWith(`<data encoding="base64" compression="zstd">AAAA</data>`),
		"base64 that is not base64":             tmxWith(`<data encoding="base64">not base64 !!</data>`),
	} {
		_, err := tiled.Parse([]byte(src), "level1")
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		// Named, because the message is what someone reads when a map they just
		// exported will not load.
		if !strings.Contains(err.Error(), "level1") {
			t.Errorf("%s: the refusal does not name the file: %v", name, err)
		}
	}
}

// A map with no layers at all is a map, not an error: Tiled will make one.
func TestParse_AcceptsAMapWithNoLayers(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?><map width="1" height="1" tilewidth="8" tileheight="8"/>`)
	if len(m.Layers) != 0 {
		t.Errorf("%d layers", len(m.Layers))
	}
}

// ── the gaps the mutation battery found ───────────────────────────────────────

// Each flag alone, because a fixture that sets two at once cannot tell them
// apart: reading the diagonal bit as the vertical one passes a test that only
// ever sees both set.
func TestParse_ReadsEachFlipFlagIndependently(t *testing.T) {
	const gid = 5
	for name, tc := range map[string]struct {
		raw  uint32
		want tiled.Tile
	}{
		"horizontal only": {gid | 0x80000000, tiled.Tile{GID: gid, FlipH: true}},
		"vertical only":   {gid | 0x40000000, tiled.Tile{GID: gid, FlipV: true}},
		"diagonal only":   {gid | 0x20000000, tiled.Tile{GID: gid, FlipD: true}},
		"hex only":        {gid | 0x10000000, tiled.Tile{GID: gid, RotatedHex: true}},
		"all four":        {gid | 0xF0000000, tiled.Tile{GID: gid, FlipH: true, FlipV: true, FlipD: true, RotatedHex: true}},
	} {
		if got := tiled.TileOf(tc.raw); got != tc.want {
			t.Errorf("%s: %#x → %+v, want %+v", name, tc.raw, got, tc.want)
		}
	}
}

// The XML tile-element path keeps the raw gid too. Masking it there loses the
// flags for a map Tiled wrote without an encoding, which is what the editor
// produces when a project is set to "XML" tile format.
func TestParse_KeepsTheRawGIDInTheXMLTileForm(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <layer name="l" width="1" height="1"><data><tile gid="2147483653"/></data></layer>
</map>`)

	if got := m.Layers[0].Data[0]; got != 0x80000000|5 {
		t.Errorf("raw gid = %#x, want the flag still in it", got)
	}
	if got := m.Layers[0].TileAt(0, 0); got != (tiled.Tile{GID: 5, FlipH: true}) {
		t.Errorf("TileAt = %+v", got)
	}
}

// A renderer walking a viewport that overhangs the map is ordinary, and the
// alternative to answering is every caller bounds-checking the same way.
func TestLayer_TileAtOutsideTheLayerIsEmpty(t *testing.T) {
	layer := parse(t, tmxCSV).Layers[0]

	for _, p := range [][2]int{{-1, 0}, {0, -1}, {3, 0}, {0, 2}, {99, 99}} {
		if got := layer.TileAt(p[0], p[1]); got != (tiled.Tile{}) {
			t.Errorf("TileAt(%d,%d) = %+v, want the empty tile", p[0], p[1], got)
		}
	}
}

// A refusal has to say what was wrong, not merely that something was. Every one
// of these produces *an* error however the decoder is broken; the message is
// what distinguishes a missing decoder from a corrupt map.
func TestParse_RefusalsSayWhatWasWrong(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"zstd", tmxWith(`<data encoding="base64" compression="zstd">AAAAAA==</data>`), "zstd"},
		{"an unknown compression", tmxWith(`<data encoding="base64" compression="lz4">AAAAAA==</data>`), "lz4"},
		{"an unknown encoding", tmxWith(`<data encoding="runlength">1,2,3,4</data>`), "runlength"},
		// "is not base64", not merely "base64": the wrapped stdlib error says
		// "illegal base64 data" whatever went wrong with it, so matching the
		// word alone passes on a message about decompression.
		{"base64 that is not", tmxWith(`<data encoding="base64">not base64 !!</data>`), "is not base64"},
		{"a CSV field that is not a number", tmxWith(`<data encoding="csv">1,two,3,4</data>`), "two"},
		{"a short layer", tmxWith(`<data encoding="csv">1,2</data>`), "2 tiles"},
	} {
		_, err := tiled.Parse([]byte(tc.src), "level1")
		if err == nil {
			t.Errorf("%s was accepted", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: refusal %q does not mention %q", tc.name, err, tc.want)
		}
	}
}

// Tiled writes a trailing comma at the end of each row. They are separators,
// not cells, and counting them makes every layer wider than its map.
func TestParse_DoesNotCountCSVSeparatorsAsCells(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="2" height="2" tilewidth="8" tileheight="8">
 <layer name="l" width="2" height="2">
  <data encoding="csv">
1,2,
3,4,
</data>
 </layer>
</map>`)

	if !equalData(m.Layers[0].Data, []uint32{1, 2, 3, 4}) {
		t.Errorf("data = %v, want four cells", m.Layers[0].Data)
	}
}

// The .tmj path has its own length check, and a copy that is not exercised is a
// copy that is not there.
func TestParse_RefusesAShortTMJLayer(t *testing.T) {
	_, err := tiled.Parse([]byte(`{
	  "width": 4, "height": 1, "tilewidth": 8, "tileheight": 8,
	  "layers": [{"name": "l", "type": "tilelayer", "width": 4, "height": 1, "data": [1,2]}]
	}`), "level1")

	if err == nil {
		t.Fatal("a .tmj layer shorter than its map was accepted")
	}
	if !strings.Contains(err.Error(), "2 tiles") {
		t.Errorf("refusal %q does not say how many it held", err)
	}
}

// Tiled 1.9 renamed the object's "type" attribute to "class" and still reads
// both. A reader that knows only the old one loses every spawn's entity type on
// a map saved by a current editor.
func TestParse_ReadsAnObjectsClassAsItsType(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
 <objectgroup name="spawns">
  <object id="1" name="hero" class="Player" x="0" y="8"/>
 </objectgroup>
</map>`)

	if got := m.ObjectGroups[0].Objects[0].Type; got != "Player" {
		t.Errorf("type = %q, want Player from the class attribute", got)
	}
}

// A tileset defined inside the map is kept verbatim so Story 2 can read it with
// the same parser it uses for a .tsx file.
func TestParse_KeepsAnEmbeddedTileset(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <tileset firstgid="1" name="inline" tilewidth="8" tileheight="8" tilecount="4" columns="2">
  <image source="dungeon.png" width="16" height="16"/>
 </tileset>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	if len(m.Tilesets) != 1 {
		t.Fatalf("%d tilesets", len(m.Tilesets))
	}
	ref := m.Tilesets[0]
	if ref.Source != "" {
		t.Errorf("an embedded tileset has a source: %q", ref.Source)
	}
	// The whole element, not its contents. `,innerxml` gives what is *between*
	// the tags, so name, tilewidth, tilecount and columns — most of what a
	// tileset is — were gone, and what was left was not a <tileset> element and
	// could not be handed to a .tsx parser at all.
	embedded := string(ref.Embedded)
	if !strings.HasPrefix(strings.TrimSpace(embedded), "<tileset") {
		t.Errorf("what was kept is not a tileset element: %q", embedded)
	}
	for _, want := range []string{`name="inline"`, `tilecount="4"`, `columns="2"`, "dungeon.png"} {
		if !strings.Contains(embedded, want) {
			t.Errorf("the embedded tileset lost %s: %q", want, embedded)
		}
	}
	// And a .tmj embedded tileset is kept too, which was silently always nil.
	j := parse(t, `{"width": 1, "height": 1, "tilewidth": 8, "tileheight": 8,
	  "tilesets": [{"firstgid": 1, "name": "inline", "tilecount": 4, "columns": 2,
	                "image": "dungeon.png"}],
	  "layers": [{"name": "l", "type": "tilelayer", "width": 1, "height": 1, "data": [0]}]}`)
	if !strings.Contains(string(j.Tilesets[0].Embedded), "dungeon.png") {
		t.Errorf("a .tmj embedded tileset was dropped: %q", j.Tilesets[0].Embedded)
	}
}

// ── properties ────────────────────────────────────────────────────────────────

// Tiled omits the type attribute for strings, which is its default. A property
// whose type came back empty would make Story 2 unable to tell a string from
// something it failed to read.
func TestParse_TypesAnUntypedPropertyAsAString(t *testing.T) {
	m := parse(t, tmxCSV)

	if got := m.Properties["author"].Type; got != "string" {
		t.Errorf("author's type = %q, want string", got)
	}
	if got := m.Properties["level"].Type; got != "int" {
		t.Errorf("level's type = %q", got)
	}
}

// Tiled writes a multiline string as the element's content rather than as an
// attribute.
func TestParse_ReadsAPropertyWrittenAsElementContent(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <properties><property name="notes">line one
line two</property></properties>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	if got := m.Properties.Get("notes"); !strings.Contains(got, "line two") {
		t.Errorf("notes = %q", got)
	}
}

// Has is the answer to "was this declared", which is different from its value
// being empty — and for the passable property Story 4 reads, the difference
// between a tileset that says nothing and one that says no.
func TestProperties_HasSeparatesAbsentFromEmpty(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <properties><property name="empty" value=""/></properties>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	if !m.Properties.Has("empty") {
		t.Error("a property declared with an empty value reads as absent")
	}
	if m.Properties.Has("missing") {
		t.Error("a property nobody declared reads as present")
	}
}

// Tiled writes "true" and "false". ParseBool also takes "1", "t", "TRUE" and
// five others the format never produces, and accepting them would be inventing
// a format rather than reading one.
func TestProperties_BoolTakesOnlyWhatTiledWrites(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <properties>
  <property name="yes" type="bool" value="true"/>
  <property name="one" type="bool" value="1"/>
 </properties>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	if got, ok := m.Properties.Bool("yes"); !ok || !got {
		t.Errorf("true = %v, %v", got, ok)
	}
	if _, ok := m.Properties.Bool("one"); ok {
		t.Error(`"1" was read as a boolean, which Tiled never writes`)
	}
}

// ── what is and is not a map ──────────────────────────────────────────────────

// A .tsx and a .tmj tileset are both things someone will point at a map loader
// by mistake, and both have to say so rather than parsing into an empty map.
func TestParse_RefusesAFileThatIsATilesetNotAMap(t *testing.T) {
	for name, src := range map[string]string{
		"a .tsx": `<?xml version="1.0"?><tileset name="dungeon" tilewidth="8" tileheight="8" tilecount="4" columns="2"/>`,
		"a .tsj": `{"type": "tileset", "name": "dungeon", "tilewidth": 8, "tileheight": 8,
		            "tilecount": 4, "columns": 2, "width": 2, "height": 2}`,
	} {
		if _, err := tiled.Parse([]byte(src), "dungeon"); err == nil {
			t.Errorf("%s was read as a map", name)
		}
	}
}

// ── the gaps a review against the format found ────────────────────────────────

// A <group> is a layer folder, and dragging layers into one is a routine editor
// action. Dropping it loads a map with every tile and every spawn in it as
// empty — silently, which is the failure this package exists to avoid.
func TestParse_FlattensLayerFolders(t *testing.T) {
	for name, src := range map[string]string{
		"tmx": `<?xml version="1.0"?>
<map width="2" height="1" tilewidth="8" tileheight="8">
 <layer name="outside" width="2" height="1"><data encoding="csv">1,2</data></layer>
 <group id="5" name="Folder">
  <layer name="inside" width="2" height="1"><data encoding="csv">7,8</data></layer>
  <objectgroup name="spawns"><object id="1" name="hero" type="Player" x="0" y="8"/></objectgroup>
  <group name="Nested">
   <layer name="deeper" width="2" height="1"><data encoding="csv">9,10</data></layer>
  </group>
 </group>
</map>`,
		"tmj": `{"width": 2, "height": 1, "tilewidth": 8, "tileheight": 8, "layers": [
  {"name": "outside", "type": "tilelayer", "width": 2, "height": 1, "data": [1,2]},
  {"name": "Folder", "type": "group", "layers": [
    {"name": "inside", "type": "tilelayer", "width": 2, "height": 1, "data": [7,8]},
    {"name": "spawns", "type": "objectgroup",
     "objects": [{"id": 1, "name": "hero", "type": "Player", "x": 0, "y": 8}]},
    {"name": "Nested", "type": "group", "layers": [
      {"name": "deeper", "type": "tilelayer", "width": 2, "height": 1, "data": [9,10]}
    ]}
  ]}
]}`,
	} {
		m := parse(t, src)
		var names []string
		for _, l := range m.Layers {
			names = append(names, l.Name)
		}
		if strings.Join(names, ",") != "outside,inside,deeper" {
			t.Errorf("%s: layers = %v, want outside,inside,deeper", name, names)
		}
		if len(m.ObjectGroups) != 1 || m.ObjectGroups[0].Name != "spawns" {
			t.Errorf("%s: an object layer inside a folder was dropped: %+v", name, m.ObjectGroups)
		}
	}
}

// A layer inside a hidden folder is hidden. A renderer that drew it would be
// showing what the editor does not.
func TestParse_AHiddenFolderHidesWhatIsInIt(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <group name="Folder" visible="0">
  <layer name="inside" width="1" height="1"><data encoding="csv">1</data></layer>
 </group>
</map>`)

	if len(m.Layers) != 1 {
		t.Fatalf("%d layers", len(m.Layers))
	}
	if m.Layers[0].Visible {
		t.Error("a layer inside a hidden folder is visible")
	}
}

// Width and height come out of a file and are not numbers this package chose.
func TestParse_RefusesASizeThatIsNotOne(t *testing.T) {
	for name, src := range map[string]string{
		"a negative width in tmx": `<?xml version="1.0"?><map width="-1" height="1" tilewidth="8" tileheight="8">
 <layer name="l" width="-1" height="1"><data encoding="csv">1</data></layer></map>`,
		"a negative height in tmj": `{"width": 1, "height": -4, "tilewidth": 8, "tileheight": 8, "layers": []}`,
	} {
		_, err := tiled.Parse([]byte(src), "level1")
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "not a size") {
			t.Errorf("%s: refusal %q does not say what was wrong", name, err)
		}
	}
}

// A compressed layer is decompressed before anything checks its size, so the
// bound has to be the size the layer says it is — otherwise a small file asks
// for an allocation measured in hundreds of megabytes before it is refused.
func TestParse_DoesNotDecompressWithoutABound(t *testing.T) {
	// 64 MB of zeros, which zlib squeezes to a few hundred bytes, on a layer
	// that says it is one cell.
	huge := b64(make([]uint32, 16<<20), "zlib")
	src := `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <layer name="l" width="1" height="1">
  <data encoding="base64" compression="zlib">` + huge + `</data>
 </layer>
</map>`

	_, err := tiled.Parse([]byte(src), "level1")
	if err == nil {
		t.Fatal("a layer claiming 16 million tiles in one cell was accepted")
	}
	// Bounded: the reader stops at one more than the layer can hold, so what it
	// reports is a handful of tiles rather than sixteen million.
	if strings.Contains(err.Error(), "16777216") {
		t.Errorf("the whole stream was decompressed before being refused: %v", err)
	}
}

// An infinite map is written in chunks, which nothing here decodes. Without a
// refusal the chunks decode to nothing and the message is about a layer holding
// no tiles, which reads as a corrupt map.
func TestParse_RefusesAnInfiniteMapByName(t *testing.T) {
	for name, src := range map[string]string{
		"tmx": `<?xml version="1.0"?><map width="32" height="32" tilewidth="8" tileheight="8" infinite="1">
 <layer name="l" width="32" height="32"><data encoding="csv"><chunk x="0" y="0" width="16" height="16">0</chunk></data></layer></map>`,
		"tmj": `{"width": 32, "height": 32, "tilewidth": 8, "tileheight": 8, "infinite": true, "layers": []}`,
	} {
		_, err := tiled.Parse([]byte(src), "level1")
		if err == nil {
			t.Errorf("%s: an infinite map was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "infinite") {
			t.Errorf("%s: refusal %q blames something else", name, err)
		}
	}
}

// ── objects ───────────────────────────────────────────────────────────────────

// A template instance carries almost nothing inline — its name, class and
// properties live in the .tx file — so an object with only a template set is
// not an empty object, and dropping the reference makes it look like one.
func TestParse_KeepsWhatAnObjectCarries(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
 <objectgroup name="spawns">
  <object id="1" template="hero.tx" x="32" y="32"/>
  <object id="2" name="turned" type="Goblin" x="8" y="16" rotation="90" visible="0"/>
  <object id="3" name="flipped" gid="2147483651" x="0" y="8"/>
 </objectgroup>
</map>`)
	objs := m.ObjectGroups[0].Objects

	if objs[0].Template != "hero.tx" {
		t.Errorf("a template reference was dropped: %+v", objs[0])
	}
	if objs[1].Rotation != 90 {
		t.Errorf("rotation = %v, want 90", objs[1].Rotation)
	}
	if objs[1].Visible {
		t.Error("an object Tiled marked hidden is visible")
	}
	if !objs[0].Visible {
		t.Error("an object with no visible attribute is hidden")
	}
	// The gid is raw, and Tile is how a caller gets the id without being wrong
	// by 0x80000000 the first time somebody flips a spawn.
	if got := objs[2].Tile(); got != (tiled.Tile{GID: 3, FlipH: true}) {
		t.Errorf("Tile() = %+v", got)
	}
}

// ── tilesets ──────────────────────────────────────────────────────────────────

// A gid belongs to the tileset with the highest first gid at or below it. Every
// caller from Story 4 needs this and would otherwise write the scan itself.
func TestMap_TilesetForResolvesAGIDToItsTileset(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <tileset firstgid="1" source="a.tsx"/>
 <tileset firstgid="65" source="b.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	for _, tc := range []struct {
		gid    uint32
		source string
		local  uint32
		ok     bool
	}{
		{1, "a.tsx", 0, true},
		{64, "a.tsx", 63, true},
		{65, "b.tsx", 0, true},
		{0x80000041, "b.tsx", 0, true}, // flagged, and the flags are not part of the id
		{0, "", 0, false},              // the empty cell belongs to no tileset
	} {
		ref, local, ok := m.TilesetFor(tc.gid)
		if ok != tc.ok || ref.Source != tc.source || local != tc.local {
			t.Errorf("TilesetFor(%#x) = %q,%d,%v; want %q,%d,%v",
				tc.gid, ref.Source, local, ok, tc.source, tc.local, tc.ok)
		}
	}
}

// The highest first gid at or below, not the last one in the file that fits.
// Tiled writes its tilesets in ascending order; a hand-edited or third-party
// file need not, and taking the last match then picks the wrong one.
func TestMap_TilesetForDoesNotDependOnFileOrder(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <tileset firstgid="65" source="b.tsx"/>
 <tileset firstgid="1" source="a.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	if ref, local, ok := m.TilesetFor(70); !ok || ref.Source != "b.tsx" || local != 5 {
		t.Errorf("TilesetFor(70) = %q,%d,%v; want b.tsx,5,true", ref.Source, local, ok)
	}
	if ref, _, ok := m.TilesetFor(3); !ok || ref.Source != "a.tsx" {
		t.Errorf("TilesetFor(3) = %q,%v; want a.tsx", ref.Source, ok)
	}
}

// A file this package did not write may number a tileset from zero. The empty
// cell is still the empty cell.
func TestMap_TilesetForNeverResolvesTheEmptyCell(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <tileset firstgid="0" source="odd.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	if ref, _, ok := m.TilesetFor(0); ok {
		t.Errorf("the empty cell resolved to %q", ref.Source)
	}
}

// A class-typed property is a nested <properties> block in XML and an object in
// JSON, so both the attribute and the chardata are empty and what is left is
// whitespace. It is the one Tiled type where "survives as a string" was false.
func TestParse_KeepsAClassTypedProperty(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <properties>
  <property name="cfg" type="class" propertytype="Loadout">
   <properties><property name="hp" type="int" value="3"/></properties>
  </property>
 </properties>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	got := m.Properties["cfg"]
	if got.Type != "class" {
		t.Errorf("type = %q", got.Type)
	}
	if got.PropertyType != "Loadout" {
		t.Errorf("the custom type's name was dropped: %q", got.PropertyType)
	}
	if !strings.Contains(got.Value, `value="3"`) {
		t.Errorf("the nested block was dropped, leaving %q", got.Value)
	}
}

// The TMJ object converter had no test at all: every object test was XML, so
// one of the two formats was verified for the acceptance criterion that says
// object layers are preserved.
func TestParse_ReadsTMJObjects(t *testing.T) {
	m := parse(t, `{"width": 1, "height": 1, "tilewidth": 8, "tileheight": 8, "layers": [
	  {"name": "spawns", "type": "objectgroup", "id": 3, "objects": [
	    {"id": 7, "name": "hero", "class": "Player", "x": 32, "y": 48,
	     "rotation": 45, "gid": 2147483651, "template": "hero.tx",
	     "properties": [{"name": "hp", "type": "int", "value": 10}]}
	  ]}
	]}`)

	if len(m.ObjectGroups) != 1 {
		t.Fatalf("%d object groups", len(m.ObjectGroups))
	}
	obj := m.ObjectGroups[0].Objects[0]
	if obj.ID != 7 || obj.Name != "hero" || obj.Type != "Player" {
		t.Errorf("object = %+v", obj)
	}
	if obj.X != 32 || obj.Y != 48 || obj.Rotation != 45 {
		t.Errorf("placement = %+v", obj)
	}
	if obj.Template != "hero.tx" {
		t.Errorf("template = %q", obj.Template)
	}
	if got := obj.Tile(); got != (tiled.Tile{GID: 3, FlipH: true}) {
		t.Errorf("Tile() = %+v", got)
	}
	if got, ok := obj.Properties.Int("hp"); !ok || got != 10 {
		t.Errorf("hp = %v, %v", got, ok)
	}
}

// Most emitters wrap base64 at 76 columns even though Tiled writes one line,
// and Story 3 re-reads maps other tools may have written.
func TestParse_DecodesWrappedBase64(t *testing.T) {
	encoded := b64([]uint32{1, 2, 3, 4}, "")
	wrapped := encoded[:4] + "\n  " + encoded[4:]

	m := parse(t, tmxWith(`<data encoding="base64">`+wrapped+`</data>`))
	if !equalData(m.Layers[0].Data, []uint32{1, 2, 3, 4}) {
		t.Errorf("data = %v", m.Layers[0].Data)
	}
}

// checkCells makes a layer's data match its own size; this makes that size
// match the map's. A caller iterating the map's dimensions and indexing the
// layer's data reads the wrong cells otherwise.
func TestParse_RefusesALayerThatIsNotTheShapeOfItsMap(t *testing.T) {
	_, err := tiled.Parse([]byte(`<?xml version="1.0"?>
<map width="10" height="10" tilewidth="8" tileheight="8">
 <layer name="small" width="2" height="1"><data encoding="csv">1,2</data></layer>
</map>`), "level1")

	if err == nil {
		t.Fatal("a 2x1 layer in a 10x10 map was accepted")
	}
	if !strings.Contains(err.Error(), "2x1") || !strings.Contains(err.Error(), "10x10") {
		t.Errorf("refusal %q does not say which disagrees with which", err)
	}
}

// Layer order is draw order, and Story 4 decides a cell's passability from the
// topmost layer that has a tile in it. So the order has to be the file's order
// — and it has to be the same order whichever serialisation the file is in,
// which is this package's whole claim about the two of them.
//
// A layer folder written above a plain layer is what tells them apart: reading
// every direct layer first and only then descending into folders puts the
// folder's contents last, which is the opposite of where the editor drew them.
func TestParse_LayerOrderIsTheFilesOrderInBothSerialisations(t *testing.T) {
	const tmx = `<?xml version="1.0"?>
<map version="1.10" orientation="orthogonal" width="1" height="1" tilewidth="8" tileheight="8">
 <layer name="floor" width="1" height="1"><data encoding="csv">1</data></layer>
 <group name="scenery">
  <layer name="rug" width="1" height="1"><data encoding="csv">2</data></layer>
 </group>
 <layer name="roof" width="1" height="1"><data encoding="csv">3</data></layer>
</map>`
	const tmj = `{"width":1,"height":1,"tilewidth":8,"tileheight":8,"layers":[
 {"type":"tilelayer","name":"floor","width":1,"height":1,"data":[1]},
 {"type":"group","name":"scenery","layers":[
   {"type":"tilelayer","name":"rug","width":1,"height":1,"data":[2]}]},
 {"type":"tilelayer","name":"roof","width":1,"height":1,"data":[3]}]}`

	want := []string{"floor", "rug", "roof"}
	for _, c := range []struct{ kind, src string }{{"tmx", tmx}, {"tmj", tmj}} {
		m := parse(t, c.src)
		var got []string
		for _, l := range m.Layers {
			got = append(got, l.Name)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s layer order = %v, want %v", c.kind, got, want)
		}
	}
}

// The same argument for object layers, which Story 6 reads: a spawn layer
// inside a folder that sits between two others has a place in the file, and a
// reader that hoisted every folder to the end would report them out of it.
func TestParse_ObjectLayerOrderIsTheFilesOrder(t *testing.T) {
	const tmx = `<?xml version="1.0"?>
<map version="1.10" orientation="orthogonal" width="1" height="1" tilewidth="8" tileheight="8">
 <objectgroup name="first"/>
 <group name="folder">
  <objectgroup name="second"/>
 </group>
 <objectgroup name="third"/>
</map>`
	const tmj = `{"width":1,"height":1,"tilewidth":8,"tileheight":8,"layers":[
 {"type":"objectgroup","name":"first"},
 {"type":"group","name":"folder","layers":[{"type":"objectgroup","name":"second"}]},
 {"type":"objectgroup","name":"third"}]}`

	want := []string{"first", "second", "third"}
	for _, c := range []struct{ kind, src string }{{"tmx", tmx}, {"tmj", tmj}} {
		m := parse(t, c.src)
		var got []string
		for _, g := range m.ObjectGroups {
			got = append(got, g.Name)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s object layer order = %v, want %v", c.kind, got, want)
		}
	}
}

// Parse dispatches on the first meaningful byte, which is what makes a renamed
// file load as what it is rather than as what it is called — and there is only
// one format now, so it is also the whole of "this is not a map".
//
// The byte-order mark is the case worth keeping: Tiled does not write one, an
// editor in between may, and a map that fails to load because of three
// invisible bytes is not a failure anybody guesses.
func TestParse_DispatchesOnWhatTheFileHolds(t *testing.T) {
	const tmx = `<?xml version="1.0"?><map version="1.10" width="1" height="1" tilewidth="8" tileheight="8"/>`
	cases := []struct {
		name string
		data string
		ok   bool
	}{
		{"a tmx map", tmx, true},
		{"a tmj map", `{"width":1,"height":1,"tilewidth":8,"tileheight":8}`, true},
		{"leading whitespace", "\n\n  " + tmx, true},
		{"a byte-order mark somebody's editor added", "\xef\xbb\xbf" + tmx, true},
		{"the character format it replaces", "width = 3\nheight = 3\n", false},
		{"nothing at all", "", false},
		{"whitespace and nothing else", "  \n\t", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := tiled.Parse([]byte(c.data), "level.tmx")
			if c.ok {
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("a file that is not a map was accepted")
			}
			if !strings.Contains(err.Error(), "level.tmx") {
				t.Errorf("error = %q, want it to name the file", err)
			}
		})
	}
}
