package tiled_test

import (
	"testing"

	"github.com/tmbritton/ecs-db/internal/tiled"
)

// Placements is what DrawList is built from: one entry per non-empty cell,
// carrying either a placed tile or the reason it could not be placed. The
// engine wants only the first half; Forge draws a marker for the second,
// because a tile you cannot see is one you cannot fix.
func TestPlacements_ReportTheCellsDrawListSkips(t *testing.T) {
	m := mapOf(2, 2, 16, 16, oneSheet(), layer("ground", 2, 2, true, 1, 9999, 0, 2))

	places := m.Placements()
	if len(places) != 3 {
		t.Fatalf("got %d placements, want 3 — the empty cell is not one", len(places))
	}

	var bad []tiled.Placement
	for _, p := range places {
		if p.Problem != "" {
			bad = append(bad, p)
		}
	}
	if len(bad) != 1 {
		t.Fatalf("got %d refused cells, want 1: %+v", len(bad), bad)
	}
	if bad[0].X != 1 || bad[0].Y != 0 {
		t.Errorf("the refused cell is (%d,%d), want (1,0)", bad[0].X, bad[0].Y)
	}
	if bad[0].Layer != "ground" {
		t.Errorf("the refused cell names layer %q", bad[0].Layer)
	}
	if bad[0].Image != "" {
		t.Errorf("a cell that could not be placed carries an image: %q", bad[0].Image)
	}
}

// The projection has to agree with what it is a projection of, or the engine
// and the editor draw different maps.
func TestPlacements_DrawListIsThePlacedOnes(t *testing.T) {
	m := mapOf(2, 2, 16, 16, oneSheet(), layer("ground", 2, 2, true, 1, 9999, 0, 2))

	draws, problems := m.DrawList()
	places := m.Placements()

	var placed []tiled.Draw
	for _, p := range places {
		if p.Problem == "" {
			placed = append(placed, p.Draw)
		}
	}
	if len(draws) != len(placed) {
		t.Fatalf("DrawList has %d, Placements has %d placed", len(draws), len(placed))
	}
	for i := range draws {
		if draws[i] != placed[i] {
			t.Errorf("draw %d differs:\n draw: %+v\nplace: %+v", i, draws[i], placed[i])
		}
	}
	if len(problems) != 1 || problems[0].Count != 1 {
		t.Errorf("problems: %+v", problems)
	}
}

func TestPlacements_SkipAHiddenLayerLikeDrawListDoes(t *testing.T) {
	m := mapOf(2, 2, 16, 16, oneSheet(), layer("ground", 2, 2, false, 1, 1, 1, 1))
	if got := m.Placements(); len(got) != 0 {
		t.Errorf("got %d placements from a hidden layer, want none", len(got))
	}
}

func TestPlacements_CarryTheCellOfEveryPlacedTile(t *testing.T) {
	m := mapOf(2, 2, 16, 16, oneSheet(), layer("ground", 2, 2, true, 1, 2, 2, 1))
	for _, p := range m.Placements() {
		if p.Problem != "" {
			t.Fatalf("unexpected problem: %v", p.Problem)
		}
		wantX := p.DX / m.TileWidth
		if p.X != wantX {
			t.Errorf("placement at DX=%d says cell x=%d, want %d", p.DX, p.X, wantX)
		}
	}
}

// Each refused cell carries its own reason. Reading it back off the aggregated
// problem list gets this wrong the moment two reasons interleave: the list
// holds one entry per distinct reason and a repeat only bumps a counter, so the
// third cell below would claim the second's reason.
func TestPlacements_EachRefusedCellCarriesItsOwnReason(t *testing.T) {
	// A sheet that names an image the tileset could not resolve. Its refusal
	// names the tileset and not the tile, so two cells in it share a reason
	// exactly — which is the case the aggregated problem list cannot answer.
	props := sheet("props", "props.png", 4, 8, 16, 16)
	props.Image.Path = ""
	refs := []tiled.TilesetRef{
		{FirstGID: 10, Tileset: sheet("floor", "floor.png", 4, 8, 16, 16)},
		{FirstGID: 100, Tileset: props},
	}
	// gid 5 sits between them with a different reason: it is below the first
	// tile of every tileset the map declares, so none holds it.
	m := mapOf(3, 1, 16, 16, refs, layer("ground", 3, 1, true, 100, 5, 101))

	places := m.Placements()
	if len(places) != 3 {
		t.Fatalf("got %d placements, want 3", len(places))
	}
	if places[0].Problem == "" || places[2].Problem == "" {
		t.Fatalf("cells 0 and 2 should both be refused: %+v", places)
	}
	if places[0].Problem != places[2].Problem {
		t.Errorf("two cells with the same fault report differently:\n [0] %q\n [2] %q",
			places[0].Problem, places[2].Problem)
	}
	if places[1].Problem == places[0].Problem {
		t.Errorf("a cell with a different fault reports the first one's reason: %q", places[1].Problem)
	}
}
