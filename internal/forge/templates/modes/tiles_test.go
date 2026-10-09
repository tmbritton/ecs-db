package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/tilesurface"
)

func TestTileArtStyle_LargeCollectionThumbnailFitsItsGridCell(t *testing.T) {
	for _, tt := range []struct {
		name     string
		art      tilesurface.Tile
		max      int
		wantSize string
	}{
		{"wide collection", tilesurface.Tile{SW: 192, SH: 128, ImageURL: "/forge/asset?path=wide.png"}, 48, "width:48px;height:32px"},
		{"tall collection", tilesurface.Tile{SW: 128, SH: 240, ImageURL: "/forge/asset?path=tall.png"}, 48, "width:25.6px;height:48px"},
		{"sheet crop", tilesurface.Tile{SW: 16, SH: 16, SX: 19, SY: 19, SheetW: 37, SheetH: 55, ImageURL: "/forge/asset?path=sheet.png"}, 48, "background-position:-57px -57px"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			style := string(tileArtStyle(tt.art, tt.max))
			if !strings.Contains(style, tt.wantSize) {
				t.Fatalf("art CSS %q omitted %q", style, tt.wantSize)
			}
		})
	}
}
