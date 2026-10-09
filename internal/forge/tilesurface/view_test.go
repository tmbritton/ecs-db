package tilesurface

import (
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

func TestBuild_SheetUsesTilesetRectsAndImplicitLocalIDs(t *testing.T) {
	sheet := &tiled.Tileset{
		Name: "atlas", TileWidth: 16, TileHeight: 16, TileCount: 5, Columns: 2,
		Margin: 1, Spacing: 2,
		Image: tiled.Image{Source: "atlas.png", Path: "atlas.png", Width: 37, Height: 55},
		Tiles: map[uint32]tiled.TilesetTile{3: {ID: 3, Type: "wall", Properties: tiled.Properties{
			"hardness": {Type: "int", Value: "4"}, "locked": {Type: "bool", Value: "true"},
		}}},
	}
	v := Build(sheet, "3", "", func(path string) bool { return path == "atlas.png" })
	if v.Collection || len(v.Tiles) != 5 || v.Selected == nil || v.Selected.ID != 3 || v.Pages != 1 || v.Page != 0 {
		t.Fatalf("sheet grid/selection = %+v", v)
	}
	if got := v.Tiles[3]; got.ID != 3 || got.SX != 19 || got.SY != 19 || got.SW != 16 || got.SH != 16 || got.Image != "atlas.png" || got.Class != "wall" || len(got.Properties) != 2 || got.Properties[0].Name != "hardness" {
		t.Fatalf("sheet tile rect/metadata = %+v", got)
	}
	if got := v.Tiles[0]; got.ID != 0 || got.Explicit || got.Class != "" {
		t.Fatalf("implicit sheet tile invented metadata: %+v", got)
	}
}

func TestBuild_CollectionKeepsSparseIDsAndIndividualImages(t *testing.T) {
	set := &tiled.Tileset{TileCount: 2, Tiles: map[uint32]tiled.TilesetTile{
		21: {ID: 21, Type: "portal", Image: tiled.Image{Path: "missing.png", Width: 24, Height: 32}},
		1:  {ID: 1, Type: "door", Image: tiled.Image{Path: "door.png", Width: 16, Height: 16}},
	}}
	v := Build(set, "", "", func(path string) bool { return path == "door.png" })
	if !v.Collection || len(v.Tiles) != 2 || v.Tiles[0].ID != 1 || v.Tiles[1].ID != 21 || v.Selected == nil || v.Selected.ID != 1 {
		t.Fatalf("sparse collection grid = %+v", v)
	}
	if v.Tiles[0].MissingArt || !v.Tiles[1].MissingArt || v.Tiles[1].Image != "" || v.Tiles[1].SW != 24 || v.Tiles[1].SH != 32 {
		t.Fatalf("collection image fallback = %+v", v.Tiles)
	}
}

func TestBuild_CollectionDeepLinkOpensThePageContainingTheTile(t *testing.T) {
	set := &tiled.Tileset{Tiles: map[uint32]tiled.TilesetTile{}}
	for i := uint32(0); i < 300; i++ {
		set.Tiles[2*i] = tiled.TilesetTile{ID: 2 * i}
	}
	v := Build(set, "598", "", func(string) bool { return false })
	if v.Page != 1 || len(v.Tiles) != 44 || v.Tiles[0].ID != 512 || v.Selected == nil || v.Selected.ID != 598 {
		t.Fatalf("sparse tile link missed its grid page: page %d, tiles %d, selected %+v", v.Page, len(v.Tiles), v.Selected)
	}
}

func TestBuild_InvalidSelectionDoesNotRetarget(t *testing.T) {
	for _, tt := range []struct {
		name, selected string
		set            *tiled.Tileset
		wantProblem    bool
	}{
		{"not a number", "bad", &tiled.Tileset{TileCount: 1, Columns: 1}, true},
		{"outside sheet", "9", &tiled.Tileset{TileCount: 1, Columns: 1}, true},
		{"collection gap", "10", &tiled.Tileset{Tiles: map[uint32]tiled.TilesetTile{21: {ID: 21}}}, true},
		{"empty", "", &tiled.Tileset{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := Build(tt.set, tt.selected, "", func(string) bool { return false })
			if (v.Problem != "") != tt.wantProblem || v.Selected != nil {
				t.Fatalf("invalid selection = %+v", v)
			}
		})
	}
}

func TestBuild_LargeSheetPagesDoNotAllocateEveryTile(t *testing.T) {
	set := &tiled.Tileset{
		TileWidth: 8, TileHeight: 8, TileCount: 1_000_000, Columns: 1000,
		Image: tiled.Image{Source: "sheet.png", Path: "sheet.png", Width: 8000, Height: 8000},
	}
	v := Build(set, "999999", "3906", func(string) bool { return true })
	if len(v.Tiles) != 64 || v.Pages != 3907 || v.Page != 3906 || v.Selected == nil || v.Selected.ID != 999999 || v.Tiles[0].ID != 999936 {
		t.Fatalf("last page/selection: %d tiles, page %d/%d, selected %+v", len(v.Tiles), v.Page, v.Pages, v.Selected)
	}
	if bad := Build(set, "", "9999999999", func(string) bool { return true }); bad.PageProblem == "" || len(bad.Tiles) != 0 {
		t.Fatalf("out-of-range page was silently accepted: %+v", bad)
	}
}

func TestBuild_SheetBeyondUint32IDsReportsProblemRatherThanWrapping(t *testing.T) {
	set := &tiled.Tileset{
		TileCount: int(uint64(^uint32(0))) + 2, Columns: 1,
		Image: tiled.Image{Source: "huge.png", Path: "huge.png"},
	}
	v := Build(set, "", "", func(string) bool { return true })
	if v.Problem == "" || len(v.Tiles) != 0 {
		t.Fatalf("unaddressable IDs were offered as wrapped local tiles: %d tiles, %q", len(v.Tiles), v.Problem)
	}
}
