package tilemap

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
	"github.com/tmbritton/ecs-db/internal/world"
)

// ── The diff, with no database in sight ───────────────────────────────────────

func floor(x, y int) (Point, TileState) {
	return Point{X: x, Y: y}, TileState{Passable: true, TileType: "floor"}
}

func wall(x, y int) (Point, TileState) {
	return Point{X: x, Y: y}, TileState{Passable: false, TileType: "wall"}
}

func want(pairs ...func() (Point, TileState)) map[Point]TileState {
	m := make(map[Point]TileState, len(pairs))
	for _, f := range pairs {
		p, s := f()
		m[p] = s
	}
	return m
}

func at(x, y int) func() (Point, TileState) {
	return func() (Point, TileState) { return floor(x, y) }
}

func wallAt(x, y int) func() (Point, TileState) {
	return func() (Point, TileState) { return wall(x, y) }
}

func stored(id int64, x, y int, passable bool, tileType string) storedTile {
	return storedTile{ID: id, X: x, Y: y, State: TileState{Passable: passable, TileType: tileType}}
}

func have(tiles ...storedTile) []storedTile { return tiles }

func TestPlanTiles_UnchangedTilesAreNotTouched(t *testing.T) {
	p := planTiles(have(stored(7, 1, 1, true, "floor")), want(at(1, 1)))

	if len(p.create) != 0 || len(p.update) != 0 || len(p.deletions()) != 0 {
		t.Fatalf("plan = %+v, want nothing to do", p)
	}
	if p.unchanged != 1 {
		t.Errorf("unchanged = %d, want 1", p.unchanged)
	}
	if !p.empty() {
		t.Error("empty() = false, want true when there is nothing to write")
	}
	if got := p.keep[Point{X: 1, Y: 1}]; got != 7 {
		t.Errorf("keep[1,1] = %d, want the stored id 7", got)
	}
}

func TestPlanTiles_ChangedTileIsUpdatedAndKeepsItsID(t *testing.T) {
	p := planTiles(have(stored(7, 1, 1, true, "floor")), want(wallAt(1, 1)))

	if len(p.create) != 0 || len(p.deletions()) != 0 {
		t.Fatalf("plan = %+v, want an update and nothing else", p)
	}
	got, ok := p.update[Point{X: 1, Y: 1}]
	if !ok {
		t.Fatalf("update has no (1,1): %+v", p.update)
	}
	if got.Passable || got.TileType != "wall" {
		t.Errorf("update[1,1] = %+v, want the file's wall", got)
	}
	if p.keep[Point{X: 1, Y: 1}] != 7 {
		t.Errorf("keep[1,1] = %d, want the id preserved across an update", p.keep[Point{X: 1, Y: 1}])
	}
	if p.empty() {
		t.Error("empty() = true, but there is an update to write")
	}
}

func TestPlanTiles_TileTypeAloneIsAChange(t *testing.T) {
	// Same passability, different appearance: the file still wins.
	p := planTiles(
		have(stored(7, 1, 1, true, "floor")),
		map[Point]TileState{{X: 1, Y: 1}: {Passable: true, TileType: "grass"}},
	)
	if len(p.update) != 1 {
		t.Fatalf("plan = %+v, want tile_type alone to count as a change", p)
	}
}

func TestPlanTiles_NewCellIsCreated(t *testing.T) {
	p := planTiles(have(stored(7, 1, 1, true, "floor")), want(at(1, 1), at(2, 1)))

	if len(p.update) != 0 || len(p.deletions()) != 0 {
		t.Fatalf("plan = %+v, want a create and nothing else", p)
	}
	if _, ok := p.create[Point{X: 2, Y: 1}]; !ok {
		t.Fatalf("create has no (2,1): %+v", p.create)
	}
	if p.unchanged != 1 {
		t.Errorf("unchanged = %d, want the untouched tile still counted", p.unchanged)
	}
}

func TestPlanTiles_MissingCellIsDeleted(t *testing.T) {
	p := planTiles(
		have(stored(7, 1, 1, true, "floor"), stored(8, 2, 1, true, "floor")),
		want(at(1, 1)),
	)

	if !reflect.DeepEqual(p.deletions(), []int64{8}) {
		t.Fatalf("delete = %v, want the entity at the cell the file dropped", p.deletions())
	}
	if _, ok := p.keep[Point{X: 2, Y: 1}]; ok {
		t.Error("keep still names a deleted position")
	}
}

func TestPlanTiles_EmptyFileDeletesEverything(t *testing.T) {
	p := planTiles(
		have(stored(7, 1, 1, true, "floor"), stored(8, 2, 1, true, "floor")),
		map[Point]TileState{},
	)
	if !reflect.DeepEqual(p.deletions(), []int64{7, 8}) {
		t.Fatalf("delete = %v, want both", p.deletions())
	}
}

func TestPlanTiles_DeleteIsAscendingSoTheOrderIsNotTheMapsMood(t *testing.T) {
	p := planTiles(
		have(
			stored(31, 3, 0, true, "floor"),
			stored(4, 0, 0, true, "floor"),
			stored(17, 1, 0, true, "floor"),
			stored(9, 2, 0, true, "floor"),
		),
		map[Point]TileState{},
	)
	if !sort.SliceIsSorted(p.deletions(), func(i, j int) bool { return p.deletions()[i] < p.deletions()[j] }) {
		t.Fatalf("delete = %v, want ascending ids", p.deletions())
	}
}

func TestPlanTiles_OneChangedCellLeavesTheOthersAlone(t *testing.T) {
	var haveTiles []storedTile
	wantTiles := map[Point]TileState{}
	for y := range 10 {
		for x := range 10 {
			haveTiles = append(haveTiles, stored(int64(y*10+x+1), x, y, true, "floor"))
			wantTiles[Point{X: x, Y: y}] = TileState{Passable: true, TileType: "floor"}
		}
	}
	wantTiles[Point{X: 4, Y: 4}] = TileState{Passable: false, TileType: "wall"}

	p := planTiles(haveTiles, wantTiles)
	if len(p.update) != 1 || len(p.create) != 0 || len(p.deletions()) != 0 {
		t.Fatalf("plan = %d creates, %d updates, %d deletes; want exactly one update",
			len(p.create), len(p.update), len(p.deletions()))
	}
	if p.unchanged != 99 {
		t.Errorf("unchanged = %d, want 99", p.unchanged)
	}
}

func TestPlanTiles_DuplicateCellKeepsTheLowestIDAndDeletesTheRest(t *testing.T) {
	p := planTiles(
		have(
			stored(12, 1, 1, true, "floor"),
			stored(5, 1, 1, false, "wall"),
			stored(40, 1, 1, true, "floor"),
		),
		want(at(1, 1)),
	)

	if p.keep[Point{X: 1, Y: 1}] != 5 {
		t.Errorf("keep[1,1] = %d, want the lowest id 5", p.keep[Point{X: 1, Y: 1}])
	}
	if !reflect.DeepEqual(p.deletions(), []int64{12, 40}) {
		t.Fatalf("delete = %v, want the two extra rows at that cell", p.deletions())
	}
	// The survivor was the wall row, and the file says floor.
	if _, ok := p.update[Point{X: 1, Y: 1}]; !ok {
		t.Error("the surviving duplicate was not brought in line with the file")
	}
}

func TestPlanTiles_DuplicateCellTheFileDroppedDeletesAllOfThem(t *testing.T) {
	p := planTiles(
		have(stored(12, 1, 1, true, "floor"), stored(5, 1, 1, true, "floor")),
		map[Point]TileState{},
	)
	if !reflect.DeepEqual(p.deletions(), []int64{5, 12}) {
		t.Fatalf("delete = %v, want every row at the dropped cell", p.deletions())
	}
}

func TestPlanTiles_DuplicateThatAlreadyAgreesStillCosts(t *testing.T) {
	// The survivor matches the file, so there is no update — but the extra row
	// still has to go, and that means the plan is not empty.
	p := planTiles(
		have(stored(5, 1, 1, true, "floor"), stored(12, 1, 1, true, "floor")),
		want(at(1, 1)),
	)
	if len(p.update) != 0 {
		t.Errorf("update = %+v, want none — the survivor already agrees", p.update)
	}
	if p.empty() {
		t.Fatal("empty() = true, but a duplicate row still has to be deleted")
	}
}

// ── Applying it, against a real database ──────────────────────────────────────

// watchTiles installs triggers that record every write to comp_tile, so a test
// can assert what the database was told rather than what the caller believes it
// asked for. A no-op UPDATE is a write and shows up here.
func watchTiles(t *testing.T, db *sql.DB) func() []string {
	t.Helper()
	stmts := []string{
		`CREATE TABLE tile_writes (op TEXT NOT NULL, entity_id INTEGER NOT NULL)`,
		`CREATE TRIGGER tw_ins AFTER INSERT ON comp_tile
		 BEGIN INSERT INTO tile_writes VALUES ('insert', NEW.entity_id); END`,
		`CREATE TRIGGER tw_upd AFTER UPDATE ON comp_tile
		 BEGIN INSERT INTO tile_writes VALUES ('update', NEW.entity_id); END`,
		`CREATE TRIGGER tw_del AFTER DELETE ON comp_tile
		 BEGIN INSERT INTO tile_writes VALUES ('delete', OLD.entity_id); END`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("installing tile watch: %v", err)
		}
	}
	return func() []string {
		rows, err := db.Query(`SELECT op FROM tile_writes ORDER BY rowid`)
		if err != nil {
			t.Fatalf("reading tile writes: %v", err)
		}
		defer rows.Close()
		var ops []string
		for rows.Next() {
			var op string
			if err := rows.Scan(&op); err != nil {
				t.Fatalf("scanning tile writes: %v", err)
			}
			ops = append(ops, op)
		}
		return ops
	}
}

func tileRows(t *testing.T, db *sql.DB) map[Point]storedTile {
	t.Helper()
	tiles, err := storedTiles(context.Background(), db)
	if err != nil {
		t.Fatalf("storedTiles: %v", err)
	}
	out := make(map[Point]storedTile, len(tiles))
	for _, tile := range tiles {
		out[Point{X: tile.X, Y: tile.Y}] = tile
	}
	return out
}

func syncFixture(t *testing.T) (*world.EntityService, *sql.DB) {
	t.Helper()
	ds := tileSchema()
	store := newTestStore(t)
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)
	return svc, store.DB()
}

func mustSync(t *testing.T, svc *world.EntityService, db *sql.DB, w map[Point]TileState) Result {
	t.Helper()
	res, err := SyncTiles(context.Background(), svc, db, w)
	if err != nil {
		t.Fatalf("SyncTiles: %v", err)
	}
	return res
}

func TestSyncTiles_CreatesOnAnEmptyDatabase(t *testing.T) {
	svc, db := syncFixture(t)

	res := mustSync(t, svc, db, want(at(0, 0), wallAt(1, 0)))

	if res.Created != 2 || res.Updated != 0 || res.Deleted != 0 {
		t.Fatalf("result = %+v, want two creates", res)
	}
	rows := tileRows(t, db)
	if len(rows) != 2 {
		t.Fatalf("stored %d tiles, want 2", len(rows))
	}
	if rows[Point{X: 1, Y: 0}].State.Passable {
		t.Error("the wall came back passable")
	}
	if rows[Point{X: 1, Y: 0}].State.TileType != "wall" {
		t.Errorf("tile_type = %q, want wall", rows[Point{X: 1, Y: 0}].State.TileType)
	}
}

func TestSyncTiles_SecondLoadWithNoEditWritesNothing(t *testing.T) {
	svc, db := syncFixture(t)
	w := want(at(0, 0), at(1, 0), wallAt(2, 0))
	mustSync(t, svc, db, w)
	before := tileRows(t, db)

	writes := watchTiles(t, db)
	res := mustSync(t, svc, db, w)

	if ops := writes(); len(ops) != 0 {
		t.Fatalf("the database was told %v, want nothing at all", ops)
	}
	if res.Unchanged != 3 || res.Created != 0 || res.Updated != 0 || res.Deleted != 0 {
		t.Errorf("result = %+v, want three unchanged and nothing else", res)
	}
	if !reflect.DeepEqual(tileRows(t, db), before) {
		t.Error("the rows changed under a load that had nothing to do")
	}
}

func TestSyncTiles_OneChangedCellWritesOneRow(t *testing.T) {
	svc, db := syncFixture(t)
	w := want(at(0, 0), at(1, 0), at(2, 0), at(3, 0))
	mustSync(t, svc, db, w)
	before := tileRows(t, db)

	writes := watchTiles(t, db)
	w[Point{X: 2, Y: 0}] = TileState{Passable: false, TileType: "wall"}
	res := mustSync(t, svc, db, w)

	if ops := writes(); !reflect.DeepEqual(ops, []string{"update"}) {
		t.Fatalf("the database was told %v, want a single update", ops)
	}
	if res.Updated != 1 || res.Unchanged != 3 {
		t.Errorf("result = %+v, want one update and three untouched", res)
	}
	after := tileRows(t, db)
	for _, p := range []Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 3, Y: 0}} {
		if after[p] != before[p] {
			t.Errorf("tile %v changed: %+v → %+v", p, before[p], after[p])
		}
	}
}

func TestSyncTiles_AnUpdatedTileKeepsItsEntityID(t *testing.T) {
	svc, db := syncFixture(t)
	mustSync(t, svc, db, want(at(0, 0)))
	id := tileRows(t, db)[Point{X: 0, Y: 0}].ID

	mustSync(t, svc, db, want(wallAt(0, 0)))

	got := tileRows(t, db)[Point{X: 0, Y: 0}]
	if got.ID != id {
		t.Errorf("entity id = %d, want the original %d", got.ID, id)
	}
	if got.State.Passable {
		t.Error("the file said wall and the row is still passable")
	}
}

func TestSyncTiles_TheFileWinsOverRuntimeState(t *testing.T) {
	// setTilePassable opened a door at (1,0). The file still calls it a wall.
	svc, db := syncFixture(t)
	mustSync(t, svc, db, want(at(0, 0), wallAt(1, 0)))
	id := tileRows(t, db)[Point{X: 1, Y: 0}].ID
	if _, err := db.Exec(`UPDATE comp_tile SET passable = 1 WHERE entity_id = ?`, id); err != nil {
		t.Fatalf("opening the door: %v", err)
	}

	mustSync(t, svc, db, want(at(0, 0), wallAt(1, 0)))

	got := tileRows(t, db)[Point{X: 1, Y: 0}]
	if got.State.Passable {
		t.Error("the runtime's open door survived a re-import; the file is supposed to win")
	}
	if got.ID != id {
		t.Errorf("entity id = %d, want the original %d — the file wins, the identity does not change", got.ID, id)
	}
}

func TestSyncTiles_ADroppedCellIsDeletedRowAndEntityBoth(t *testing.T) {
	svc, db := syncFixture(t)
	mustSync(t, svc, db, want(at(0, 0), at(1, 0)))
	id := tileRows(t, db)[Point{X: 1, Y: 0}].ID

	res := mustSync(t, svc, db, want(at(0, 0)))

	if res.Deleted != 1 {
		t.Fatalf("result = %+v, want one delete", res)
	}
	var comps int
	if err := db.QueryRow(`SELECT COUNT(*) FROM comp_tile WHERE entity_id = ?`, id).Scan(&comps); err != nil {
		t.Fatalf("counting comp_tile: %v", err)
	}
	if comps != 0 {
		t.Errorf("comp_tile still holds %d rows for the deleted tile — an orphan the cascade did not take", comps)
	}
	var ents int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, id).Scan(&ents); err != nil {
		t.Fatalf("counting entities: %v", err)
	}
	if ents != 0 {
		t.Errorf("the entities row survived its last component")
	}
}

func TestSyncTiles_ARowCreatedByHandAtADuplicatePositionIsCleanedUp(t *testing.T) {
	svc, db := syncFixture(t)
	mustSync(t, svc, db, want(at(0, 0)))
	kept := tileRows(t, db)[Point{X: 0, Y: 0}].ID
	// A second tile at the same cell, the way nothing should but the database allows.
	if _, err := db.Exec(`INSERT INTO entities (entity_type, created_tick) VALUES ('Tile', 0)`); err != nil {
		t.Fatalf("inserting duplicate entity: %v", err)
	}
	var dupe int64
	if err := db.QueryRow(`SELECT MAX(id) FROM entities`).Scan(&dupe); err != nil {
		t.Fatalf("reading duplicate id: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO comp_tile (entity_id, passable, tile_type, x, y) VALUES (?, 0, 'wall', 0, 0)`,
		dupe,
	); err != nil {
		t.Fatalf("inserting duplicate tile: %v", err)
	}

	res := mustSync(t, svc, db, want(at(0, 0)))

	if res.Repaired != 1 || res.Deleted != 0 {
		t.Fatalf("result = %+v, want one row repaired and no cell dropped", res)
	}
	rows := tileRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("stored %d tiles at one cell, want 1", len(rows))
	}
	if rows[Point{X: 0, Y: 0}].ID != kept {
		t.Errorf("kept id %d, want the lower original %d", rows[Point{X: 0, Y: 0}].ID, kept)
	}
}

func TestSyncTiles_AFailurePartWayThroughLeavesNothingBehind(t *testing.T) {
	svc, db := syncFixture(t)
	mustSync(t, svc, db, want(at(0, 0)))
	before := tileRows(t, db)

	// The database refuses one particular cell, mid-apply.
	if _, err := db.Exec(`CREATE TRIGGER no_five BEFORE INSERT ON comp_tile
		WHEN NEW.x = 5 BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("installing refusal: %v", err)
	}

	w := want(wallAt(0, 0), at(1, 0), at(2, 0), at(3, 0), at(4, 0), at(5, 0), at(6, 0))
	if _, err := SyncTiles(context.Background(), svc, db, w); err == nil {
		t.Fatal("SyncTiles succeeded against a database that refused a row")
	}

	if got := tileRows(t, db); !reflect.DeepEqual(got, before) {
		t.Errorf("the database moved under a failed re-import:\n have %+v\n want %+v", got, before)
	}
	var ents int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE entity_type = 'Tile'`).Scan(&ents); err != nil {
		t.Fatalf("counting entities: %v", err)
	}
	if ents != 1 {
		t.Errorf("%d Tile entities survive a rolled-back import, want 1 — entity rows without components", ents)
	}
}

func TestSyncTiles_ADeleteThatFailsRollsBackTheWholeImport(t *testing.T) {
	svc, db := syncFixture(t)
	mustSync(t, svc, db, want(at(0, 0), at(1, 0), at(2, 0)))
	before := tileRows(t, db)

	if _, err := db.Exec(`CREATE TRIGGER no_delete BEFORE DELETE ON comp_tile
		BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("installing refusal: %v", err)
	}

	if _, err := SyncTiles(context.Background(), svc, db, want(at(0, 0), wallAt(1, 0))); err == nil {
		t.Fatal("SyncTiles succeeded against a database that refused the delete")
	}
	if got := tileRows(t, db); !reflect.DeepEqual(got, before) {
		t.Errorf("the update landed even though the delete beside it failed:\n have %+v\n want %+v", got, before)
	}
}

func TestSyncTiles_RefusalNamesTheCell(t *testing.T) {
	svc, db := syncFixture(t)
	if _, err := db.Exec(`CREATE TRIGGER no_five BEFORE INSERT ON comp_tile
		WHEN NEW.x = 5 BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("installing refusal: %v", err)
	}

	_, err := SyncTiles(context.Background(), svc, db, want(at(5, 3)))
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "(5,3)") {
		t.Errorf("error = %q, want it to name the cell it failed on", err)
	}
}

// countingStore records how many transactions were begun, so a test can say
// "nothing was written" about the transaction and not only about the rows.
type countingStore struct {
	world.EntityStore
	began int
}

func (c *countingStore) BeginTx(ctx context.Context) (world.Tx, error) {
	c.began++
	return c.EntityStore.BeginTx(ctx)
}

func TestSyncTiles_ALoadWithNothingToDoOpensNoTransaction(t *testing.T) {
	ds := tileSchema()
	store := newTestStore(t)
	counting := &countingStore{EntityStore: store}
	svc := world.NewEntityService(counting)
	svc.SetSchema(ds)
	db := store.DB()

	w := want(at(0, 0), wallAt(1, 0))
	mustSync(t, svc, db, w)
	if counting.began == 0 {
		t.Fatal("the first load wrote nothing")
	}

	counting.began = 0
	mustSync(t, svc, db, w)
	if counting.began != 0 {
		t.Errorf("a load with nothing to do opened %d transactions, want none", counting.began)
	}
}

func TestSyncTiles_TheCellItFailsOnIsTheSameEveryTime(t *testing.T) {
	svc, db := syncFixture(t)
	// Two cells the database refuses, on different rows and in opposite order
	// by column. Row-major reaches (5,0) first; anything else does not.
	if _, err := db.Exec(`CREATE TRIGGER refuse BEFORE INSERT ON comp_tile
		WHEN (NEW.x = 5 AND NEW.y = 0) OR (NEW.x = 2 AND NEW.y = 1)
		BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("installing refusal: %v", err)
	}

	w := want(at(0, 0), at(5, 0), at(0, 1), at(2, 1))
	_, err := SyncTiles(context.Background(), svc, db, w)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "(5,0)") {
		t.Errorf("error = %q, want the first cell in row order, (5,0)", err)
	}
}

func TestSyncTiles_AnEntityOfAnotherTypeIsNotAMapTile(t *testing.T) {
	// A Tile component on something that is not a Tile: the map does not
	// describe it, so re-import must not decide it is a stray tile and delete it.
	ds := tileSchema()
	ds.EntityTypes["Marker"] = schema.EntityType{
		RequiredComponents:   []string{"Tile"},
		AllowExtraComponents: false,
		ValidationLevel:      "strict",
	}
	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)
	db := store.DB()

	// Deliberately at a cell the map says nothing about: if re-import read it as
	// a tile, it would be a tile the file dropped, and it would be deleted.
	marker, err := svc.CreateEntity(context.Background(), "Marker", []world.EntityComponent{
		{Name: "Tile", Values: world.ComponentValues{"x": 9, "y": 9, "passable": true, "tile_type": "marker"}},
	})
	if err != nil {
		t.Fatalf("creating the marker: %v", err)
	}

	res := mustSync(t, svc, db, want(at(0, 0)))
	if res.Deleted != 0 {
		t.Errorf("result = %+v, want nothing deleted", res)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, marker.ID).Scan(&n); err != nil {
		t.Fatalf("counting the marker: %v", err)
	}
	if n != 1 {
		t.Error("re-import deleted an entity that is not a Tile")
	}
}

func TestSyncTiles_CellsAreWrittenRowByRow(t *testing.T) {
	// Entity ids are handed out in insertion order, so they are a readable
	// record of the order the cells were written in.
	svc, db := syncFixture(t)

	mustSync(t, svc, db, want(at(2, 1), at(0, 0), at(5, 0), at(0, 1)))

	rows := tileRows(t, db)
	order := []Point{{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 0, Y: 1}, {X: 2, Y: 1}}
	for i := 1; i < len(order); i++ {
		if rows[order[i-1]].ID >= rows[order[i]].ID {
			t.Fatalf("%v was written after %v; want row by row, left to right",
				order[i-1], order[i])
		}
	}
}

func TestSyncTiles_AChangeOfTypeAloneReachesTheDatabase(t *testing.T) {
	svc, db := syncFixture(t)
	mustSync(t, svc, db, want(at(0, 0)))

	grass := map[Point]TileState{{X: 0, Y: 0}: {Passable: true, TileType: "grass"}}
	mustSync(t, svc, db, grass)

	if got := tileRows(t, db)[Point{X: 0, Y: 0}].State.TileType; got != "grass" {
		t.Errorf("tile_type = %q, want grass", got)
	}
	// And the update has to have landed in full: a half-written row leaves the
	// same difference behind, so every load from here on has work to do.
	res := mustSync(t, svc, db, grass)
	if res.Updated != 0 {
		t.Errorf("the load after the edit still had %d updates to make", res.Updated)
	}
}

func TestSyncTiles_AChangeOfPassabilityAloneReachesTheDatabase(t *testing.T) {
	svc, db := syncFixture(t)
	mustSync(t, svc, db, want(at(0, 0)))

	shut := map[Point]TileState{{X: 0, Y: 0}: {Passable: false, TileType: "floor"}}
	mustSync(t, svc, db, shut)

	if tileRows(t, db)[Point{X: 0, Y: 0}].State.Passable {
		t.Error("the cell is still passable")
	}
	res := mustSync(t, svc, db, shut)
	if res.Updated != 0 {
		t.Errorf("the load after the edit still had %d updates to make", res.Updated)
	}
}

func TestSyncTiles_ADatabaseThatWillNotAnswerIsNotAnEmptyOne(t *testing.T) {
	svc, db := syncFixture(t)
	mustSync(t, svc, db, want(at(0, 0)))

	if _, err := db.Exec(`DROP TABLE comp_tile`); err != nil {
		t.Fatalf("dropping comp_tile: %v", err)
	}
	if _, err := SyncTiles(context.Background(), svc, db, want(at(0, 0))); err == nil {
		t.Fatal("a database that cannot say what it holds was read as one holding nothing")
	}
}

func TestSyncTiles_ARowThatWillNotScanIsReportedNotSkipped(t *testing.T) {
	svc, db := syncFixture(t)
	mustSync(t, svc, db, want(at(0, 0), at(1, 0)))

	// SQLite will keep a string in an INTEGER column; scanning it will not.
	if _, err := db.Exec(`UPDATE comp_tile SET x = 'nowhere' WHERE x = 1`); err != nil {
		t.Fatalf("corrupting a row: %v", err)
	}

	if _, err := SyncTiles(context.Background(), svc, db, want(at(0, 0), at(1, 0))); err == nil {
		t.Fatal("a row that could not be read was passed over; the cell would be created again beside it")
	}
}

func TestSyncTiles_ARuntimeComponentOnATileSurvivesTheFileWinning(t *testing.T) {
	// The file describes passability and type. It says nothing about anything
	// else a tile picked up while the game ran, and re-import says nothing about
	// it either — which is only true because a changed cell is updated in place
	// rather than deleted and made again.
	ds := tileSchema()
	ds.Components["Sprite"] = schema.Component{
		Type: "object",
		Properties: map[string]schema.Property{
			"sheet":     {Type: "string"},
			"animation": {Type: "string"},
		},
	}
	ds.EntityTypes["Tile"] = schema.EntityType{
		RequiredComponents:   []string{"Tile"},
		OptionalComponents:   []string{"Sprite"},
		AllowExtraComponents: false,
		ValidationLevel:      "strict",
	}
	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.sqlite", ds, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := world.NewEntityService(store)
	svc.SetSchema(ds)
	db := store.DB()

	mustSync(t, svc, db, want(at(0, 0)))
	id := tileRows(t, db)[Point{X: 0, Y: 0}].ID
	if err := svc.AttachComponent(context.Background(), id, "Sprite", world.ComponentValues{
		"sheet": "dungeon.png", "animation": "torch",
	}); err != nil {
		t.Fatalf("attaching a sprite to a tile: %v", err)
	}

	// The file changes the cell, so this is the update path and not a no-op.
	mustSync(t, svc, db, want(wallAt(0, 0)))

	var anim string
	if err := db.QueryRow(`SELECT animation FROM comp_sprite WHERE entity_id = ?`, id).Scan(&anim); err != nil {
		t.Fatalf("the tile lost a component the file never described: %v", err)
	}
	if anim != "torch" {
		t.Errorf("animation = %q, want torch", anim)
	}
}

func TestSyncTiles_TheEntityItFailsToDeleteIsTheSameEveryTime(t *testing.T) {
	// Cells the map lost and duplicate rows are planned separately, so the two
	// lists have to be merged in id order rather than concatenated — otherwise
	// which delete fails first depends on which list an entity landed in.
	svc, db := syncFixture(t)
	mustSync(t, svc, db, want(at(0, 0)))

	// A duplicate at (0,0), then a tile at (5,0): the duplicate has the lower id
	// and is a repair, the tile has the higher and is a cell the map lost.
	ids := map[string]int64{}
	for _, cell := range []struct {
		name string
		x, y int
	}{{"duplicate", 0, 0}, {"dropped", 5, 0}} {
		if _, err := db.Exec(`INSERT INTO entities (entity_type, created_tick) VALUES ('Tile', 0)`); err != nil {
			t.Fatalf("inserting %s entity: %v", cell.name, err)
		}
		var id int64
		if err := db.QueryRow(`SELECT MAX(id) FROM entities`).Scan(&id); err != nil {
			t.Fatalf("reading %s id: %v", cell.name, err)
		}
		if _, err := db.Exec(
			`INSERT INTO comp_tile (entity_id, passable, tile_type, x, y) VALUES (?, 1, 'floor', ?, ?)`,
			id, cell.x, cell.y,
		); err != nil {
			t.Fatalf("inserting %s tile: %v", cell.name, err)
		}
		ids[cell.name] = id
	}
	if ids["duplicate"] >= ids["dropped"] {
		t.Fatalf("the fixture cannot tell the orders apart: duplicate=%d dropped=%d",
			ids["duplicate"], ids["dropped"])
	}

	if _, err := db.Exec(`CREATE TRIGGER no_delete BEFORE DELETE ON comp_tile
		BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("installing refusal: %v", err)
	}

	_, err := SyncTiles(context.Background(), svc, db, want(at(0, 0)))
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), strconv.FormatInt(ids["duplicate"], 10)) {
		t.Errorf("error = %q, want the lower id %d — the lists are merged, not concatenated",
			err, ids["duplicate"])
	}
}
