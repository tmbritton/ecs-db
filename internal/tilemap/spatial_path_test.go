package tilemap

import "testing"

func TestAStarFor_UsesTheMoverAndAllDestinationOccupants(t *testing.T) {
	space := Space{Width: 4, Height: 3, Schema: exampleInteractions(), Entities: []SpatialEntity{
		{ID: 10, Position: Point{1, 0}, Passability: "solid"},
		{ID: 20, Position: Point{2, 0}, Cells: []Point{{0, 0}, {0, 1}}, Passability: "liquid"},
	}}
	tests := []struct {
		name    string
		mover   SpatialEntity
		wantLen int
	}{
		{name: "walking detours around wall and river", mover: SpatialEntity{ID: 1, Capabilities: map[string]bool{"Walking": true}}, wantLen: 7},
		{name: "flying crosses both", mover: SpatialEntity{ID: 2, Capabilities: map[string]bool{"Flying": true}}, wantLen: 3},
		{name: "phased still detours around water", mover: SpatialEntity{ID: 3, Capabilities: map[string]bool{"Phased": true}}, wantLen: 7},
		{name: "mod-defined burrowing crosses wall but not water", mover: SpatialEntity{ID: 4, Capabilities: map[string]bool{"Burrowing": true}}, wantLen: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := AStarFor(space, tt.mover, Point{0, 0}, Point{3, 0})
			if err != nil || len(path) != tt.wantLen || path[len(path)-1] != (Point{3, 0}) {
				t.Fatalf("path = %v, err %v, want length %d ending (3,0)", path, err, tt.wantLen)
			}
		})
	}
}

func TestReachableFor_UsesTheSameOccupantDecisionAsPathfinding(t *testing.T) {
	space := Space{Width: 3, Height: 2, Schema: exampleInteractions(), Entities: []SpatialEntity{
		{ID: 10, Position: Point{1, 0}, Passability: "solid"},
	}}
	for _, tt := range []struct {
		name  string
		mover SpatialEntity
		want  int
	}{
		{"walker", SpatialEntity{ID: 1}, 1},
		{"flyer", SpatialEntity{ID: 2, Capabilities: map[string]bool{"Flying": true}}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cells, err := ReachableFor(space, tt.mover, Point{0, 0}, 1)
			if err != nil || len(cells) != tt.want {
				t.Fatalf("reachable = %v, err %v; want %d", cells, err, tt.want)
			}
		})
	}
}

func TestAStarFor_BlockedGoalEnclosedStartAndSameCell(t *testing.T) {
	blockers := []SpatialEntity{
		{ID: 10, Position: Point{1, 0}, Passability: "solid"},
		{ID: 11, Position: Point{0, 1}, Passability: "solid"},
	}
	for _, tt := range []struct {
		name  string
		start Point
		goal  Point
		walls []SpatialEntity
	}{
		{"blocked goal", Point{0, 0}, Point{1, 0}, blockers},
		{"enclosed start", Point{0, 0}, Point{2, 2}, blockers},
		{"same cell", Point{1, 1}, Point{1, 1}, nil},
		{"out of bounds goal", Point{0, 0}, Point{3, 0}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := Space{Width: 3, Height: 3, Schema: exampleInteractions(), Entities: tt.walls}
			path, err := AStarFor(s, SpatialEntity{ID: 1}, tt.start, tt.goal)
			if err != nil || path != nil {
				t.Fatalf("AStarFor = %v,%v; want no path", path, err)
			}
		})
	}
}

func TestReachableFor_NonpositiveStepsReturnNoCells(t *testing.T) {
	s := Space{Width: 3, Height: 3, Schema: exampleInteractions()}
	for _, steps := range []int{-1, 0} {
		cells, err := ReachableFor(s, SpatialEntity{ID: 1}, Point{0, 0}, steps)
		if err != nil || len(cells) != 0 {
			t.Fatalf("ReachableFor steps %d = %v,%v; want none", steps, cells, err)
		}
	}
}
