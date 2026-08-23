package tiled_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

// dungeonTSX is a 4×4 sheet of 16px tiles with a margin and spacing, two tiles
// carrying properties and one carrying a class.
const dungeonTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="dungeon" tilewidth="16" tileheight="16"
         tilecount="16" columns="4" spacing="2" margin="1">
 <properties><property name="theme" value="stone"/></properties>
 <image source="dungeon.png" width="73" height="73"/>
 <tile id="0"><properties><property name="passable" type="bool" value="true"/></properties></tile>
 <tile id="5" type="Wall">
  <properties>
   <property name="passable" type="bool" value="false"/>
   <property name="terrain" value="brick"/>
  </properties>
 </tile>
</tileset>
`

func parseTileset(t *testing.T, src string) *tiled.Tileset {
	t.Helper()
	ts, err := tiled.ParseTileset([]byte(src), "dungeon.tsx", "tilesets")
	if err != nil {
		t.Fatalf("ParseTileset: %v", err)
	}
	return ts
}

func TestParseTileset_ReadsTheSheetsShape(t *testing.T) {
	ts := parseTileset(t, dungeonTSX)

	if ts.Name != "dungeon" {
		t.Errorf("name = %q", ts.Name)
	}
	if ts.TileWidth != 16 || ts.TileHeight != 16 {
		t.Errorf("tile size = %dx%d", ts.TileWidth, ts.TileHeight)
	}
	if ts.TileCount != 16 || ts.Columns != 4 {
		t.Errorf("count/columns = %d/%d", ts.TileCount, ts.Columns)
	}
	if ts.Spacing != 2 || ts.Margin != 1 {
		t.Errorf("spacing/margin = %d/%d", ts.Spacing, ts.Margin)
	}
	if ts.Image.Source != "dungeon.png" || ts.Image.Width != 73 {
		t.Errorf("image = %+v", ts.Image)
	}
}

// The arithmetic a renderer would otherwise write itself, and the last thing in
// this epic anyone would get subtly wrong by hand. margin 1, spacing 2, 16px
// tiles: the second column starts at 1+16+2 = 19.
func TestTileset_SourceRect(t *testing.T) {
	ts := parseTileset(t, dungeonTSX)

	for _, tc := range []struct {
		id         uint32
		x, y, w, h int
		ok         bool
	}{
		{0, 1, 1, 16, 16, true},
		{1, 19, 1, 16, 16, true},
		{3, 55, 1, 16, 16, true},
		{4, 1, 19, 16, 16, true},   // wraps to the second row
		{15, 55, 55, 16, 16, true}, // the last tile
		{16, 0, 0, 0, 0, false},    // one past the end
	} {
		x, y, w, h, ok := ts.SourceRect(tc.id)
		if ok != tc.ok || x != tc.x || y != tc.y || w != tc.w || h != tc.h {
			t.Errorf("SourceRect(%d) = %d,%d,%d,%d,%v; want %d,%d,%d,%d,%v",
				tc.id, x, y, w, h, ok, tc.x, tc.y, tc.w, tc.h, tc.ok)
		}
	}
}

// Only the tiles that declare something. A 256-tile sheet with two walls has two
// entries, not 256 mostly-empty ones.
func TestParseTileset_KeepsOnlyTheTilesThatSaySomething(t *testing.T) {
	ts := parseTileset(t, dungeonTSX)

	if len(ts.Tiles) != 2 {
		t.Errorf("%d tiles carry data, want 2: %v", len(ts.Tiles), ts.Tiles)
	}
	if got := ts.Tiles[5].Type; got != "Wall" {
		t.Errorf("tile 5's class = %q", got)
	}
	if got := ts.Tiles[5].Properties.Get("terrain"); got != "brick" {
		t.Errorf("tile 5's terrain = %q", got)
	}
}

// The one place this epic's convention is named, and the second result is what
// Story 4 needs: a tileset that says nothing is different from one that says no.
func TestTileset_PassableSeparatesSilenceFromNo(t *testing.T) {
	ts := parseTileset(t, dungeonTSX)

	if got, ok := ts.Passable(0); !ok || !got {
		t.Errorf("tile 0 = %v, %v; want passable", got, ok)
	}
	if got, ok := ts.Passable(5); !ok || got {
		t.Errorf("tile 5 = %v, %v; want impassable", got, ok)
	}
	if _, ok := ts.Passable(9); ok {
		t.Error("a tile that says nothing about passability answered anyway")
	}
}

// Tiled has no per-tile inheritance, and inventing it would let a tileset-level
// passable silently override a tile someone set deliberately.
func TestParseTileset_ATileInheritsNothing(t *testing.T) {
	ts := parseTileset(t, `<?xml version="1.0"?>
<tileset name="t" tilewidth="8" tileheight="8" tilecount="2" columns="2">
 <properties><property name="passable" type="bool" value="true"/></properties>
 <image source="t.png" width="16" height="8"/>
 <tile id="1"><properties><property name="terrain" value="mud"/></properties></tile>
</tileset>`)

	if got, ok := ts.Properties.Bool("passable"); !ok || !got {
		t.Errorf("the tileset's own property = %v, %v", got, ok)
	}
	if _, ok := ts.Passable(1); ok {
		t.Error("a tile inherited the tileset's passable")
	}
	if _, ok := ts.Passable(0); ok {
		t.Error("a tile that declares nothing inherited the tileset's passable")
	}
}

// ── refusals ──────────────────────────────────────────────────────────────────

func TestParseTileset_RefusesWhatIsNotATileset(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"a .tmx map": {
			`<?xml version="1.0"?><map width="1" height="1" tilewidth="8" tileheight="8"/>`,
			"dungeon.tsx",
		},
		// Valid tile sizes and no image, so nothing but the type says this is
		// not a tileset — which is what makes it a test of the type check
		// rather than of the size check behind it.
		"a .tmj map": {
			`{"type": "map", "width": 1, "height": 1, "tilewidth": 8, "tileheight": 8}`,
			"not a tileset",
		},
		"nonsense":   {`just some text`, "neither a .tsx nor a .tsj"},
		"broken xml": {`<tileset name="t"`, "dungeon.tsx"},
	} {
		_, err := tiled.ParseTileset([]byte(tc.src), "dungeon.tsx", "tilesets")
		if err == nil {
			t.Errorf("%s was read as a tileset", name)
			continue
		}
		// What was wrong, not merely that something was — the weakness this
		// package's own Story 1 wrote up and then repeated here.
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: refusal %q does not mention %q", name, err, tc.want)
		}
	}
}

// A wrong columns puts every tile after the first row at the wrong place in the
// image, and nothing downstream could tell — the renderer would just draw the
// wrong picture.
func TestParseTileset_RefusesACountAndColumnsThatDisagree(t *testing.T) {
	for name, src := range map[string]string{
		"columns that do not divide the count": `<?xml version="1.0"?>
<tileset name="t" tilewidth="8" tileheight="8" tilecount="10" columns="4">
 <image source="t.png" width="32" height="24"/></tileset>`,
		"no columns at all": `<?xml version="1.0"?>
<tileset name="t" tilewidth="8" tileheight="8" tilecount="4" columns="0">
 <image source="t.png" width="32" height="8"/></tileset>`,
	} {
		_, err := tiled.ParseTileset([]byte(src), "dungeon.tsx", "tilesets")
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "dungeon.tsx") {
			t.Errorf("%s: the refusal does not name the file: %v", name, err)
		}
		// And says which of the two rules it broke: swapping the messages would
		// otherwise pass.
		if !strings.Contains(err.Error(), "rows of") && !strings.Contains(err.Error(), "columns") {
			t.Errorf("%s: refusal %q says nothing about columns", name, err)
		}
	}
}

// ── resolution ────────────────────────────────────────────────────────────────

// The map says where its tilesets are, relative to itself.
func TestMap_ResolveTilesetsReadsExternalReferences(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="16" tileheight="16">
 <tileset firstgid="1" source="tilesets/dungeon.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	var asked []string
	open := func(name string) ([]byte, error) {
		asked = append(asked, name)
		return []byte(dungeonTSX), nil
	}
	if err := m.ResolveTilesets("maps", open); err != nil {
		t.Fatalf("ResolveTilesets: %v", err)
	}

	// Relative to the map file, which is what Tiled writes.
	if len(asked) != 1 || asked[0] != "maps/tilesets/dungeon.tsx" {
		t.Errorf("opened %v, want maps/tilesets/dungeon.tsx", asked)
	}
	if m.Tilesets[0].Tileset == nil || m.Tilesets[0].Tileset.Name != "dungeon" {
		t.Fatalf("the tileset did not resolve: %+v", m.Tilesets[0])
	}
}

// An embedded tileset needs no file, and asking for one would be a read that
// cannot succeed.
func TestMap_ResolveTilesetsReadsAnEmbeddedOneWithoutOpeningAnything(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <tileset firstgid="1" name="inline" tilewidth="8" tileheight="8" tilecount="4" columns="2">
  <image source="inline.png" width="16" height="16"/>
 </tileset>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	opened := false
	open := func(string) ([]byte, error) {
		opened = true
		return nil, errors.New("should not be called")
	}
	if err := m.ResolveTilesets(".", open); err != nil {
		t.Fatalf("ResolveTilesets: %v", err)
	}
	if opened {
		t.Error("an embedded tileset was looked for on disk")
	}
	if m.Tilesets[0].Tileset == nil || m.Tilesets[0].Tileset.Name != "inline" {
		t.Errorf("the embedded tileset did not resolve: %+v", m.Tilesets[0])
	}
}

// The image is relative to the *tileset*, not the map — and the difference only
// shows when the two are in different directories, which is how Tiled lays a
// project out.
func TestMap_ResolveTilesetsResolvesTheImageAgainstItsTileset(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="16" tileheight="16">
 <tileset firstgid="1" source="tilesets/dungeon.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	open := func(string) ([]byte, error) {
		return []byte(`<?xml version="1.0"?>
<tileset name="dungeon" tilewidth="16" tileheight="16" tilecount="4" columns="2">
 <image source="../art/dungeon.png" width="32" height="32"/></tileset>`), nil
	}
	if err := m.ResolveTilesets("maps", open); err != nil {
		t.Fatal(err)
	}

	img := m.Tilesets[0].Tileset.Image
	if img.Source != "../art/dungeon.png" {
		t.Errorf("the raw attribute was rewritten: %q", img.Source)
	}
	// maps/tilesets/../art/dungeon.png
	if img.Path != "maps/art/dungeon.png" {
		t.Errorf("resolved path = %q, want maps/art/dungeon.png", img.Path)
	}
}

func TestMap_ResolveTilesetsRefusesAReferenceThatWillNotOpen(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <tileset firstgid="1" source="missing.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	err := m.ResolveTilesets("maps", func(string) ([]byte, error) {
		return nil, errors.New("no such file")
	})
	if err == nil {
		t.Fatal("a tileset that does not open was accepted")
	}
	for _, want := range []string{"missing.tsx", "no such file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}
}

// Resolving is what makes TilesetFor useful: a gid becomes a tileset and a
// local id, and the local id is what SourceRect and Passable take.
func TestMap_AGIDResolvesToATileInATileset(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="16" tileheight="16">
 <tileset firstgid="1" source="a.tsx"/>
 <tileset firstgid="17" source="b.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	open := func(name string) ([]byte, error) {
		switch name {
		case "maps/a.tsx":
			return []byte(dungeonTSX), nil
		default:
			return []byte(`<?xml version="1.0"?>
<tileset name="props" tilewidth="16" tileheight="16" tilecount="4" columns="2">
 <image source="props.png" width="32" height="32"/>
 <tile id="2"><properties><property name="passable" type="bool" value="false"/></properties></tile>
</tileset>`), nil
		}
	}
	if err := m.ResolveTilesets("maps", open); err != nil {
		t.Fatal(err)
	}

	// gid 19 is the third tile of the second tileset.
	ref, local, ok := m.TilesetFor(19)
	if !ok || ref.Tileset == nil {
		t.Fatalf("gid 19 resolved to %+v, %v", ref, ok)
	}
	if ref.Tileset.Name != "props" || local != 2 {
		t.Errorf("gid 19 = %s tile %d, want props tile 2", ref.Tileset.Name, local)
	}
	if passable, said := ref.Tileset.Passable(local); !said || passable {
		t.Errorf("props tile 2 = %v, %v; want impassable", passable, said)
	}
	// And gid 6 is the sixth tile of the first, which the fixture calls a Wall.
	ref, local, _ = m.TilesetFor(6)
	if ref.Tileset.Tiles[local].Type != "Wall" {
		t.Errorf("gid 6 = %+v", ref.Tileset.Tiles[local])
	}
}

// ── the gaps a review against the format found ────────────────────────────────

// Tiled writes .tsj files and embeds the same object in a .tmj map, so a
// package that read only XML loaded a JSON project's map and then died on its
// tilesets — with an EOF that named no cause.
func TestParseTileset_ReadsTheJSONSerialisation(t *testing.T) {
	ts, err := tiled.ParseTileset([]byte(`{
	  "type": "tileset", "name": "dungeon", "tilewidth": 16, "tileheight": 16,
	  "tilecount": 16, "columns": 4, "spacing": 2, "margin": 1,
	  "image": "dungeon.png", "imagewidth": 73, "imageheight": 73,
	  "transparentcolor": "#ff00ff", "tileoffset": {"x": 0, "y": -8},
	  "properties": [{"name": "theme", "type": "string", "value": "stone"}],
	  "tiles": [
	    {"id": 0, "properties": [{"name": "passable", "type": "bool", "value": true}]},
	    {"id": 5, "type": "Wall",
	     "properties": [{"name": "passable", "type": "bool", "value": false}]}
	  ]
	}`), "dungeon.tsj", "tilesets")
	if err != nil {
		t.Fatalf("ParseTileset: %v", err)
	}

	if ts.Name != "dungeon" || ts.Columns != 4 || ts.Margin != 1 || ts.Spacing != 2 {
		t.Errorf("shape = %+v", ts)
	}
	if ts.Image.Source != "dungeon.png" || ts.Image.Height != 73 {
		t.Errorf("image = %+v", ts.Image)
	}
	if ts.TileOffsetY != -8 {
		t.Errorf("tile offset y = %d, want -8", ts.TileOffsetY)
	}
	if got, ok := ts.Passable(5); !ok || got {
		t.Errorf("tile 5 = %v, %v; want impassable", got, ok)
	}
	if ts.Tiles[5].Type != "Wall" {
		t.Errorf("tile 5's class = %q", ts.Tiles[5].Type)
	}
	if got := ts.Properties.Get("theme"); got != "stone" {
		t.Errorf("theme = %q", got)
	}
}

// A .tmj map embeds a JSON tileset object, which is the same shape without the
// "type" field.
func TestMap_ResolveTilesetsReadsAnEmbeddedJSONTileset(t *testing.T) {
	m := parse(t, `{"width": 1, "height": 1, "tilewidth": 8, "tileheight": 8,
	  "tilesets": [{"firstgid": 1, "name": "inline", "tilewidth": 8, "tileheight": 8,
	                "tilecount": 4, "columns": 2, "image": "inline.png",
	                "imagewidth": 16, "imageheight": 16}],
	  "layers": [{"name": "l", "type": "tilelayer", "width": 1, "height": 1, "data": [0]}]}`)

	if err := m.ResolveTilesets("maps", func(string) ([]byte, error) {
		return nil, errors.New("should not be called")
	}); err != nil {
		t.Fatalf("ResolveTilesets: %v", err)
	}
	if m.Tilesets[0].Tileset == nil || m.Tilesets[0].Tileset.Name != "inline" {
		t.Fatalf("the embedded JSON tileset did not resolve: %+v", m.Tilesets[0])
	}
	if got := m.Tilesets[0].Tileset.Image.Path; got != "maps/inline.png" {
		t.Errorf("image path = %q", got)
	}
}

// A collection of images — one file per tile, no sheet — is how prop and
// creature art is authored, and Tiled writes columns="0" for it. Refusing that
// made a whole class of real project unloadable.
func TestParseTileset_ReadsACollectionOfImages(t *testing.T) {
	ts := parseTileset(t, `<?xml version="1.0"?>
<tileset name="props" tilewidth="32" tileheight="48" tilecount="3" columns="0">
 <grid orientation="orthogonal" width="1" height="1"/>
 <tile id="0"><image source="barrel.png" width="32" height="48"/></tile>
 <tile id="1">
  <image source="crate.png" width="32" height="32"/>
  <properties><property name="passable" type="bool" value="false"/></properties>
 </tile>
 <tile id="7"><image source="sack.png" width="32" height="32"/></tile>
</tileset>`)

	if !ts.Collection() {
		t.Fatal("a tileset with no sheet is not reported as a collection")
	}
	// Sparse ids are ordinary: deleting a tile from a collection leaves a gap.
	if got := ts.Tiles[7].Image.Source; got != "sack.png" {
		t.Errorf("tile 7's image = %q", got)
	}
	if got := ts.Tiles[0].Image.Path; got != "tilesets/barrel.png" {
		t.Errorf("tile 0's image path = %q", got)
	}
	if got, ok := ts.Passable(1); !ok || got {
		t.Errorf("tile 1 = %v, %v; want impassable", got, ok)
	}
	// No grid to have a rectangle in: each tile is a whole file.
	if _, _, _, _, ok := ts.SourceRect(0); ok {
		t.Error("a collection's tile was given a source rectangle")
	}
}

// The divisibility rule is a sheet's. A collection's columns is a display width
// somebody dragged and has nothing to do with its tile count.
func TestParseTileset_DoesNotApplyTheSheetRuleToACollection(t *testing.T) {
	ts := parseTileset(t, `<?xml version="1.0"?>
<tileset name="props" tilewidth="32" tileheight="32" tilecount="3" columns="5">
 <tile id="0"><image source="a.png" width="32" height="32"/></tile>
</tileset>`)

	if !ts.Collection() {
		t.Error("a tileset with no sheet image is not a collection")
	}
	// And its columns, whatever somebody dragged it to, buys no rectangles:
	// there is no sheet for one to be in. A collection with columns > 0 is the
	// only fixture that can tell this from the columns <= 0 guard behind it.
	if _, _, _, _, ok := ts.SourceRect(0); ok {
		t.Error("a collection with a display width was given a source rectangle")
	}
}

// Tiled writes an absolute path whenever art lives outside the project
// directory, which happens the first time somebody drags a sheet in from
// Downloads. path.Join does not treat a leading slash as anchoring.
func TestParseTileset_LeavesAnAbsoluteImagePathAlone(t *testing.T) {
	ts := parseTileset(t, `<?xml version="1.0"?>
<tileset name="t" tilewidth="8" tileheight="8" tilecount="4" columns="2">
 <image source="/home/tom/art/dungeon.png" width="16" height="16"/></tileset>`)

	if got := ts.Image.Path; got != "/home/tom/art/dungeon.png" {
		t.Errorf("path = %q, want the absolute one unchanged", got)
	}
}

func TestMap_ResolveTilesetsLeavesAnAbsoluteSourceAlone(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <tileset firstgid="1" source="/opt/shared/dungeon.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	var asked string
	_ = m.ResolveTilesets("maps", func(name string) ([]byte, error) {
		asked = name
		return []byte(dungeonTSX), nil
	})
	if asked != "/opt/shared/dungeon.tsx" {
		t.Errorf("opened %q, want the absolute path unchanged", asked)
	}
}

// A tileset of tiles taller than the grid lines up with an offset, and dropping
// it draws the whole map in the wrong place — which reads as a layout fault
// rather than a missing attribute.
func TestParseTileset_KeepsTheDrawingOffsetAndColourKey(t *testing.T) {
	ts := parseTileset(t, `<?xml version="1.0"?>
<tileset name="t" tilewidth="16" tileheight="32" tilecount="4" columns="2" class="Terrain">
 <tileoffset x="0" y="-16"/>
 <image source="t.png" trans="ff00ff" width="32" height="64"/></tileset>`)

	if ts.TileOffsetX != 0 || ts.TileOffsetY != -16 {
		t.Errorf("offset = %d,%d; want 0,-16", ts.TileOffsetX, ts.TileOffsetY)
	}
	if ts.Image.Trans != "ff00ff" {
		t.Errorf("colour key = %q", ts.Image.Trans)
	}
	if ts.Class != "Terrain" {
		t.Errorf("the tileset's own class = %q", ts.Class)
	}
}

// Tiled 1.9 renamed a tile's "type" to "class" and 1.10 renamed it back, so a
// project's files may use either.
func TestParseTileset_ReadsATilesClassAsItsType(t *testing.T) {
	ts := parseTileset(t, `<?xml version="1.0"?>
<tileset name="t" tilewidth="8" tileheight="8" tilecount="2" columns="2">
 <image source="t.png" width="16" height="8"/>
 <tile id="1" class="Wall"/></tileset>`)

	if got := ts.Tiles[1].Type; got != "Wall" {
		t.Errorf("type = %q, want Wall from the class attribute", got)
	}
}

// A size out of a file is not a number this package chose — the rule the map
// parser already applies, and which nothing here applied.
func TestParseTileset_RefusesASizeThatIsNotOne(t *testing.T) {
	for name, src := range map[string]string{
		"negative tiles": `<?xml version="1.0"?><tileset name="t" tilewidth="-16" tileheight="-16"
 tilecount="8" columns="4"><image source="t.png" width="16" height="16"/></tileset>`,
		"zero-sized tiles": `<?xml version="1.0"?><tileset name="t" tilewidth="0" tileheight="8"
 tilecount="4" columns="2"><image source="t.png" width="16" height="16"/></tileset>`,
		"a negative margin": `<?xml version="1.0"?><tileset name="t" tilewidth="8" tileheight="8"
 tilecount="4" columns="2" margin="-5"><image source="t.png" width="16" height="16"/></tileset>`,
	} {
		if _, err := tiled.ParseTileset([]byte(src), "dungeon.tsx", "."); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Last-wins would be a tileset quietly disagreeing with the file it came from,
// and the only way to notice is a tile drawn or walked wrongly.
func TestParseTileset_RefusesTwoTilesWithOneID(t *testing.T) {
	_, err := tiled.ParseTileset([]byte(`<?xml version="1.0"?>
<tileset name="t" tilewidth="8" tileheight="8" tilecount="4" columns="2">
 <image source="t.png" width="16" height="16"/>
 <tile id="1"><properties><property name="a" value="1"/></properties></tile>
 <tile id="1"><properties><property name="a" value="2"/></properties></tile>
</tileset>`), "dungeon.tsx", ".")

	if err == nil {
		t.Fatal("two tiles with one id were accepted")
	}
	if !strings.Contains(err.Error(), "id 1") {
		t.Errorf("refusal %q does not say which id", err)
	}
}

// ── resolution's own refusals ─────────────────────────────────────────────────

// When a relative reference is wrong, where it looked is the one fact worth
// having — and which map pulled it in, because a tileset is shared.
func TestMap_ResolveTilesetsRefusalSaysWhereItLooked(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <tileset firstgid="1" source="tilesets/missing.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	err := m.ResolveTilesets("maps", func(string) ([]byte, error) {
		return nil, errors.New("no such file")
	})
	if err == nil {
		t.Fatal("a tileset that does not open was accepted")
	}
	for _, want := range []string{"level1", "maps/tilesets/missing.tsx", "no such file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}
}

// A tileset that opens and will not parse is refused, not attached empty.
func TestMap_ResolveTilesetsRefusesATilesetThatWillNotParse(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <tileset firstgid="1" source="broken.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	err := m.ResolveTilesets("maps", func(string) ([]byte, error) {
		return []byte(`<?xml version="1.0"?><tileset name="t" tilewidth="8" tileheight="8"
 tilecount="10" columns="4"><image source="t.png" width="32" height="24"/></tileset>`), nil
	})
	if err == nil {
		t.Fatal("a tileset that will not parse was attached")
	}
	if !strings.Contains(err.Error(), "broken.tsx") {
		t.Errorf("refusal %q does not name the tileset", err)
	}
}

// Resolving twice reads nothing twice: a caller that resolves defensively does
// not pay for it, and a tileset already attached is not replaced.
func TestMap_ResolveTilesetsIsIdempotent(t *testing.T) {
	m := parse(t, `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="16" tileheight="16">
 <tileset firstgid="1" source="dungeon.tsx"/>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)

	reads := 0
	open := func(string) ([]byte, error) {
		reads++
		return []byte(dungeonTSX), nil
	}
	for i := 0; i < 3; i++ {
		if err := m.ResolveTilesets("maps", open); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if reads != 1 {
		t.Errorf("read the tileset %d times, want once", reads)
	}
}
