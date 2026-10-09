// Package tilesetvalidation reports authoring problems in the visible TILES
// page. It distinguishes import refusals from metadata the game ignores.
package tilesetvalidation

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/tilesurface"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
	"github.com/tmbritton/ecs-db/internal/tilemap"
)

type Finding struct {
	Message string
	Warning bool
}

type Report struct {
	Tileset  []Finding
	Tiles    map[uint32][]Finding
	Errors   int
	Warnings int
}

func (r *Report) add(id *uint32, message string, warning bool) {
	finding := Finding{Message: message, Warning: warning}
	if id == nil {
		r.Tileset = append(r.Tileset, finding)
	} else {
		if r.Tiles == nil {
			r.Tiles = map[uint32][]Finding{}
		}
		r.Tiles[*id] = append(r.Tiles[*id], finding)
	}
	if warning {
		r.Warnings++
	} else {
		r.Errors++
	}
}

// Check visits the visible page and selected tile, never every implicit cell
// on a potentially huge sheet. knownTypes is nil when the schema is not open;
// in that case it does not invent unknown-entityType errors.
func Check(set *tiled.Tileset, view tilesurface.View, knownTypes map[string]bool, ambiguous []uint32, db *schema.DatabaseSchema) Report {
	var report Report
	if set == nil {
		return report
	}
	if !view.Collection {
		for _, tile := range view.Tiles {
			if strings.Contains(tile.ArtProblem, "tileset image") {
				report.add(nil, tile.ArtProblem, false)
				break
			}
		}
	}
	validateProperties(&report, nil, set.Properties)
	seen := map[uint32]bool{}
	check := func(tile tilesurface.Tile) {
		if seen[tile.ID] {
			return
		}
		seen[tile.ID] = true
		id := tile.ID
		index := sort.Search(len(ambiguous), func(i int) bool { return ambiguous[i] >= id })
		if index < len(ambiguous) && ambiguous[index] == id {
			report.add(&id, fmt.Sprintf("tile %d declares both type and class; resolve the ambiguity in Tiled before editing", id), false)
		}
		if tile.ArtProblem != "" {
			report.add(&id, tile.ArtProblem, false)
		}
		authored, exists := set.Tiles[id]
		entityType := authored.Properties.Get("entityType")
		if entityType == "" {
			entityType = set.Properties.Get("entityType")
		}
		if entityType == "" {
			report.add(&id, fmt.Sprintf("tile %d has no entityType reference template; painting it cannot import an entity", id), false)
		} else if knownTypes != nil && !knownTypes[entityType] {
			report.add(&id, fmt.Sprintf("entityType %q is not declared in schema.json", entityType), false)
		}
		if exists {
			validateProperties(&report, &id, authored.Properties)
		}
		if db != nil {
			if _, known := db.EntityTypes[entityType]; known {
				contract := tilemap.ValidateTileTemplateComponents(db, set, id)
				for _, message := range contract.Errors {
					report.add(&id, message, false)
				}
				for _, message := range contract.Warnings {
					report.add(&id, message, true)
				}
			}
		}
		if db != nil {
			fields := make(map[string]tiled.Property)
			for name, prop := range set.Properties {
				if strings.Contains(name, ".") {
					fields[name] = prop
				}
			}
			for name, prop := range authored.Properties {
				if strings.Contains(name, ".") {
					fields[name] = prop
				}
			}
			names := make([]string, 0, len(fields))
			for name := range fields {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if err := tilemap.ValidateTileTemplateField(db, name, fields[name].Value); err != nil {
					report.add(&id, err.Error(), false)
				}
			}
		}
	}
	for _, tile := range view.Tiles {
		check(tile)
	}
	if view.Selected != nil {
		check(*view.Selected)
	}
	return report
}

func validateProperties(report *Report, id *uint32, props tiled.Properties) {
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		prop := props[name]
		if name == tiled.PropPassable {
			report.add(id, "the game refuses deprecated artwork passable; author Passability on a referenced entity in MAP", false)
		}
		if name == "entityType" || strings.Contains(name, ".") {
			continue // component fields are read by schema type, not Tiled type
		}
		switch prop.Type {
		case "int":
			if _, err := strconv.ParseInt(prop.Value, 10, 64); err != nil {
				report.add(id, fmt.Sprintf("property %s needs a whole number, got %q", name, prop.Value), false)
			}
		case "float":
			f, err := strconv.ParseFloat(prop.Value, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				report.add(id, fmt.Sprintf("property %s needs a finite number, got %q", name, prop.Value), false)
			}
		case "bool":
			if prop.Value != "true" && prop.Value != "false" {
				report.add(id, fmt.Sprintf("property %s needs true or false, got %q", name, prop.Value), false)
			}
		}
	}
}
