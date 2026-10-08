package tilemap

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

// LayerTile identifies an authored tile by its map, stable layer ID and cell.
// The legacy Tile state is kept only for the grid until occupant traversal
// replaces it; appearance belongs to Visual, never to tile_type.
type LayerTile struct {
	MapID      string
	LayerID    int
	X, Y       int
	LayerOrder int
	DrawOrder  int
	State      TileState
	Visual     tiled.Draw
	Visible    bool
}

type layerKey struct {
	mapID   string
	layerID int
	x, y    int
}

func (t LayerTile) key() layerKey { return layerKey{t.MapID, t.LayerID, t.X, t.Y} }

// LayerTilesOfTiled projects all authored cells, including hidden layers, into
// independently addressable tile instances. Invalid layer IDs cannot be
// replaced by indexes: indexes change when an author reorders the layers.
func LayerTilesOfTiled(m *tiled.Map, mapID string) ([]LayerTile, error) {
	if err := checkShape(m); err != nil {
		return nil, err
	}
	seen := make(map[int]bool)
	for _, layer := range m.Layers {
		if layer.ID <= 0 || seen[layer.ID] {
			return nil, fmt.Errorf("tilemap: %s: layer %q has missing, nonpositive or duplicate ID %d", m.Name, layer.Name, layer.ID)
		}
		seen[layer.ID] = true
	}
	out := make([]LayerTile, 0)
	for _, p := range m.AllPlacements() {
		layer := m.Layers[p.LayerIndex]
		gid := layer.TileAt(p.X, p.Y).GID
		where := fmt.Sprintf("tilemap: %s: layer %q: tile at (%d,%d) has global id %d", m.Name, layer.Name, p.X, p.Y, gid)
		ts, local, err := tileOf(m, where, gid)
		if err != nil {
			return nil, err
		}
		state, err := stateOf(ts, local, where)
		if err != nil {
			return nil, err
		}
		if p.Problem != "" {
			return nil, fmt.Errorf("tilemap: %s: layer %q cell (%d,%d): %s", m.Name, p.Layer, p.X, p.Y, p.Problem)
		}
		out = append(out, LayerTile{
			MapID: mapID, LayerID: layer.ID, X: p.X, Y: p.Y,
			LayerOrder: p.LayerIndex, DrawOrder: len(out), State: state,
			Visual: p.Draw, Visible: layer.Visible,
		})
	}
	return out, nil
}

func layerValues(t LayerTile) world.ComponentValues {
	return world.ComponentValues{
		"map_id": t.MapID, "layer_id": t.LayerID,
		"layer_order": t.LayerOrder, "draw_order": t.DrawOrder,
	}
}

func visualValues(t LayerTile) world.ComponentValues {
	d := t.Visual
	return world.ComponentValues{
		"image": d.Image, "source_x": d.SX, "source_y": d.SY,
		"source_w": d.SW, "source_h": d.SH, "dest_x": d.DX,
		"dest_y": d.DY, "flip_h": d.FlipH, "flip_v": d.FlipV,
		"flip_d": d.FlipD, "alpha": d.Alpha, "visible": t.Visible,
	}
}

type storedLayerTile struct {
	id   int64
	tile LayerTile
}

// SyncLayerTiles applies the map-owned portion of each Tile entity in one
// transaction. Other components attached at runtime survive every re-import.
// pathID adopts tiles imported before an author added the mapId property.
// Like spawns, imports happen at startup, before the tick loop or other writers.
func SyncLayerTiles(ctx context.Context, svc *world.EntityService, db *sql.DB, mapID, pathID string, tiles []LayerTile) (Result, error) {
	rows, err := db.QueryContext(ctx, `SELECT e.id, t.x, t.y, t.passable, t.tile_type,
		l.map_id, l.layer_id, l.layer_order, l.draw_order,
		v.image, v.source_x, v.source_y, v.source_w, v.source_h,
		v.dest_x, v.dest_y, v.flip_h, v.flip_v, v.flip_d, v.alpha, v.visible
		FROM entities e
		LEFT JOIN comp_tile t ON t.entity_id = e.id
		LEFT JOIN comp_tilelayer l ON l.entity_id = e.id
		LEFT JOIN comp_tilevisual v ON v.entity_id = e.id
		WHERE e.entity_type = 'Tile'`)
	if err != nil {
		return Result{}, fmt.Errorf("reading layer tiles: %w", err)
	}
	have := make(map[layerKey]storedLayerTile)
	var duplicate []int64
	for rows.Next() {
		var s storedLayerTile
		var owner sql.NullString
		var layerID, layerOrder, drawOrder sql.NullInt64
		var image sql.NullString
		var sx, sy, sw, sh, dx, dy, fh, fv, fd, visible sql.NullInt64
		var alpha sql.NullFloat64
		var x, y, passable sql.NullInt64
		var tileType sql.NullString
		err = rows.Scan(&s.id, &x, &y, &passable, &tileType,
			&owner, &layerID, &layerOrder, &drawOrder, &image,
			&sx, &sy, &sw, &sh, &dx, &dy, &fh, &fv, &fd, &alpha, &visible)
		if err != nil {
			break
		}
		if !owner.Valid || !layerID.Valid {
			_ = rows.Close()
			return Result{}, fmt.Errorf("tilemap: legacy Tile %d has no layer ownership; migrate or recreate the old world database before importing a layered map", s.id)
		}
		if owner.String != mapID && owner.String != pathID {
			continue
		}
		if !x.Valid || !y.Valid || !passable.Valid || !tileType.Valid {
			_ = rows.Close()
			return Result{}, fmt.Errorf("tilemap: tile %d in map %q is missing its Tile component", s.id, mapID)
		}
		if !image.Valid || !layerOrder.Valid || !drawOrder.Valid {
			_ = rows.Close()
			return Result{}, fmt.Errorf("tilemap: tile %d in map %q has layer identity but is missing visual or order state", s.id, mapID)
		}
		s.tile.MapID, s.tile.LayerID = owner.String, int(layerID.Int64)
		s.tile.X, s.tile.Y = int(x.Int64), int(y.Int64)
		s.tile.LayerOrder, s.tile.DrawOrder = int(layerOrder.Int64), int(drawOrder.Int64)
		s.tile.State.Passable, s.tile.State.TileType = passable.Int64 != 0, tileType.String
		s.tile.Visual = tiled.Draw{
			Image: image.String,
			SX:    int(sx.Int64), SY: int(sy.Int64), SW: int(sw.Int64), SH: int(sh.Int64),
			DX: int(dx.Int64), DY: int(dy.Int64), FlipH: fh.Int64 != 0,
			FlipV: fv.Int64 != 0, FlipD: fd.Int64 != 0, Alpha: alpha.Float64,
		}
		s.tile.Visible = visible.Int64 != 0
		key := s.tile.key()
		key.mapID = mapID // path-keyed tiles adopt the authored mapId in place
		if old, found := have[key]; found {
			if old.tile.MapID != s.tile.MapID {
				_ = rows.Close()
				return Result{}, fmt.Errorf("tilemap: map %q has both path-keyed and mapId-keyed tiles in layer %d at (%d,%d); cannot choose which identity to keep", mapID, key.layerID, key.x, key.y)
			}
			if old.id < s.id {
				duplicate = append(duplicate, s.id)
				continue
			}
			duplicate = append(duplicate, old.id)
		}
		have[key] = s
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return Result{}, fmt.Errorf("reading layer tiles: %w", err)
	}

	want := make(map[layerKey]LayerTile, len(tiles))
	for _, tile := range tiles {
		if tile.MapID != mapID {
			return Result{}, fmt.Errorf("tilemap: tile belongs to map %q, expected %q", tile.MapID, mapID)
		}
		key := tile.key()
		if _, exists := want[key]; exists {
			return Result{}, fmt.Errorf("tilemap: duplicate authored tile at layer %d cell (%d,%d)", tile.LayerID, tile.X, tile.Y)
		}
		want[key] = tile
	}

	var create []LayerTile
	var update []storedLayerTile
	var drop []int64
	res := Result{Repaired: len(duplicate)}
	for key, tile := range want {
		stored, found := have[key]
		switch {
		case !found:
			create = append(create, tile)
		case stored.tile != tile:
			update = append(update, storedLayerTile{stored.id, tile})
		default:
			res.Unchanged++
		}
	}
	for key, tile := range have {
		if _, found := want[key]; !found {
			drop = append(drop, tile.id)
		}
	}
	res.Created, res.Updated, res.Deleted = len(create), len(update), len(drop)
	if res.Created+res.Updated+res.Deleted+res.Repaired == 0 {
		return res, nil
	}
	sort.Slice(create, func(i, j int) bool { return lessLayerTile(create[i], create[j]) })
	sort.Slice(update, func(i, j int) bool { return lessLayerTile(update[i].tile, update[j].tile) })
	drop = append(drop, duplicate...)
	sort.Slice(drop, func(i, j int) bool { return drop[i] < drop[j] })
	err = svc.InTx(ctx, func(tx world.Tx) error {
		for _, tile := range create {
			_, err := svc.CreateEntityInTx(ctx, tx, "Tile", []world.EntityComponent{
				{Name: "Tile", Values: tileValues(Point{X: tile.X, Y: tile.Y}, tile.State)},
				{Name: "TileLayer", Values: layerValues(tile)},
				{Name: "TileVisual", Values: visualValues(tile)},
			})
			if err != nil {
				return fmt.Errorf("creating tile in layer %d at (%d,%d): %w", tile.LayerID, tile.X, tile.Y, err)
			}
		}
		for _, stored := range update {
			tile := stored.tile
			for _, component := range []world.EntityComponent{
				{Name: "Tile", Values: tileValues(Point{X: tile.X, Y: tile.Y}, tile.State)},
				{Name: "TileLayer", Values: layerValues(tile)},
				{Name: "TileVisual", Values: visualValues(tile)},
			} {
				if err := tx.SetComponentValues(ctx, stored.id, component.Name, component.Values); err != nil {
					return fmt.Errorf("updating tile %d %s: %w", stored.id, component.Name, err)
				}
			}
		}
		for _, id := range drop {
			if err := tx.DeleteEntity(ctx, id); err != nil {
				return fmt.Errorf("deleting tile %d: %w", id, err)
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

func lessLayerTile(a, b LayerTile) bool {
	if a.LayerOrder != b.LayerOrder {
		return a.LayerOrder < b.LayerOrder
	}
	if a.DrawOrder != b.DrawOrder {
		return a.DrawOrder < b.DrawOrder
	}
	return a.LayerID < b.LayerID
}

// ReadTileDraws reads the current database appearance in draw order. It is
// intentionally queried on each game draw so a runtime edit takes effect on
// the next frame, without loading or reparsing a TMX file.
func ReadTileDraws(ctx context.Context, db *sql.DB, mapID string) ([]tiled.Draw, error) {
	rows, err := db.QueryContext(ctx, `SELECT v.image, v.source_x, v.source_y,
		v.source_w, v.source_h, v.dest_x, v.dest_y, v.flip_h, v.flip_v,
		v.flip_d, v.alpha FROM comp_tilelayer l JOIN comp_tilevisual v
		ON v.entity_id = l.entity_id JOIN entities e ON e.id = l.entity_id
		WHERE e.entity_type = 'Tile' AND l.map_id = ? AND v.visible = 1 AND v.alpha > 0
		ORDER BY l.layer_order, l.draw_order, e.id`, mapID)
	if err != nil {
		return nil, fmt.Errorf("reading tile visuals: %w", err)
	}
	defer rows.Close()
	var out []tiled.Draw
	for rows.Next() {
		var d tiled.Draw
		var fh, fv, fd int
		if err := rows.Scan(&d.Image, &d.SX, &d.SY, &d.SW, &d.SH,
			&d.DX, &d.DY, &fh, &fv, &fd, &d.Alpha); err != nil {
			return nil, fmt.Errorf("reading tile visuals: %w", err)
		}
		d.FlipH, d.FlipV, d.FlipD = fh != 0, fv != 0, fd != 0
		out = append(out, d)
	}
	return out, rows.Err()
}
