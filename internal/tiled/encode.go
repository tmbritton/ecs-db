package tiled

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strconv"
)

// encodeLayerData renders a layer's tile ids as the text its <data> element
// holds, in the encoding the file already used.
//
// **The encoding is an argument rather than a choice.** Which one a project uses
// is a setting somebody ticked in Tiled, and a save that decided for itself
// would turn a one-cell edit into a whole-file diff nobody can review — and
// would do it to a colleague's repository, not the author's.
//
// It is the exact inverse of decodeLayerData, and the tests pair them: every
// encoding is written and read back. What it cannot write, it refuses by name,
// for decodeLayerData's reason — a layer silently written in a different
// encoding from the one it says it uses is a corrupt map, and it would be
// discovered by the engine rather than here.
func encodeLayerData(encoding, compression string, gids []uint32, width int) (string, error) {
	switch encoding {
	case "csv":
		if compression != "" {
			return "", fmt.Errorf("tiled: csv data cannot be %s-compressed", compression)
		}
		return encodeCSV(gids, width), nil
	case "base64":
		return encodeBase64(compression, gids)
	case "":
		// One <tile> element per cell. Real, and not text — see
		// Document.SetLayerData, which writes the elements itself.
		return "", fmt.Errorf("tiled: a layer with no encoding attribute holds <tile> elements, not text")
	default:
		return "", fmt.Errorf("tiled: layer data cannot be written as %q", encoding)
	}
}

// encodeCSV writes one map row per line, which is what Tiled writes.
//
// The shape is load-bearing rather than cosmetic. A map's data is most of its
// file, and a version-controlled level whose rows run together is a level whose
// every future change shows as one modified line — so the format that makes a
// wall visible in a diff is worth reproducing exactly, down to the row-ending
// comma and the newline before the closing tag.
//
// A width that does not divide the data is not this function's to refuse: it
// writes every id it was given, one row short at the end, and checkCells is
// what reports the mismatch when the file is read back.
func encodeCSV(gids []uint32, width int) string {
	if len(gids) == 0 {
		return "\n"
	}
	if width <= 0 {
		width = len(gids)
	}
	var buf bytes.Buffer
	buf.WriteByte('\n')
	for i, gid := range gids {
		buf.WriteString(strconv.FormatUint(uint64(gid), 10))
		switch {
		case i == len(gids)-1:
			// No trailing comma on the very last cell, and no trailing comma
			// on the row it ends.
		case (i+1)%width == 0:
			buf.WriteString(",\n")
		default:
			buf.WriteByte(',')
		}
	}
	buf.WriteByte('\n')
	return buf.String()
}

func encodeBase64(compression string, gids []uint32) (string, error) {
	raw := make([]byte, 4*len(gids))
	for i, gid := range gids {
		// Little-endian, which is what Tiled writes on every platform.
		binary.LittleEndian.PutUint32(raw[i*4:], gid)
	}
	var buf bytes.Buffer
	switch compression {
	case "":
		buf.Write(raw)
	case "zlib":
		w := zlib.NewWriter(&buf)
		if _, err := w.Write(raw); err != nil {
			return "", err
		}
		if err := w.Close(); err != nil {
			return "", err
		}
	case "gzip":
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(raw); err != nil {
			return "", err
		}
		if err := w.Close(); err != nil {
			return "", err
		}
	case "zstd":
		// The one Tiled offers that this package does not read either, and for
		// the same reason: it needs a dependency. Refused rather than written
		// as something else, so a map that arrived zstd-compressed stays
		// unwritable instead of silently changing format on its author.
		return "", fmt.Errorf("tiled: layer data cannot be written zstd-compressed; " +
			"re-export the map as zlib, gzip or CSV")
	default:
		return "", fmt.Errorf("tiled: layer data cannot be written %s-compressed", compression)
	}
	// One unwrapped line, which is what Tiled writes.
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
