package tilemap

import "testing"

func TestNewTileGrid_TracksBoundsAndArtworkIdentitySeparately(t *testing.T) {
	g := NewTileGrid(5, 3)
	if g.Width != 5 || g.Height != 3 {
		t.Fatalf("grid bounds = %dx%d, want 5x3", g.Width, g.Height)
	}
	if _, found := g.EntityAt(1, 1); found {
		t.Fatal("unpainted cell has an art entity")
	}
	g.SetEntityID(1, 1, 77)
	if id, found := g.EntityAt(1, 1); !found || id != 77 {
		t.Fatalf("art entity = %d,%v; want 77,true", id, found)
	}
}
