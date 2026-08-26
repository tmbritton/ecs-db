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
	// Updated counts entities the file described differently from the database.
	Updated int
	// Deleted counts entities whose object is no longer in the map.
	Deleted int
	// Unchanged counts objects the database already agreed with, on the same
	// terms as SyncTiles' Result: a load with no edit to the map writes nothing.
	Unchanged int
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

// SyncSpawns brings the entities a map's object layers describe in line with
// what the file says.
//
// **The file wins for what the file describes**, which is SyncTiles' rule and is
// now this one too. An object's position and every Component.property it
// declares are re-applied on every load, so an author who drags a goblin in the
// editor, saves and runs finds it where they put it — and one who deletes it
// finds it gone.
//
// This story reversed the previous rule, which was create-once-never-touch. That
// was argued as "an entity stops being the file's the moment the game runs",
// which is the same sentence SyncTiles had already rejected with "corridor" in
// place of "goblin": letting the database win means an author's edit does
// nothing, which is the failure re-import exists to end.
//
// **The entity id survives.** behavior_components and transitions name it and a
// machine's running state hangs off it, so a changed spawn is an update and
// never a delete and an insert.
//
// **What the file says nothing about, this says nothing about.** An entity's hp
// after a fight, a component attached at runtime, a machine mid-flight: all left
// alone, because the update is partial.
//
// What it costs, and it is the same cost the tile rule carries: a goblin that
// walked across the room returns to its spawn point when the map is re-imported,
// exactly as a door opened by setTilePassable closes again. Re-import happens on
// load, so this is a level load and not a per-tick correction.
//
// Which map this is comes from the map's own tiled.PropMapID property, and falls
// back to mapPath when it declares none — object ids are numbered per file, so
// two maps may both hold an object 1 and the rows have to be told apart somehow.
//
// A path is not an identity: renaming or moving the file made it a different map,
// spawning its objects again beside the ones already there and leaving the
// originals where deletion could not reach them. A map that says which map it is
// survives being renamed, moved and re-spelled. One that does not is warned
// about, because the trap only springs later.
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

	// No early return for a map with no objects. Under the create-once rule
	// there was one, and it kept LoadMap working against a store where the
	// engine tables had never been created — but deleting the entity of an
	// object the map no longer has means reading what this map spawned, and a
	// map with nothing left in it is exactly the case where everything it
	// spawned should go. Loading a Tiled map needs the engine's tables.

	// Which map this is, which is not the same question as where its file is.
	key := m.Properties.Get(tiled.PropMapID)
	if key == "" {
		key = mapPath
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"%s declares no %q property, so its spawns are filed under its path; "+
				"renaming or moving the file will spawn them again and leave the originals "+
				"where nothing can reach them", mapPath, tiled.PropMapID))
	} else if err := adoptSpawns(ctx, svc, db, mapPath, key); err != nil {
		return res, err
	}

	done, err := spawnedObjects(ctx, db, key)
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
				// An object that *had* a class and lost one is about to have
				// its entity deleted by the loop below, which is right under
				// the rule and is a surprising amount of consequence for
				// clearing a field in the editor.
				if _, live := done[obj.ID]; live {
					res.Warnings = append(res.Warnings, describeWarning(obj, m,
						"has no class, so it is no longer a spawn and its entity is deleted"))
				}
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

			components, err := spawnComponents(svc.Schema(), m, obj)
			if err != nil {
				res.Refused = append(res.Refused, describeRefusal(obj, m, err))
				continue
			}

			// The same check on both paths, so the same file makes the same
			// world whether or not it has been loaded before.
			//
			// The update path used to skip this entirely — it wrote through the
			// storage port and never validated — so an object could add a
			// component its type forbids, or stop naming a required one, and a
			// map that was refused outright on a fresh database updated
			// cheerfully on a used one. "The file wins" cannot mean "and also
			// what the file means depends on history".
			warnings, err := checkSpawn(svc, obj.Type, components)
			if err != nil {
				res.Refused = append(res.Refused, describeRefusal(obj, m, err))
				continue
			}
			for _, w := range warnings {
				res.Warnings = append(res.Warnings, describeWarning(obj, m, w))
			}

			existing, live := done[obj.ID]
			// A class change is a different entity, not a changed one: the type
			// decides which components are required, and writing the new
			// class's components onto an entity still typed as the old one
			// produces a row that violates its own contract. The file wins, so
			// the old entity goes and a new one takes its place — and its id
			// changes, because it is not the same thing any more.
			if live && !strings.EqualFold(existing.EntityType, obj.Type) {
				if err := deleteSpawn(ctx, svc, key, obj.ID, existing.EntityID); err != nil {
					res.Refused = append(res.Refused, describeRefusal(obj, m, err))
					continue
				}
				res.Deleted++
				live = false
			}

			if live {
				changed, err := needsUpdate(ctx, db, svc.Schema(), existing.EntityID, components)
				if err != nil {
					return res, err
				}
				if !changed {
					res.Unchanged++
					continue
				}
				if err := updateSpawn(ctx, svc, existing.EntityID, components); err != nil {
					res.Refused = append(res.Refused, describeRefusal(obj, m, err))
					continue
				}
				res.Updated++
				continue
			}

			if err := createSpawn(ctx, svc, key, obj, components); err != nil {
				res.Refused = append(res.Refused, describeRefusal(obj, m, err))
				continue
			}
			res.Created++
		}
	}

	// Objects the file no longer has. The tile rule's other half: a cell the map
	// stopped describing loses its tile, and a goblin the map stopped describing
	// loses its entity — otherwise deleting one in the editor has no effect a
	// running game can show.
	//
	// Only entities reached through a spawn row, so anything created by hand, by
	// a script or by an older engine is out of scope.
	gone := make([]int, 0, len(done))
	for _, objectID := range sortedIDs(done) {
		if !seen[objectID] {
			gone = append(gone, objectID)
		}
	}
	if len(gone) > 0 {
		// One transaction for all of them, which is SyncTiles' reason: a
		// re-import that commits half its deletions is a level with some of the
		// old world still in it, and the engine reads the grid on the next tick.
		// Per-object transactions are right for *creates*, where one bad object
		// must not stop nineteen good ones; a delete has no refusal to make, so
		// any failure aborts and there is nothing to preserve by having
		// committed some of it.
		err := svc.InTx(ctx, func(tx world.Tx) error {
			for _, objectID := range gone {
				if err := tx.DeleteEntity(ctx, done[objectID].EntityID); err != nil {
					return err
				}
				if err := tx.ForgetSpawn(ctx, key, objectID); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return res, fmt.Errorf("tilemap: removing spawns %q no longer has: %w", key, err)
		}
		res.Deleted += len(gone)
	}
	return res, nil
}

// checkSpawn runs the entity-type contract against what the object describes,
// and returns the warnings a lenient type produced.
//
// The same validation CreateEntity performs, called here so the update path
// performs it too. Warnings are read straight away because EntityService keeps
// only the last call's.
func checkSpawn(svc *world.EntityService, entityType string, components []world.EntityComponent) ([]string, error) {
	names := make([]string, len(components))
	for i, c := range components {
		names[i] = c.Name
	}
	vr := world.ValidateEntityCreation(svc.Schema(), entityType, names)
	if !vr.Valid() {
		return nil, &world.ValidationError{Type: entityType, Errors: vr.Errors, Warnings: vr.Warnings}
	}
	return vr.Warnings, nil
}

// updateSpawn re-applies what the object describes to the entity it made.
//
// Partial, per component: the properties the object names are set and nothing
// else is read or written, so an hp lost in a fight goes back to what the file
// says and a Speed attached at runtime is not noticed at all. A component the
// object has newly started describing is attached rather than refused.
//
// Called only for an object the database already disagrees with — needsUpdate
// decides that on the pool first, so a load with no edit opens no transaction.
func updateSpawn(
	ctx context.Context,
	svc *world.EntityService,
	entityID int64,
	components []world.EntityComponent,
) error {
	return svc.InTx(ctx, func(tx world.Tx) error {
		for _, comp := range components {
			err := tx.SetComponentValues(ctx, entityID, comp.Name, comp.Values)
			if errors.Is(err, world.ErrNoSuchComponent) {
				// The object started describing a component the entity does not
				// have. Attaching is what the file asking for it means.
				err = tx.AttachComponent(ctx, entityID, comp.Name, comp.Values)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// needsUpdate reports whether the database already says what the object says.
//
// Read on the pool before any transaction opens, which is what SyncTiles does
// and for the same reason — "a restart with no edit to the map is a read". The
// first version of this skipped the comparison and wrote unconditionally,
// justified by "Tx has no reader"; SyncTiles has no Tx reader either, it reads
// the pool, and this function is handed the same *sql.DB and already reads it
// three lines earlier. The constraint was invented.
//
// A component the entity does not have counts as a difference, because
// attaching it is a change. So does a column that will not scan: whatever is
// there is not what the file says.
func needsUpdate(
	ctx context.Context,
	db *sql.DB,
	s *schema.DatabaseSchema,
	entityID int64,
	components []world.EntityComponent,
) (bool, error) {
	for _, comp := range components {
		declared, canonical := schema.ComponentByName(s, comp.Name)
		if canonical == "" {
			return true, nil
		}
		cols := make([]string, 0, len(comp.Values))
		names := make([]string, 0, len(comp.Values))
		for name := range comp.Values {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if schema.StorageLayout(declared.Type) == schema.LayoutColumns {
				cols = append(cols, `"`+strings.ToLower(name)+`"`)
			} else {
				cols = append(cols, `"`+schema.LayoutColumnName(schema.StorageLayout(declared.Type))+`"`)
			}
		}
		if len(cols) == 0 {
			continue
		}

		query := fmt.Sprintf(`SELECT %s FROM %q WHERE entity_id = ?`,
			strings.Join(cols, ", "), "comp_"+strings.ToLower(canonical))
		row := db.QueryRowContext(ctx, query, entityID)

		held := make([]any, len(cols))
		into := make([]any, len(cols))
		for i := range held {
			into[i] = &held[i]
		}
		if err := row.Scan(into...); err != nil {
			// No row means no component, which attaching will change. Anything
			// else is a table this cannot read, and re-applying is the safe
			// answer rather than deciding nothing needs doing.
			return true, nil //nolint:nilerr // a difference, not a failure
		}
		for i, name := range names {
			if !sameValue(held[i], comp.Values[name]) {
				return true, nil
			}
		}
	}
	return false, nil
}

// sameValue compares a value read back from SQLite against one about to be
// written to it.
//
// Through their text, because the driver hands back int64, float64, string and
// []byte for columns this writes as int64, float64, string and bool, and a
// type-by-type comparison would be a table of conversions that is wrong the
// first time somebody adds a property type. Two values that format identically
// write identically, which is the question being asked.
func sameValue(held, want any) bool {
	return formatValue(held) == formatValue(want)
}

func formatValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case bool:
		// SQLite has no boolean: the column holds 1 or 0 and reads back as an
		// int64, so a bool has to be compared as the number it becomes.
		if t {
			return "1"
		}
		return "0"
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'g', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// deleteSpawn removes the entity an object made, and the record of it.
func deleteSpawn(
	ctx context.Context,
	svc *world.EntityService,
	mapPath string,
	objectID int,
	entityID int64,
) error {
	return svc.InTx(ctx, func(tx world.Tx) error {
		if err := tx.DeleteEntity(ctx, entityID); err != nil {
			return err
		}
		// Belt and braces beside the cascade, which normally gets there first:
		// the row's foreign key takes it when the entity goes. Explicit for the
		// reason DeleteEntity removes component rows explicitly — a database
		// opened without foreign keys enforced would otherwise keep a row
		// pointing at an entity that is not there, and the object would look
		// spawned forever.
		return tx.ForgetSpawn(ctx, mapPath, objectID)
	})
}

// sortedIDs is map iteration made deterministic, so a map with two objects to
// delete produces the same statements in the same order every run.
func sortedIDs(m map[int]spawned) []int {
	out := make([]int, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Ints(out)
	return out
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
) error {
	return svc.InTx(ctx, func(tx world.Tx) error {
		e, err := svc.CreateEntityInTx(ctx, tx, obj.Type, components)
		if err != nil {
			return err
		}
		return tx.RecordSpawn(ctx, mapPath, obj.ID, e.ID)
	})
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

// adoptSpawns re-keys a map's rows from its path to its id.
//
// Every database built before a map declared an id has its spawns filed under a
// path. Without this, adding the property is itself a rename: the map looks
// unspawned, the world is created a second time, and the first copy becomes
// unreachable — the fix introducing the bug it fixes.
//
// Where an object has a row under both keys, the id-keyed one is the live one
// and the path-keyed entity is a duplicate of the same object, so it goes. That
// happens when a map gained an id on a database that had already loaded it both
// ways round.
func adoptSpawns(ctx context.Context, svc *world.EntityService, db *sql.DB, mapPath, key string) error {
	if mapPath == key {
		return nil
	}
	old, err := spawnedObjects(ctx, db, mapPath)
	if err != nil {
		return err
	}
	if len(old) == 0 {
		return nil
	}
	claimed, err := spawnedObjects(ctx, db, key)
	if err != nil {
		return err
	}

	return svc.InTx(ctx, func(tx world.Tx) error {
		for _, objectID := range sortedIDs(old) {
			if _, taken := claimed[objectID]; taken {
				// The id-keyed row already speaks for this object; this one is
				// a duplicate spawn of it.
				if err := tx.DeleteEntity(ctx, old[objectID].EntityID); err != nil {
					return err
				}
				if err := tx.ForgetSpawn(ctx, mapPath, objectID); err != nil {
					return err
				}
				continue
			}
			if err := tx.RecordSpawn(ctx, key, objectID, old[objectID].EntityID); err != nil {
				return err
			}
			if err := tx.ForgetSpawn(ctx, mapPath, objectID); err != nil {
				return err
			}
		}
		return nil
	})
}

// spawned is an entity one of this map's objects has already made.
type spawned struct {
	EntityID   int64
	EntityType string
}

// spawnedObjects is the entity each of this map's objects has already made,
// keyed by object id.
//
// The row is a live link and nothing more: its foreign key cascades, so an
// entity deleted at runtime takes its row with it and the object looks
// unspawned again — which under the file-wins rule is right, because the file
// still says there is a goblin there.
func spawnedObjects(ctx context.Context, db *sql.DB, mapPath string) (map[int]spawned, error) {
	// Joined to entities, so a row pointing at an entity that is not there does
	// not count as spawned. The foreign key cascades and should make that
	// impossible — but a connection opened without foreign keys enforced can
	// delete an entity and leave the row, and then every load reported success
	// for a goblin that did not exist and wrote its values into orphaned
	// component rows. The same reasoning DeleteEntity gives for deleting
	// component rows explicitly rather than trusting the cascade.
	rows, err := db.QueryContext(ctx,
		`SELECT spawns.object_id, entities.id, entities.entity_type
		   FROM spawns JOIN entities ON entities.id = spawns.entity_id
		  WHERE spawns.map = ?`, mapPath)
	if err != nil {
		return nil, fmt.Errorf("tilemap: reading spawns for %q: %w", mapPath, err)
	}
	defer func() { _ = rows.Close() }()
	done := map[int]spawned{}
	for rows.Next() {
		var objectID int
		var e spawned
		if err := rows.Scan(&objectID, &e.EntityID, &e.EntityType); err != nil {
			return nil, fmt.Errorf("tilemap: scanning a spawn row: %w", err)
		}
		done[objectID] = e
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
		// A component that is not an object has no properties to name: it is one
		// column, called "value" or "target_entity_id", and its type is the
		// component's own. So "Label.value" addresses it and "Label.anything"
		// does not — where an object component's properties are looked up in
		// what it declares.
		//
		// Both halves of this were wrong. spawnComponents looked every property
		// up in declared.Properties, which is nil for a scalar, so a scalar
		// component could never be given a value from a map at all; and the
		// value was keyed by the property name, which the insert path tolerates
		// and SetComponentValues does not, so anything that did get through was
		// refused on the next load.
		if layout := schema.StorageLayout(declared.Type); layout != schema.LayoutColumns {
			column := schema.LayoutColumnName(layout)
			if !strings.EqualFold(prop, column) {
				return nil, fmt.Errorf(
					"property %q names %q, but component %q is a %s and holds its value in %q",
					name, prop, canonical, declared.Type, column)
			}
			v, err := propertyValue(declared.Type, obj.Properties.Get(name))
			if err != nil {
				return nil, fmt.Errorf("property %q: %w", name, err)
			}
			if values[canonical] == nil {
				values[canonical] = world.ComponentValues{}
			}
			values[canonical][column] = v
			continue
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
