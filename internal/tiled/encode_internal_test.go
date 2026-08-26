package tiled

import (
	"os"
	"strings"
	"testing"
)

func TestEncodeLayerData_RoundTripsThroughTheDecoder(t *testing.T) {
	gids := []uint32{1, 2, 3, 4, 0, 6, 7, 0x80000005}
	for _, tc := range []struct{ encoding, compression string }{
		{"csv", ""},
		{"base64", ""},
		{"base64", "zlib"},
		{"base64", "gzip"},
	} {
		t.Run(tc.encoding+" "+tc.compression, func(t *testing.T) {
			text, err := encodeLayerData(tc.encoding, tc.compression, gids, 4)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			back, err := decodeLayerData(tc.encoding, tc.compression, text, len(gids), "m.tmx", "l")
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(back) != len(gids) {
				t.Fatalf("got %d gids, want %d", len(back), len(gids))
			}
			for i := range gids {
				if back[i] != gids[i] {
					t.Fatalf("cell %d came back as %d, want %d", i, back[i], gids[i])
				}
			}
		})
	}
}

// The shape matters as much as the values: Tiled writes CSV one map row per
// line, so a save that ran it together would turn every future diff of the
// file into one enormous line.
func TestEncodeLayerData_CSVIsWrittenTheWayTiledWritesIt(t *testing.T) {
	got, err := encodeLayerData("csv", "", []uint32{1, 2, 3, 4, 5, 6}, 3)
	if err != nil {
		t.Fatal(err)
	}
	const want = "\n1,2,3,\n4,5,6\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The strongest form of the claim: the layer the repository ships re-encodes to
// exactly the bytes that are in the file.
func TestEncodeLayerData_ReproducesTheShippedLevelExactly(t *testing.T) {
	raw, err := os.ReadFile("../../mods/map/level1.tmx")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := parseTree(raw)
	if err != nil {
		t.Fatal(err)
	}
	layer := tree.root.firstChild("layer")
	data := layer.firstChild("data")
	original := string(data.kids[0].raw)

	gids, err := decodeLayerData("csv", "", original, 20*15, "level1.tmx", "ground")
	if err != nil {
		t.Fatal(err)
	}
	got, err := encodeLayerData("csv", "", gids, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got != original {
		t.Errorf("re-encoding the shipped layer changed it\n got: %q\nwant: %q", got, original)
	}
}

func TestEncodeLayerData_RefusesWhatItCannotWrite(t *testing.T) {
	for _, tc := range []struct{ name, encoding, compression, want string }{
		{"zstd", "base64", "zstd", "zstd"},
		{"an encoding nobody has", "runes", "", "runes"},
		{"a compression nobody has", "base64", "brotli", "brotli"},
		{"the element form has no text", "", "", "encoding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := encodeLayerData(tc.encoding, tc.compression, []uint32{1}, 1)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestEncodeLayerData_AWidthThatDoesNotDivideIsStillWritten(t *testing.T) {
	// A caller passing a width the data does not fit is a bug above here, and
	// the encoder's job is to not lose a cell over it: every gid comes out, and
	// the decoder's own cell check is what reports the mismatch.
	got, err := encodeLayerData("csv", "", []uint32{1, 2, 3, 4, 5}, 3)
	if err != nil {
		t.Fatal(err)
	}
	back, err := decodeLayerData("csv", "", got, 5, "m", "l")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 5 {
		t.Errorf("got %d gids back, want 5 (%q)", len(back), got)
	}
}
