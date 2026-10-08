//go:build ebitengine

package renderer

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

type tileDrawRecorder struct {
	sourceRects []image.Rectangle
	positions   []image.Point
}

func (r *tileDrawRecorder) DrawImage(src *ebiten.Image, op *ebiten.DrawImageOptions) {
	r.sourceRects = append(r.sourceRects, src.Bounds())
	r.positions = append(r.positions, image.Point{X: int(op.GeoM.Element(0, 2)), Y: int(op.GeoM.Element(1, 2))})
}

type spriteDrawRecorder struct{ translations []image.Point }

func (r *spriteDrawRecorder) DrawImage(_ *ebiten.Image, op *ebiten.DrawImageOptions) {
	r.translations = append(r.translations, image.Point{X: int(op.GeoM.Element(0, 2)), Y: int(op.GeoM.Element(1, 2))})
}

func TestDrawSpriteFootprint_RepeatsOneEntitiesFrameAtEveryOccupiedCell(t *testing.T) {
	r := &spriteDrawRecorder{}
	frame := ebiten.NewImage(16, 16)
	drawSpriteFootprint(r, frame, 32, 16, []tilemap.Point{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 1, Y: 1}}, false)
	want := []image.Point{{32, 16}, {64, 16}, {48, 32}}
	if len(r.translations) != len(want) {
		t.Fatalf("draw calls = %v, want %v", r.translations, want)
	}
	for i := range want {
		if r.translations[i] != want[i] {
			t.Errorf("draw %d = %v, want %v", i, r.translations[i], want[i])
		}
	}
}

func TestGameDrawSprites_MovingRiverChangesDrawCallsWithoutCloningEntity(t *testing.T) {
	db := newTestDB(t)
	for _, stmt := range []string{
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL, y REAL)`,
		`CREATE TABLE comp_sprite (entity_id INTEGER PRIMARY KEY, sheet TEXT, animation TEXT, flip_x INTEGER)`,
		`CREATE TABLE comp_occupiedcells (entity_id INTEGER PRIMARY KEY, value TEXT)`,
		`INSERT INTO entities (entity_type, created_tick) VALUES ('River', 0)`,
		`INSERT INTO comp_position VALUES (1,1,1)`,
		`INSERT INTO comp_sprite VALUES (1,'','',0)`,
		`INSERT INTO comp_occupiedcells VALUES (1,'[{"x":0,"y":0},{"x":2,"y":0},{"x":1,"y":1}]')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	g := NewGame(NewGameParams{
		DB: db, Width: 64, Height: 48, TileSize: 16,
		Grid: tilemap.NewTileGridForWorld(4, 3, db, schema.DatabaseSchema{Components: map[string]schema.Component{
			"OccupiedCells": {Type: "array"},
		}}),
	})
	check := func(want ...image.Point) {
		t.Helper()
		recorder := &spriteDrawRecorder{}
		g.drawSprites(recorder)
		if len(recorder.translations) != len(want) || len(g.animStates) != 1 {
			t.Fatalf("draw calls = %v, animation identities = %d, want %v and one", recorder.translations, len(g.animStates), want)
		}
		for i := range want {
			if recorder.translations[i] != want[i] {
				t.Errorf("draw %d = %v, want %v", i, recorder.translations[i], want[i])
			}
		}
	}
	check(image.Point{16, 16}, image.Point{48, 16}, image.Point{32, 32})
	if _, err := db.Exec(`UPDATE comp_position SET x=0 WHERE entity_id=1`); err != nil {
		t.Fatal(err)
	}
	check(image.Point{0, 16}, image.Point{32, 16}, image.Point{16, 32})
	if _, err := db.Exec(`UPDATE comp_occupiedcells SET value='[{"x":0,"y":0}]' WHERE entity_id=1`); err != nil {
		t.Fatal(err)
	}
	check(image.Point{0, 16})
}

func TestGameDrawSprites_GridSchemaWithoutOccupiedCellsDrawsAnchorSprite(t *testing.T) {
	db := newTestDB(t)
	for _, stmt := range []string{
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL, y REAL)`,
		`CREATE TABLE comp_sprite (entity_id INTEGER PRIMARY KEY, sheet TEXT, animation TEXT, flip_x INTEGER)`,
		`INSERT INTO entities (entity_type, created_tick) VALUES ('Player', 0)`,
		`INSERT INTO comp_position VALUES (1,1,0)`,
		`INSERT INTO comp_sprite VALUES (1,'','',0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	g := NewGame(NewGameParams{
		DB: db, Width: 48, Height: 16, TileSize: 16,
		Grid: tilemap.NewTileGridForWorld(3, 1, db, schema.DatabaseSchema{}),
	})
	r := &spriteDrawRecorder{}
	g.drawSprites(r)
	if len(r.translations) != 1 || r.translations[0] != (image.Point{16, 0}) {
		t.Fatalf("grid without OccupiedCells drew %v, want anchor at (16,0)", r.translations)
	}
}

func TestGameDrawSprites_OnlyActiveMapAndRuntimeSpritesAreDrawn(t *testing.T) {
	db := newTestDB(t)
	for _, stmt := range []string{
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL, y REAL)`,
		`CREATE TABLE comp_sprite (entity_id INTEGER PRIMARY KEY, sheet TEXT, animation TEXT, flip_x INTEGER)`,
		`CREATE TABLE comp_tilelayer (entity_id INTEGER PRIMARY KEY, map_id TEXT NOT NULL)`,
		`CREATE TABLE comp_tileentityowner (entity_id INTEGER PRIMARY KEY, target_entity_id INTEGER NOT NULL)`,
		`INSERT INTO entities (entity_type, created_tick) VALUES ('Player',0),('Player',0),('RuntimeSprite',0),('Tile',0),('Floor',0),('Tile',0),('Floor',0)`,
		`INSERT INTO comp_position VALUES (1,0,0),(2,1,0),(3,2,0),(5,0,0),(7,1,0)`,
		`INSERT INTO comp_sprite VALUES (1,'','',0),(2,'','',0),(3,'','',0),(5,'','',0),(7,'','',0)`,
		`INSERT INTO comp_tilelayer VALUES (4,'map-a'),(6,'map-b')`,
		`INSERT INTO comp_tileentityowner VALUES (5,4),(7,6)`,
		`INSERT INTO spawns (map, object_id, entity_id) VALUES ('map-a',1,1),('map-b',1,2)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	g := NewGame(NewGameParams{
		DB: db, Width: 48, Height: 16, TileSize: 16,
		Grid: tilemap.NewTileGridForMap(3, 1, db, schema.DatabaseSchema{}, "map-b"),
	})
	r := &spriteDrawRecorder{}
	g.drawSprites(r)
	if len(r.translations) != 3 || r.translations[0] != (image.Point{16, 0}) ||
		r.translations[1] != (image.Point{32, 0}) || r.translations[2] != (image.Point{16, 0}) {
		t.Fatalf("active map sprite calls = %v, want map-b Player, runtime and owned Floor only", r.translations)
	}
}

func TestGameDrawSprites_ReferencedRiverFollowsTilesInsteadOfItsAnchor(t *testing.T) {
	db := newTestDB(t)
	for _, stmt := range []string{
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL, y REAL)`,
		`CREATE TABLE comp_sprite (entity_id INTEGER PRIMARY KEY, sheet TEXT, animation TEXT, flip_x INTEGER)`,
		`CREATE TABLE comp_tilelayer (entity_id INTEGER PRIMARY KEY, map_id TEXT, layer_order INTEGER, draw_order INTEGER)`,
		`CREATE TABLE comp_tilereferences (entity_id INTEGER PRIMARY KEY, value TEXT)`,
		`CREATE TABLE comp_tileentityowner (entity_id INTEGER PRIMARY KEY, target_entity_id INTEGER)`,
		`INSERT INTO entities (entity_type, created_tick) VALUES ('Tile',0),('Tile',0),('River',0),('Tile',0),('River',0)`,
		`INSERT INTO comp_tilelayer VALUES (1,'map-a',0,0),(2,'map-a',0,1),(4,'map-a',0,2)`,
		`INSERT INTO comp_position VALUES (1,0,0),(2,1,1),(3,3,0),(4,2,0)`,
		`INSERT INTO comp_tilereferences VALUES (1,'[3]'),(2,'[3]'),(4,'[5]')`,
		`INSERT INTO comp_sprite VALUES (3,'','',0),(5,'','',0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	g := NewGame(NewGameParams{
		DB: db, Width: 64, Height: 32, TileSize: 16,
		Grid: tilemap.NewTileGridForMap(4, 2, db, schema.DatabaseSchema{Components: map[string]schema.Component{
			"TileReferences": {Type: "array"},
		}}, "map-a"),
	})
	check := func(want ...image.Point) {
		t.Helper()
		r := &spriteDrawRecorder{}
		g.drawSprites(r)
		if len(r.translations) != len(want) {
			t.Fatalf("linked sprites at %v, want %v", r.translations, want)
		}
		for i := range want {
			if r.translations[i] != want[i] {
				t.Errorf("sprite %d at %v, want %v", i, r.translations[i], want[i])
			}
		}
	}
	check(image.Point{0, 0}, image.Point{16, 16}, image.Point{32, 0})
	if _, err := db.Exec(`UPDATE comp_position SET x=0,y=1 WHERE entity_id=2`); err != nil {
		t.Fatal(err)
	}
	check(image.Point{0, 0}, image.Point{0, 16}, image.Point{32, 0})
	if _, err := db.Exec(`INSERT INTO spawns(map,object_id,entity_id) VALUES ('map-b',2,3)`); err != nil {
		t.Fatal(err)
	}
	check(image.Point{32, 0}) // foreign-map River is not projected into map-a
	otherMap := NewGame(NewGameParams{
		DB: db, Width: 64, Height: 32, TileSize: 16,
		Grid: tilemap.NewTileGridForMap(4, 2, db, schema.DatabaseSchema{Components: map[string]schema.Component{
			"TileReferences": {Type: "array"},
		}}, "map-b"),
	})
	otherDraws := &spriteDrawRecorder{}
	otherMap.drawSprites(otherDraws)
	if len(otherDraws.translations) != 1 || otherDraws.translations[0] != (image.Point{48, 0}) {
		t.Fatalf("map-b spawn hidden by map-a Tile reference: %v", otherDraws.translations)
	}
	for _, stmt := range []string{
		`DELETE FROM spawns`,
		`INSERT INTO entities (entity_type, created_tick) VALUES ('Tile',0)`,
		`INSERT INTO comp_position VALUES (6,1,0)`,
		`INSERT INTO comp_tilelayer VALUES (6,'map-b',0,0)`,
		`INSERT INTO comp_tilereferences VALUES (6,'[]')`,
		`INSERT INTO comp_tileentityowner VALUES (3,6)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	check(image.Point{32, 0}) // a foreign-map owned River also stays in its map
}

func TestTilemapRenderer_DrawsCurrentTileVisualFromDatabase(t *testing.T) {
	db := newTestDB(t)
	for _, stmt := range []string{
		`CREATE TABLE comp_tilelayer (entity_id INTEGER PRIMARY KEY, map_id TEXT, layer_order INTEGER, draw_order INTEGER,
			cell_w INTEGER, cell_h INTEGER, visible INTEGER, opacity REAL)`,
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL, y REAL)`,
		`CREATE TABLE comp_tilereferences (entity_id INTEGER PRIMARY KEY, value TEXT)`,
		`CREATE TABLE comp_tilevisual (entity_id INTEGER PRIMARY KEY, image TEXT, source_x INTEGER,
			source_y INTEGER, source_w INTEGER, source_h INTEGER, dest_x INTEGER, dest_y INTEGER,
			flip_h INTEGER, flip_v INTEGER, flip_d INTEGER, alpha REAL, visible INTEGER)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	picture := image.NewRGBA(image.Rect(0, 0, 2, 1))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	picture.Set(1, 0, color.RGBA{B: 255, A: 255})
	path := filepath.Join(t.TempDir(), "tiles.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, picture); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	entity, err := db.Exec(`INSERT INTO entities (entity_type, created_tick) VALUES ('Tile', 0),('Floor',0)`)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comp_tilelayer VALUES (?, 'map', 0, 0, 1, 1, 1, 1)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comp_position VALUES (?,0,0)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comp_tilereferences VALUES (?, '[2]')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comp_tilevisual VALUES (2, ?, 0, 0, 1, 1, 0, 0, 0, 0, 0, 1, 1)`, path); err != nil {
		t.Fatal(err)
	}
	r, err := NewTilemapRenderer(db, "map", NewImageCache())
	if err != nil {
		t.Fatal(err)
	}
	assertSource := func(want ...image.Rectangle) {
		t.Helper()
		recorder := &tileDrawRecorder{}
		if err := r.drawTo(recorder); err != nil {
			t.Fatal(err)
		}
		if len(recorder.sourceRects) != len(want) {
			t.Fatalf("draw calls = %v, want %v", recorder.sourceRects, want)
		}
		for i, rect := range want {
			if recorder.sourceRects[i] != rect {
				t.Errorf("draw %d source = %v, want %v", i, recorder.sourceRects[i], rect)
			}
		}
	}
	assertSource(image.Rect(0, 0, 1, 1))
	if _, err := db.Exec(`UPDATE comp_position SET x=1 WHERE entity_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	moved := &tileDrawRecorder{}
	if err := r.drawTo(moved); err != nil || len(moved.positions) != 1 || moved.positions[0] != (image.Point{1, 0}) {
		t.Fatalf("Tile Position change did not move referenced art: %v,%v", moved.positions, err)
	}
	if _, err := db.Exec(`UPDATE comp_tilevisual SET source_x = 1 WHERE entity_id = 2`); err != nil {
		t.Fatal(err)
	}
	assertSource(image.Rect(1, 0, 2, 1))
	if _, err := db.Exec(`UPDATE comp_tilevisual SET visible = 0 WHERE entity_id = 2`); err != nil {
		t.Fatal(err)
	}
	assertSource()
}
