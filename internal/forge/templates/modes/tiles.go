package modes

import (
	"net/url"
	"path/filepath"
	"strconv"

	"github.com/a-h/templ"
	"github.com/tmbritton/ecs-db/internal/forge/tilesets"
	"github.com/tmbritton/ecs-db/internal/forge/tilesurface"
)

// tilesetRowTestID keeps rows addressable when separate maps reference files
// with the same basename in different directories.
func tilesetRowTestID(entries []tilesets.Entry, path string) string {
	base := filepath.Base(path)
	n := 0
	for _, entry := range entries {
		if filepath.Base(entry.Path) == base {
			n++
		}
		if entry.Path == path {
			break
		}
	}
	if n > 1 {
		return "tileset-" + base + "-" + strconv.Itoa(n)
	}
	return "tileset-" + base
}

func tilesetTileHref(path string, id uint32, page int) string {
	q := url.Values{"file": {path}, "tile": {strconv.FormatUint(uint64(id), 10)}}
	if page > 0 {
		q.Set("tilepage", strconv.Itoa(page))
	}
	return "/forge/tiles?" + q.Encode()
}

func tilesetPageHref(path string, page int) string {
	q := url.Values{"file": {path}, "tilepage": {strconv.Itoa(page)}}
	return "/forge/tiles?" + q.Encode()
}

// tileArtStyle uses the same source coordinates as tiled.Tileset.SourceRect,
// scaled as one sheet so the background crop lands on the intended pixels.
// A collection image uses its own dimensions as the whole sheet.
func tileArtStyle(tile tilesurface.Tile, maxPixels int) templ.SafeCSS {
	w, h := max(1, tile.SW), max(1, tile.SH)
	scale := min(6.0, float64(maxPixels)/float64(max(w, h)))
	sheetW, sheetH := tile.SheetW, tile.SheetH
	if sheetW <= 0 || sheetH <= 0 {
		sheetW, sheetH = w, h
	}
	return templ.SafeCSS(
		"width:" + ftoa(float64(w)*scale) + "px;height:" + ftoa(float64(h)*scale) + "px;" +
			"background-image:url(" + tile.ImageURL + ");" +
			"background-size:" + ftoa(float64(sheetW)*scale) + "px " + ftoa(float64(sheetH)*scale) + "px;" +
			"background-position:" + ftoa(-float64(tile.SX)*scale) + "px " + ftoa(-float64(tile.SY)*scale) + "px")
}

func tilesetPropertyType(p tilesurface.Property) string {
	if p.PropertyType != "" {
		return p.Type + "/" + p.PropertyType
	}
	return p.Type
}
