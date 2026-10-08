package tilemap

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

// TileState is the authored class of one layer's artwork. Movement and sight
// belong to separate occupants and never read tile_type.
type TileState struct {
	TileType   string
	EntityType string
}

// Result reports a map import's changes.
type Result struct {
	Created, Updated, Deleted, Repaired, Unchanged int
}

func tileValues(at Point, state TileState) world.ComponentValues {
	return world.ComponentValues{
		"x": at.X, "y": at.Y,
		"tile_type": state.TileType,
	}
}

// LayerTile identifies an authored tile by its map, stable layer ID and cell.
// Appearance belongs to Visual; interaction rules belong to occupants.
type LayerTile struct {
	MapID              string
	LayerID            int
	X, Y               int
	LayerOrder         int
	DrawOrder          int
	CellWidth          int
	CellHeight         int
	MapWidth           int
	MapHeight          int
	Opacity            float64
	State              TileState
	Visual             tiled.Draw
	Visible            bool
	SharedReference    bool
	TemplateProperties tiled.Properties
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
		visual := p.Draw
		visual.DX -= p.X * m.TileWidth
		visual.DY -= p.Y * m.TileHeight
		visual.Alpha = 1 // layer opacity belongs to TileLayer, not shared art
		out = append(out, LayerTile{
			MapID: mapID, LayerID: layer.ID, X: p.X, Y: p.Y,
			LayerOrder: p.LayerIndex, DrawOrder: len(out), State: state,
			CellWidth: m.TileWidth, CellHeight: m.TileHeight,
			MapWidth: m.Width, MapHeight: m.Height, Opacity: layer.Opacity,
			Visual: visual, Visible: layer.Visible,
			TemplateProperties: tileTemplateProperties(ts, local),
		})
	}
	return out, nil
}

func tileTemplateProperties(ts *tiled.Tileset, local uint32) tiled.Properties {
	props := make(tiled.Properties, len(ts.Properties)+len(ts.Tiles[local].Properties))
	for name, prop := range ts.Properties {
		if _, _, componentField := strings.Cut(name, "."); componentField {
			props[name] = prop
		}
	}
	for name, prop := range ts.Tiles[local].Properties {
		if _, _, componentField := strings.Cut(name, "."); componentField {
			props[name] = prop
		}
	}
	return props
}

func layerValues(t LayerTile) world.ComponentValues {
	return world.ComponentValues{
		"map_id": t.MapID, "layer_id": t.LayerID,
		"layer_order": t.LayerOrder, "draw_order": t.DrawOrder,
		"cell_w": t.CellWidth, "cell_h": t.CellHeight,
		"visible": t.Visible, "opacity": t.Opacity,
	}
}

func visualValues(t LayerTile) world.ComponentValues {
	d := t.Visual
	return world.ComponentValues{
		"image": d.Image, "source_x": d.SX, "source_y": d.SY,
		"source_w": d.SW, "source_h": d.SH, "dest_x": d.DX,
		"dest_y": d.DY, "flip_h": d.FlipH, "flip_v": d.FlipV,
		"flip_d": d.FlipD, "alpha": d.Alpha, "visible": true,
	}
}

type storedLayerTile struct {
	id                int64
	artID             int64
	artType           string
	directVisual      bool
	tile              LayerTile
	referenced        []world.EntityComponent
	authored          []string
	authoredKnown     bool
	removed           []string
	positionMissing   bool
	positionX         float64
	positionY         float64
	referencesMissing bool
}

// SyncLayerTiles applies the map-owned portion of each Tile entity in one
// transaction. Other components attached at runtime survive every re-import.
// pathID adopts tiles imported before an author added the mapId property.
// Like spawns, imports happen at startup, before the tick loop or other writers.
func SyncLayerTiles(ctx context.Context, svc *world.EntityService, db *sql.DB, mapID, pathID string, tiles []LayerTile) (Result, error) {
	query := `SELECT e.id, t.x, t.y, t.tile_type,
		l.map_id, l.layer_id, l.layer_order, l.draw_order,
		l.cell_w, l.cell_h, l.visible, l.opacity, art.entity_id, art_entity.entity_type,
		v.image, v.source_x, v.source_y, v.source_w, v.source_h,
		v.dest_x, v.dest_y, v.flip_h, v.flip_v, v.flip_d, v.alpha, v.visible,
		p.x, p.y, refs.value, direct_visual.entity_id, authored.components
		FROM entities e
		LEFT JOIN comp_tile t ON t.entity_id = e.id
		LEFT JOIN comp_tilelayer l ON l.entity_id = e.id
		LEFT JOIN comp_tileentityowner art ON art.target_entity_id = e.id
		LEFT JOIN entities art_entity ON art_entity.id = art.entity_id
		LEFT JOIN comp_tilevisual v ON v.entity_id = art.entity_id
		LEFT JOIN comp_position p ON p.entity_id = e.id
		LEFT JOIN comp_tilereferences refs ON refs.entity_id = e.id
		LEFT JOIN comp_tilevisual direct_visual ON direct_visual.entity_id = e.id
		LEFT JOIN tile_art_components authored ON authored.entity_id = art.entity_id
		WHERE e.entity_type = 'Tile'`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return Result{}, fmt.Errorf("reading layer tiles: %w", err)
	}
	have := make(map[layerKey]storedLayerTile)
	dropArt := make(map[int64]int64)
	var duplicate []int64
	for rows.Next() {
		var s storedLayerTile
		var owner sql.NullString
		var layerID, layerOrder, drawOrder, cellW, cellH, layerVisible, artID sql.NullInt64
		var layerOpacity sql.NullFloat64
		var image sql.NullString
		var sx, sy, sw, sh, dx, dy, fh, fv, fd, visible sql.NullInt64
		var alpha sql.NullFloat64
		var x, y sql.NullInt64
		var positionX, positionY sql.NullFloat64
		var tileType, referencedType sql.NullString
		var references sql.NullString
		var authored sql.NullString
		var directVisual sql.NullInt64
		err = rows.Scan(&s.id, &x, &y, &tileType,
			&owner, &layerID, &layerOrder, &drawOrder,
			&cellW, &cellH, &layerVisible, &layerOpacity, &artID, &referencedType, &image,
			&sx, &sy, &sw, &sh, &dx, &dy, &fh, &fv, &fd, &alpha, &visible,
			&positionX, &positionY, &references, &directVisual, &authored)
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
		if !x.Valid || !y.Valid || !tileType.Valid {
			_ = rows.Close()
			return Result{}, fmt.Errorf("tilemap: tile %d in map %q is missing its Tile component", s.id, mapID)
		}
		if !layerOrder.Valid || !drawOrder.Valid || !cellW.Valid || !cellH.Valid || !layerVisible.Valid || !layerOpacity.Valid {
			_ = rows.Close()
			return Result{}, fmt.Errorf("tilemap: tile %d in map %q has incomplete layer order or placement state", s.id, mapID)
		}
		if artID.Valid && !image.Valid {
			_ = rows.Close()
			return Result{}, fmt.Errorf("tilemap: referenced entity %d for Tile %d has no TileVisual", artID.Int64, s.id)
		}
		s.artID = artID.Int64
		if authored.Valid {
			if err := json.Unmarshal([]byte(authored.String), &s.authored); err != nil {
				_ = rows.Close()
				return Result{}, fmt.Errorf("tilemap: owned entity %d has invalid authored component list: %w", s.artID, err)
			}
			s.authoredKnown = true
		}
		s.directVisual = directVisual.Valid
		s.artType = referencedType.String
		s.tile.State.EntityType = referencedType.String
		s.tile.MapID, s.tile.LayerID = owner.String, int(layerID.Int64)
		if positionX.Valid != positionY.Valid {
			_ = rows.Close()
			return Result{}, fmt.Errorf("tilemap: Tile %d has incomplete Position", s.id)
		}
		s.positionMissing = !positionX.Valid
		s.positionX, s.positionY = positionX.Float64, positionY.Float64
		s.referencesMissing = !references.Valid
		if references.Valid {
			if _, err := ParseTileReferences(references.String); err != nil {
				_ = rows.Close()
				return Result{}, fmt.Errorf("tilemap: Tile %d: %w", s.id, err)
			}
		}
		s.tile.X, s.tile.Y = int(x.Int64), int(y.Int64)
		s.tile.LayerOrder, s.tile.DrawOrder = int(layerOrder.Int64), int(drawOrder.Int64)
		s.tile.CellWidth, s.tile.CellHeight = int(cellW.Int64), int(cellH.Int64)
		s.tile.Visible, s.tile.Opacity = layerVisible.Int64 != 0, layerOpacity.Float64
		s.tile.State.TileType = tileType.String
		s.tile.Visual = tiled.Draw{
			Image: image.String,
			SX:    int(sx.Int64), SY: int(sy.Int64), SW: int(sw.Int64), SH: int(sh.Int64),
			DX: int(dx.Int64), DY: int(dy.Int64), FlipH: fh.Int64 != 0,
			FlipV: fv.Int64 != 0, FlipD: fd.Int64 != 0, Alpha: alpha.Float64,
		}
		// TileVisual.visible is intrinsic to the referenced art. A hidden map
		// layer is carried separately by TileLayer.visible.
		if artID.Valid && visible.Int64 == 0 {
			s.tile.Visual.Image = "" // forces a file-owned visual update
		}
		key := s.tile.key()
		key.mapID = mapID // path-keyed tiles adopt the authored mapId in place
		if old, found := have[key]; found {
			if old.id == s.id {
				_ = rows.Close()
				return Result{}, fmt.Errorf("tilemap: Tile %d has multiple owned referenced entities", s.id)
			}
			if old.tile.MapID != s.tile.MapID {
				_ = rows.Close()
				return Result{}, fmt.Errorf("tilemap: map %q has both path-keyed and mapId-keyed tiles in layer %d at (%d,%d); cannot choose which identity to keep", mapID, key.layerID, key.x, key.y)
			}
			if old.id < s.id {
				duplicate = append(duplicate, s.id)
				dropArt[s.id] = s.artID
				continue
			}
			duplicate = append(duplicate, old.id)
			dropArt[old.id] = old.artID
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
		referenceChanged := false
		if found && stored.artID != 0 && stored.artType == tile.State.EntityType && !tile.SharedReference {
			stored.referenced, err = referencedTileComponents(svc.Schema(), tile, stored.id)
			if err != nil {
				return Result{}, err
			}
			referenceChanged, err = needsUpdate(ctx, db, svc.Schema(), stored.artID, stored.referenced)
			if err != nil {
				return Result{}, fmt.Errorf("comparing referenced entity for Tile %d: %w", stored.id, err)
			}
			names := authoredNames(stored.referenced)
			if !stored.authoredKnown || !slices.Equal(stored.authored, names) {
				referenceChanged = true
			}
			current := make(map[string]bool, len(names))
			for _, name := range names {
				current[name] = true
			}
			entityType := svc.Schema().EntityTypes[tile.State.EntityType]
			for _, name := range stored.authored {
				if current[name] || entityType.IsComponentRequired(name) {
					continue
				}
				if _, declared := svc.Schema().Components[name]; declared {
					var attached int
					if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM "comp_`+strings.ToLower(name)+`" WHERE entity_id=?)`, stored.artID).Scan(&attached); err != nil {
						return Result{}, fmt.Errorf("checking formerly authored %s on entity %d: %w", name, stored.artID, err)
					}
					if attached != 0 {
						stored.removed = append(stored.removed, name)
					}
				}
			}
		}
		switch {
		case !found:
			create = append(create, tile)
		case !sameImportedTile(stored.tile, tile) || referenceChanged || (!tile.SharedReference && stored.artID == 0) ||
			(tile.SharedReference && stored.artID != 0) || stored.directVisual || stored.positionMissing || stored.positionX != float64(tile.X) ||
			stored.positionY != float64(tile.Y) || stored.referencesMissing:
			stored.tile = tile
			update = append(update, stored)
		default:
			res.Unchanged++
		}
	}
	for key, tile := range have {
		if _, found := want[key]; !found {
			drop = append(drop, tile.id)
			dropArt[tile.id] = tile.artID
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
			placed, err := svc.CreateEntityInTx(ctx, tx, "Tile", []world.EntityComponent{
				{Name: "Tile", Values: tileValues(Point{X: tile.X, Y: tile.Y}, tile.State)},
				{Name: "TileLayer", Values: layerValues(tile)},
				{Name: "Position", Values: world.ComponentValues{"x": tile.X, "y": tile.Y}},
				{Name: "TileReferences", Values: world.ComponentValues{"value": []int64{}}},
			})
			if err != nil {
				return fmt.Errorf("creating tile in layer %d at (%d,%d): %w", tile.LayerID, tile.X, tile.Y, err)
			}
			if !tile.SharedReference {
				referenced, err := referencedTileComponents(svc.Schema(), tile, placed.ID)
				if err != nil {
					return err
				}
				art, err := svc.CreateEntityInTx(ctx, tx, tile.State.EntityType, referenced)
				if err != nil {
					return fmt.Errorf("creating referenced art for tile %d: %w", placed.ID, err)
				}
				if err := tx.SetComponentValues(ctx, placed.ID, "TileReferences", world.ComponentValues{"value": []int64{art.ID}}); err != nil {
					return fmt.Errorf("linking tile %d to art %d: %w", placed.ID, art.ID, err)
				}
				if err := tx.SetTileArtComponents(ctx, art.ID, authoredNames(referenced)); err != nil {
					return err
				}
			}
		}
		for _, stored := range update {
			tile := stored.tile
			if stored.directVisual {
				if err := tx.DetachComponent(ctx, stored.id, "TileVisual"); err != nil {
					return fmt.Errorf("removing legacy Tile %d visual: %w", stored.id, err)
				}
			}
			for _, component := range []world.EntityComponent{
				{Name: "Tile", Values: tileValues(Point{X: tile.X, Y: tile.Y}, tile.State)},
				{Name: "TileLayer", Values: layerValues(tile)},
			} {
				if err := tx.SetComponentValues(ctx, stored.id, component.Name, component.Values); err != nil {
					return fmt.Errorf("updating tile %d %s: %w", stored.id, component.Name, err)
				}
			}
			if tile.SharedReference {
				if stored.artID != 0 {
					if err := tx.DeleteEntity(ctx, stored.artID); err != nil {
						return fmt.Errorf("removing replaced visual entity %d: %w", stored.artID, err)
					}
					if err := tx.SetComponentValues(ctx, stored.id, "TileReferences", world.ComponentValues{"value": []int64{}}); err != nil {
						return fmt.Errorf("resetting Tile %d links for shared entity: %w", stored.id, err)
					}
				}
			} else if stored.artID == 0 || stored.artType != tile.State.EntityType {
				if stored.artID != 0 {
					if err := tx.DeleteEntity(ctx, stored.artID); err != nil {
						return fmt.Errorf("deleting replaced %s entity %d: %w", stored.artType, stored.artID, err)
					}
				}
				referenced, err := referencedTileComponents(svc.Schema(), tile, stored.id)
				if err != nil {
					return err
				}
				art, err := svc.CreateEntityInTx(ctx, tx, tile.State.EntityType, referenced)
				if err != nil {
					return fmt.Errorf("creating missing art for tile %d: %w", stored.id, err)
				}
				refs := world.ComponentValues{"value": []int64{art.ID}}
				if stored.referencesMissing {
					if err := tx.AttachComponent(ctx, stored.id, "TileReferences", refs); err != nil {
						return fmt.Errorf("adding Tile %d references to new entity: %w", stored.id, err)
					}
					stored.referencesMissing = false
				} else if err := tx.SetComponentValues(ctx, stored.id, "TileReferences", refs); err != nil {
					return fmt.Errorf("linking tile %d to new entity: %w", stored.id, err)
				}
				if err := tx.SetTileArtComponents(ctx, art.ID, authoredNames(referenced)); err != nil {
					return err
				}
			} else {
				for _, name := range stored.removed {
					if err := tx.DetachComponent(ctx, stored.artID, name); err != nil {
						return fmt.Errorf("removing formerly authored %s from referenced entity %d: %w", name, stored.artID, err)
					}
				}
				for _, component := range stored.referenced {
					err := tx.SetComponentValues(ctx, stored.artID, component.Name, component.Values)
					if errors.Is(err, world.ErrNoSuchComponent) {
						err = tx.AttachComponent(ctx, stored.artID, component.Name, component.Values)
					}
					if err != nil {
						return fmt.Errorf("updating %s on referenced entity %d for Tile %d: %w", component.Name, stored.artID, stored.id, err)
					}
				}
				if err := tx.SetTileArtComponents(ctx, stored.artID, authoredNames(stored.referenced)); err != nil {
					return err
				}
			}
			position := world.ComponentValues{"x": tile.X, "y": tile.Y}
			if stored.positionMissing {
				if err := tx.AttachComponent(ctx, stored.id, "Position", position); err != nil {
					return fmt.Errorf("adding Tile %d Position: %w", stored.id, err)
				}
			} else if stored.positionX != float64(tile.X) || stored.positionY != float64(tile.Y) {
				if err := tx.SetComponentValues(ctx, stored.id, "Position", position); err != nil {
					return fmt.Errorf("restoring Tile %d Position: %w", stored.id, err)
				}
			}
			if stored.referencesMissing {
				if err := tx.AttachComponent(ctx, stored.id, "TileReferences", world.ComponentValues{"value": []int64{}}); err != nil {
					return fmt.Errorf("adding Tile %d references: %w", stored.id, err)
				}
			}
		}
		for _, id := range drop {
			if artID := dropArt[id]; artID != 0 {
				if err := tx.DeleteEntity(ctx, artID); err != nil {
					return fmt.Errorf("deleting Tile %d's owned referenced entity %d: %w", id, artID, err)
				}
			}
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

func sameLayerTile(a, b LayerTile) bool {
	return a.MapID == b.MapID && a.LayerID == b.LayerID && a.X == b.X && a.Y == b.Y &&
		a.LayerOrder == b.LayerOrder && a.DrawOrder == b.DrawOrder &&
		a.CellWidth == b.CellWidth && a.CellHeight == b.CellHeight &&
		a.Opacity == b.Opacity && a.Visible == b.Visible && a.State == b.State && a.Visual == b.Visual
}

// A shared reference's entity type and visual live on the linked spawn; they
// have no per-Tile owned art row to compare. The Tile's own state and layer
// fields are still compared on every import.
func sameImportedTile(stored, authored LayerTile) bool {
	if authored.SharedReference {
		stored.State.EntityType = authored.State.EntityType
		stored.Visual = authored.Visual
	}
	return sameLayerTile(stored, authored)
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
	var hasOwners int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='comp_tileentityowner'`).Scan(&hasOwners); err != nil {
		return nil, fmt.Errorf("checking tile visual owners: %w", err)
	}
	query := `SELECT v.image, v.source_x, v.source_y,
		v.source_w, v.source_h, v.dest_x, v.dest_y, v.flip_h, v.flip_v,
		v.flip_d, v.alpha, p.x, p.y, l.cell_w, l.cell_h, l.opacity
		FROM comp_tilelayer l JOIN comp_position p ON p.entity_id=l.entity_id
		JOIN comp_tilereferences refs ON refs.entity_id=l.entity_id
		JOIN json_each(refs.value) target
		JOIN comp_tilevisual v ON v.entity_id=CAST(target.value AS INTEGER)
		WHERE l.map_id = ? AND l.visible = 1 AND l.opacity > 0 AND v.visible = 1 AND v.alpha > 0
		AND (NOT EXISTS (SELECT 1 FROM spawns foreign_spawn WHERE foreign_spawn.entity_id=v.entity_id)
			OR EXISTS (SELECT 1 FROM spawns local_spawn WHERE local_spawn.entity_id=v.entity_id AND local_spawn.map=?))`
	args := []any{mapID, mapID}
	if hasOwners != 0 {
		query += ` AND NOT EXISTS (SELECT 1 FROM comp_tileentityowner foreign_owner
			JOIN comp_tilelayer foreign_tile ON foreign_tile.entity_id=foreign_owner.target_entity_id
			WHERE foreign_owner.entity_id=v.entity_id AND foreign_tile.map_id<>?)`
		args = append(args, mapID)
	}
	query += ` ORDER BY l.layer_order, l.draw_order, l.entity_id, CAST(target.key AS INTEGER)`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading tile visuals: %w", err)
	}
	defer rows.Close()
	var out []tiled.Draw
	for rows.Next() {
		var d tiled.Draw
		var fh, fv, fd int
		var px, py, layerAlpha float64
		var cellW, cellH int
		if err := rows.Scan(&d.Image, &d.SX, &d.SY, &d.SW, &d.SH,
			&d.DX, &d.DY, &fh, &fv, &fd, &d.Alpha,
			&px, &py, &cellW, &cellH, &layerAlpha); err != nil {
			return nil, fmt.Errorf("reading tile visuals: %w", err)
		}
		d.FlipH, d.FlipV, d.FlipD = fh != 0, fv != 0, fd != 0
		d.DX += int(px * float64(cellW))
		d.DY += int(py * float64(cellH))
		d.Alpha *= layerAlpha
		out = append(out, d)
	}
	return out, rows.Err()
}
