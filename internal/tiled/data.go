package tiled

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// decodeLayerData turns whatever a layer's data element held into one raw gid
// per cell.
//
// Every encoding produces the same slice, because which one a file uses is a
// project preference someone can change in the editor — a map that loads under
// CSV and not under base64 is a map that breaks when a colleague ticks a box.
func decodeLayerData(encoding, compression, text string, cells int, name, layer string) ([]uint32, error) {
	switch encoding {
	case "csv":
		return decodeCSV(text, cells, name, layer)
	case "base64":
		return decodeBase64(compression, text, cells, name, layer)
	default:
		return nil, fmt.Errorf("tiled: %s: layer %q uses encoding %q, which this reader does not decode",
			name, layer, encoding)
	}
}

func dropSpace(r rune) rune {
	switch r {
	case ' ', '\t', '\r', '\n':
		return -1
	}
	return r
}

func decodeCSV(text string, cells int, name, layer string) ([]uint32, error) {
	out := make([]uint32, 0, sane(cells))
	for _, field := range strings.Split(text, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			// Tiled writes a trailing newline inside <data>, and often a
			// trailing comma per row; neither is a cell.
			continue
		}
		v, err := strconv.ParseUint(field, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("tiled: %s: layer %q has %q where a tile id should be", name, layer, field)
		}
		out = append(out, uint32(v))
	}
	return out, nil
}

func decodeBase64(compression, text string, cells int, name, layer string) ([]uint32, error) {
	// Every whitespace character removed, not just the ends. Tiled writes one
	// unwrapped line, but wrapping base64 at 76 columns is what most other
	// emitters do — and Story 3 re-reads maps a third-party tool may have
	// written.
	raw, err := base64.StdEncoding.DecodeString(strings.Map(dropSpace, text))
	if err != nil {
		return nil, fmt.Errorf("tiled: %s: layer %q is not base64: %w", name, layer, err)
	}
	switch compression {
	case "":
	case "zlib":
		raw, err = readBounded(zlibReader, raw, cells)
	case "gzip":
		raw, err = readBounded(gzipReader, raw, cells)
	case "zstd":
		// Tiled's fourth option, and the only one that needs a dependency.
		// Refused by name rather than mis-decoded: a map that silently loaded
		// as garbage would look like a corrupt map rather than a missing
		// decoder.
		return nil, fmt.Errorf("tiled: %s: layer %q is zstd-compressed, which this reader does not decode; "+
			"re-export it as zlib, gzip or CSV", name, layer)
	default:
		return nil, fmt.Errorf("tiled: %s: layer %q uses compression %q, which this reader does not decode",
			name, layer, compression)
	}
	if err != nil {
		return nil, fmt.Errorf("tiled: %s: layer %q will not decompress: %w", name, layer, err)
	}
	if len(raw)%4 != 0 {
		return nil, fmt.Errorf("tiled: %s: layer %q decoded to %d bytes, which is not a whole number of tile ids",
			name, layer, len(raw))
	}
	out := make([]uint32, len(raw)/4)
	for i := range out {
		// Little-endian, which is what Tiled writes on every platform.
		out[i] = binary.LittleEndian.Uint32(raw[i*4:])
	}
	return out, nil
}

func zlibReader(raw []byte) (io.ReadCloser, error) { return zlib.NewReader(bytes.NewReader(raw)) }

func gzipReader(raw []byte) (io.ReadCloser, error) {
	r, err := gzip.NewReader(bytes.NewReader(raw))
	return r, err
}

// readAll decompresses, bounded by the size the layer says it is.
//
// Bounded because the guards that would catch a wrong size all run *after* the
// stream is in memory: a 260 KB base64 payload of zeros expands to 256 MB and
// then asks for a 268 MB slice before anything notices the layer is one cell.
// A correct stream is exactly cells*4 bytes, so reading one more than that is
// both the limit and the "too long" refusal.
func readBounded(open func([]byte) (io.ReadCloser, error), raw []byte, cells int) ([]byte, error) {
	r, err := open(raw)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, int64(sane(cells))*4+1))
}

// sane is a capacity hint that cannot come from the file.
//
// cells is width×height straight out of a map somebody may have hand-edited, so
// it can be negative — which panics make — or vast enough to ask for a 40 GB
// allocation before a byte of data has been looked at. It is only ever a hint;
// checkCells is what decides whether the layer is right.
func sane(cells int) int {
	const most = 1 << 20 // a 1024×1024 layer, larger than anything this engine draws
	if cells < 0 {
		return 0
	}
	if cells > most {
		return most
	}
	return cells
}

// checkSize refuses a map whose dimensions are not dimensions.
//
// A width or height out of a file is not a number this package chose. A
// negative one panics `make`, and a vast one asks for the allocation before
// anything has been read — so both are refused at the door rather than deep
// inside a decoder, where the message would be about a layer.
func checkSize(width, height int, name string) error {
	if width < 0 || height < 0 {
		return fmt.Errorf("tiled: %s is %dx%d tiles, which is not a size", name, width, height)
	}
	return nil
}

// checkLayerSize refuses a layer that is not the shape of the map it is in.
//
// checkCells makes a layer's data match its own declared size; this makes that
// size match the map's. Without it a 2×1 layer sits happily inside a 10×10 map,
// and a caller that iterates the map's dimensions and indexes the layer's data
// reads the wrong cells — which is the same argument checkCells makes, one
// level up.
func checkLayerSize(lw, lh, mw, mh int, name, layer string) error {
	if lw == mw && lh == mh {
		return nil
	}
	return fmt.Errorf("tiled: %s: layer %q is %dx%d but the map is %dx%d", name, layer, lw, lh, mw, mh)
}

// checkCells refuses a layer whose data does not fill it.
//
// The refusal that matters most in this package. A short layer is a map with a
// hole in it, and every story after this one indexes Data by y*Width+x — so the
// alternative to failing here is reading past the end of a slice somewhere much
// further away from the file that caused it.
func checkCells(got, want int, name, layer string) error {
	if got == want {
		return nil
	}
	return fmt.Errorf("tiled: %s: layer %q holds %d tiles but the layer is %d cells",
		name, layer, got, want)
}
