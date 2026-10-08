package tilemap

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// SpaceQuerier is the read port shared by a game database and the current tick
// transaction. A transaction snapshot includes its own uncommitted movement.
type SpaceQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// ParseTileReferences reads a Tile's ordered collection of entity references.
// Entity existence is checked against the active spatial snapshot afterward.
func ParseTileReferences(raw string) ([]int64, error) {
	if !strings.HasPrefix(strings.TrimSpace(raw), "[") {
		return nil, fmt.Errorf("TileReferences must be a JSON array of positive entity IDs")
	}
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, fmt.Errorf("TileReferences needs integer entity IDs in a JSON array: %w", err)
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("TileReferences requires positive entity IDs, got %d", id)
		}
		if seen[id] {
			return nil, fmt.Errorf("TileReferences has duplicate entity ID %d", id)
		}
		seen[id] = true
	}
	return ids, nil
}

// ReadSpace constructs a fresh view from component rows. The caller controls
// when it takes a snapshot; a wall moved since startup must not remain in its
// old cells for player input, paths or sight.
func ReadSpace(ctx context.Context, db SpaceQuerier, rules schema.DatabaseSchema, width, height int, mapIDs ...string) (Space, error) {
	s := Space{Width: width, Height: height, Schema: rules}
	fields := []string{"e.id", "p.x", "p.y"}
	joins := ""
	for _, optional := range []struct{ component, alias, column string }{
		{"Passability", "pass", "kind"},
		{"Visibility", "sight", "kind"},
		{"OccupiedCells", "footprint", "value"},
	} {
		if _, declared := rules.Components[optional.component]; declared {
			fields = append(fields, optional.alias+"."+optional.column)
			joins += fmt.Sprintf(" LEFT JOIN comp_%s %s ON %s.entity_id = e.id", strings.ToLower(optional.component), optional.alias, optional.alias)
		} else {
			fields = append(fields, "NULL")
		}
	}
	if _, declared := rules.Components["TileLayer"]; declared {
		fields = append(fields, "tiles.entity_id")
		joins += " LEFT JOIN comp_tilelayer tiles ON tiles.entity_id = e.id"
	} else {
		fields = append(fields, "NULL")
	}
	if _, declared := rules.Components["TileReferences"]; declared {
		fields = append(fields, "refs.value")
		joins += " LEFT JOIN comp_tilereferences refs ON refs.entity_id = e.id"
	} else {
		fields = append(fields, "NULL")
	}
	query := "SELECT " + strings.Join(fields, ", ") +
		" FROM entities e LEFT JOIN comp_position p ON p.entity_id = e.id" + joins
	var args []any
	if len(mapIDs) > 0 && mapIDs[0] != "" {
		// Tile instances and object-layer spawns are authored for one map.
		// Runtime entities without either owner remain visible everywhere.
		if _, declared := rules.Components["TileLayer"]; declared {
			currentReferences, anyReferences := "", ""
			ownerScope := ""
			if _, owned := rules.Components["TileEntityOwner"]; owned {
				ownerScope = ` AND NOT EXISTS (SELECT 1 FROM comp_tileentityowner foreign_owner
					JOIN comp_tilelayer foreign_tile ON foreign_tile.entity_id=foreign_owner.target_entity_id
					WHERE foreign_owner.entity_id=e.id AND foreign_tile.map_id<>?)`
			}
			if _, linked := rules.Components["TileReferences"]; linked {
				currentReferences = ` OR EXISTS (SELECT 1 FROM comp_tilelayer parent
					JOIN comp_tilereferences refs ON refs.entity_id=parent.entity_id
					JOIN json_each(refs.value) target
					WHERE CAST(target.value AS INTEGER)=e.id AND parent.map_id=?
					AND (NOT EXISTS (SELECT 1 FROM spawns foreign_spawn WHERE foreign_spawn.entity_id=e.id)
					OR EXISTS (SELECT 1 FROM spawns local_spawn WHERE local_spawn.entity_id=e.id AND local_spawn.map=?))` + ownerScope + `)`
				anyReferences = ` AND NOT EXISTS (SELECT 1 FROM comp_tilereferences refs
					JOIN comp_tilelayer linked_tile ON linked_tile.entity_id=refs.entity_id
					JOIN json_each(refs.value) target WHERE CAST(target.value AS INTEGER)=e.id AND linked_tile.map_id=?)
					AND (NOT EXISTS (SELECT 1 FROM comp_tilereferences any_refs
						JOIN json_each(any_refs.value) any_target WHERE CAST(any_target.value AS INTEGER)=e.id)
						OR EXISTS (SELECT 1 FROM spawns local_spawn WHERE local_spawn.entity_id=e.id AND local_spawn.map=?))`
			}
			if _, owned := rules.Components["TileEntityOwner"]; owned {
				query += ` WHERE (EXISTS (SELECT 1 FROM comp_tilelayer own_tile WHERE own_tile.entity_id=e.id AND own_tile.map_id=?)
					OR EXISTS (SELECT 1 FROM comp_tileentityowner owner
						JOIN comp_tilelayer parent ON parent.entity_id=owner.target_entity_id
						WHERE owner.entity_id=e.id AND parent.map_id=?)` + currentReferences + `
					OR (NOT EXISTS (SELECT 1 FROM comp_tilelayer any_tile WHERE any_tile.entity_id=e.id)
						AND NOT EXISTS (SELECT 1 FROM comp_tileentityowner any_owner WHERE any_owner.entity_id=e.id)` + anyReferences + `
						AND (NOT EXISTS (SELECT 1 FROM spawns other WHERE other.entity_id=e.id)
						OR EXISTS (SELECT 1 FROM spawns current WHERE current.entity_id=e.id AND current.map=?))))`
				args = append(args, mapIDs[0], mapIDs[0])
			} else {
				query += ` WHERE (EXISTS (SELECT 1 FROM comp_tilelayer own_tile WHERE own_tile.entity_id=e.id AND own_tile.map_id=?)
					` + currentReferences + `
					OR (NOT EXISTS (SELECT 1 FROM comp_tilelayer any_tile WHERE any_tile.entity_id=e.id)
					` + anyReferences + `
					AND (NOT EXISTS (SELECT 1 FROM spawns other WHERE other.entity_id=e.id)
					OR EXISTS (SELECT 1 FROM spawns current WHERE current.entity_id=e.id AND current.map=?))))`
				args = append(args, mapIDs[0])
			}
			if currentReferences != "" {
				args = append(args, mapIDs[0], mapIDs[0])
				if ownerScope != "" {
					args = append(args, mapIDs[0])
				}
			}
			if anyReferences != "" {
				args = append(args, mapIDs[0], mapIDs[0])
			}
			args = append(args, mapIDs[0])
		} else {
			query += ` WHERE (NOT EXISTS (SELECT 1 FROM spawns other WHERE other.entity_id=e.id)
				OR EXISTS (SELECT 1 FROM spawns current WHERE current.entity_id=e.id AND current.map=?))`
			args = append(args, mapIDs[0])
		}
	}
	query += " ORDER BY e.id"
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return Space{}, fmt.Errorf("reading grid occupants: %w", err)
	}
	rawFootprints := make(map[int64]string)
	for rows.Next() {
		var entity SpatialEntity
		var x, y sql.NullFloat64
		var pass, sight, footprint, references sql.NullString
		var tileID sql.NullInt64
		if err := rows.Scan(&entity.ID, &x, &y, &pass, &sight, &footprint, &tileID, &references); err != nil {
			_ = rows.Close()
			return Space{}, fmt.Errorf("reading grid occupant: %w", err)
		}
		if x.Valid != y.Valid {
			_ = rows.Close()
			return Space{}, fmt.Errorf("occupant %d has incomplete Position", entity.ID)
		}
		if x.Valid && (math.IsNaN(x.Float64) || math.IsNaN(y.Float64) || math.IsInf(x.Float64, 0) || math.IsInf(y.Float64, 0)) {
			_ = rows.Close()
			return Space{}, fmt.Errorf("occupant %d has non-finite Position", entity.ID)
		}
		entity.HasPosition = x.Valid
		if x.Valid {
			entity.Position = Point{X: int(math.Floor(x.Float64)), Y: int(math.Floor(y.Float64))}
		}
		entity.IsTile = tileID.Valid
		entity.Passability, entity.Visibility = pass.String, sight.String
		entity.HasPassability, entity.HasVisibility = pass.Valid, sight.Valid
		if footprint.Valid {
			rawFootprints[entity.ID] = footprint.String
		}
		if references.Valid {
			if !entity.IsTile {
				_ = rows.Close()
				return Space{}, fmt.Errorf("entity %d has TileReferences without a TileLayer", entity.ID)
			}
			entity.References, err = ParseTileReferences(references.String)
			if err != nil {
				_ = rows.Close()
				return Space{}, fmt.Errorf("tile %d: %w", entity.ID, err)
			}
		}
		entity.Capabilities = map[string]bool{}
		s.Entities = append(s.Entities, entity)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return Space{}, fmt.Errorf("reading grid occupants: %w", err)
	}
	if err := rows.Close(); err != nil {
		return Space{}, fmt.Errorf("closing grid occupants: %w", err)
	}
	byID := make(map[int64]int, len(s.Entities))
	for i, entity := range s.Entities {
		byID[entity.ID] = i
	}
	linked := make(map[int64]bool)
	for _, tile := range s.Entities {
		if !tile.IsTile {
			continue
		}
		if !tile.HasPosition {
			return Space{}, fmt.Errorf("tile %d has no Position", tile.ID)
		}
		for _, target := range tile.References {
			if _, found := byID[target]; !found {
				return Space{}, fmt.Errorf("tile %d references missing entity %d in the active map", tile.ID, target)
			}
			linked[target] = true
		}
	}
	for i, entity := range s.Entities {
		if raw, declared := rawFootprints[entity.ID]; declared && !linked[entity.ID] {
			if !entity.HasPosition {
				return Space{}, fmt.Errorf("unreferenced occupant %d has OccupiedCells without Position", entity.ID)
			}
			cells, err := ParseOccupiedCells(raw, entity.Position, width, height)
			if err != nil {
				return Space{}, fmt.Errorf("occupant %d: %w", entity.ID, err)
			}
			s.Entities[i].Cells = cells
		}
	}
	capabilities := make(map[string]bool)
	for name, component := range rules.Components {
		if component.Type == schema.ComponentTypeBoolean {
			capabilities[name] = true
		}
	}
	for _, categories := range rules.Interactions {
		for _, rule := range categories {
			for _, name := range rule.Allows {
				capabilities[name] = true
			}
		}
	}
	names := make([]string, 0, len(capabilities))
	for name := range capabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !schema.ValidIdentifier(name) {
			return Space{}, fmt.Errorf("invalid capability component name %q", name)
		}
		query := fmt.Sprintf(`SELECT entity_id, value FROM %q`, "comp_"+strings.ToLower(name))
		abilityRows, err := db.QueryContext(ctx, query)
		if err != nil {
			return Space{}, fmt.Errorf("reading %s capabilities: %w", name, err)
		}
		for abilityRows.Next() {
			var id int64
			var active bool
			if err := abilityRows.Scan(&id, &active); err != nil {
				_ = abilityRows.Close()
				return Space{}, fmt.Errorf("reading %s capabilities: %w", name, err)
			}
			if i, found := byID[id]; found {
				s.Entities[i].Capabilities[name] = active
			}
		}
		if err := abilityRows.Err(); err != nil {
			_ = abilityRows.Close()
			return Space{}, fmt.Errorf("reading %s capabilities: %w", name, err)
		}
		if err := abilityRows.Close(); err != nil {
			return Space{}, fmt.Errorf("closing %s capabilities: %w", name, err)
		}
	}
	s.buildIndex()
	return s, nil
}

// ParseOccupiedCells validates a declared footprint and returns offsets from
// the entity's Position. An absent component is handled by SpatialEntity's
// anchor default; an explicitly empty or malformed component is refused.
func ParseOccupiedCells(raw string, anchor Point, width, height int) ([]Point, error) {
	var data []struct {
		X *int `json:"x"`
		Y *int `json:"y"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("OccupiedCells must be JSON offsets with integer x and y: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("OccupiedCells has trailing JSON data")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("OccupiedCells cannot be empty")
	}
	seen := make(map[Point]bool, len(data))
	out := make([]Point, 0, len(data))
	for _, offset := range data {
		if offset.X == nil || offset.Y == nil {
			return nil, fmt.Errorf("OccupiedCells offset needs integer x and y")
		}
		at := Point{X: *offset.X, Y: *offset.Y}
		if seen[at] {
			return nil, fmt.Errorf("OccupiedCells has duplicate offset (%d,%d)", at.X, at.Y)
		}
		seen[at] = true
		cell := Point{X: anchor.X + at.X, Y: anchor.Y + at.Y}
		if cell.X < 0 || cell.Y < 0 || cell.X >= width || cell.Y >= height {
			return nil, fmt.Errorf("OccupiedCells offset (%d,%d) places cell (%d,%d) outside %dx%d map", at.X, at.Y, cell.X, cell.Y, width, height)
		}
		out = append(out, at)
	}
	return out, nil
}

// SpatialEntity is an entity's current grid footprint and the components
// relevant to movement and sight. Capabilities are named by the schema, never
// by switches in the engine. Nil Cells means the single Position cell.
type SpatialEntity struct {
	ID             int64
	Position       Point
	HasPosition    bool
	IsTile         bool
	References     []int64
	Cells          []Point
	Passability    string
	Visibility     string
	HasPassability bool
	HasVisibility  bool
	Capabilities   map[string]bool
}

func (e SpatialEntity) offsets() []Point {
	if e.Cells == nil {
		return []Point{{}}
	}
	return e.Cells
}

func (e SpatialEntity) occupies(at Point) bool {
	for _, offset := range e.offsets() {
		if e.Position.X+offset.X == at.X && e.Position.Y+offset.Y == at.Y {
			return true
		}
	}
	return false
}

func (s Space) occupiesEntityAt(entity SpatialEntity, at Point) bool {
	// When Tiles reference an entity, those Tile Positions own its placement;
	// its own Position/OccupiedCells cannot add a phantom second footprint.
	linked := false
	for _, tile := range s.Entities {
		if !tile.IsTile {
			continue
		}
		for _, id := range tile.References {
			if id == entity.ID {
				linked = true
				if tile.HasPosition && tile.Position == at {
					return true
				}
			}
		}
	}
	if linked {
		return false
	}
	if !entity.HasPosition && entity.Position == (Point{}) {
		return false
	}
	return entity.occupies(at)
}

func (s Space) checkReferencesAt(at Point) error {
	if s.index != nil {
		// ReadSpace validated every reference before indexing. Pure Space
		// values with no index validate lazily at the queried cell.
		return nil
	}
	for _, tile := range s.Entities {
		if !tile.IsTile || !tile.HasPosition || tile.Position != at {
			continue
		}
		seen := make(map[int64]bool, len(tile.References))
		for _, id := range tile.References {
			if seen[id] {
				return fmt.Errorf("tile %d at (%d,%d) references entity %d twice", tile.ID, at.X, at.Y, id)
			}
			seen[id] = true
			if _, found := s.Entity(id); !found {
				return fmt.Errorf("tile %d at (%d,%d) references missing entity %d", tile.ID, at.X, at.Y, id)
			}
		}
	}
	return nil
}

// Space is a current snapshot of grid occupants. Build one from live state
// before each movement decision or path search; it never uses Tile artwork.
type Space struct {
	Width, Height int
	Schema        schema.DatabaseSchema
	Entities      []SpatialEntity
	index         map[Point][]SpatialEntity
}

func (s *Space) buildIndex() {
	s.index = make(map[Point][]SpatialEntity)
	byID := make(map[int64]SpatialEntity, len(s.Entities))
	for _, entity := range s.Entities {
		byID[entity.ID] = entity
	}
	linked := make(map[int64]bool)
	seen := make(map[Point]map[int64]bool)
	add := func(at Point, entity SpatialEntity) {
		if !s.inBounds(at) {
			return
		}
		if seen[at] == nil {
			seen[at] = make(map[int64]bool)
		}
		if seen[at][entity.ID] {
			return
		}
		seen[at][entity.ID] = true
		s.index[at] = append(s.index[at], entity)
	}
	for _, tile := range s.Entities {
		if !tile.IsTile || !tile.HasPosition {
			continue
		}
		add(tile.Position, tile)
		for _, id := range tile.References {
			linked[id] = true
			add(tile.Position, byID[id])
		}
	}
	for _, entity := range s.Entities {
		if linked[entity.ID] || entity.IsTile || !entity.HasPosition {
			continue
		}
		for _, offset := range entity.offsets() {
			add(Point{X: entity.Position.X + offset.X, Y: entity.Position.Y + offset.Y}, entity)
		}
	}
}

func (s Space) occupantsAt(at Point) []SpatialEntity {
	if s.index != nil {
		return s.index[at]
	}
	var out []SpatialEntity
	for _, entity := range s.Entities {
		if s.occupiesEntityAt(entity, at) {
			out = append(out, entity)
		}
	}
	return out
}

// Entity returns one occupant by identity, including its current capabilities
// and footprint, for movement and sight callers.
func (s Space) Entity(id int64) (SpatialEntity, bool) {
	for _, entity := range s.Entities {
		if entity.ID == id {
			return entity, true
		}
	}
	return SpatialEntity{}, false
}

func (s Space) inBounds(at Point) bool {
	return at.X >= 0 && at.Y >= 0 && at.X < s.Width && at.Y < s.Height
}

// CanEnter checks every destination cell of the mover against every occupant.
// One denying occupant vetoes movement, even if another allows it.
func (s Space) CanEnter(mover SpatialEntity, destination Point) (bool, error) {
	if len(mover.Cells) == 0 && mover.Cells != nil {
		return false, fmt.Errorf("mover %d has an empty OccupiedCells footprint", mover.ID)
	}
	for _, offset := range mover.offsets() {
		at := Point{X: destination.X + offset.X, Y: destination.Y + offset.Y}
		if !s.inBounds(at) {
			return false, nil
		}
		if err := s.checkReferencesAt(at); err != nil {
			return false, err
		}
		for _, occupant := range s.occupantsAt(at) {
			if occupant.ID == mover.ID || (!occupant.HasPassability && occupant.Passability == "") {
				continue
			}
			allowed, err := s.Schema.AllowsInteraction("Passability", occupant.Passability, mover.Capabilities)
			if err != nil {
				return false, fmt.Errorf("occupant %d at (%d,%d): %w", occupant.ID, at.X, at.Y, err)
			}
			if !allowed {
				return false, nil
			}
		}
	}
	return true, nil
}

// CanSee traces the sight ray through occupants' Visibility categories. An
// opaque target is visible at the end of the ray; a different occupant at the
// same endpoint can still obscure it. Movement abilities have no special case.
func (s Space) CanSee(observer SpatialEntity, end Point, targetID int64) (bool, error) {
	start := observer.Position
	if !s.inBounds(start) || !s.inBounds(end) {
		return false, nil
	}
	if start == end {
		return true, nil
	}
	x, y := start.X, start.Y
	dx, dy := iabs(end.X-x), iabs(end.Y-y)
	sx, sy := isign(end.X-x), isign(end.Y-y)
	err := dx - dy
	for {
		if x != start.X || y != start.Y {
			at := Point{X: x, Y: y}
			if err := s.checkReferencesAt(at); err != nil {
				return false, err
			}
			for _, occupant := range s.occupantsAt(at) {
				if occupant.ID == observer.ID || (at == end && occupant.ID == targetID) ||
					(!occupant.HasVisibility && occupant.Visibility == "") {
					continue
				}
				clear, interactionErr := s.Schema.AllowsInteraction("Visibility", occupant.Visibility, observer.Capabilities)
				if interactionErr != nil {
					return false, fmt.Errorf("occupant %d at (%d,%d): %w", occupant.ID, x, y, interactionErr)
				}
				if !clear {
					return false, nil
				}
			}
		}
		if x == end.X && y == end.Y {
			return true, nil
		}
		doubled := 2 * err
		if doubled > -dy {
			err -= dy
			x += sx
		}
		if doubled < dx {
			err += dx
			y += sy
		}
	}
}
