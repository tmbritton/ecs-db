package paint_test

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/paint"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

// A 3x2 map with one layer, every cell holding gid 1.
func mapOf(gids ...uint32) *tiled.Map {
	if len(gids) == 0 {
		gids = []uint32{1, 1, 1, 1, 1, 1}
	}
	return &tiled.Map{
		Width: 3, Height: 2, TileWidth: 16, TileHeight: 16,
		Layers: []tiled.Layer{{
			ID: 1, Name: "ground", Width: 3, Height: 2, Visible: true, Opacity: 1,
			Data: gids,
		}},
	}
}

func op(t *testing.T, kind paint.Kind) paint.Op {
	t.Helper()
	return paint.Op{Kind: kind, Layer: 0, Tile: tiled.Tile{GID: 7}}
}

// A click is a stroke whose ends are the same cell. Both are set, always: a
// zero To is the cell (0,0) and not "no second corner", so a caller that sent
// one end would paint a rectangle from the origin without meaning to.
func TestStamp_WritesOneCell(t *testing.T) {
	at := paint.Cell{X: 1, Y: 0}
	got, err := paint.Apply(mapOf(), paint.Op{
		Kind: paint.Stamp, Layer: 0, From: at, To: at, Tile: tiled.Tile{GID: 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{1, 7, 1, 1, 1, 1}
	if !equal(got.Data, want) {
		t.Errorf("data = %v, want %v", got.Data, want)
	}
	if got.Layer != 0 {
		t.Errorf("layer = %d", got.Layer)
	}
}

// Inclusive of both corners, and in any direction — a drag up-and-left is the
// same rectangle as the drag down-and-right that covers it.
func TestFill_CoversBothCornersFromAnyDirection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to paint.Cell
	}{
		{"down-right", paint.Cell{X: 0, Y: 0}, paint.Cell{X: 1, Y: 1}},
		{"up-left", paint.Cell{X: 1, Y: 1}, paint.Cell{X: 0, Y: 0}},
		{"down-left", paint.Cell{X: 1, Y: 0}, paint.Cell{X: 0, Y: 1}},
		{"up-right", paint.Cell{X: 0, Y: 1}, paint.Cell{X: 1, Y: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := paint.Apply(mapOf(), paint.Op{
				Kind: paint.Fill, Layer: 0, From: tc.from, To: tc.to, Tile: tiled.Tile{GID: 7},
			})
			if err != nil {
				t.Fatal(err)
			}
			want := []uint32{7, 7, 1, 7, 7, 1}
			if !equal(got.Data, want) {
				t.Errorf("data = %v, want %v", got.Data, want)
			}
		})
	}
}

// Gid 0 is a genuinely empty cell, not "the tile that looks like nothing".
func TestErase_WritesTheEmptyCell(t *testing.T) {
	got, err := paint.Apply(mapOf(), paint.Op{
		Kind: paint.Erase, Layer: 0, From: paint.Cell{X: 0, Y: 0}, To: paint.Cell{X: 1, Y: 0},
		// The tile in hand is ignored: erasing is not stamping a blank.
		Tile: tiled.Tile{GID: 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{0, 0, 1, 1, 1, 1}
	if !equal(got.Data, want) {
		t.Errorf("data = %v, want %v", got.Data, want)
	}
}

// Rotation is flag arithmetic on the tile in hand, not a different tile. The
// pairs come from Tiled's own encoding: D is the diagonal, and H/V with it make
// the quarter turns.
func TestTurn_IsFlagArithmetic(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   tiled.Tile
		want tiled.Tile
	}{
		{"0 to 90", tiled.Tile{GID: 5}, tiled.Tile{GID: 5, FlipD: true, FlipH: true}},
		{"90 to 180", tiled.Tile{GID: 5, FlipD: true, FlipH: true}, tiled.Tile{GID: 5, FlipH: true, FlipV: true}},
		{"180 to 270", tiled.Tile{GID: 5, FlipH: true, FlipV: true}, tiled.Tile{GID: 5, FlipD: true, FlipV: true}},
		{"270 back to 0", tiled.Tile{GID: 5, FlipD: true, FlipV: true}, tiled.Tile{GID: 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := paint.RotateCW(tc.in); got != tc.want {
				t.Errorf("RotateCW(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// Four turns is where you started. Asserted separately because each step above
// could be wrong in a way that cancels out.
func TestTurn_FourTimesIsIdentity(t *testing.T) {
	start := tiled.Tile{GID: 5, RotatedHex: true}
	got := start
	for range 4 {
		got = paint.RotateCW(got)
	}
	if got != start {
		t.Errorf("four turns gave %+v, want %+v", got, start)
	}
}

// Refused with a reason, never clamped. A stamp that lands quietly on the edge
// cell instead of the one you aimed at is worse than one that says no.
func TestApply_RefusesWhatItCannotDo(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		op         paint.Op
	}{
		{"a cell past the right edge", "outside", paint.Op{Kind: paint.Stamp, From: paint.Cell{X: 3, Y: 0}}},
		{"a cell past the bottom", "outside", paint.Op{Kind: paint.Stamp, From: paint.Cell{X: 0, Y: 2}}},
		{"a negative cell", "outside", paint.Op{Kind: paint.Stamp, From: paint.Cell{X: -1, Y: 0}}},
		{"a far corner off the map", "outside", paint.Op{Kind: paint.Fill, From: paint.Cell{X: 0, Y: 0}, To: paint.Cell{X: 9, Y: 9}}},
		{"a layer that is not there", "no layer", paint.Op{Kind: paint.Stamp, Layer: 4}},
		{"a tool nobody has written", "not something", paint.Op{Kind: paint.Kind("scribble")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.op
			o.Tile = tiled.Tile{GID: 7}
			_, err := paint.Apply(mapOf(), o)
			if err == nil {
				t.Fatal("this should have been refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refused with %q, which does not say %q", err, tc.want)
			}
		})
	}
}

// Painting into a layer you cannot see is the most common way to lose an hour in
// a tile editor. It is refused rather than done invisibly.
func TestApply_RefusesAHiddenLayer(t *testing.T) {
	o := op(t, paint.Stamp)
	o.Hidden = true

	_, err := paint.Apply(mapOf(), o)
	if err == nil {
		t.Fatal("painting into a hidden layer was allowed")
	}
	if !strings.Contains(err.Error(), "not drawing") {
		t.Errorf("refused with %q, which does not say the layer is hidden", err)
	}
}

// An edit that changes nothing writes nothing. A footer that says "unsaved"
// after stamping the tile a cell already holds teaches you to ignore the footer.
func TestApply_ReportsWhenNothingChanged(t *testing.T) {
	got, err := paint.Apply(mapOf(), paint.Op{
		Kind: paint.Stamp, Layer: 0, From: paint.Cell{X: 0, Y: 0}, To: paint.Cell{X: 0, Y: 0},
		Tile: tiled.Tile{GID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Changed {
		t.Error("stamping the tile that was already there reported a change")
	}
	if got.Data != nil {
		t.Error("nothing changed, so there is nothing to write")
	}
}

// And the flags count as a change even when the id does not: a tile rotated in
// place is a different cell.
func TestApply_ARotationInPlaceIsAChange(t *testing.T) {
	got, err := paint.Apply(mapOf(), paint.Op{
		Kind: paint.Stamp, Layer: 0, From: paint.Cell{X: 0, Y: 0}, To: paint.Cell{X: 0, Y: 0},
		Tile: tiled.Tile{GID: 1, FlipD: true, FlipH: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Changed {
		t.Fatal("rotating a tile in place reported no change")
	}
	if got.Data[0] == 1 {
		t.Error("the rotation was not written")
	}
}

// Apply must not edit the map it was given: the session holds it, and a caller
// that refused the result would otherwise have kept it anyway.
func TestApply_DoesNotChangeTheMapItWasGiven(t *testing.T) {
	m := mapOf()
	if _, err := paint.Apply(m, paint.Op{
		Kind: paint.Fill, Layer: 0, From: paint.Cell{X: 0, Y: 0}, To: paint.Cell{X: 2, Y: 1},
		Tile: tiled.Tile{GID: 7},
	}); err != nil {
		t.Fatal(err)
	}
	for i, gid := range m.Layers[0].Data {
		if gid != 1 {
			t.Fatalf("cell %d of the caller's map became %d", i, gid)
		}
	}
}

func equal(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Each turn is a quarter turn clockwise, checked against the transform the
// renderer actually applies rather than against the flag table this package
// wrote. The table is Tiled's encoding; this is what it comes to on screen, and
// the two agreeing is the only thing that makes a rotated stamp look rotated.
//
// The check is where a tile's top-left corner ends up. On a 16x16 tile, one
// quarter turn clockwise puts it at the top-right, then the bottom-right, then
// the bottom-left.
// The rotation cycle checked against the transform the renderer actually
// applies, over all eight flag combinations rather than the four an unmirrored
// stamp visits.
//
// The test this replaces asserted only TX/TY, which cannot tell a rotation from
// a mirror: every rotation shares its translation with a reflection — (0,0) is
// both upright and the diagonal transpose, (16,0) is both 90° and a horizontal
// mirror — so it passed against a RotateCW that sent every mirrored stamp back
// to upright. That bug shipped past it. The full matrix is what identifies a
// transform.
func TestRotateCW_IsWhatTheRendererDraws(t *testing.T) {
	for key := range 8 {
		before := tileOf(key)
		after := paint.RotateCW(before)
		// Composing a quarter turn with what the renderer already draws has to
		// give what it draws for the turned flags.
		want := turnQuarter(matrixOf(before))
		if got := matrixOf(after); got != want {
			t.Errorf("turning %s gives %s, which draws %+v; a quarter turn of %+v is %+v",
				name(key), name(keyOf(after)), got, matrixOf(before), want)
		}
	}
}

// And the permutation is complete: a quarter turn is a bijection over the eight
// symmetries, so nothing may collapse onto anything else. This is the shape of
// the bug that got through — four states all mapping to upright.
func TestRotateCW_MovesEveryOrientationSomewhereDifferent(t *testing.T) {
	seen := map[int]int{}
	for key := range 8 {
		to := keyOf(paint.RotateCW(tileOf(key)))
		if from, clash := seen[to]; clash {
			t.Errorf("%s and %s both turn into %s", name(from), name(key), name(to))
		}
		seen[to] = key
	}
	if len(seen) != 8 {
		t.Errorf("a quarter turn reaches %d of the 8 orientations", len(seen))
	}
}

// Four turns is where it started, from every orientation and not just upright.
func TestRotateCW_FourTurnsIsIdentityFromAnywhere(t *testing.T) {
	for key := range 8 {
		got := tileOf(key)
		for range 4 {
			got = paint.RotateCW(got)
		}
		if got != tileOf(key) {
			t.Errorf("four turns from %s gave %s", name(key), name(keyOf(got)))
		}
	}
}

// --- the eight orientations, as the renderer sees them ---

func tileOf(key int) tiled.Tile {
	return tiled.Tile{GID: 5, FlipD: key&4 != 0, FlipH: key&2 != 0, FlipV: key&1 != 0}
}

func keyOf(t tiled.Tile) int {
	key := 0
	if t.FlipD {
		key |= 4
	}
	if t.FlipH {
		key |= 2
	}
	if t.FlipV {
		key |= 1
	}
	return key
}

func name(key int) string {
	return [8]string{".", "V", "H", "HV", "D", "DV", "DH", "DHV"}[key]
}

func matrixOf(t tiled.Tile) tiled.Matrix {
	return tiled.Draw{SW: 16, SH: 16, FlipH: t.FlipH, FlipV: t.FlipV, FlipD: t.FlipD}.Transform()
}

// turnQuarter composes a screen-space quarter turn clockwise onto a transform.
// Screen y points down, so clockwise about the tile's centre sends (x, y) to
// (16-y, x) — which is the mapping this applies to each column of the matrix.
//
// The matrix is read as x' = A*x + B*y + TX, y' = C*x + D*y + TY. That
// convention is not guessable from the field names, so it is pinned by
// TestMatrixConvention_IsWhatTheRendererMeans below.
func turnQuarter(m tiled.Matrix) tiled.Matrix {
	return tiled.Matrix{
		A: -m.C, B: -m.D, TX: 16 - m.TY,
		C: m.A, D: m.B, TY: m.TX,
	}
}

// The composition above is only meaningful if the matrix is read the way the
// renderer writes it. Three transforms whose geometry is not in doubt say which
// way round A/B/C/D go — and reading them transposed is a mistake that makes
// every rotation assertion above pass against the wrong thing.
func TestMatrixConvention_IsWhatTheRendererMeans(t *testing.T) {
	at := func(m tiled.Matrix, x, y float64) [2]float64 {
		return [2]float64{m.A*x + m.B*y + m.TX, m.C*x + m.D*y + m.TY}
	}
	for _, tc := range []struct {
		name string
		tile tiled.Tile
		from [2]float64
		want [2]float64
	}{
		{"upright leaves the top-left alone", tileOf(0), [2]float64{0, 0}, [2]float64{0, 0}},
		{"a horizontal mirror sends the top-left to the top-right", tileOf(2), [2]float64{0, 0}, [2]float64{16, 0}},
		{"a horizontal mirror leaves the top edge on top", tileOf(2), [2]float64{16, 0}, [2]float64{0, 0}},
		{"a vertical mirror sends the top-left to the bottom-left", tileOf(1), [2]float64{0, 0}, [2]float64{0, 16}},
		{"the diagonal flag transposes", tileOf(4), [2]float64{16, 0}, [2]float64{0, 16}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := at(matrixOf(tc.tile), tc.from[0], tc.from[1]); got != tc.want {
				t.Errorf("%v goes to %v, want %v", tc.from, got, tc.want)
			}
		})
	}
}

// A drag starts on the map and ends wherever the pointer went, which may be
// off it. Checking only the corner the stroke started from would clamp the
// rectangle to the map's edge without saying so.
func TestApply_ChecksBothEndsOfADrag(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to paint.Cell
	}{
		{"the far corner is off the map", paint.Cell{X: 0, Y: 0}, paint.Cell{X: 99, Y: 99}},
		{"the near corner is off the map", paint.Cell{X: -1, Y: -1}, paint.Cell{X: 1, Y: 1}},
		// Past the right-hand edge but still inside the data: on a 3-wide map
		// cell (5,0) indexes to 5, which is (2,1). Nothing further down catches
		// this one — the stroke would land a row lower than it was drawn.
		{"the far corner wraps onto the next row", paint.Cell{X: 0, Y: 0}, paint.Cell{X: 5, Y: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mapOf()
			_, err := paint.Apply(m, paint.Op{
				Kind: paint.Fill, Layer: 0, From: tc.from, To: tc.to, Tile: tiled.Tile{GID: 5},
			})
			if err == nil {
				t.Fatal("the drag was accepted")
			}
			if !strings.Contains(err.Error(), "outside") {
				t.Errorf("refused with %q, which does not say the cell is off the map", err)
			}
		})
	}
}

// One past the last layer is the index an off-by-one produces, and the one that
// panics rather than refusing.
func TestApply_RefusesTheIndexJustPastTheLastLayer(t *testing.T) {
	m := mapOf()
	_, err := paint.Apply(m, paint.Op{
		Kind: paint.Stamp, Layer: len(m.Layers), Tile: tiled.Tile{GID: 5},
	})
	if err == nil {
		t.Fatalf("layer %d was accepted on a map with %d", len(m.Layers), len(m.Layers))
	}
	if !strings.Contains(err.Error(), "no layer") {
		t.Errorf("refused with %q", err)
	}
}

// The exported API's nil case. A caller that failed to load a document and
// carried on would otherwise dereference it here.
func TestApply_RefusesWhenThereIsNoMap(t *testing.T) {
	_, err := paint.Apply(nil, paint.Op{Kind: paint.Stamp, Tile: tiled.Tile{GID: 1}})
	if err == nil {
		t.Fatal("a nil map was accepted")
	}
	if !strings.Contains(err.Error(), "no map") {
		t.Errorf("refused with %q", err)
	}
}

// A layer whose data is shorter than its declared size. Tiled files come off
// disk and can be malformed, and a cell that is inside the declared bounds but
// past the end of the data is the case that reaches the index guard — the
// alternative to which is a panic in the middle of someone's save.
func TestApply_RefusesALayerShorterThanItSaysItIs(t *testing.T) {
	m := mapOf()
	m.Layers[0].Data = m.Layers[0].Data[:3] // 3x2 declared, 3 cells of data

	at := paint.Cell{X: 0, Y: 1} // inside 3x2, past the end of the data
	_, err := paint.Apply(m, paint.Op{
		Kind: paint.Stamp, Layer: 0, From: at, To: at, Tile: tiled.Tile{GID: 9},
	})
	if err == nil {
		t.Fatal("a cell past the end of the layer's data was accepted")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Errorf("refused with %q", err)
	}
}

// A stroke can be wrong in more than one way. The reason it gives should be the
// one that says most about what went wrong, so the checks are ordered rather
// than whichever happens to run first.
func TestApply_GivesTheMostSpecificReason(t *testing.T) {
	off := paint.Cell{X: 99, Y: 99}
	_, err := paint.Apply(mapOf(), paint.Op{
		Kind: paint.Stamp, Layer: 0, From: off, To: off, // nothing in hand either
	})
	if err == nil {
		t.Fatal("accepted")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Errorf("refused with %q, want the cell being off the map before the empty hand", err)
	}
}

func TestApply_RefusesAStampWithNothingInHand(t *testing.T) {
	at := paint.Cell{X: 0, Y: 0}
	_, err := paint.Apply(mapOf(), paint.Op{Kind: paint.Stamp, Layer: 0, From: at, To: at})
	if err == nil {
		t.Fatal("a stamp with no tile in hand was accepted")
	}
	if !strings.Contains(err.Error(), "no tile in hand") {
		t.Errorf("refused with %q", err)
	}
	// Erase is the one tool that means something with an empty hand.
	if _, err := paint.Apply(mapOf(), paint.Op{Kind: paint.Erase, Layer: 0, From: at, To: at}); err != nil {
		t.Errorf("erase with nothing in hand was refused: %v", err)
	}
}
