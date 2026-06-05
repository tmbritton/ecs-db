package tilemap

import "testing"

func TestNewTileGrid_Dimensions(t *testing.T) {
	g := NewTileGrid(5, 3)
	if g.Width != 5 || g.Height != 3 {
		t.Errorf("got %d×%d, want 5×3", g.Width, g.Height)
	}
}

func TestIsPassable_DefaultsFalse(t *testing.T) {
	g := NewTileGrid(3, 3)
	if g.IsPassable(1, 1) {
		t.Error("new grid cell should be impassable by default")
	}
}

func TestSetPassable_UpdatesCell(t *testing.T) {
	g := NewTileGrid(3, 3)
	g.SetPassable(1, 1, true)
	if !g.IsPassable(1, 1) {
		t.Error("cell should be passable after SetPassable(true)")
	}
}

func TestIsPassable_OutOfBounds(t *testing.T) {
	g := NewTileGrid(3, 3)
	g.SetPassable(0, 0, true)
	cases := [][2]int{{-1, 0}, {0, -1}, {3, 0}, {0, 3}, {-1, -1}}
	for _, c := range cases {
		if g.IsPassable(c[0], c[1]) {
			t.Errorf("IsPassable(%d,%d) = true, want false (out of bounds)", c[0], c[1])
		}
	}
}

func TestSetPassable_OutOfBounds_NoOp(t *testing.T) {
	g := NewTileGrid(3, 3)
	g.SetPassable(-1, 0, true)
	g.SetPassable(0, 3, true)
}
