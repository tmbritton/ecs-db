package modes

import (
	"strconv"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/paint"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

// The tables are generated from paint.RotateCW, so what is worth checking is
// the part that is not generated: that the key the browser computes indexes
// them the way the generator assumed. Getting that backwards would still
// produce a rotation — just a different one from the server's — and only a
// tile pointing the wrong way on screen would say so.
func TestTurnTables_AreIndexedTheWayTheBrowserComputesTheKey(t *testing.T) {
	d, h, v := turnTables()
	dv, hv, vv := parseBools(t, d), parseBools(t, h), parseBools(t, v)

	for key := 0; key < 8; key++ {
		// The same arithmetic turnKey emits: D is the 4s bit, H the 2s, V the 1s.
		before := tiled.Tile{GID: 1, FlipD: key&4 != 0, FlipH: key&2 != 0, FlipV: key&1 != 0}
		want := paint.RotateCW(before)
		got := tiled.Tile{GID: 1, FlipD: dv[key], FlipH: hv[key], FlipV: vv[key]}
		if got.FlipD != want.FlipD || got.FlipH != want.FlipH || got.FlipV != want.FlipV {
			t.Errorf("turning %+v: the browser writes D=%v H=%v V=%v, paint.RotateCW gives D=%v H=%v V=%v",
				before, got.FlipD, got.FlipH, got.FlipV, want.FlipD, want.FlipH, want.FlipV)
		}
	}
}

// The bit weights in turnKey are the other half of that contract.
func TestTurnKey_WeightsTheFlagsAsTheTablesExpect(t *testing.T) {
	for _, want := range []string{"$flipD?4:0", "$flipH?2:0", "$flipV?1:0"} {
		if !strings.Contains(turnKey, want) {
			t.Errorf("turnKey is %q, which does not weight the flags as %q", turnKey, want)
		}
	}
}

// Four quarter turns is where it started — the property that makes the button
// safe to hold down, and the one an off-by-one in the tables breaks.
func TestTurnTables_FourTurnsIsIdentity(t *testing.T) {
	d, h, v := turnTables()
	dv, hv, vv := parseBools(t, d), parseBools(t, h), parseBools(t, v)

	// From upright, which is the state the signals start in.
	key := 0
	for range 4 {
		key = bit(dv[key], 4) + bit(hv[key], 2) + bit(vv[key], 1)
	}
	if key != 0 {
		t.Errorf("four quarter turns from upright lands on key %d, not back at upright", key)
	}
}

// A label for every combination the buttons can reach. A missing one renders
// "stamp undefined", which is the sort of thing only a screenshot catches.
func TestTurnLabels_NameEveryCombination(t *testing.T) {
	labels := turnLabelList()
	if len(labels) != 8 {
		t.Fatalf("turnLabelList has %d entries, want one per flag combination", len(labels))
	}
	seen := map[string]bool{}
	for i, l := range labels {
		if l == "" {
			t.Errorf("combination %d has no label", i)
		}
		if seen[l] {
			t.Errorf("%q labels two different orientations", l)
		}
		seen[l] = true
	}
}

// Every signal the toolbar reads has to be declared, or Datastar invents it as
// "" — which is falsy, so a missing $tool would silently paint nothing and a
// missing flag would read as upright. This has bitten the layer signals once.
func TestMapSignals_DeclareWhatTheToolbarReads(t *testing.T) {
	declared := MapSignals(mapRegionFixture())
	for _, signal := range []string{`"tool"`, `"flipH"`, `"flipV"`, `"flipD"`, `"tile"`, `"layer"`, `"zoom"`} {
		if !strings.Contains(declared, signal) {
			t.Errorf("MapSignals does not declare %s: %s", signal, declared)
		}
	}
	// The tool has to start at something paint accepts, not at "".
	if !strings.Contains(declared, `"tool":"`+string(paint.Stamp)+`"`) {
		t.Errorf("MapSignals does not start the tool on %q: %s", paint.Stamp, declared)
	}
}

// Painting tools must be accepted by paint; inspect selects a Tile without
// posting a paint operation.
func TestTools_AreKindsPaintKnows(t *testing.T) {
	for _, tool := range Tools() {
		t.Run(tool.Kind, func(t *testing.T) {
			if tool.Kind == "inspect" {
				if action := strokeAction(mapRegionFixture()); !strings.Contains(action, "$tool === 'inspect' ?") || !strings.Contains(action, "window.location.assign(") || !strings.Contains(action, "sessionStorage.setItem(") {
					t.Fatal("inspect tool cannot select a Tile")
				}
				return
			}
			switch paint.Kind(tool.Kind) {
			case paint.Stamp, paint.Fill, paint.Erase:
			default:
				t.Errorf("the toolbar offers %q, which paint.Apply refuses", tool.Kind)
			}
			if tool.Glyph == "" || tool.Title == "" {
				t.Errorf("%q has no glyph or no title: %+v", tool.Kind, tool)
			}
		})
	}
}

func parseBools(t *testing.T, list string) []bool {
	t.Helper()
	out := []bool{}
	for _, f := range strings.Split(strings.Trim(list, "[]"), ",") {
		b, err := strconv.ParseBool(f)
		if err != nil {
			t.Fatalf("parsing %q from %s: %v", f, list, err)
		}
		out = append(out, b)
	}
	if len(out) != 8 {
		t.Fatalf("%s has %d entries, want 8", list, len(out))
	}
	return out
}

func bit(on bool, weight int) int {
	if on {
		return weight
	}
	return 0
}

// The Datastar guard scans a rendered MAP page for the dash-form attributes
// that render perfectly and do nothing. The toolbar is inside a Drawn() branch,
// so if the shared fixture ever stops drawing, the guard would still pass while
// scanning a page with no toolbar on it at all.
func TestMapRegionFixture_DrawsSoTheDatastarGuardSeesTheToolbar(t *testing.T) {
	if !mapRegionFixture().Canvas.Drawn() {
		t.Fatal("the fixture no longer draws, so the toolbar is not in what the guard scans")
	}
	markup := render(t, Render("map", mapRegionFixture()))
	// Everything 15-painting.spec.js selects on. AGENTS asks for the pin so a
	// rename fails here in seconds rather than as a browser timeout.
	for _, id := range []string{
		"tool-control", "turn-control", "map-tools-note",
		"tool-stamp", "tool-fill", "tool-erase",
		"turn-cw", "flip-h", "flip-v",
	} {
		if !strings.Contains(markup, `data-testid="`+id+`"`) {
			t.Errorf("%s is not in the markup the Datastar guard scans", id)
		}
	}
}

// Each label has to name the orientation it is indexed by. Counting eight
// distinct strings is what the first version of this checked, and it passed
// with two of them swapped.
func TestTurnLabels_NameTheOrientationTheyAreIndexedBy(t *testing.T) {
	labels := turnLabelList()
	// Walk the rotation cycle from upright and check the angle each label
	// claims is the number of quarter turns it actually took to get there.
	for _, start := range []tiled.Tile{{GID: 1}, {GID: 1, FlipH: true}} {
		at := start
		for turns, want := range []string{"", "90°", "180°", "270°"} {
			got := labels[turnKeyOf(at)]
			if want == "" && strings.ContainsAny(got, "0123456789") {
				t.Errorf("%d turns from %+v is labelled %q, which claims an angle", turns, start, got)
			}
			if want != "" && !strings.Contains(got, want) {
				t.Errorf("%d turns from %+v is labelled %q, want it to say %q", turns, start, got, want)
			}
			mirrored := strings.Contains(got, "mirrored")
			if mirrored != start.FlipH {
				t.Errorf("%q %s says mirrored, and the orientation %+v is not (or the reverse)",
					got, map[bool]string{true: "", false: "does not"}[mirrored], at)
			}
			at = paint.RotateCW(at)
		}
	}
}

// turnKeyOf has to agree with the arithmetic turnKey emits, or every label and
// every table entry is indexed by one thing and read by another.
func TestTurnKeyOf_MatchesTheKeyTheBrowserComputes(t *testing.T) {
	for _, tc := range []struct {
		tile tiled.Tile
		want int
	}{
		{tiled.Tile{}, 0},
		{tiled.Tile{FlipV: true}, 1},
		{tiled.Tile{FlipH: true}, 2},
		{tiled.Tile{FlipD: true}, 4},
		{tiled.Tile{FlipD: true, FlipH: true, FlipV: true}, 7},
	} {
		if got := turnKeyOf(tc.tile); got != tc.want {
			t.Errorf("turnKeyOf(%+v) = %d, want %d", tc.tile, got, tc.want)
		}
	}
}

// The seed is what the note says before Datastar runs, so it has to be the
// label for the orientation the signals start in — upright.
func TestTurnLabelSeed_MatchesWhatTheSignalsStartIn(t *testing.T) {
	seed := turnLabelSeed()
	if !strings.HasPrefix(seed, "stamp ") {
		t.Fatalf("the seed reads %q, which does not match the expression's shape", seed)
	}
	want := "stamp " + turnLabelList()[turnKeyOf(tiled.Tile{})]
	if seed != want {
		t.Errorf("the seed reads %q, but an unturned stamp is %q", seed, want)
	}
	// And it is in the markup, not an empty element waiting to be filled in.
	markup := render(t, Render("map", mapRegionFixture()))
	if !strings.Contains(markup, ">"+want+"<") {
		t.Errorf("the note renders empty; %q is not in the markup", want)
	}
}

// The list becomes a JavaScript array literal, so it has to come back out of
// that literal unchanged. Scanned the way a JavaScript parser would rather than
// matched with a regexp: a stray unescaped quote just makes a regexp
// re-synchronise on the next one, so the first version of this test passed with
// quoteJS bypassed entirely and a label containing an apostrophe.
func TestTurnLabels_SurviveTheTripIntoAJavaScriptLiteral(t *testing.T) {
	got := scanJSStrings(t, turnLabels())
	want := turnLabelList()
	if len(got) != len(want) {
		t.Fatalf("the literal %s holds %d strings, but there are %d labels", turnLabels(), len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("label %d is %q in the list and %q in the literal", i, want[i], got[i])
		}
	}
}

// scanJSStrings reads a `['a','b',…]` literal the way a parser does: a quote
// opens a string, a backslash escapes whatever follows it, and the next
// unescaped quote closes it. Anything outside a string that is not a comma or
// bracket is a syntax error, which is how a bypassed escape shows up.
func scanJSStrings(t *testing.T, literal string) []string {
	t.Helper()
	if len(literal) < 2 || literal[0] != '[' || literal[len(literal)-1] != ']' {
		t.Fatalf("not an array literal: %s", literal)
	}
	var out []string
	var cur strings.Builder
	inString, escaped := false, false
	pending, code := 0, 0 // digits still to read of a \xNN or \uNNNN escape
	for _, r := range literal[1 : len(literal)-1] {
		if pending > 0 {
			n, err := strconv.ParseUint(string(r), 16, 8)
			if err != nil {
				t.Fatalf("%q is not a hex digit in %s", r, literal)
			}
			code = code<<4 | int(n)
			if pending--; pending == 0 {
				cur.WriteRune(rune(code))
				code = 0
			}
			continue
		}
		switch {
		case escaped:
			escaped = false
			// Decoded, not just unbackslashed: quoteJS writes \n, \r, \xNN and
			// \uNNNN, and a scanner that dropped the backslash would report
			// "\x40" as the four characters x, 4, 0 rather than as "@".
			switch r {
			case 'n':
				cur.WriteByte('\n')
			case 'r':
				cur.WriteByte('\r')
			case 'x':
				pending = 2
			case 'u':
				pending = 4
			default:
				cur.WriteRune(r)
			}
		case inString && r == '\\':
			escaped = true
		case inString && r == '\'':
			out = append(out, cur.String())
			cur.Reset()
			inString = false
		case inString:
			cur.WriteRune(r)
		case r == '\'':
			inString = true
		case r == ',':
		default:
			t.Fatalf("%q sits outside a string in %s, so the literal is malformed", r, literal)
		}
	}
	if inString {
		t.Fatalf("a string is left open in %s", literal)
	}
	return out
}

// The escaping, with strings that need it. The orientation labels do not, so
// this is the only place the quoting is actually exercised — and a label
// carrying an apostrophe would otherwise close its own string and turn the rest
// of the expression into syntax, which Datastar swallows silently.
func TestJSStringArray_KeepsEachStringWhole(t *testing.T) {
	awkward := []string{
		"upright",
		"it's turned",         // closes the string
		`back\slash`,          // escapes the next character
		"a,comma",             // splits an element
		"@post('/somewhere')", // Datastar rewrites @name( even inside a literal
		"two\nlines",
	}
	got := scanJSStrings(t, jsStringArray(awkward))
	if len(got) != len(awkward) {
		t.Fatalf("%d strings went in and %d came out: %s", len(awkward), len(got), jsStringArray(awkward))
	}
	for i := range awkward {
		if got[i] != awkward[i] {
			t.Errorf("element %d went in as %q and came out as %q", i, awkward[i], got[i])
		}
	}
	// The @ has to leave as an escape rather than as itself.
	if strings.Contains(jsStringArray([]string{"@post(x)"}), "@") {
		t.Errorf("an @ survives into the literal: %s", jsStringArray([]string{"@post(x)"}))
	}
}
