package tiled

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
)

// The XML wire structs. They exist only to be unmarshalled into and then
// converted; nothing outside this file sees them, which is what lets the
// exported Map stay the same shape for both serialisations.

type xmlMap struct {
	XMLName     xml.Name      `xml:"map"`
	Version     string        `xml:"version,attr"`
	Orientation string        `xml:"orientation,attr"`
	RenderOrder string        `xml:"renderorder,attr"`
	Width       int           `xml:"width,attr"`
	Height      int           `xml:"height,attr"`
	TileWidth   int           `xml:"tilewidth,attr"`
	TileHeight  int           `xml:"tileheight,attr"`
	Infinite    int           `xml:"infinite,attr"`
	Properties  xmlProperties `xml:"properties"`
	Tilesets    []xmlTileset  `xml:"tileset"`
	Children    []xmlNode     `xml:",any"`
}

// xmlNode is one of the things a map or a layer folder holds: a tile layer, an
// object layer, or another folder. One union struct rather than three slices,
// because three slices are three passes and the order between them is the
// file's layer order — which is draw order, and which Story 4 reads a cell's
// passability from.
//
// encoding/xml fills a ",any" field with every child element no other field
// claimed, in the order it read them, and that is the only place the order
// survives. With a slice per element type, a <group> written above a <layer>
// came out below it while the JSON reader — one pass over one list — put it
// above: the same map, two answers, depending on which serialisation someone
// had saved it as.
//
// A layer folder is Tiled's "organize the layers of the map in a hierarchy",
// and dragging two layers into one is a routine editor action. A reader that
// skipped it would load a map with every tile and every spawn in it as empty,
// silently, which is the exact failure this package argues against.
type xmlNode struct {
	XMLName xml.Name

	// Common to all three.
	ID         int           `xml:"id,attr"`
	Name       string        `xml:"name,attr"`
	Visible    *int          `xml:"visible,attr"`
	Properties xmlProperties `xml:"properties"`

	// A tile layer's.
	Width   int      `xml:"width,attr"`
	Height  int      `xml:"height,attr"`
	Opacity *float64 `xml:"opacity,attr"`
	Data    xmlData  `xml:"data"`

	// An object layer's.
	Objects []xmlObject `xml:"object"`

	// A folder's layers — and, for an element this reader does not model, that
	// element's children: an <imagelayer>'s <image> lands here too, because
	// ",any" catches whatever no named field above claimed. collectXML only
	// descends where the element is a folder, so the rest are read and dropped
	// together with the element that held them.
	Children []xmlNode `xml:",any"`
}

type xmlProperties struct {
	Properties []xmlProperty `xml:"property"`
}

type xmlProperty struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	// PropertyType names the custom type or enum a "class"-typed property is an
	// instance of. Carried rather than dropped, for the reason the value is.
	PropertyType string `xml:"propertytype,attr"`
	// Tiled writes a value either as an attribute or, for multiline text, as
	// the element's own content — and for a class-typed property as a nested
	// <properties> block, which is neither.
	//
	// One fallback covers both of the latter: innerxml is the element's content
	// whether that content is text or markup. A separate chardata field was
	// here first and could not tell the two apart.
	Value string `xml:"value,attr"`
	Raw   []byte `xml:",innerxml"`
}

// xmlTileset captures the whole <tileset> element rather than its contents.
//
// `,innerxml` gives what is *between* the tags, so name, tilewidth, tilecount,
// columns, spacing and margin — every attribute a tileset is mostly made of —
// were lost, and what was left was not a <tileset> element and so could not be
// handed to a .tsx parser at all. Re-encoding the token stream keeps the
// element intact, which is what TilesetRef.Embedded promises.
type xmlTileset struct {
	FirstGID uint32
	Source   string
	Raw      []byte
}

func (t *xmlTileset) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "firstgid":
			v, err := strconv.ParseUint(attr.Value, 10, 32)
			if err != nil {
				return fmt.Errorf("tileset firstgid %q: %w", attr.Value, err)
			}
			t.FirstGID = uint32(v)
		case "source":
			t.Source = attr.Value
		}
	}
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	depth := 1
	for depth > 0 {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
		if err := enc.EncodeToken(tok); err != nil {
			return err
		}
	}
	if err := enc.Flush(); err != nil {
		return err
	}
	t.Raw = buf.Bytes()
	return nil
}

type xmlData struct {
	Encoding    string    `xml:"encoding,attr"`
	Compression string    `xml:"compression,attr"`
	Text        string    `xml:",chardata"`
	Tiles       []xmlTile `xml:"tile"`
}

type xmlTile struct {
	GID uint32 `xml:"gid,attr"`
}

type xmlObject struct {
	ID         int           `xml:"id,attr"`
	Name       string        `xml:"name,attr"`
	Type       string        `xml:"type,attr"`
	Class      string        `xml:"class,attr"`
	X          float64       `xml:"x,attr"`
	Y          float64       `xml:"y,attr"`
	Width      float64       `xml:"width,attr"`
	Height     float64       `xml:"height,attr"`
	GID        uint32        `xml:"gid,attr"`
	Rotation   float64       `xml:"rotation,attr"`
	Visible    *int          `xml:"visible,attr"`
	Template   string        `xml:"template,attr"`
	Properties xmlProperties `xml:"properties"`
}

func parseTMX(data []byte, name string) (*Map, error) {
	var wire xmlMap
	if err := xml.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("tiled: %s: %w", name, err)
	}
	// No check that the root element is <map>: the XMLName field's `xml:"map"`
	// tag already makes Unmarshal refuse anything else, and a second guard over
	// the first one's answer reads as a safety net and is not one.

	if wire.Infinite != 0 {
		// Chunked layer data, which nothing here decodes. Refused by name:
		// without this the chunks decode to nothing and the message is "this
		// layer holds 0 tiles but is 1024 cells", which reads as a corrupt map
		// and sends someone looking for a hole that is not there.
		return nil, fmt.Errorf("tiled: %s is an infinite map, which this reader does not decode; "+
			"in Tiled, Map ▸ Map Properties ▸ Infinite ▸ off, then save", name)
	}
	if err := checkSize(wire.Width, wire.Height, name); err != nil {
		return nil, err
	}
	m := &Map{
		Width:       wire.Width,
		Height:      wire.Height,
		TileWidth:   wire.TileWidth,
		TileHeight:  wire.TileHeight,
		Orientation: wire.Orientation,
		RenderOrder: wire.RenderOrder,
		Infinite:    wire.Infinite != 0,
		Properties:  wire.Properties.convert(),
	}
	for _, ts := range wire.Tilesets {
		ref := TilesetRef{FirstGID: ts.FirstGID, Source: ts.Source}
		if ts.Source == "" {
			// Defined inline. Kept verbatim so Story 2 can read it with the
			// same parser it uses for a .tsx file.
			ref.Embedded = ts.Raw
		}
		m.Tilesets = append(m.Tilesets, ref)
	}
	if err := collectXML(m, wire.Children, true, name); err != nil {
		return nil, err
	}
	return m, nil
}

// collectXML flattens a map's layers and its layer folders into one list each,
// in document order — see xmlNode for why the order is the point.
//
// visible is folded down rather than kept on a folder nothing models: a layer
// inside a hidden group is hidden, and a renderer that drew it would be showing
// what the editor does not.
func collectXML(m *Map, nodes []xmlNode, visible bool, name string) error {
	for _, n := range nodes {
		shown := visible && (n.Visible == nil || *n.Visible != 0)
		switch n.XMLName.Local {
		case "layer":
			layer, err := n.convertTileLayer(name)
			if err != nil {
				return err
			}
			layer.Visible = shown
			if err := checkLayerSize(layer.Width, layer.Height, m.Width, m.Height, name, layer.Name); err != nil {
				return err
			}
			m.Layers = append(m.Layers, layer)
		case "objectgroup":
			group := n.convertObjectGroup()
			group.Visible = shown
			m.ObjectGroups = append(m.ObjectGroups, group)
		case "group":
			if err := collectXML(m, n.Children, shown, name); err != nil {
				return err
			}
		default:
			// An <imagelayer>, an <editorsettings>, or an element a later Tiled
			// adds: not modelled, and — unlike a folder — holding no layer that
			// would go missing with it.
		}
	}
	return nil
}

func (n xmlNode) convertTileLayer(name string) (Layer, error) {
	out := Layer{
		ID:     n.ID,
		Name:   n.Name,
		Width:  n.Width,
		Height: n.Height,
		// Visible is collectXML's: it has to fold in the folders this layer sits
		// in, so a value set here would only be one it overwrites.
		Opacity:    1,
		Properties: n.Properties.convert(),
	}
	if n.Opacity != nil {
		out.Opacity = *n.Opacity
	}

	cells := n.Width * n.Height
	var err error
	if n.Data.Encoding == "" {
		// No encoding attribute means one <tile> element per cell, in order.
		out.Data = make([]uint32, 0, len(n.Data.Tiles))
		for _, t := range n.Data.Tiles {
			out.Data = append(out.Data, t.GID)
		}
	} else {
		out.Data, err = decodeLayerData(n.Data.Encoding, n.Data.Compression, n.Data.Text, cells, name, n.Name)
		if err != nil {
			return Layer{}, err
		}
	}
	return out, checkCells(len(out.Data), cells, name, n.Name)
}

func (n xmlNode) convertObjectGroup() ObjectGroup {
	out := ObjectGroup{
		ID:         n.ID,
		Name:       n.Name,
		Properties: n.Properties.convert(),
	}
	for _, o := range n.Objects {
		kind := o.Type
		if kind == "" {
			// Tiled 1.9 renamed the attribute from "type" to "class", and
			// 1.10 renamed it back — so a project's maps may use either
			// depending on which version last saved them. Both mean the entity
			// type here.
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
			Visible:    o.Visible == nil || *o.Visible != 0,
			Template:   o.Template,
			Properties: o.Properties.convert(),
		})
	}
	return out
}

func (p xmlProperties) convert() Properties {
	if len(p.Properties) == 0 {
		return nil
	}
	out := make(Properties, len(p.Properties))
	for _, prop := range p.Properties {
		// The attribute if there is one, and the element's content otherwise —
		// which is a multiline string for a text property and a nested
		// <properties> block for a class-typed one. Keeping the block is what
		// makes "a type this package does not model survives as a string" true
		// of the one type where it was not.
		value := prop.Value
		if value == "" {
			value = string(prop.Raw)
		}
		kind := prop.Type
		if kind == "" {
			// Tiled omits the type for strings, which is its default.
			kind = "string"
		}
		out[prop.Name] = Property{Type: kind, PropertyType: prop.PropertyType, Value: value}
	}
	return out
}
