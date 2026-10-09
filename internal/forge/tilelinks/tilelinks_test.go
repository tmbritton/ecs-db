package tilelinks

import (
	"strconv"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

func linkedFixture(t *testing.T) (*tiled.Document, int) {
	t.Helper()
	d, err := tiled.NewDocument("level.tmx", tiled.NewMapSpec{MapID: "level", Width: 3, Height: 2, TileWidth: 8, TileHeight: 8})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetLayerData(0, []uint32{1, 0, 1, 0, 1, 0}); err != nil {
		t.Fatal(err)
	}
	id, err := d.AddObject(0, tiled.Object{Type: "River", X: 0, Y: 0, Properties: tiled.Properties{
		"Passability.kind": {Value: "liquid"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return d, id
}

func TestLink_PreservesOneSharedObjectAcrossIrregularPaintedCells(t *testing.T) {
	d, id := linkedFixture(t)
	for _, cell := range []Cell{{0, 0}, {2, 0}, {1, 1}} {
		changed, err := Link(d, id, 1, cell)
		if err != nil || !changed {
			t.Fatalf("linking %+v: changed=%v, err=%v", cell, changed, err)
		}
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.ObjectGroups[0].Objects) != 1 || m.ObjectGroups[0].Objects[0].ID != id {
		t.Fatalf("shared River was copied or replaced: %+v", m.ObjectGroups)
	}
	props := m.ObjectGroups[0].Objects[0].Properties
	if props.Get("TileLink.layerID") != "1" ||
		props.Get("TileLink.cells") != `[{"x":0,"y":0},{"x":2,"y":0},{"x":1,"y":1}]` {
		t.Fatalf("authored links = %v; want three offsets on layer 1", props)
	}
	if !strings.Contains(string(d.Bytes()), `name="Passability.kind"`) {
		t.Fatal("linking erased an unrelated object property")
	}
	before := string(d.Bytes())
	if changed, err := Link(d, id, 1, Cell{2, 0}); err != nil || changed || string(d.Bytes()) != before {
		t.Fatalf("re-linking the same cell changed bytes: changed=%v, err=%v", changed, err)
	}
}

func TestLink_RefusesInvalidTargetsWithoutChangingMap(t *testing.T) {
	for _, tt := range []struct {
		name    string
		layerID int
		cell    Cell
		want    string
	}{
		{"empty painted cell", 1, Cell{1, 0}, "empty"},
		{"unknown layer", 9, Cell{0, 0}, "layer"},
		{"outside map", 1, Cell{3, 0}, "outside"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, id := linkedFixture(t)
			before := string(d.Bytes())
			if changed, err := Link(d, id, tt.layerID, tt.cell); changed || err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Link = %v,%v; want refusal containing %q", changed, err, tt.want)
			}
			if string(d.Bytes()) != before {
				t.Fatal("refused link changed document bytes")
			}
		})
	}
}

func TestUnlink_RemovesOneCellThenMetadataWithoutDeletingSpawn(t *testing.T) {
	d, id := linkedFixture(t)
	for _, cell := range []Cell{{0, 0}, {2, 0}} {
		if _, err := Link(d, id, 1, cell); err != nil {
			t.Fatal(err)
		}
	}
	if changed, err := Unlink(d, id, 1, Cell{0, 0}); err != nil || !changed {
		t.Fatalf("unlink origin = %v,%v", changed, err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ObjectGroups[0].Objects[0].Properties.Get("TileLink.cells"); got != `[{"x":2,"y":0}]` {
		t.Fatalf("remaining cell = %q", got)
	}
	if changed, err := Unlink(d, id, 1, Cell{2, 0}); err != nil || !changed {
		t.Fatalf("unlink last cell = %v,%v", changed, err)
	}
	m, err = d.Map()
	if err != nil {
		t.Fatal(err)
	}
	obj := m.ObjectGroups[0].Objects[0]
	if obj.ID != id || obj.Type != "River" || obj.Properties.Get("Passability.kind") != "liquid" {
		t.Fatalf("unlink deleted the independent spawn: %+v", obj)
	}
	if _, found := obj.Properties["TileLink.layerID"]; found {
		t.Fatal("last unlink kept TileLink metadata")
	}
}

func TestUnlink_RemovingExtraCellLeavesOnlyTheAnchor(t *testing.T) {
	d, id := linkedFixture(t)
	for _, cell := range []Cell{{0, 0}, {2, 0}} {
		if _, err := Link(d, id, 1, cell); err != nil {
			t.Fatal(err)
		}
	}
	if changed, err := Unlink(d, id, 1, Cell{2, 0}); err != nil || !changed {
		t.Fatalf("unlink extra cell = %v,%v", changed, err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ObjectGroups[0].Objects[0].Properties.Get("TileLink.cells"); got != `[{"x":0,"y":0}]` {
		t.Fatalf("remaining footprint = %q, still names erased cell", got)
	}
}

func TestLink_RefusesAnExistingLinkToAnErasedCellBeforeWriting(t *testing.T) {
	d, id := linkedFixture(t)
	if err := d.SetObjectProperty(id, "TileLink.layerID", tiled.Property{Type: "int", Value: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetLayerData(0, []uint32{0, 0, 1, 0, 1, 0}); err != nil {
		t.Fatal(err)
	}
	before := string(d.Bytes())
	if changed, err := Link(d, id, 1, Cell{2, 0}); err == nil || changed || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("Link into existing bad footprint = %v,%v, want empty-cell refusal", changed, err)
	}
	if string(d.Bytes()) != before {
		t.Fatal("failed link modified a map already needing repair")
	}
}

func TestLink_ReusesExistingTileLinkPropertyCasingWithoutCreatingDuplicates(t *testing.T) {
	d, id := linkedFixture(t)
	if err := d.SetObjectProperty(id, "tilelink.layerid", tiled.Property{Type: "int", Value: "1"}); err != nil {
		t.Fatal(err)
	}
	if changed, err := Link(d, id, 1, Cell{2, 0}); err != nil || !changed {
		t.Fatalf("linking an existing differently-cased property: %v,%v", changed, err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	props := m.ObjectGroups[0].Objects[0].Properties
	if _, duplicated := props["TileLink.layerID"]; duplicated || props.Get("tilelink.layerid") != "1" {
		t.Fatalf("author's TileLink spelling was duplicated or lost: %+v", props)
	}
}

func TestLinkAndErase_ParseEquivalentLayerIDSpellings(t *testing.T) {
	d, id := linkedFixture(t)
	if err := d.SetObjectProperty(id, "TileLink.layerID", tiled.Property{Type: "int", Value: "01"}); err != nil {
		t.Fatal(err)
	}
	before := string(d.Bytes())
	if changed, err := Link(d, id, 1, Cell{0, 0}); err != nil || changed || string(d.Bytes()) != before {
		t.Fatalf("re-linking implicit anchor rewrote equivalent ID: %v,%v", changed, err)
	}
	if changed, err := Link(d, id, 1, Cell{2, 0}); err != nil || !changed {
		t.Fatalf("adding a cell to layer 01: %v,%v", changed, err)
	}
	if err := SetLayerData(d, 0, []uint32{1, 0, 0, 0, 1, 0}); err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ObjectGroups[0].Objects[0].Properties.Get("TileLink.cells"); got != `[{"x":0,"y":0}]` {
		t.Fatalf("erasing a layer-01 linked Tile left footprint %q", got)
	}
}

func TestInspect_SeparatesStackedTileTemplatesFromSharedObjectLinks(t *testing.T) {
	m := &tiled.Map{
		Width: 2, Height: 1, TileWidth: 8, TileHeight: 8,
		Layers: []tiled.Layer{
			{ID: 4, Width: 2, Height: 1, Data: []uint32{1, 0}},
			{ID: 9, Width: 2, Height: 1, Data: []uint32{2, 1}},
		},
		Tilesets: []tiled.TilesetRef{{FirstGID: 1, Tileset: &tiled.Tileset{
			TileCount: 2, Columns: 2, TileWidth: 8, TileHeight: 8,
			Properties: tiled.Properties{"entityType": {Value: "Floor"}},
			Tiles: map[uint32]tiled.TilesetTile{
				1: {ID: 1, Type: "wall", Properties: tiled.Properties{"entityType": {Value: "Wall"}}},
			},
		}}},
		ObjectGroups: []tiled.ObjectGroup{{Objects: []tiled.Object{
			{ID: 5, Type: "River", X: 0, Y: 0, Properties: tiled.Properties{
				"TileLink.layerID": {Type: "int", Value: "09"},
				"TileLink.cells":   {Value: `[{"x":0,"y":0},{"x":1,"y":0}]`},
			}},
			{ID: 6, Type: "Gate", X: 0, Y: 0, Properties: tiled.Properties{
				"TileLink.layerID": {Type: "int", Value: "9"},
			}},
		}}},
	}
	for _, tt := range []struct {
		name          string
		layerID       int
		cell          Cell
		wantType      string
		wantClass     string
		wantLinkedIDs []int
		wantEmpty     bool
		wantErr       string
	}{
		{"lower floor", 4, Cell{0, 0}, "Floor", "", nil, false, ""},
		{"upper wall and two objects", 9, Cell{0, 0}, "Wall", "wall", []int{5, 6}, false, ""},
		{"upper floor shared river", 9, Cell{1, 0}, "Floor", "", []int{5}, false, ""},
		{"empty lower cell", 4, Cell{1, 0}, "", "", nil, true, ""},
		{"unknown layer", 999, Cell{0, 0}, "", "", nil, false, "layer"},
		{"outside map", 9, Cell{2, 0}, "", "", nil, false, "outside"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Inspect(m, tt.layerID, tt.cell)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Inspect error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got.EntityType != tt.wantType || got.ArtworkClass != tt.wantClass || got.Empty != tt.wantEmpty ||
				len(got.ObjectIDs) != len(tt.wantLinkedIDs) {
				t.Fatalf("Inspect = %+v,%v, want type %q links %v empty=%v", got, err, tt.wantType, tt.wantLinkedIDs, tt.wantEmpty)
			}
			for i, id := range tt.wantLinkedIDs {
				if got.ObjectIDs[i] != id {
					t.Fatalf("linked objects = %v, want %v", got.ObjectIDs, tt.wantLinkedIDs)
				}
			}
		})
	}
}

func TestInspect_MalformedOtherLinkDoesNotHideAValidTile(t *testing.T) {
	ts, err := tiled.ParseTileset([]byte(`<tileset name="floor" tilewidth="8" tileheight="8" tilecount="1" columns="1"><image source="floor.png" width="8" height="8"/><properties><property name="entityType" value="Floor"/></properties></tileset>`), "floor.tsx", ".")
	if err != nil {
		t.Fatal(err)
	}
	m := &tiled.Map{
		Width: 1, Height: 1, TileWidth: 8, TileHeight: 8,
		Layers:   []tiled.Layer{{ID: 1, Width: 1, Height: 1, Data: []uint32{1}}},
		Tilesets: []tiled.TilesetRef{{FirstGID: 1, Tileset: ts}},
		ObjectGroups: []tiled.ObjectGroup{{Objects: []tiled.Object{
			{ID: 5, Type: "River", Properties: tiled.Properties{"TileLink.layerID": {Value: "oops"}}},
			{ID: 6, Type: "Gate", Properties: tiled.Properties{"TileLink.layerID": {Value: "1"}}},
		}}},
	}
	got, err := Inspect(m, 1, Cell{0, 0})
	if err != nil || got.EntityType != "Floor" || len(got.ObjectIDs) != 1 || got.ObjectIDs[0] != 6 {
		t.Fatalf("bad object hid the independently valid Tile: %+v, %v", got, err)
	}
}

func TestValidateSharedArt_RejectsOneVisualAcrossDistinctPaintedSlices(t *testing.T) {
	for _, tt := range []struct {
		name, templateType string
		gids               []uint32
		visual             bool
		want               string
	}{
		{"one matching segment", "River", []uint32{1, 1}, true, ""},
		{"two different matching segments", "River", []uint32{1, 2}, true, "distinct artwork"},
		{"matching restriction without visual", "River", []uint32{1, 1}, false, "TileVisual"},
		{"separate per-cell art", "Floor", []uint32{1, 2}, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			props := tiled.Properties{
				"TileLink.layerID": {Type: "int", Value: "1"},
				"TileLink.cells":   {Value: `[{"x":0,"y":0},{"x":1,"y":0}]`},
			}
			if tt.visual {
				props["TileVisual.image"] = tiled.Property{Value: "river.png"}
			}
			ts, err := tiled.ParseTileset([]byte(`<tileset name="cells" tilewidth="8" tileheight="8" tilecount="2" columns="2"><image source="cells.png" width="16" height="8"/><properties><property name="entityType" value="`+tt.templateType+`"/></properties></tileset>`), "cells.tsx", ".")
			if err != nil {
				t.Fatal(err)
			}
			m := &tiled.Map{
				Width: 2, Height: 1, TileWidth: 8, TileHeight: 8,
				Layers:       []tiled.Layer{{ID: 1, Name: "ground", Width: 2, Height: 1, Visible: true, Opacity: 1, Data: tt.gids}},
				Tilesets:     []tiled.TilesetRef{{FirstGID: 1, Tileset: ts}},
				ObjectGroups: []tiled.ObjectGroup{{Objects: []tiled.Object{{ID: 5, Type: "River", X: 0, Y: 0, Properties: props}}}},
			}
			err = ValidateSharedArt(m, 5)
			if tt.want == "" && err != nil || tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("ValidateSharedArt = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateSharedArt_SameTypeRestrictionMayUseAnotherObjectsVisual(t *testing.T) {
	ts, err := tiled.ParseTileset([]byte(`<tileset name="river" tilewidth="8" tileheight="8" tilecount="1" columns="1"><image source="river.png" width="8" height="8"/><properties><property name="entityType" value="River"/></properties></tileset>`), "river.tsx", ".")
	if err != nil {
		t.Fatal(err)
	}
	visual := tiled.Object{ID: 5, Type: "River", X: 0, Y: 0, Properties: tiled.Properties{
		"TileLink.layerID": {Type: "int", Value: "1"}, "TileVisual.image": {Value: "river.png"},
	}}
	restriction := tiled.Object{ID: 6, Type: "River", X: 0, Y: 0, Properties: tiled.Properties{
		"TileLink.layerID": {Type: "int", Value: "1"},
	}}
	m := &tiled.Map{
		Width: 1, Height: 1, TileWidth: 8, TileHeight: 8,
		Layers:       []tiled.Layer{{ID: 1, Width: 1, Height: 1, Data: []uint32{1}}},
		Tilesets:     []tiled.TilesetRef{{FirstGID: 1, Tileset: ts}},
		ObjectGroups: []tiled.ObjectGroup{{Objects: []tiled.Object{visual, restriction}}},
	}
	if err := ValidateSharedArt(m, restriction.ID); err != nil {
		t.Fatalf("restriction-only River refused despite peer artwork: %v", err)
	}
	m.ObjectGroups[0].Objects = []tiled.Object{restriction}
	if err := ValidateSharedArt(m, restriction.ID); err == nil || !strings.Contains(err.Error(), "TileVisual") {
		t.Fatalf("restriction-only River accepted without any referenced artwork: %v", err)
	}
}

func TestCanLink_RejectsSharedArtMismatchBeforeShowingACandidate(t *testing.T) {
	ts, err := tiled.ParseTileset([]byte(`<tileset name="river" tilewidth="8" tileheight="8" tilecount="2" columns="2"><image source="river.png" width="16" height="8"/><properties><property name="entityType" value="River"/></properties></tileset>`), "river.tsx", ".")
	if err != nil {
		t.Fatal(err)
	}
	m := &tiled.Map{
		Width: 2, Height: 1, TileWidth: 8, TileHeight: 8,
		Layers:   []tiled.Layer{{ID: 1, Name: "ground", Width: 2, Height: 1, Data: []uint32{1, 2}}},
		Tilesets: []tiled.TilesetRef{{FirstGID: 1, Tileset: ts}},
		ObjectGroups: []tiled.ObjectGroup{{Objects: []tiled.Object{{ID: 5, Type: "River", X: 0, Y: 0, Properties: tiled.Properties{
			"TileLink.layerID": {Type: "int", Value: "1"}, "TileVisual.image": {Value: "river.png"},
		}}}}},
	}
	if err := CanLink(m, 5, 1, Cell{1, 0}); err == nil || !strings.Contains(err.Error(), "distinct artwork") {
		t.Fatalf("candidate with different painted art was offered: %v", err)
	}
	if err := CanLink(m, 5, 1, Cell{0, 0}); err != nil {
		t.Fatalf("existing compatible link was refused: %v", err)
	}
}

func TestLink_RejectsAmbiguousAndMalformedObjectLinksWithoutPartialEdit(t *testing.T) {
	for _, tt := range []struct {
		name, malformed, want string
	}{
		{"duplicate object ID", "duplicate", "duplicated"},
		{"malformed cells", "malformed", "TileLink.cells"},
		{"different layer", "other layer", "already links layer"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, id := linkedFixture(t)
			switch tt.malformed {
			case "duplicate":
				// Parse can open hand-authored duplicate IDs, while editing cannot
				// choose one claimant. AddObject never allocates a duplicate itself.
				src := strings.Replace(string(d.Bytes()), `</objectgroup>`, `<object id="`+strconv.Itoa(id)+`" type="River" x="0" y="0"/></objectgroup>`, 1)
				var err error
				d, err = tiled.ParseDocument([]byte(src), "duplicates.tmx")
				if err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := d.SetObjectProperty(id, "TileLink.layerID", tiled.Property{Type: "int", Value: "1"}); err != nil {
					t.Fatal(err)
				}
				if err := d.SetObjectProperty(id, "TileLink.cells", tiled.Property{Value: `[{"x":0,"y":0},{"x":0,"y":0}]`}); err != nil {
					t.Fatal(err)
				}
			case "other layer":
				if err := d.SetObjectProperty(id, "TileLink.layerID", tiled.Property{Type: "int", Value: "2"}); err != nil {
					t.Fatal(err)
				}
			}
			before := string(d.Bytes())
			if changed, err := Link(d, id, 1, Cell{2, 0}); changed || err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Link = %v,%v, want %q", changed, err, tt.want)
			}
			if string(d.Bytes()) != before {
				t.Fatal("refusal changed working TMX")
			}
		})
	}
}

func TestLink_OneTileMayReferenceSeveralIndependentObjects(t *testing.T) {
	d, river := linkedFixture(t)
	gate, err := d.AddObject(0, tiled.Object{Type: "Gate", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{river, gate} {
		if changed, err := Link(d, id, 1, Cell{0, 0}); err != nil || !changed {
			t.Fatalf("linking object %d: %v,%v", id, changed, err)
		}
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	for _, obj := range m.ObjectGroups[0].Objects {
		if obj.Properties.Get("TileLink.layerID") != "1" {
			t.Errorf("object %d not linked to the same Tile", obj.ID)
		}
	}
	if changed, err := Unlink(d, gate, 1, Cell{0, 0}); err != nil || !changed {
		t.Fatalf("unlinking gate: %v,%v", changed, err)
	}
	m, err = d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if m.ObjectGroups[0].Objects[0].Properties.Get("TileLink.layerID") != "1" {
		t.Fatal("unlinking Gate also removed River from the Tile")
	}
}

func TestMove_RefusesAnOffMapAnchorWithoutChangingLinks(t *testing.T) {
	d, id := linkedFixture(t)
	for _, cell := range []Cell{{0, 0}, {2, 0}} {
		if _, err := Link(d, id, 1, cell); err != nil {
			t.Fatal(err)
		}
	}
	before := string(d.Bytes())
	if err := Move(d, id, 4, 0); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("Move outside map = %v, want refusal", err)
	}
	if got := string(d.Bytes()); got != before {
		t.Fatal("failed move changed the object anchor or TileLink offsets")
	}
}

func TestSetLayerData_RemovesErasedCellFromEveryLinkedObject(t *testing.T) {
	d, first := linkedFixture(t)
	second, err := d.AddObject(0, tiled.Object{Type: "Gate", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{first, second} {
		for _, cell := range []Cell{{0, 0}, {1, 1}} {
			if _, err := Link(d, id, 1, cell); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := SetLayerData(d, 0, []uint32{0, 0, 1, 0, 1, 0}); err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	for _, obj := range m.ObjectGroups[0].Objects {
		if got := obj.Properties.Get("TileLink.cells"); got != `[{"x":1,"y":1}]` {
			t.Errorf("object %d still links erased cell: %q", obj.ID, got)
		}
	}
}

func TestSetLayerData_DuplicateLinkObjectRefusesBeforeErasingAnything(t *testing.T) {
	d, id := linkedFixture(t)
	if _, err := Link(d, id, 1, Cell{0, 0}); err != nil {
		t.Fatal(err)
	}
	duplicate := strings.Replace(string(d.Bytes()), `</objectgroup>`, `<object id="`+strconv.Itoa(id)+`" type="River" x="0" y="0"/></objectgroup>`, 1)
	d, err := tiled.ParseDocument([]byte(duplicate), "duplicate.tmx")
	if err != nil {
		t.Fatal(err)
	}
	before := string(d.Bytes())
	if err := SetLayerData(d, 0, []uint32{0, 0, 1, 0, 1, 0}); err == nil {
		t.Fatal("erase committed despite duplicate linked object id")
	}
	if string(d.Bytes()) != before {
		t.Fatal("refused erase partially changed a linked map")
	}
}

func TestDeleteLayer_ClearsLinksButRetainsTheirIndependentObject(t *testing.T) {
	d, id := linkedFixture(t)
	for _, cell := range []Cell{{0, 0}, {2, 0}} {
		if _, err := Link(d, id, 1, cell); err != nil {
			t.Fatal(err)
		}
	}
	if err := DeleteLayer(d, 0); err != nil {
		t.Fatal(err)
	}
	m, err := d.Map()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Layers) != 0 || len(m.ObjectGroups[0].Objects) != 1 {
		t.Fatalf("deletion lost unrelated object or kept linked layer: %+v", m)
	}
	obj := m.ObjectGroups[0].Objects[0]
	if obj.ID != id || obj.Type != "River" || obj.Properties.Get("Passability.kind") != "liquid" {
		t.Fatalf("formerly linked object lost its authored values: %+v", obj)
	}
	if _, found := obj.Properties["TileLink.layerID"]; found {
		t.Fatalf("deleted layer left dangling link: %+v", obj.Properties)
	}
}

func TestDeleteLayer_DuplicateLinkedObjectRefusesBeforeAnyMutation(t *testing.T) {
	d, id := linkedFixture(t)
	if _, err := Link(d, id, 1, Cell{0, 0}); err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(d.Bytes()), `</objectgroup>`, `<object id="`+strconv.Itoa(id)+`" type="River" x="0" y="0"/></objectgroup>`, 1)
	d, err := tiled.ParseDocument([]byte(source), "duplicate.tmx")
	if err != nil {
		t.Fatal(err)
	}
	before := string(d.Bytes())
	if err := DeleteLayer(d, 0); err == nil || string(d.Bytes()) != before {
		t.Fatalf("ambiguous linked object partly deleted its layer: %v", err)
	}
}
