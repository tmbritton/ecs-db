package storage

import (
	"context"
	"database/sql"
	"fmt"
)

type mapTableQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// HasTileReferences reports whether the current database has the optional
// TileReferences component table; legacy projects may not declare it.
func HasTileReferences(ctx context.Context, db mapTableQuerier) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='comp_tilereferences'`).Scan(&count); err != nil {
		return false, fmt.Errorf("checking TileReferences table: %w", err)
	}
	return count != 0, nil
}

// ActiveMapEntityClause scopes an entity to the active map. A Tile reference
// gives a runtime entity map-local ownership; an unreferenced runtime entity
// remains global. The column is a trusted internal SQL identifier.
func ActiveMapEntityClause(entityColumn string, hasReferences bool) string {
	clause := fmt.Sprintf(`(EXISTS (SELECT 1 FROM comp_tilelayer tile WHERE tile.entity_id=%[1]s AND tile.map_id=?)
		OR EXISTS (SELECT 1 FROM comp_tileentityowner owner
			JOIN comp_tilelayer parent ON parent.entity_id=owner.target_entity_id
			WHERE owner.entity_id=%[1]s AND parent.map_id=?)`, entityColumn)
	if hasReferences {
		clause += fmt.Sprintf(` OR EXISTS (SELECT 1 FROM comp_tilelayer parent
			JOIN comp_tilereferences refs ON refs.entity_id=parent.entity_id
			JOIN json_each(refs.value) target WHERE CAST(target.value AS INTEGER)=%[1]s AND parent.map_id=?
			AND (NOT EXISTS (SELECT 1 FROM spawns foreign_spawn WHERE foreign_spawn.entity_id=%[1]s)
				OR EXISTS (SELECT 1 FROM spawns local_spawn WHERE local_spawn.entity_id=%[1]s AND local_spawn.map=?))
			AND NOT EXISTS (SELECT 1 FROM comp_tileentityowner foreign_owner
				JOIN comp_tilelayer foreign_tile ON foreign_tile.entity_id=foreign_owner.target_entity_id
				WHERE foreign_owner.entity_id=%[1]s AND foreign_tile.map_id<>?))`, entityColumn)
	}
	clause += fmt.Sprintf(` OR (NOT EXISTS (SELECT 1 FROM comp_tilelayer any_tile WHERE any_tile.entity_id=%[1]s)
		AND NOT EXISTS (SELECT 1 FROM comp_tileentityowner any_owner WHERE any_owner.entity_id=%[1]s)`, entityColumn)
	if hasReferences {
		clause += fmt.Sprintf(` AND (NOT EXISTS (SELECT 1 FROM comp_tilereferences any_refs
			JOIN json_each(any_refs.value) target WHERE CAST(target.value AS INTEGER)=%[1]s)
			OR EXISTS (SELECT 1 FROM spawns local_spawn WHERE local_spawn.entity_id=%[1]s AND local_spawn.map=?))`, entityColumn)
	}
	clause += fmt.Sprintf(` AND (NOT EXISTS (SELECT 1 FROM spawns other WHERE other.entity_id=%[1]s)
		OR EXISTS (SELECT 1 FROM spawns current WHERE current.entity_id=%[1]s AND current.map=?))))`, entityColumn)
	return clause
}

// ActiveMapEntityArgs matches the placeholders in ActiveMapEntityClause.
func ActiveMapEntityArgs(mapID string, hasReferences bool) []any {
	args := []any{mapID, mapID}
	if hasReferences {
		args = append(args, mapID, mapID, mapID, mapID)
	}
	return append(args, mapID)
}

// FindEntityByTypeInMap prefers the map's authored spawn, owned art and Tile
// references before falling back to an unowned, unlinked runtime entity.
// The caller decides how to report sql.ErrNoRows.
func FindEntityByTypeInMap(ctx context.Context, db mapTableQuerier, entityType, mapID string) (int64, error) {
	type lookup struct {
		statement string
		args      []any
	}
	queries := []lookup{
		{`SELECT e.id FROM entities e JOIN spawns s ON s.entity_id=e.id
			WHERE e.entity_type=? AND s.map=? ORDER BY e.id LIMIT 1`, []any{entityType, mapID}},
		{`SELECT e.id FROM entities e JOIN comp_tileentityowner owner ON owner.entity_id=e.id
			JOIN comp_tilelayer tile ON tile.entity_id=owner.target_entity_id
			WHERE e.entity_type=? AND tile.map_id=? ORDER BY e.id LIMIT 1`, []any{entityType, mapID}},
	}
	hasReferences, err := HasTileReferences(ctx, db)
	if err != nil {
		return 0, err
	}
	if hasReferences {
		queries = append(queries, lookup{`SELECT e.id FROM entities e JOIN comp_tilelayer parent ON parent.map_id=?
			JOIN comp_tilereferences refs ON refs.entity_id=parent.entity_id
			JOIN json_each(refs.value) target ON CAST(target.value AS INTEGER)=e.id
			WHERE e.entity_type=?
			AND (NOT EXISTS (SELECT 1 FROM spawns other WHERE other.entity_id=e.id)
				OR EXISTS (SELECT 1 FROM spawns local WHERE local.entity_id=e.id AND local.map=?))
			AND NOT EXISTS (SELECT 1 FROM comp_tileentityowner owner
				JOIN comp_tilelayer owned ON owned.entity_id=owner.target_entity_id
				WHERE owner.entity_id=e.id AND owned.map_id<>?)
			ORDER BY e.id LIMIT 1`, []any{mapID, entityType, mapID, mapID}})
	}
	fallback := `SELECT e.id FROM entities e WHERE e.entity_type=?
		AND NOT EXISTS (SELECT 1 FROM spawns s WHERE s.entity_id=e.id)
		AND NOT EXISTS (SELECT 1 FROM comp_tileentityowner owner WHERE owner.entity_id=e.id)
		AND NOT EXISTS (SELECT 1 FROM comp_tilelayer tile WHERE tile.entity_id=e.id)`
	if hasReferences {
		fallback += ` AND NOT EXISTS (SELECT 1 FROM comp_tilereferences refs
			JOIN json_each(refs.value) target WHERE CAST(target.value AS INTEGER)=e.id)`
	}
	fallback += ` ORDER BY e.id LIMIT 1`
	queries = append(queries, lookup{fallback, []any{entityType}})
	for _, query := range queries {
		var id int64
		err := db.QueryRowContext(ctx, query.statement, query.args...).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}
	return 0, sql.ErrNoRows
}
