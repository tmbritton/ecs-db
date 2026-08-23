package tiled_test

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

func show(t *testing.T, label, src string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[%s] PANIC: %v\n", label, r)
		}
	}()
	m, err := tiled.Parse([]byte(src), "probe")
	if err != nil {
		fmt.Printf("[%s] err: %v\n", label, err)
		return
	}
	fmt.Printf("[%s] OK infinite=%v layers=%d objgroups=%d tilesets=%d\n", label, m.Infinite, len(m.Layers), len(m.ObjectGroups), len(m.Tilesets))
	for _, l := range m.Layers {
		fmt.Printf("    layer %q %dx%d data=%v\n", l.Name, l.Width, l.Height, l.Data)
	}
	for _, ts := range m.Tilesets {
		fmt.Printf("    tileset firstgid=%d source=%q embedded=%q\n", ts.FirstGID, ts.Source, string(ts.Embedded))
	}
	for _, g := range m.ObjectGroups {
		for _, o := range g.Objects {
			fmt.Printf("    obj id=%d name=%q type=%q gid=%d at(%v,%v) %vx%v props=%v\n", o.ID, o.Name, o.Type, o.GID, o.X, o.Y, o.Width, o.Height, o.Properties)
		}
	}
	fmt.Printf("    map props=%v\n", m.Properties)
}

// ---- 1. negative / huge dimensions
func TestProbeNegativeDims(t *testing.T) {
	show(t, "neg-width-csv", `<map width="-1" height="1" tilewidth="8" tileheight="8">
 <layer name="l" width="-1" height="1"><data encoding="csv">1</data></layer></map>`)
	show(t, "neg-both-csv", `<map width="-2" height="-2" tilewidth="8" tileheight="8">
 <layer name="l" width="-2" height="-2"><data encoding="csv">1,2,3,4</data></layer></map>`)
	show(t, "neg-tmj", `{"width":-1,"height":1,"layers":[{"type":"tilelayer","name":"l","width":-1,"height":1,"data":[1]}]}`)
}

// ---- 2. infinite TMX with chunks (what Tiled 1.11 actually writes)
func TestProbeInfiniteTMX(t *testing.T) {
	show(t, "infinite-tmx-csv", `<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" renderorder="right-down" width="32" height="32" tilewidth="16" tileheight="16" infinite="1">
 <tileset firstgid="1" source="d.tsx"/>
 <layer id="1" name="Tile Layer 1" width="32" height="32">
  <data encoding="csv">
   <chunk x="0" y="0" width="16" height="16">
1,1,
1,1
</chunk>
  </data>
 </layer>
</map>`)
}

func TestProbeInfiniteTMJ(t *testing.T) {
	show(t, "infinite-tmj", `{"type":"map","infinite":true,"width":32,"height":32,"tilewidth":16,"tileheight":16,
 "layers":[{"id":1,"name":"Tile Layer 1","type":"tilelayer","x":0,"y":0,"startx":0,"starty":0,"width":32,"height":32,
 "chunks":[{"x":0,"y":0,"width":16,"height":16,"data":[1,1,1,1]}]}]}`)
	// infinite where layer w/h are 0 (some writers)
	show(t, "infinite-tmj-zero-wh", `{"type":"map","infinite":true,"width":32,"height":32,"tilewidth":16,"tileheight":16,
 "layers":[{"id":1,"name":"L","type":"tilelayer","width":0,"height":0,
 "chunks":[{"x":0,"y":0,"width":16,"height":16,"data":[1,1,1,1]}]}]}`)
}

// ---- 3. group layers
func TestProbeGroups(t *testing.T) {
	show(t, "group-tmx", `<?xml version="1.0"?>
<map width="2" height="1" tilewidth="8" tileheight="8">
 <group id="5" name="Folder">
  <layer id="1" name="inside" width="2" height="1"><data encoding="csv">7,8</data></layer>
  <objectgroup id="2" name="spawns"><object id="1" name="hero" type="Player" x="0" y="8"/></objectgroup>
 </group>
</map>`)
	show(t, "group-tmj", `{"type":"map","width":2,"height":1,"tilewidth":8,"tileheight":8,
 "layers":[{"id":5,"name":"Folder","type":"group","layers":[
   {"id":1,"name":"inside","type":"tilelayer","width":2,"height":1,"data":[7,8]},
   {"id":2,"name":"spawns","type":"objectgroup","objects":[{"id":1,"name":"hero","type":"Player","x":0,"y":8}]}]}]}`)
}

// ---- 4. embedded tileset both ways
func TestProbeEmbedded(t *testing.T) {
	show(t, "embedded-tmx", `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <tileset firstgid="1" name="inline" tilewidth="8" tileheight="8" tilecount="4" columns="2">
  <image source="dungeon.png" width="16" height="16"/>
 </tileset>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)
	show(t, "embedded-tmj", `{"type":"map","width":1,"height":1,"tilewidth":8,"tileheight":8,
 "tilesets":[{"firstgid":1,"name":"inline","tilewidth":8,"tileheight":8,"tilecount":4,"columns":2,
   "image":"dungeon.png","imagewidth":16,"imageheight":16}],
 "layers":[{"name":"l","type":"tilelayer","width":1,"height":1,"data":[0]}]}`)
}

// ---- 5. objects: point, rotation, template, tile-object gid with flags, visible
func TestProbeObjects(t *testing.T) {
	show(t, "objects-tmx", `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="16" tileheight="16">
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
 <objectgroup name="spawns">
  <object id="1" name="pt" type="Player" x="8" y="8"><point/></object>
  <object id="2" name="rot" type="Goblin" x="0" y="16" width="16" height="16" rotation="90" visible="0"/>
  <object id="3" gid="2147483651" x="0" y="16" width="16" height="16"/>
  <object id="4" template="hero.tx" x="32" y="32"/>
  <object id="5" name="poly" x="0" y="0"><polygon points="0,0 8,0 8,8"/></object>
 </objectgroup>
</map>`)
}

// ---- 6. properties: class type, color, file, object; JSON vs XML agreement
func TestProbeProps(t *testing.T) {
	show(t, "props-xml", `<?xml version="1.0"?>
<map width="1" height="1" tilewidth="8" tileheight="8">
 <properties>
  <property name="tint" type="color" value="#ffaabbcc"/>
  <property name="ref" type="object" value="7"/>
  <property name="path" type="file" value="a/b.png"/>
  <property name="cfg" type="class" propertytype="Cfg">
   <properties><property name="hp" type="int" value="3"/></properties>
  </property>
  <property name="grav" type="float" value="9"/>
  <property name="enum" type="string" propertytype="Kind" value="A"/>
 </properties>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer>
</map>`)
	show(t, "props-json", `{"type":"map","width":1,"height":1,"tilewidth":8,"tileheight":8,
 "properties":[
  {"name":"tint","type":"color","value":"#ffaabbcc"},
  {"name":"ref","type":"object","value":7},
  {"name":"path","type":"file","value":"a/b.png"},
  {"name":"cfg","type":"class","propertytype":"Cfg","value":{"hp":3}},
  {"name":"grav","type":"float","value":9.0},
  {"name":"enum","type":"string","propertytype":"Kind","value":"A"}],
 "layers":[{"name":"l","type":"tilelayer","width":1,"height":1,"data":[0]}]}`)
}

// ---- 7. base64 wrapped across lines
func TestProbeWrappedBase64(t *testing.T) {
	raw := []byte{1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0, 4, 0, 0, 0}
	enc := base64.StdEncoding.EncodeToString(raw)
	wrapped := enc[:8] + "\n   " + enc[8:]
	show(t, "b64-wrapped", `<map width="4" height="1" tilewidth="8" tileheight="8">
 <layer name="l" width="4" height="1"><data encoding="base64">`+wrapped+`</data></layer></map>`)
}

// ---- 8. zlib bomb: tiny input, huge output
func TestProbeZlibBomb(t *testing.T) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	zeros := make([]byte, 1<<20)
	for i := 0; i < 256; i++ { // 256 MiB of zeros
		_, _ = w.Write(zeros)
	}
	_ = w.Close()
	fmt.Printf("[zlib-bomb] compressed size = %d bytes for 256 MiB\n", buf.Len())
	enc := base64.StdEncoding.EncodeToString(buf.Bytes())
	src := `<map width="1" height="1" tilewidth="8" tileheight="8">
 <layer name="l" width="1" height="1"><data encoding="base64" compression="zlib">` + enc + `</data></layer></map>`
	show(t, "zlib-bomb", src)
}

// ---- 9. layer dims disagreeing with map dims
func TestProbeLayerDimsDisagree(t *testing.T) {
	show(t, "layer-smaller-than-map", `<map width="10" height="10" tilewidth="8" tileheight="8">
 <layer name="l" width="2" height="1"><data encoding="csv">1,2</data></layer></map>`)
}

// ---- 10. tmj missing "type" on layer with objects
func TestProbeTMJUntypedObjectLayer(t *testing.T) {
	show(t, "tmj-untyped-objectlayer", `{"width":1,"height":1,"tilewidth":8,"tileheight":8,
 "layers":[{"id":2,"name":"spawns","objects":[{"id":1,"name":"hero","type":"Player","x":0,"y":8}]}]}`)
}

// ---- 11. imagelayer tmx
func TestProbeImageLayer(t *testing.T) {
	show(t, "imagelayer-tmx", `<map width="1" height="1" tilewidth="8" tileheight="8">
 <imagelayer name="bg"><image source="bg.png"/></imagelayer>
 <layer name="l" width="1" height="1"><data encoding="csv">0</data></layer></map>`)
}
