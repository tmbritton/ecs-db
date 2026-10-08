package tilemap

import (
	"strings"
	"testing"
)

func TestParseTileReferences_ValidatesIDsAndDuplicates(t *testing.T) {
	for _, tt := range []struct {
		name, raw, wantErr string
		want               []int64
	}{
		{"empty collection", `[]`, "", []int64{}},
		{"multiple references", `[10,20]`, "", []int64{10, 20}},
		{"duplicate reference", `[10,10]`, "duplicate", nil},
		{"nonpositive ID", `[0]`, "positive", nil},
		{"fractional ID", `[1.5]`, "integer", nil},
		{"not an array", `"10"`, "JSON", nil},
		{"null reference collection", `null`, "array", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTileReferences(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseTileReferences = %v,%v; want %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil || len(got) != len(tt.want) {
				t.Fatalf("ParseTileReferences = %v,%v; want %v", got, err, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("reference %d = %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSpace_ReferencedOccupantsUseTilePositionsAndEveryRestriction(t *testing.T) {
	wall := SpatialEntity{ID: 10, Passability: "solid", Visibility: "opaque"}
	river := SpatialEntity{ID: 20, Passability: "liquid"}
	for _, tt := range []struct {
		name    string
		tiles   []SpatialEntity
		actors  []SpatialEntity
		mover   SpatialEntity
		at      Point
		want    bool
		wantErr bool
	}{
		{name: "wall at referencing tile", tiles: []SpatialEntity{{ID: 100, IsTile: true, HasPosition: true, Position: Point{1, 0}, References: []int64{10}}}, actors: []SpatialEntity{wall}, mover: SpatialEntity{ID: 1}, at: Point{1, 0}},
		{name: "same wall has no implicit origin cell", tiles: []SpatialEntity{{ID: 100, IsTile: true, HasPosition: true, Position: Point{1, 0}, References: []int64{10}}}, actors: []SpatialEntity{wall}, mover: SpatialEntity{ID: 1}, at: Point{0, 0}, want: true},
		{name: "all tile references veto independently", tiles: []SpatialEntity{{ID: 100, IsTile: true, HasPosition: true, Position: Point{1, 0}, References: []int64{10, 20}}}, actors: []SpatialEntity{wall, river}, mover: SpatialEntity{ID: 1, Capabilities: map[string]bool{"Phased": true}}, at: Point{1, 0}},
		{name: "one river referenced at irregular tiles", tiles: []SpatialEntity{
			{ID: 100, IsTile: true, HasPosition: true, Position: Point{1, 0}, References: []int64{20}},
			{ID: 101, IsTile: true, HasPosition: true, Position: Point{2, 0}, References: []int64{20}},
			{ID: 102, IsTile: true, HasPosition: true, Position: Point{1, 1}, References: []int64{20}},
		}, actors: []SpatialEntity{river}, mover: SpatialEntity{ID: 1, Capabilities: map[string]bool{"Swimming": true}}, at: Point{1, 1}, want: true},
		{name: "unreferenced runtime entity still occupies own Position", actors: []SpatialEntity{{ID: 12, HasPosition: true, Position: Point{1, 0}, Passability: "solid"}}, mover: SpatialEntity{ID: 1}, at: Point{1, 0}},
		{name: "dangling tile reference is an error", tiles: []SpatialEntity{{ID: 100, IsTile: true, HasPosition: true, Position: Point{1, 0}, References: []int64{999}}}, mover: SpatialEntity{ID: 1}, at: Point{1, 0}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := Space{Width: 4, Height: 3, Schema: exampleInteractions(), Entities: append(tt.tiles, tt.actors...)}
			allowed, err := s.CanEnter(tt.mover, tt.at)
			if (err != nil) != tt.wantErr || (!tt.wantErr && allowed != tt.want) {
				t.Fatalf("CanEnter(%v) = %v,%v; want %v,error=%v", tt.at, allowed, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestSpace_ReferencedVisibilityBlocksSightAtTileNotTargetOrigin(t *testing.T) {
	s := Space{Width: 4, Height: 1, Schema: exampleInteractions(), Entities: []SpatialEntity{
		{ID: 100, IsTile: true, HasPosition: true, Position: Point{1, 0}, References: []int64{10}},
		{ID: 10, Visibility: "opaque"},
	}}
	seen, err := s.CanSee(SpatialEntity{ID: 1}, Point{3, 0}, 2)
	if err != nil || seen {
		t.Fatalf("sight crossed referenced wall: %v,%v", seen, err)
	}
}

func TestSpace_MovingOrRelinkingTileMovesReferencedOccupancy(t *testing.T) {
	s := Space{Width: 4, Height: 2, Schema: exampleInteractions(), Entities: []SpatialEntity{
		{ID: 100, IsTile: true, HasPosition: true, Position: Point{1, 0}, References: []int64{10}},
		{ID: 10, Passability: "solid"},
		{ID: 20, Passability: "liquid"},
	}}
	if can, err := s.CanEnter(SpatialEntity{ID: 1}, Point{1, 0}); err != nil || can {
		t.Fatalf("original wall was not at tile: %v,%v", can, err)
	}
	s.Entities[0].Position = Point{2, 0}
	if can, err := s.CanEnter(SpatialEntity{ID: 1}, Point{1, 0}); err != nil || !can {
		t.Fatalf("old Tile cell still occupied after moving it: %v,%v", can, err)
	}
	if can, err := s.CanEnter(SpatialEntity{ID: 1}, Point{2, 0}); err != nil || can {
		t.Fatalf("new Tile cell not occupied after move: %v,%v", can, err)
	}
	s.Entities[0].References = []int64{20}
	if can, err := s.CanEnter(SpatialEntity{ID: 1, Capabilities: map[string]bool{"Swimming": true}}, Point{2, 0}); err != nil || !can {
		t.Fatalf("relinking Wall to River did not change traversal: %v,%v", can, err)
	}
}
