package tiled

import (
	"encoding/json"
	"fmt"
)

// The JSON tileset. Tiled writes .tsj files and embeds the same object in a
// .tmj map, so a package that read only XML would load a JSON project's map and
// then die on its tilesets — which is what this did until review, with an EOF
// that named no cause.

type jsonTileset struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Class       string         `json:"class"`
	TileWidth   int            `json:"tilewidth"`
	TileHeight  int            `json:"tileheight"`
	TileCount   int            `json:"tilecount"`
	Columns     int            `json:"columns"`
	Spacing     int            `json:"spacing"`
	Margin      int            `json:"margin"`
	Image       string         `json:"image"`
	ImageWidth  int            `json:"imagewidth"`
	ImageHeight int            `json:"imageheight"`
	Trans       string         `json:"transparentcolor"`
	TileOffset  *jsonOffset    `json:"tileoffset"`
	Properties  []jsonProperty `json:"properties"`
	Tiles       []jsonTsjTile  `json:"tiles"`
}

type jsonOffset struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type jsonTsjTile struct {
	ID          uint32         `json:"id"`
	Type        string         `json:"type"`
	Class       string         `json:"class"`
	Image       string         `json:"image"`
	ImageWidth  int            `json:"imagewidth"`
	ImageHeight int            `json:"imageheight"`
	Properties  []jsonProperty `json:"properties"`
}

func parseTSJ(data []byte, name, dir string) (*Tileset, error) {
	var wire jsonTileset
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("tiled: %s: %w", name, err)
	}
	// A .tsj carries "type": "tileset"; the object a .tmj embeds does not,
	// because the array it sits in says what it is.
	if wire.Type != "" && wire.Type != "tileset" {
		return nil, fmt.Errorf("tiled: %s is JSON of type %q, not a tileset", name, wire.Type)
	}

	ts := &Tileset{
		Name:       wire.Name,
		Class:      wire.Class,
		TileWidth:  wire.TileWidth,
		TileHeight: wire.TileHeight,
		TileCount:  wire.TileCount,
		Columns:    wire.Columns,
		Spacing:    wire.Spacing,
		Margin:     wire.Margin,
		Image: Image{
			Source: wire.Image,
			Path:   resolvePath(dir, wire.Image),
			Trans:  wire.Trans,
			Width:  wire.ImageWidth,
			Height: wire.ImageHeight,
		},
		Properties: convertJSONProperties(wire.Properties),
	}
	if wire.TileOffset != nil {
		ts.TileOffsetX, ts.TileOffsetY = wire.TileOffset.X, wire.TileOffset.Y
	}
	for _, tile := range wire.Tiles {
		kind := tile.Type
		if kind == "" {
			kind = tile.Class
		}
		if err := ts.addTile(TilesetTile{
			ID:   tile.ID,
			Type: kind,
			Image: Image{
				Source: tile.Image,
				Path:   resolvePath(dir, tile.Image),
				Width:  tile.ImageWidth,
				Height: tile.ImageHeight,
			},
			Properties: convertJSONProperties(tile.Properties),
		}, name); err != nil {
			return nil, err
		}
	}
	return ts, ts.check(name)
}
