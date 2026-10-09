package tilesetvalidation

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/tilesurface"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func TestCheck_AttributesImageAndSourceRectangleFailuresToVisibleTiles(t *testing.T) {
	set := &tiled.Tileset{
		Name: "sheet", TileCount: 3, Columns: 3, TileWidth: 16, TileHeight: 16,
		Image:      tiled.Image{Source: "sheet.png", Path: "/proj/sheet.png", Width: 32, Height: 16},
		Properties: tiled.Properties{"entityType": {Type: "string", Value: "Wall"}},
	}
	view := tilesurface.Build(set, "2", "", func(string) bool { return true })
	report := Check(set, view, nil, nil, nil)
	if len(report.Tiles[0]) != 0 || len(report.Tiles[1]) != 0 || len(report.Tiles[2]) != 1 ||
		!strings.Contains(report.Tiles[2][0].Message, "outside") || report.Errors != 1 {
		t.Fatalf("sheet rectangle findings = %+v", report)
	}
	missing := tilesurface.Build(set, "0", "", func(string) bool { return false })
	missingReport := Check(set, missing, nil, nil, nil)
	if len(missingReport.Tileset) != 1 || len(missingReport.Tiles[0]) == 0 ||
		!strings.Contains(missingReport.Tileset[0].Message, "sheet.png") {
		t.Fatalf("missing sheet image did not name file and cell: %+v", missingReport)
	}
}

func TestCheck_CollectionImageAndTypedPropertyWarningsAreAttributed(t *testing.T) {
	set := &tiled.Tileset{TileCount: 2, Properties: tiled.Properties{"entityType": {Type: "string", Value: "Wall"}}, Tiles: map[uint32]tiled.TilesetTile{
		1: {
			ID: 1, Type: "wall", Image: tiled.Image{Source: "one.png", Path: "/proj/one.png", Width: 16, Height: 16},
			Properties: tiled.Properties{"entityType": {Type: "string", Value: "NotInSchema"}, "passable": {Type: "bool", Value: "true"}, "height": {Type: "int", Value: "many"}},
		},
		21: {ID: 21, Image: tiled.Image{Source: "missing.png", Path: "/proj/missing.png", Width: 16, Height: 16}},
	}}
	view := tilesurface.Build(set, "21", "", func(path string) bool { return strings.HasSuffix(path, "one.png") })
	report := Check(set, view, map[string]bool{"Wall": true}, nil, nil)
	if len(report.Tiles[1]) != 3 || len(report.Tiles[21]) != 1 || report.Errors != 4 || report.Warnings != 0 {
		t.Fatalf("collection findings = %+v", report)
	}
	for _, finding := range report.Tiles[1] {
		if strings.Contains(finding.Message, "passable") && (!strings.Contains(finding.Message, "refuses") || finding.Warning) {
			t.Errorf("passable was reported as harmless instead of an import refusal: %+v", finding)
		}
	}
	for _, needle := range []string{"NotInSchema", "passable", "whole number"} {
		combined := ""
		for _, f := range report.Tiles[1] {
			combined += f.Message
		}
		if !strings.Contains(combined, needle) {
			t.Errorf("tile 1 findings omit %q: %+v", needle, report.Tiles[1])
		}
	}
}

func TestCheck_DeprecatedTilesetPassableIsAnImportRefusal(t *testing.T) {
	set := &tiled.Tileset{
		TileCount: 1, Columns: 1, TileWidth: 8, TileHeight: 8,
		Image:      tiled.Image{Source: "art.png", Path: "art.png", Width: 8, Height: 8},
		Properties: tiled.Properties{"passable": {Type: "bool", Value: "true"}, "entityType": {Type: "string", Value: "Wall"}},
	}
	view := tilesurface.Build(set, "0", "", func(string) bool { return true })
	report := Check(set, view, nil, nil, nil)
	if report.Errors != 1 || report.Warnings != 0 || len(report.Tileset) != 1 || !strings.Contains(report.Tileset[0].Message, "refuses") {
		t.Fatalf("deprecated tileset passable import refusal = %+v", report)
	}
}

func TestCheck_BoundsWorkToVisiblePageAndSelectedTile(t *testing.T) {
	set := &tiled.Tileset{
		TileCount: 1_000_000, Columns: 1000, TileWidth: 8, TileHeight: 8,
		Image:      tiled.Image{Source: "sheet.png", Path: "/proj/sheet.png", Width: 8000, Height: 8000},
		Properties: tiled.Properties{"entityType": {Type: "string", Value: "Wall"}},
	}
	view := tilesurface.Build(set, "999999", "3906", func(string) bool { return false })
	report := Check(set, view, nil, nil, nil)
	if len(report.Tiles) != 64 || len(report.Tileset) != 1 {
		t.Fatalf("validation scanned beyond the visible page: tiles %d, set %+v", len(report.Tiles), report.Tileset)
	}
}

func TestCheck_AmbiguousClassIsAttributedToItsTile(t *testing.T) {
	set := &tiled.Tileset{
		TileCount: 2, Columns: 2, TileWidth: 16, TileHeight: 16,
		Image:      tiled.Image{Source: "art.png", Path: "art.png", Width: 32, Height: 16},
		Properties: tiled.Properties{"entityType": {Type: "string", Value: "Wall"}},
	}
	view := tilesurface.Build(set, "1", "", func(string) bool { return true })
	report := Check(set, view, nil, []uint32{1}, nil)
	if report.Errors != 1 || len(report.Tiles[1]) != 1 || !strings.Contains(report.Tiles[1][0].Message, "both type and class") || len(report.Tiles[0]) != 0 {
		t.Fatalf("ambiguous tile metadata = %+v", report)
	}
}

func TestCheck_MissingEntityTemplateExplainsWhyPaintingCannotImportATile(t *testing.T) {
	set := &tiled.Tileset{
		TileCount: 1, Columns: 1, TileWidth: 8, TileHeight: 8,
		Image: tiled.Image{Source: "art.png", Path: "art.png", Width: 8, Height: 8},
	}
	view := tilesurface.Build(set, "0", "", func(string) bool { return true })
	report := Check(set, view, nil, nil, nil)
	if report.Errors != 1 || len(report.Tiles[0]) != 1 || !strings.Contains(report.Tiles[0][0].Message, "no entityType") {
		t.Fatalf("missing template had no tile-attributed refusal: %+v", report)
	}
}

func TestCheck_EmptyTileEntityTypeFallsBackToTheTilesetTemplate(t *testing.T) {
	set := &tiled.Tileset{
		TileCount: 1, Columns: 1, TileWidth: 8, TileHeight: 8,
		Image:      tiled.Image{Source: "art.png", Path: "art.png", Width: 8, Height: 8},
		Properties: tiled.Properties{"entityType": {Type: "string", Value: "Wall"}},
		Tiles:      map[uint32]tiled.TilesetTile{0: {ID: 0, Properties: tiled.Properties{"entityType": {Type: "string", Value: ""}}}},
	}
	view := tilesurface.Build(set, "0", "", func(string) bool { return true })
	report := Check(set, view, map[string]bool{"Wall": true}, nil, nil)
	if report.Errors != 0 || len(report.Tiles[0]) != 0 {
		t.Fatalf("empty per-tile entityType should use the declared tileset fallback: %+v", report)
	}
}

func TestCheck_SchemaComponentFieldRefusalsUseTheEffectiveTileTemplate(t *testing.T) {
	set := &tiled.Tileset{
		TileCount: 2, Columns: 2, TileWidth: 8, TileHeight: 8,
		Image:      tiled.Image{Source: "art.png", Path: "art.png", Width: 16, Height: 8},
		Properties: tiled.Properties{"entityType": {Value: "Wall"}, "Health.hp": {Type: "string", Value: "lots"}},
		Tiles:      map[uint32]tiled.TilesetTile{1: {ID: 1, Properties: tiled.Properties{"Health.hp": {Type: "string", Value: "5"}}}},
	}
	view := tilesurface.Build(set, "0", "", func(string) bool { return true })
	db := &schema.DatabaseSchema{Components: map[string]schema.Component{
		"Health": {Type: "object", Properties: map[string]schema.Property{"hp": {Type: "integer"}}},
	}}
	report := Check(set, view, map[string]bool{"Wall": true}, nil, db)
	if report.Errors != 1 || len(report.Tiles[0]) != 1 || !strings.Contains(report.Tiles[0][0].Message, "whole number") || len(report.Tiles[1]) > 0 {
		t.Fatalf("effective schema-typed template field findings = %+v", report)
	}
}

func TestCheck_EntityTypeComponentContractChecksEachVisibleTile(t *testing.T) {
	set := &tiled.Tileset{
		TileCount: 2, Columns: 2, TileWidth: 8, TileHeight: 8,
		Image:      tiled.Image{Source: "art.png", Path: "art.png", Width: 16, Height: 8},
		Properties: tiled.Properties{"entityType": {Value: "Wall"}},
		Tiles:      map[uint32]tiled.TilesetTile{1: {ID: 1, Properties: tiled.Properties{"Health.hp": {Type: "string", Value: "5"}}}},
	}
	view := tilesurface.Build(set, "0", "", func(string) bool { return true })
	db := &schema.DatabaseSchema{Components: map[string]schema.Component{
		"Health":     {Type: "object", Properties: map[string]schema.Property{"hp": {Type: "integer"}}},
		"TileVisual": {Type: "object"}, "TileEntityOwner": {Type: "entityRef"},
	}, EntityTypes: map[string]schema.EntityType{
		"Wall": {RequiredComponents: []string{"Health"}, OptionalComponents: []string{"TileVisual", "TileEntityOwner"}, ValidationLevel: schema.ValidationStrict},
	}}
	report := Check(set, view, map[string]bool{"Wall": true}, nil, db)
	if len(report.Tiles[0]) != 1 || !strings.Contains(report.Tiles[0][0].Message, "missing required component") || len(report.Tiles[1]) != 0 {
		t.Fatalf("per-tile effective component contract findings = %+v", report)
	}
}

func TestCheck_TilesetTypeOverrideDoesNotCondemnValidTiles(t *testing.T) {
	set := &tiled.Tileset{
		TileCount: 1, Columns: 1, TileWidth: 8, TileHeight: 8,
		Image:      tiled.Image{Source: "art.png", Path: "art.png", Width: 8, Height: 8},
		Properties: tiled.Properties{"entityType": {Value: "RemovedType"}},
		Tiles:      map[uint32]tiled.TilesetTile{0: {ID: 0, Properties: tiled.Properties{"entityType": {Value: "Wall"}}}},
	}
	view := tilesurface.Build(set, "0", "", func(string) bool { return true })
	db := &schema.DatabaseSchema{Components: map[string]schema.Component{
		"TileVisual": {Type: "object"}, "TileEntityOwner": {Type: "entityRef"},
	}, EntityTypes: map[string]schema.EntityType{
		"Wall": {OptionalComponents: []string{"TileVisual", "TileEntityOwner"}, ValidationLevel: schema.ValidationStrict},
	}}
	report := Check(set, view, map[string]bool{"Wall": true}, nil, db)
	if report.Errors != 0 || len(report.Tileset) > 0 || len(report.Tiles[0]) > 0 {
		t.Fatalf("overridden, unreferenced tileset template was reported as an import refusal: %+v", report)
	}
}
