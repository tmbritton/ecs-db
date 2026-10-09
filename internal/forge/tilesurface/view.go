// Package tilesurface prepares the Tiled reading model for Forge's read-only
// grid and inspector. It never changes the editing document.
package tilesurface

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

const PageSize = 256

type Property struct {
	Name, Type, PropertyType, Value string
}

type Tile struct {
	ID         uint32
	Class      string
	Explicit   bool
	Properties []Property
	Image      string // empty if no project-contained image can be served
	ImageURL   string // filled by the HTTP adapter; never by this reading model
	MissingArt bool
	SX, SY     int
	SW, SH     int
	SheetW     int
	SheetH     int
}

type View struct {
	Tiles       []Tile
	Selected    *Tile
	Collection  bool
	Problem     string
	PageProblem string
	Page, Pages int
	TileCount   int
	Properties  []Property
}

// Build makes at most PageSize cells even for a large sheet. A collection only
// exposes its explicitly authored IDs; they need not be contiguous. available
// is supplied by the HTTP boundary and answers whether a picture can be served
// through MAP's existing project-scoped image route.
func Build(set *tiled.Tileset, selected, page string, available func(string) bool) View {
	var v View
	if set == nil {
		return v
	}
	v.Collection = set.Collection()
	v.Properties = properties(set.Properties)
	if v.Collection {
		v.TileCount = len(set.Tiles)
	} else if set.TileCount > 0 {
		v.TileCount = set.TileCount
	}
	if !v.Collection && uint64(v.TileCount) > uint64(^uint32(0))+1 {
		v.Problem = "this tileset declares more local tiles than a Tiled ID can address"
		return v
	}
	if v.TileCount == 0 {
		if selected != "" {
			v.Problem = fmt.Sprintf("local tile %q is not in this tileset", selected)
		}
		return v
	}
	v.Pages = 1 + (v.TileCount-1)/PageSize
	var ids []uint32
	if v.Collection {
		ids = make([]uint32, 0, len(set.Tiles))
		for id := range set.Tiles {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	}
	if page != "" {
		parsed, err := strconv.ParseUint(page, 10, 64)
		if err != nil || parsed >= uint64(v.Pages) {
			v.PageProblem = fmt.Sprintf("page %q is not in this tileset", page)
			return v
		}
		v.Page = int(parsed)
	} else if selected != "" {
		if id, err := strconv.ParseUint(selected, 10, 32); err == nil {
			if v.Collection {
				index := sort.Search(len(ids), func(i int) bool { return ids[i] >= uint32(id) })
				if index < len(ids) && ids[index] == uint32(id) {
					v.Page = index / PageSize
				}
			} else if uint64(id) < uint64(v.TileCount) {
				v.Page = int(id) / PageSize
			}
		}
	}
	start := v.Page * PageSize
	end := min(start+PageSize, v.TileCount)
	cache := map[string]bool{}
	imageOK := func(path string) bool {
		if path == "" || available == nil {
			return false
		}
		if ok, found := cache[path]; found {
			return ok
		}
		ok := available(path)
		cache[path] = ok
		return ok
	}
	makeTile := func(id uint32) Tile {
		tile := Tile{ID: id, SW: set.TileWidth, SH: set.TileHeight}
		if explicit, ok := set.Tiles[id]; ok {
			tile.Class = explicit.Type
			tile.Explicit = true
			tile.Properties = properties(explicit.Properties)
		}
		if v.Collection {
			image := set.Tiles[id].Image
			tile.SW, tile.SH = image.Width, image.Height
			if imageOK(image.Path) {
				tile.Image = image.Path
			}
		} else {
			x, y, w, h, ok := set.SourceRect(id)
			if ok {
				tile.SX, tile.SY, tile.SW, tile.SH = x, y, w, h
			}
			tile.SheetW, tile.SheetH = set.Image.Width, set.Image.Height
			if ok && imageOK(set.Image.Path) {
				tile.Image = set.Image.Path
			}
		}
		tile.MissingArt = tile.Image == ""
		return tile
	}
	v.Tiles = make([]Tile, 0, end-start)
	for n := start; n < end; n++ {
		id := uint32(n)
		if v.Collection {
			id = ids[n]
		}
		v.Tiles = append(v.Tiles, makeTile(id))
	}
	if selected == "" {
		tile := v.Tiles[0]
		v.Selected = &tile
		return v
	}
	id, err := strconv.ParseUint(selected, 10, 32)
	if err != nil || (!v.Collection && id >= uint64(v.TileCount)) {
		v.Problem = fmt.Sprintf("local tile %q is not in this tileset", selected)
		return v
	}
	if v.Collection {
		if _, ok := set.Tiles[uint32(id)]; !ok {
			v.Problem = fmt.Sprintf("local tile %q is not in this tileset", selected)
			return v
		}
	}
	tile := makeTile(uint32(id))
	v.Selected = &tile
	return v
}

func properties(all tiled.Properties) []Property {
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]Property, 0, len(names))
	for _, name := range names {
		p := all[name]
		result = append(result, Property{Name: name, Type: p.Type, PropertyType: p.PropertyType, Value: p.Value})
	}
	return result
}
