package tilemap

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/tmbritton/ecs-db/internal/world"
)

// TileState is the legacy movement/type metadata of one authored layer tile.
// Story 3 replaces its Boolean; TileVisual owns its appearance meanwhile.
type TileState struct {
	Passable bool
	TileType string
}

// storedTile is one row of the Tile join, position and identity together.
type storedTile struct {
	ID    int64
	X, Y  int
	State TileState
}

// tilePlan is the difference between what the database holds and what the file
// says, decided per cell. It is unexported along with planTiles: the diff is
// stated over rows only this package can read, and SyncTiles is the way in.
type tilePlan struct {
	// create is the cells the file has and the database does not.
	create map[Point]TileState
	// update is the cells both have, disagreeing about.
	update map[Point]TileState
	// drop is the entity ids of cells the file no longer has, ascending.
	drop []int64
	// duplicate is the entity ids of extra rows at a cell that held more than
	// one. Kept apart from drop because they are different events: a map that
	// shrank, and a database repaired.
	duplicate []int64
	// keep maps every surviving cell to the entity id that survives there.
	keep map[Point]int64
	// unchanged counts the cells the file and the database already agree on.
	unchanged int
}

// empty reports whether applying the plan would write nothing.
func (p tilePlan) empty() bool {
	return len(p.create) == 0 && len(p.update) == 0 &&
		len(p.drop) == 0 && len(p.duplicate) == 0
}

// deletions is every entity id to remove, ascending, so the order does not
// depend on which list an id came from.
func (p tilePlan) deletions() []int64 {
	all := make([]int64, 0, len(p.drop)+len(p.duplicate))
	all = append(all, p.drop...)
	all = append(all, p.duplicate...)
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	return all
}

// Result is what a re-import did, for a caller that wants to log it.
type Result struct {
	Created int
	Updated int
	// Deleted counts cells the file no longer describes.
	Deleted int
	// Repaired counts duplicate rows removed during import.
	Repaired  int
	Unchanged int
}

// PlanTiles decides, per cell, what has to happen for the database to say what
// the file says.
//
// **The file wins for what the file describes.** A tile's passability and its
// type come from the map on every load, overwriting whatever the running game
// left there — setTilePassable opening a door is state from one session, and
// the file is the level. The alternative, letting the database win, would mean
// an author who walls off a corridor in Forge saves, runs, and finds the
// corridor still open: the exact failure re-import exists to end, moved one
// field to the left.
//
// **The entity id survives.** It is the one thing that cannot be recreated —
// TileGrid indexes by it, and every row in transitions naming a tile names its
// id — so a changed cell is an UPDATE and never a delete and an insert.
// entities.created_tick survives with it, because the entity row is not
// rewritten.
//
// **What the file says nothing about, this says nothing about.** Any other
// component attached to a tile entity at runtime is left where it is, which is
// only true because the diff updates in place rather than reloading wholesale.
//
// **Two tiles in one cell** is a state the database permits — comp_tile is
// keyed by entity_id and nothing makes (x,y) unique — and the engine cannot
// use: TileGrid.Rebuild keeps whichever row the query returned last, an order
// SQLite does not promise. The lowest id survives, being the earliest created,
// and the rest are deleted. That makes the invariant the diff rests on true
// rather than assumed.
func planTiles(have []storedTile, want map[Point]TileState) tilePlan {
	p := tilePlan{
		create: make(map[Point]TileState),
		update: make(map[Point]TileState),
		keep:   make(map[Point]int64, len(want)),
	}

	// Lowest id first, so the first row seen at a position is the survivor,
	// every later one at that position is a duplicate, and Delete comes out
	// ascending without being sorted again.
	rows := make([]storedTile, len(have))
	copy(rows, have)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })

	survivor := make(map[Point]storedTile, len(rows))
	for _, row := range rows {
		at := Point{X: row.X, Y: row.Y}
		_, seen := survivor[at]
		_, wanted := want[at]
		if seen {
			p.duplicate = append(p.duplicate, row.ID)
			continue
		}
		if !wanted {
			p.drop = append(p.drop, row.ID)
			continue
		}
		survivor[at] = row
	}

	for at, state := range want {
		row, ok := survivor[at]
		if !ok {
			p.create[at] = state
			continue
		}
		p.keep[at] = row.ID
		if row.State == state {
			p.unchanged++
			continue
		}
		p.update[at] = state
	}
	return p
}

// SyncTiles brings the Tile entities in the database in line with what a map
// file says, and reports what it did.
//
// Deprecated: this single-cell writer is retained for its legacy tests only.
// The game imports maps through SyncLayerTiles; Story 3 removes this API when
// the transitional Tile.passable field is retired.
//
// Nothing is written when the two already agree: the plan comes out empty and
// no transaction is opened, so a restart with no edit to the map is a read.
// Everything else happens in one transaction, because a re-import that commits
// half its tiles is a map with a hole in it and the engine reads the grid on
// the next tick.
//
// **This is a startup path, and the read is outside that transaction.** The
// port has no reader, so what the database holds is read on the pool before the
// write begins; a writer between the two would make the plan describe rows that
// have moved. That is not reachable from the engine, which loads the map in its
// composition root before the tick loop starts, and Forge never calls this at
// all — it writes map files and the engine re-imports them. It would become
// reachable if something loaded a map while the game was running, and the fix
// then is a reader on world.Tx, not a retry here.
//
// Where it can be made loud, it is: SetComponentValues refuses an update that
// matched no row, so a tile deleted between the read and the write is an error
// rather than a cell that quietly kept the database's version of itself.
func SyncTiles(
	ctx context.Context,
	svc *world.EntityService,
	db *sql.DB,
	want map[Point]TileState,
) (Result, error) {
	have, err := storedTiles(ctx, db)
	if err != nil {
		return Result{}, err
	}

	plan := planTiles(have, want)
	res := Result{
		Created:   len(plan.create),
		Updated:   len(plan.update),
		Deleted:   len(plan.drop),
		Repaired:  len(plan.duplicate),
		Unchanged: plan.unchanged,
	}
	if plan.empty() {
		return res, nil
	}

	if err := svc.InTx(ctx, func(tx world.Tx) error { return applyPlan(ctx, svc, tx, plan) }); err != nil {
		return Result{}, err
	}
	return res, nil
}

// applyPlan writes a plan inside a transaction the caller owns.
//
// Creates and updates run in position order rather than map order so a failing
// import fails at the same cell every time — the difference between a bug that
// can be reproduced and one that moves.
func applyPlan(ctx context.Context, svc *world.EntityService, tx world.Tx, plan tilePlan) error {
	for _, at := range sortedPoints(plan.create) {
		state := plan.create[at]
		if _, err := svc.CreateEntityInTx(ctx, tx, "Tile", []world.EntityComponent{
			{Name: "Tile", Values: tileValues(at, state)},
		}); err != nil {
			return fmt.Errorf("creating tile (%d,%d): %w", at.X, at.Y, err)
		}
	}

	for _, at := range sortedPoints(plan.update) {
		state := plan.update[at]
		if err := tx.SetComponentValues(ctx, plan.keep[at], "Tile", world.ComponentValues{
			"passable":  state.Passable,
			"tile_type": state.TileType,
		}); err != nil {
			return fmt.Errorf("updating tile (%d,%d): %w", at.X, at.Y, err)
		}
	}

	for _, id := range plan.deletions() {
		if err := tx.DeleteEntity(ctx, id); err != nil {
			return fmt.Errorf("deleting tile entity %d: %w", id, err)
		}
	}
	return nil
}

func tileValues(at Point, state TileState) world.ComponentValues {
	return world.ComponentValues{
		"x": at.X, "y": at.Y,
		"passable": state.Passable, "tile_type": state.TileType,
	}
}

func sortedPoints(m map[Point]TileState) []Point {
	pts := make([]Point, 0, len(m))
	for at := range m {
		pts = append(pts, at)
	}
	sort.Slice(pts, func(i, j int) bool {
		if pts[i].Y != pts[j].Y {
			return pts[i].Y < pts[j].Y
		}
		return pts[i].X < pts[j].X
	})
	return pts
}

// storedTiles reads every Tile entity's position and state, duplicates and all
// — deciding what to do about them is PlanTiles's job, not the query's.
func storedTiles(ctx context.Context, db *sql.DB) ([]storedTile, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT entities.id, comp_tile.x, comp_tile.y, comp_tile.passable, comp_tile.tile_type
		 FROM entities
		 JOIN comp_tile ON entities.id = comp_tile.entity_id
		 WHERE entities.entity_type = 'Tile'`)
	if err != nil {
		return nil, fmt.Errorf("reading stored tiles: %w", err)
	}
	defer rows.Close()

	var out []storedTile
	for rows.Next() {
		var t storedTile
		var passable int
		if err := rows.Scan(&t.ID, &t.X, &t.Y, &passable, &t.State.TileType); err != nil {
			return nil, fmt.Errorf("reading stored tiles: %w", err)
		}
		t.State.Passable = passable != 0
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading stored tiles: %w", err)
	}
	return out, nil
}
