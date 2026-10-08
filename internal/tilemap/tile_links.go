package tilemap

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

// tileLink is one object-layer entity's authored connection to one or more
// placed Tiles. Several links can target the same Tile; one River can supply
// many Tiles without being cloned.
type tileLink struct {
	objectID       int
	layerID        int
	entityType     string
	cells          []Point
	providesVisual bool
}

func tileLinkValue(props tiled.Properties, wanted string) (string, bool, error) {
	var value string
	found := false
	for name, prop := range props {
		if !strings.EqualFold(name, wanted) {
			continue
		}
		if found {
			return "", false, fmt.Errorf("duplicate %s properties with different casing", wanted)
		}
		value, found = prop.Value, true
	}
	return value, found, nil
}

func tileLinksOfMap(m *tiled.Map) ([]tileLink, error) {
	var links []tileLink
	layers := make(map[int]tiled.Layer, len(m.Layers))
	for _, layer := range m.Layers {
		layers[layer.ID] = layer
	}
	objectIDs := make(map[int]int)
	for _, group := range m.ObjectGroups {
		for _, obj := range group.Objects {
			objectIDs[obj.ID]++
		}
	}
	seenIDs := make(map[int]bool)
	for _, group := range m.ObjectGroups {
		for _, obj := range group.Objects {
			raw, layerPresent, err := tileLinkValue(obj.Properties, "TileLink.layerID")
			if err != nil {
				return nil, fmt.Errorf("object %d: %w", obj.ID, err)
			}
			cellsRaw, cellsPresent, err := tileLinkValue(obj.Properties, "TileLink.cells")
			if err != nil {
				return nil, fmt.Errorf("object %d: %w", obj.ID, err)
			}
			if !layerPresent && !cellsPresent {
				continue
			}
			if seenIDs[obj.ID] || obj.ID <= 0 || objectIDs[obj.ID] != 1 {
				return nil, fmt.Errorf("TileLink object ID %d is missing or duplicated", obj.ID)
			}
			seenIDs[obj.ID] = true
			layerID, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil || layerID <= 0 {
				return nil, fmt.Errorf("object %d TileLink.layerID %q is not a positive Tiled layer ID", obj.ID, raw)
			}
			layer, found := layers[layerID]
			if !found {
				return nil, fmt.Errorf("object %d TileLink.layerID %d names no tile layer", obj.ID, layerID)
			}
			x, y, err := spawnCell(m, obj)
			if err != nil {
				return nil, fmt.Errorf("object %d TileLink anchor: %w", obj.ID, err)
			}
			offsets := []Point{{}}
			if cellsPresent {
				offsets, err = ParseOccupiedCells(cellsRaw, Point{X: x, Y: y}, m.Width, m.Height)
				if err != nil {
					return nil, fmt.Errorf("object %d TileLink.cells: %w", obj.ID, err)
				}
			}
			link := tileLink{objectID: obj.ID, layerID: layerID, entityType: obj.Type}
			for name := range obj.Properties {
				component, _, _ := strings.Cut(name, ".")
				if strings.EqualFold(component, "TileVisual") {
					link.providesVisual = true
				}
			}
			for _, offset := range offsets {
				at := Point{X: x + offset.X, Y: y + offset.Y}
				if layer.TileAt(at.X, at.Y).GID == 0 {
					return nil, fmt.Errorf("object %d TileLink targets empty layer %d cell (%d,%d)", obj.ID, layerID, at.X, at.Y)
				}
				link.cells = append(link.cells, at)
			}
			links = append(links, link)
		}
	}
	return links, nil
}

// syncTileReferences reapplies authored links after object spawns have their
// stable database IDs. File-owned links overwrite runtime edits on re-import.
// Owned visual entities remain the first reference on every Tile.
func syncTileReferences(ctx context.Context, svc *world.EntityService, db *sql.DB, mapID string, links []tileLink) error {
	rows, err := db.QueryContext(ctx, `SELECT l.entity_id, l.layer_id, t.x, t.y, refs.value, owner.entity_id
		FROM comp_tilelayer l JOIN comp_tile t ON t.entity_id=l.entity_id
		JOIN comp_tilereferences refs ON refs.entity_id=l.entity_id
		LEFT JOIN comp_tileentityowner owner ON owner.target_entity_id=l.entity_id
		WHERE l.map_id=? ORDER BY l.layer_order, l.draw_order`, mapID)
	if err != nil {
		return fmt.Errorf("reading placed Tile references: %w", err)
	}
	type tileRow struct {
		id      int64
		visual  int64
		held    string
		layerID int
		at      Point
	}
	placed := make(map[layerKey]tileRow)
	for rows.Next() {
		var row tileRow
		var owner sql.NullInt64
		if err := rows.Scan(&row.id, &row.layerID, &row.at.X, &row.at.Y, &row.held, &owner); err != nil {
			_ = rows.Close()
			return fmt.Errorf("reading placed Tile references: %w", err)
		}
		row.visual = owner.Int64 // zero means an authored shared entity owns the visual
		placed[layerKey{mapID: mapID, layerID: row.layerID, x: row.at.X, y: row.at.Y}] = row
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("reading placed Tile references: %w", err)
	}
	_ = rows.Close()
	spawns, err := db.QueryContext(ctx, `SELECT object_id, entity_id FROM spawns WHERE map=?`, mapID)
	if err != nil {
		return fmt.Errorf("reading linked spawns: %w", err)
	}
	objects := make(map[int]int64)
	for spawns.Next() {
		var objectID int
		var id int64
		if err := spawns.Scan(&objectID, &id); err != nil {
			_ = spawns.Close()
			return fmt.Errorf("reading linked spawn: %w", err)
		}
		objects[objectID] = id
	}
	if err := spawns.Err(); err != nil {
		_ = spawns.Close()
		return fmt.Errorf("reading linked spawns: %w", err)
	}
	_ = spawns.Close()
	wanted := make(map[layerKey][]int64)
	for key, row := range placed {
		wanted[key] = []int64{}
		if row.visual != 0 {
			wanted[key] = append(wanted[key], row.visual)
		}
	}
	for _, link := range links {
		id, found := objects[link.objectID]
		if !found {
			return fmt.Errorf("TileLink object %d has no spawned entity", link.objectID)
		}
		for _, at := range link.cells {
			key := layerKey{mapID: mapID, layerID: link.layerID, x: at.X, y: at.Y}
			wanted[key] = append(wanted[key], id)
		}
	}
	var changes []struct {
		id   int64
		refs []int64
	}
	for key, want := range wanted {
		row, found := placed[key]
		if !found {
			return fmt.Errorf("TileLink names unimported layer %d cell (%d,%d)", key.layerID, key.x, key.y)
		}
		if len(want) == 0 {
			return fmt.Errorf("tile %d at layer %d cell (%d,%d) has no referenced entity providing artwork", row.id, key.layerID, key.x, key.y)
		}
		visuals := 0
		for _, id := range want {
			var attached int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM comp_tilevisual WHERE entity_id=?`, id).Scan(&attached); err != nil {
				return fmt.Errorf("checking Tile %d referenced visual %d: %w", row.id, id, err)
			}
			visuals += attached
		}
		if visuals == 0 {
			return fmt.Errorf("tile %d at layer %d cell (%d,%d) references no entity with TileVisual", row.id, key.layerID, key.x, key.y)
		}
		held, err := ParseTileReferences(row.held)
		if err != nil {
			return fmt.Errorf("tile %d: %w", row.id, err)
		}
		if len(held) == len(want) {
			equal := true
			for i := range held {
				if held[i] != want[i] {
					equal = false
				}
			}
			if equal {
				continue
			}
		}
		changes = append(changes, struct {
			id   int64
			refs []int64
		}{row.id, want})
	}
	if len(changes) == 0 {
		return nil
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].id < changes[j].id })
	return svc.InTx(ctx, func(tx world.Tx) error {
		for _, change := range changes {
			if err := tx.SetComponentValues(ctx, change.id, "TileReferences", world.ComponentValues{"value": change.refs}); err != nil {
				return fmt.Errorf("linking Tile %d: %w", change.id, err)
			}
		}
		return nil
	})
}
