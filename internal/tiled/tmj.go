package tiled

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// The JSON wire structs, for the same reason the XML ones exist: they are
// unmarshalled into and converted, and the exported Map is what everything
// above this package sees.

type jsonMap struct {
	Type        string            `json:"type"`
	Orientation string            `json:"orientation"`
	RenderOrder string            `json:"renderorder"`
	Width       int               `json:"width"`
	Height      int               `json:"height"`
	TileWidth   int               `json:"tilewidth"`
	TileHeight  int               `json:"tileheight"`
	Infinite    bool              `json:"infinite"`
	Properties  []jsonProperty    `json:"properties"`
	Tilesets    []json.RawMessage `json:"tilesets"`
	Layers      []jsonLayer       `json:"layers"`
}

type jsonProperty struct {
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	PropertyType string          `json:"propertytype"`
	Value        json.RawMessage `json:"value"`
}

// jsonTilesetRef is a map's reference: the first gid, plus either a source or
// the whole embedded tileset object, which tsj.go parses.
type jsonTilesetRef struct {
	FirstGID uint32 `json:"firstgid"`
	Source   string `json:"source"`
}

type jsonLayer struct {
	ID          int             `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Width       int             `json:"width"`
	Height      int             `json:"height"`
	Visible     *bool           `json:"visible"`
	Opacity     *float64        `json:"opacity"`
	Encoding    string          `json:"encoding"`
	Compression string          `json:"compression"`
	Data        json.RawMessage `json:"data"`
	Objects     []jsonObject    `json:"objects"`
	Layers      []jsonLayer     `json:"layers"`
	Properties  []jsonProperty  `json:"properties"`
}

type jsonObject struct {
	ID         int            `json:"id"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Class      string         `json:"class"`
	X          float64        `json:"x"`
	Y          float64        `json:"y"`
	Width      float64        `json:"width"`
	Height     float64        `json:"height"`
	GID        uint32         `json:"gid"`
	Rotation   float64        `json:"rotation"`
	Visible    *bool          `json:"visible"`
	Template   string         `json:"template"`
	Properties []jsonProperty `json:"properties"`
}

func parseTMJ(data []byte, name string) (*Map, error) {
	var wire jsonMap
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("tiled: %s: %w", name, err)
	}
	// A .tmj always carries "type": "map"; a tileset carries "tileset". Some
	// hand-written files omit it, so a map is also recognised by having the
	// dimensions a map must have.
	if wire.Type != "" && wire.Type != "map" {
		return nil, fmt.Errorf("tiled: %s is JSON of type %q, not a map", name, wire.Type)
	}
	if wire.Type == "" && (wire.Width == 0 || wire.Height == 0) {
		return nil, fmt.Errorf("tiled: %s is JSON but says nothing about a map's size", name)
	}

	if wire.Infinite {
		// Chunked layer data, which nothing here decodes. Refused by name:
		// without this the chunks decode to nothing and the message is "this
		// layer holds 0 tiles but is 1024 cells", which reads as a corrupt map
		// and sends someone looking for a hole that is not there.
		return nil, fmt.Errorf("tiled: %s is an infinite map, which this reader does not decode; "+
			"in Tiled, Map ▸ Map Properties ▸ Infinite ▸ off, then save", name)
	}
	if err := checkSize(wire.Width, wire.Height, wire.TileWidth, wire.TileHeight, name); err != nil {
		return nil, err
	}
	m := &Map{
		Width:       wire.Width,
		Height:      wire.Height,
		TileWidth:   wire.TileWidth,
		TileHeight:  wire.TileHeight,
		Orientation: wire.Orientation,
		RenderOrder: wire.RenderOrder,
		Infinite:    wire.Infinite,
		Properties:  convertJSONProperties(wire.Properties),
	}
	for _, raw := range wire.Tilesets {
		var ts jsonTilesetRef
		if err := json.Unmarshal(raw, &ts); err != nil {
			return nil, fmt.Errorf("tiled: %s: a tileset reference will not read: %w", name, err)
		}
		ref := TilesetRef{FirstGID: ts.FirstGID, Source: ts.Source}
		if ts.Source == "" {
			// Defined inline, kept whole for the reason the XML form keeps it.
			ref.Embedded = append([]byte(nil), raw...)
		}
		m.Tilesets = append(m.Tilesets, ref)
	}
	if err := collectJSON(m, wire.Layers, true, name); err != nil {
		return nil, err
	}
	return m, nil
}

// collectJSON flattens layers and layer folders, for the reason collectXML does.
func collectJSON(m *Map, layers []jsonLayer, visible bool, name string) error {
	for _, l := range layers {
		shown := visible && (l.Visible == nil || *l.Visible)
		switch l.Type {
		case "objectgroup":
			group := l.convertObjectGroup()
			group.Visible = shown
			m.ObjectGroups = append(m.ObjectGroups, group)
		case "group":
			// A layer folder holds the real layers. Dropping it would load a
			// map with everything in it as empty, silently.
			if err := collectJSON(m, l.Layers, shown, name); err != nil {
				return err
			}
		case "tilelayer", "":
			layer, err := l.convertTileLayer(name)
			if err != nil {
				return err
			}
			layer.Visible = shown
			if err := checkLayerSize(layer.Width, layer.Height, m.Width, m.Height, name, layer.Name); err != nil {
				return err
			}
			m.Layers = append(m.Layers, layer)
		default:
			// An image layer: not modelled, and nothing the engine reads is in
			// one. Unlike a group, it holds no other layer.
		}
	}
	return nil
}

func (l jsonLayer) convertTileLayer(name string) (Layer, error) {
	out := Layer{
		ID:         l.ID,
		Name:       l.Name,
		Width:      l.Width,
		Height:     l.Height,
		Visible:    l.Visible == nil || *l.Visible,
		Opacity:    1,
		Properties: convertJSONProperties(l.Properties),
	}
	if l.Opacity != nil {
		out.Opacity = *l.Opacity
	}

	cells := l.Width * l.Height
	switch {
	case len(l.Data) == 0:
		// No data at all is an empty layer rather than a fault.
	case l.Data[0] == '[':
		// The CSV equivalent: a plain array of numbers.
		if err := json.Unmarshal(l.Data, &out.Data); err != nil {
			return Layer{}, fmt.Errorf("tiled: %s: layer %q has data that is not a list of tile ids: %w",
				name, l.Name, err)
		}
	case l.Data[0] == '"':
		var text string
		if err := json.Unmarshal(l.Data, &text); err != nil {
			return Layer{}, fmt.Errorf("tiled: %s: layer %q: %w", name, l.Name, err)
		}
		encoding := l.Encoding
		if encoding == "" {
			// A string with no encoding named can only be base64; Tiled writes
			// the attribute, but a hand-written file may not.
			encoding = "base64"
		}
		var err error
		out.Data, err = decodeLayerData(encoding, l.Compression, text, cells, name, l.Name)
		if err != nil {
			return Layer{}, err
		}
	default:
		return Layer{}, fmt.Errorf("tiled: %s: layer %q has data that is neither a list nor a string",
			name, l.Name)
	}
	return out, checkCells(len(out.Data), cells, name, l.Name)
}

func (l jsonLayer) convertObjectGroup() ObjectGroup {
	out := ObjectGroup{
		ID:         l.ID,
		Name:       l.Name,
		Visible:    l.Visible == nil || *l.Visible,
		Properties: convertJSONProperties(l.Properties),
	}
	for _, o := range l.Objects {
		kind := o.Type
		if kind == "" {
			kind = o.Class
		}
		out.Objects = append(out.Objects, Object{
			ID:         o.ID,
			Name:       o.Name,
			Type:       kind,
			X:          o.X,
			Y:          o.Y,
			Width:      o.Width,
			Height:     o.Height,
			GID:        o.GID,
			Rotation:   o.Rotation,
			Visible:    o.Visible == nil || *o.Visible,
			Template:   o.Template,
			Properties: convertJSONProperties(o.Properties),
		})
	}
	return out
}

// convertJSONProperties flattens JSON's typed values back to the strings the
// XML form carries, so both serialisations produce identical Properties and the
// accessors are written once.
func convertJSONProperties(props []jsonProperty) Properties {
	if len(props) == 0 {
		return nil
	}
	out := make(Properties, len(props))
	for _, p := range props {
		kind := p.Type
		if kind == "" {
			kind = "string"
		}
		out[p.Name] = Property{Type: kind, PropertyType: p.PropertyType, Value: jsonValueText(p.Value)}
	}
	return out
}

// jsonValueText is a JSON property value as the string the XML form would have
// written. A quoted string loses its quotes; everything else is its own
// literal, which for numbers and booleans is exactly what Tiled writes as an
// XML attribute.
func jsonValueText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
	}
	// A whole number written as a float — JSON's 9.0 for Tiled's XML 9 — is
	// normalised to the integer, so the same property read from the two
	// serialisations gives the same string and the same Int(). The guard this
	// replaced excluded exactly the case it was written for: it skipped any
	// value containing a dot, which is every value it could have fixed.
	if f, err := strconv.ParseFloat(string(raw), 64); err == nil && f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return string(raw)
}
