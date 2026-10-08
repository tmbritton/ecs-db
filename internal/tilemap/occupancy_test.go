package tilemap

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

func exampleInteractions() schema.DatabaseSchema {
	return schema.DatabaseSchema{Components: map[string]schema.Component{
		"Passability":   {Type: "object"},
		"Visibility":    {Type: "object"},
		"OccupiedCells": {Type: "array"},
		"Walking":       {Type: "boolean"},
		"Flying":        {Type: "boolean"},
		"Swimming":      {Type: "boolean"},
		"Phased":        {Type: "boolean"},
		"Burrowing":     {Type: "boolean"},
		"NightVision":   {Type: "boolean"},
	}, Interactions: map[string]map[string]schema.InteractionRule{
		"Passability": {
			"solid":  {Allows: []string{"Flying", "Phased", "Burrowing"}},
			"liquid": {Allows: []string{"Swimming", "Flying"}},
			"sealed": {},
		},
		"Visibility": {
			"opaque":   {},
			"darkness": {Allows: []string{"NightVision"}},
		},
	}}
}

func TestParseOccupiedCells_ValidatesUniqueGridOffsetsBeforeImport(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []Point
		wantErr string
	}{
		{name: "irregular nonrectangular set", raw: `[{"x":0,"y":0},{"x":2,"y":0},{"x":1,"y":1}]`, want: []Point{{0, 0}, {2, 0}, {1, 1}}},
		{name: "origin need not be occupied", raw: `[{"x":2,"y":0}]`, want: []Point{{2, 0}}},
		{name: "empty list", raw: `[]`, wantErr: "empty"},
		{name: "duplicate offset", raw: `[{"x":0,"y":0},{"x":0,"y":0}]`, wantErr: "duplicate"},
		{name: "missing coordinate", raw: `[{"x":0}]`, wantErr: "y"},
		{name: "fractional offset", raw: `[{"x":0.5,"y":0}]`, wantErr: "integer"},
		{name: "outside map", raw: `[{"x":3,"y":0}]`, wantErr: "outside"},
		{name: "not JSON", raw: `oops`, wantErr: "JSON"},
		{name: "unknown field", raw: `[{"x":0,"y":0,"width":5}]`, wantErr: "width"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOccupiedCells(tt.raw, Point{1, 1}, 4, 3)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseOccupiedCells = %v,%v; want error %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil || len(got) != len(tt.want) {
				t.Fatalf("ParseOccupiedCells = %v,%v; want %v", got, err, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("offset %d = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSpace_CanEnterEveryOccupiedCellOnlyWhenAllRestrictionsAllowIt(t *testing.T) {
	wall := SpatialEntity{ID: 10, Position: Point{1, 0}, Passability: "solid", Visibility: "opaque"}
	river := SpatialEntity{ID: 20, Position: Point{1, 0}, Cells: []Point{{0, 0}, {1, 0}, {0, 1}}, Passability: "liquid"}
	tests := []struct {
		name        string
		mover       SpatialEntity
		destination Point
		occupants   []SpatialEntity
		want        bool
		wantErr     bool
	}{
		{"empty art cell", SpatialEntity{ID: 1}, Point{3, 2}, nil, true, false},
		{"out of bounds", SpatialEntity{ID: 1}, Point{4, 0}, nil, false, false},
		{"walker at wall", SpatialEntity{ID: 1, Capabilities: map[string]bool{"Walking": true}}, Point{1, 0}, []SpatialEntity{wall}, false, false},
		{"flyer at wall", SpatialEntity{ID: 1, Capabilities: map[string]bool{"Flying": true}}, Point{1, 0}, []SpatialEntity{wall}, true, false},
		{"swimmer at water", SpatialEntity{ID: 1, Capabilities: map[string]bool{"Swimming": true}}, Point{2, 0}, []SpatialEntity{river}, true, false},
		{"phase cannot cross water", SpatialEntity{ID: 1, Capabilities: map[string]bool{"Phased": true}}, Point{2, 0}, []SpatialEntity{river}, false, false},
		{"overlapping wall and water veto phase", SpatialEntity{ID: 1, Capabilities: map[string]bool{"Phased": true}}, Point{1, 0}, []SpatialEntity{wall, river}, false, false},
		{"overlapping wall and water admit flying", SpatialEntity{ID: 1, Capabilities: map[string]bool{"Flying": true}}, Point{1, 0}, []SpatialEntity{wall, river}, true, false},
		{"sealed restriction denies even flying", SpatialEntity{ID: 1, Capabilities: map[string]bool{"Flying": true}}, Point{1, 0}, []SpatialEntity{{ID: 42, Position: Point{1, 0}, Passability: "sealed"}}, false, false},
		{"inactive ability does not pass", SpatialEntity{ID: 1, Capabilities: map[string]bool{"Flying": false}}, Point{1, 0}, []SpatialEntity{wall}, false, false},
		{"mod-defined ability", SpatialEntity{ID: 1, Capabilities: map[string]bool{"Burrowing": true}}, Point{1, 0}, []SpatialEntity{wall}, true, false},
		{"nonrestricting occupant", SpatialEntity{ID: 1}, Point{1, 0}, []SpatialEntity{{ID: 99, Position: Point{1, 0}}}, true, false},
		{"own restriction excluded", SpatialEntity{ID: 10, Capabilities: map[string]bool{"Walking": true}}, Point{1, 0}, []SpatialEntity{wall}, true, false},
		{"unknown category fails loudly", SpatialEntity{ID: 1}, Point{1, 0}, []SpatialEntity{{ID: 11, Position: Point{1, 0}, Passability: "solidd"}}, false, true},
		{"present empty restriction fails loudly", SpatialEntity{ID: 1}, Point{1, 0}, []SpatialEntity{{ID: 11, Position: Point{1, 0}, HasPassability: true}}, false, true},
		{"multi-cell mover checks whole destination", SpatialEntity{ID: 1, Cells: []Point{{0, 0}, {1, 0}}}, Point{0, 0}, []SpatialEntity{wall}, false, false},
		{"multi-cell mover cannot extend off map", SpatialEntity{ID: 1, Cells: []Point{{0, 0}, {1, 0}}}, Point{3, 0}, nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			space := Space{Width: 4, Height: 3, Schema: exampleInteractions(), Entities: tt.occupants}
			got, err := space.CanEnter(tt.mover, tt.destination)
			if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
				t.Fatalf("CanEnter = %v,%v; want %v,error=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestSpace_CanSeeUsesVisibilityNotPassability(t *testing.T) {
	wall := SpatialEntity{ID: 10, Position: Point{1, 0}, Passability: "solid", Visibility: "opaque"}
	river := SpatialEntity{ID: 20, Position: Point{1, 0}, Cells: []Point{{0, 0}, {1, 0}}, Passability: "liquid"}
	darkness := SpatialEntity{ID: 30, Position: Point{1, 0}, Visibility: "darkness"}
	tests := []struct {
		name      string
		observer  SpatialEntity
		end       Point
		targetID  int64
		occupants []SpatialEntity
		want      bool
		wantErr   bool
	}{
		{"clear empty cell", SpatialEntity{ID: 1}, Point{2, 0}, 2, nil, true, false},
		{"river is transparent", SpatialEntity{ID: 1}, Point{2, 0}, 2, []SpatialEntity{river}, true, false},
		{"wall blocks ordinary sight", SpatialEntity{ID: 1}, Point{2, 0}, 2, []SpatialEntity{wall}, false, false},
		{"phasing through wall does not grant sight", SpatialEntity{ID: 1, Capabilities: map[string]bool{"Phased": true}}, Point{2, 0}, 2, []SpatialEntity{wall}, false, false},
		{"night vision crosses darkness", SpatialEntity{ID: 1, Capabilities: map[string]bool{"NightVision": true}}, Point{2, 0}, 2, []SpatialEntity{darkness}, true, false},
		{"night vision does not cross opaque wall", SpatialEntity{ID: 1, Capabilities: map[string]bool{"NightVision": true}}, Point{2, 0}, 2, []SpatialEntity{wall}, false, false},
		{"darkness blocks ordinary sight", SpatialEntity{ID: 1}, Point{2, 0}, 2, []SpatialEntity{darkness}, false, false},
		{"target's own opacity does not hide it", SpatialEntity{ID: 1}, Point{1, 0}, 10, []SpatialEntity{wall}, true, false},
		{"other occupant at target blocks", SpatialEntity{ID: 1}, Point{1, 0}, 2, []SpatialEntity{wall, {ID: 2, Position: Point{1, 0}}}, false, false},
		{"out of bounds", SpatialEntity{ID: 1}, Point{4, 0}, 2, nil, false, false},
		{"unknown category", SpatialEntity{ID: 1}, Point{2, 0}, 2, []SpatialEntity{{ID: 10, Position: Point{1, 0}, Visibility: "darkk"}}, false, true},
		{"present empty sight rule", SpatialEntity{ID: 1}, Point{2, 0}, 2, []SpatialEntity{{ID: 10, Position: Point{1, 0}, HasVisibility: true}}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			space := Space{Width: 4, Height: 3, Schema: exampleInteractions(), Entities: tt.occupants}
			got, err := space.CanSee(tt.observer, tt.end, tt.targetID)
			if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
				t.Fatalf("CanSee = %v,%v; want %v,error=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestSpace_CanSeeBresenhamDiagonalAndSameCell(t *testing.T) {
	s := Space{Width: 4, Height: 4, Schema: exampleInteractions(), Entities: []SpatialEntity{
		{ID: 10, Position: Point{1, 1}, Visibility: "opaque"},
	}}
	observer := SpatialEntity{ID: 1, Position: Point{0, 0}}
	if seen, err := s.CanSee(observer, Point{3, 3}, 2); err != nil || seen {
		t.Fatalf("diagonal ray went through opaque wall: %v,%v", seen, err)
	}
	if seen, err := s.CanSee(observer, Point{0, 0}, 1); err != nil || !seen {
		t.Fatalf("observer cannot see its own cell: %v,%v", seen, err)
	}
}
