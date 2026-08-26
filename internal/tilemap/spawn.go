package tilemap

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/world"
)

// SpawnResult is what an import did.
type SpawnResult struct {
	Created int
	// Refused is one sentence per object that could not be spawned, in file
	// order. Refusals do not stop the import: a map with a typo in one goblin
	// should load the other nineteen and complain once, not load nothing.
	Refused []string
	// Warnings are entities that were created and should not have been.
	//
	// A type at validationLevel "warning" is created with whatever it was given,
	// so an object missing a required component makes a Goblin with no Health
	// and the machine reads a component that is not there. EntityService records
	// why in Warnings(), which is last-call-only — so it is read here, per
	// object, or it is lost. Not refusals: the schema said to allow it.
	Warnings []string
	// Skipped counts objects that are not spawns at all — see spawnable.
	Skipped int
}

// SyncSpawns creates entities for the objects on a map's object layers.
//
// **Create once. Never update, never delete.** An entity created from a spawn
// stops being the file's the moment the game runs: it walks away, takes damage,
// dies. The file says where a world *starts*, not what it currently is — so a
// spawn moved in the editor does not teleport a goblin that has wandered off,
// and a spawn deleted from the map does not kill one.
//
// That is deliberately not SyncTiles' rule, which is that the file wins. A tile
// *is* its cell and has no history to lose; an entity has nothing but history
// after its first tick. Re-placing spawned entities is a level reset, which is a
// different operation from re-importing a file and is not one anything asks for.
//
// mapPath identifies the map, because object ids are numbered per file and two
// maps may both hold an object 1.
//
// It is a path, and therefore a key that a human can re-spell. LoadMap cleans it
// so "mods/map/l.tmx" and "./mods/map/l.tmx" are one map, but renaming or moving
// the file is still a new map, and its objects spawn again beside the ones that
// are already there. Nothing deletes the old rows. That is the cost of the only
// stable handle Tiled offers, and it is worth knowing before Forge's MAP mode
// makes moving a map a normal thing to do.
func SyncSpawns(
	ctx context.Context,
	svc *world.EntityService,
	db *sql.DB,
	mapPath string,
	m *tiled.Map,
) (SpawnResult, error) {
	var res SpawnResult
	if m == nil {
		return res, nil
	}

	// Nothing to ask the database when the map has nothing to spawn. Also what
	// keeps LoadMap working against a store where the engine tables were never
	// created, which a Tiled map with only tile layers otherwise needed.
	if !hasObjects(m) {
		return res, nil
	}

	done, err := spawnedObjects(ctx, db, mapPath)
	if err != nil {
		return res, err
	}

	// Ids seen in this import, as distinct from ids already spawned in an
	// earlier one. A repeat within one file is a bad map and is said so;
	// treating it as "already done" would pass over it in the silence that a
	// duplicate id least deserves.
	seen := map[int]bool{}

	for _, group := range m.ObjectGroups {
		// Visibility is not consulted, for the reason the tile loader does not
		// consult it: hiding a layer is what the editor shows you, not what the
		// map holds. A designer who hides the spawn layer to see the floor and
		// then finds no monsters has been robbed by a checkbox.
		for _, obj := range group.Objects {
			if !spawnable(obj) {
				res.Skipped++
				continue
			}
			// Two objects claiming one id. Ids default to zero when Tiled's
			// attribute is absent, which a generated or hand-written file
			// produces easily, and the second used to collide on the primary
			// key — aborting the load having already created an entity that no
			// spawn row pointed at, which the *next* load duplicated and never
			// mentioned again. That is fixed by the transaction in createSpawn;
			// this is so the author hears about it.
			if seen[obj.ID] {
				res.Refused = append(res.Refused, describeRefusal(obj, m,
					fmt.Errorf("object id %d belongs to another object in this map", obj.ID)))
				continue
			}
			seen[obj.ID] = true

			if done[obj.ID] {
				continue
			}

			components, err := spawnComponents(svc.Schema(), m, obj)
			if err != nil {
				res.Refused = append(res.Refused, describeRefusal(obj, m, err))
				continue
			}
			warnings, err := createSpawn(ctx, svc, mapPath, obj, components)
			if err != nil {
				res.Refused = append(res.Refused, describeRefusal(obj, m, err))
				continue
			}
			for _, w := range warnings {
				res.Warnings = append(res.Warnings, describeWarning(obj, m, w))
			}
			res.Created++
		}
	}
	return res, nil
}

// createSpawn writes the entity and the record of it in one transaction.
//
// Both or neither. They were two statements, and anything between them — a
// failed insert, a killed process — left an entity that no spawn row pointed
// at, which every later load duplicated because the object still looked
// unspawned. One transaction per object rather than one for the import: a map
// with a typo in one goblin should still load the other nineteen, which is the
// opposite of what SyncTiles wants and the reason this is not that function.
func createSpawn(
	ctx context.Context,
	svc *world.EntityService,
	mapPath string,
	obj tiled.Object,
	components []world.EntityComponent,
) ([]string, error) {
	var warnings []string
	err := svc.InTx(ctx, func(tx world.Tx) error {
		e, err := svc.CreateEntityInTx(ctx, tx, obj.Type, components)
		if err != nil {
			return err
		}
		// Read immediately: Warnings() is the last call's, so anything not
		// taken here is overwritten by the next object.
		warnings = append(warnings, svc.Warnings()...)
		return tx.RecordSpawn(ctx, mapPath, obj.ID, e.ID)
	})
	if err != nil {
		return nil, err
	}
	return warnings, nil
}

// spawnable reports whether an object is meant to be an entity at all.
//
// An object layer is not only a spawn list. Region markers, camera bounds,
// collision shapes and notes are the ordinary contents of one, and Tiled's way
// of saying what a shape *is* is its class — so an object with no class is not
// a spawn, and is passed over in silence rather than refused once per start
// forever.
//
// This is the rule Forge's MAP mode inherits: to place a spawn, give the object
// a class naming an entity type; to place anything else, leave the class empty.
func spawnable(obj tiled.Object) bool { return obj.Type != "" }

func hasObjects(m *tiled.Map) bool {
	for _, group := range m.ObjectGroups {
		if len(group.Objects) > 0 {
			return true
		}
	}
	return false
}

// spawnedObjects is the ids this map has already spawned.
func spawnedObjects(ctx context.Context, db *sql.DB, mapPath string) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT object_id FROM spawns WHERE map = ?`, mapPath)
	if err != nil {
		return nil, fmt.Errorf("tilemap: reading spawns for %q: %w", mapPath, err)
	}
	defer func() { _ = rows.Close() }()
	done := map[int]bool{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("tilemap: scanning a spawn row: %w", err)
		}
		done[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tilemap: reading spawns for %q: %w", mapPath, err)
	}
	return done, nil
}

// spawnComponents turns an object into the components an entity is made of.
//
// Position comes from the object's own coordinates. Everything else is a custom
// property named "Component.property", which is how a map says what an entity
// is made of without the engine inventing values for it.
func spawnComponents(s *schema.DatabaseSchema, m *tiled.Map, obj tiled.Object) ([]world.EntityComponent, error) {
	x, y, err := spawnCell(m, obj)
	if err != nil {
		return nil, err
	}
	// Position comes from where the object sits, and a spawnable type has to
	// declare it: an object layer places things, and a thing with no place is
	// not something this can put on a map.
	values := map[string]world.ComponentValues{
		"Position": {"x": x, "y": y},
	}

	// Sorted, so an object with two bad properties is refused for the same one
	// every run.
	names := make([]string, 0, len(obj.Properties))
	for name := range obj.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		comp, prop, ok := strings.Cut(name, ".")
		if !ok {
			// Refused rather than ignored. An author who writes "hp" meaning
			// "Health.hp" would otherwise get silence, and a spawn that
			// silently drops half of what the map said about it is the failure
			// this whole epic keeps going back to fix. Tiled has map-level and
			// layer-level properties for anything that is editor metadata.
			return nil, fmt.Errorf("property %q names no component; write it as Component.%s", name, name)
		}
		declared, canonical := schema.ComponentByName(s, comp)
		if canonical == "" {
			return nil, fmt.Errorf("property %q names component %q, which the schema does not declare", name, comp)
		}
		if strings.EqualFold(canonical, "Position") {
			// Refused rather than allowed to win. The object's own coordinates
			// are where the map *shows* the spawn, so a property that quietly
			// overruled them would put the goblin somewhere the author cannot
			// see — and there is no reason to write one, because moving the
			// object is the gesture that moves the spawn.
			return nil, fmt.Errorf(
				"property %q sets Position, which comes from where the object sits; move the object instead", name)
		}
		p, canonicalProp := schema.PropertyByName(declared.Properties, prop)
		if canonicalProp == "" {
			return nil, fmt.Errorf("property %q names %q, which component %q does not declare", name, prop, canonical)
		}
		v, err := propertyValue(p.Type, obj.Properties.Get(name))
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", name, err)
		}
		if values[canonical] == nil {
			values[canonical] = world.ComponentValues{}
		}
		// Keyed by the name the schema declares, not by what the map wrote.
		// The insert path reads its values by declared name, so writing back
		// "maxhp" for a property called maxHp produced a row with no value for
		// it and a NOT NULL failure naming a column nobody had typed.
		values[canonical][canonicalProp] = v
	}

	out := make([]world.EntityComponent, 0, len(values))
	for _, name := range sortedNames(values) {
		out = append(out, world.EntityComponent{Name: name, Values: values[name]})
	}
	return out, nil
}

func sortedNames(m map[string]world.ComponentValues) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// spawnCell is the cell an object sits in.
//
// Tiled writes object positions in pixels, and the corner they are measured
// from depends on the shape: a **tile** object is placed by its bottom-left
// corner and everything else by its top-left. Forgetting that puts every tile
// spawn one row below where the map shows it, which reads as an off-by-one in
// the map rather than in the reader.
func spawnCell(m *tiled.Map, obj tiled.Object) (int, int, error) {
	if m.TileWidth <= 0 || m.TileHeight <= 0 {
		return 0, 0, fmt.Errorf("the map's tiles are %dx%d pixels", m.TileWidth, m.TileHeight)
	}
	px, py := int(math.Floor(obj.X)), int(math.Floor(obj.Y))
	if obj.GID != 0 {
		// A tile object's Y is the *bottom* edge of the tile, so the row it
		// occupies is the one that edge closes. Subtracting a pixel first
		// rather than a whole row after: dividing and then decrementing is only
		// right when the object is grid-aligned, and an author who turned
		// snapping off is not exotic.
		py--
	}
	x, y := floorDiv(px, m.TileWidth), floorDiv(py, m.TileHeight)

	// Off the map is refused rather than stored. Truncation used to put an
	// object nudged just off the left edge *inside* the map at column 0, and
	// nothing checked the other three edges at all — a spawn at (250,-2) on a
	// 20x20 map was written without comment, where the grid cannot find it and
	// pathfinding cannot reach it.
	if m.Width > 0 && m.Height > 0 && (x < 0 || y < 0 || x >= m.Width || y >= m.Height) {
		return x, y, fmt.Errorf("cell %d,%d is outside the map, which is %dx%d", x, y, m.Width, m.Height)
	}
	return x, y, nil
}

// floorDiv rounds towards negative infinity, which Go's / does not: an object
// eight pixels off the left edge of a sixteen-pixel grid is in column -1, and
// truncation calls it column 0 — inside the map, at the wrong end.
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// propertyValue reads a Tiled property as the type the *schema* says the column
// holds.
//
// The schema decides, not Tiled: they are different vocabularies, and the column
// is the thing that has to accept the value. Reading it here rather than passing
// the string down means a number somebody typed as "plenty" is refused against
// the property that could not hold it, instead of arriving as a database type
// error naming a column.
func propertyValue(propType, raw string) (any, error) {
	switch propType {
	case schema.PropertyTypeInteger, schema.PropertyTypeEntityRef:
		text := strings.TrimSpace(raw)
		if strings.ContainsRune(text, '_') {
			return nil, fmt.Errorf("%q is not a whole number", raw)
		}
		v, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a whole number", raw)
		}
		return v, nil
	case schema.PropertyTypeNumber:
		text := strings.TrimSpace(raw)
		// ParseFloat and ParseInt both take Go's literal underscores, so
		// "1_000" becomes 1000 — a spelling no Tiled property has, and one that
		// turns a typo into a number three orders of magnitude out.
		if strings.ContainsRune(text, '_') {
			return nil, fmt.Errorf("%q is not a number", raw)
		}
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", raw)
		}
		// ParseFloat takes "NaN" and "Inf". A REAL column holds them and
		// nothing downstream expects one — a position of NaN is a sprite that
		// never draws and a path that never solves.
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("%q is not a finite number", raw)
		}
		return v, nil
	case schema.PropertyTypeBoolean:
		// "true" and "false" and nothing else, which is what Tiled writes in
		// both serialisations. ParseBool also takes "1", "t" and five other
		// spellings the format never produces — tiled.Properties.Bool makes
		// exactly this decision and says accepting them would be inventing a
		// format. Two readers of one file should not disagree about "1".
		switch strings.TrimSpace(raw) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not true or false", raw)
	case schema.PropertyTypeString:
		// Not trimmed, unlike every case above. A string's spaces are its own:
		// an animation named " idle" is a mistake worth seeing rather than one
		// worth silently correcting, and the numeric types trim only because
		// no number is spelled with a space.
		return raw, nil
	default:
		// object and array live in a JSON column. Checked here rather than
		// passed on: the insert path stores whatever it is given, so an earlier
		// comment claiming it would "accept or refuse" this was describing a
		// safety net that does not exist, and "this is not json at all" went
		// into the column verbatim.
		if !json.Valid([]byte(raw)) {
			return nil, fmt.Errorf("%q is not JSON, and %s is stored as JSON", raw, propType)
		}
		return raw, nil
	}
}

// describeWarning is describeRefusal for something that was created anyway.
func describeWarning(obj tiled.Object, m *tiled.Map, warning string) string {
	return describeRefusal(obj, m, errors.New(warning))
}

// describeRefusal says which object was refused and where to find it. An id
// alone is not findable in a room full of goblins; the cell is.
func describeRefusal(obj tiled.Object, m *tiled.Map, err error) string {
	where := "somewhere"
	if x, y, cellErr := spawnCell(m, obj); cellErr == nil {
		where = fmt.Sprintf("%d,%d", x, y)
	}
	name := obj.Name
	if name == "" {
		name = "unnamed"
	}
	return fmt.Sprintf("object %d (%q, type %q) at %s: %v", obj.ID, name, obj.Type, where, err)
}
