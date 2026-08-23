package tiled_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"strconv"
)

// tmxWith is a 4×1 map whose one layer holds whatever data element is given, so
// a test of an encoding is a test of that encoding and nothing else.
func tmxWith(data string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" width="4" height="1" tilewidth="8" tileheight="8">
 <layer id="1" name="l" width="4" height="1">
  ` + data + `
 </layer>
</map>`
}

// b64 encodes gids the way Tiled does: little-endian uint32s, optionally
// compressed, then base64. Written here rather than asserted as a literal so a
// test says what it means instead of holding an opaque string.
func b64(gids []uint32, compression string) string {
	raw := make([]byte, 4*len(gids))
	for i, g := range gids {
		binary.LittleEndian.PutUint32(raw[i*4:], g)
	}
	var buf bytes.Buffer
	switch compression {
	case "":
		buf.Write(raw)
	case "zlib":
		w := zlib.NewWriter(&buf)
		_, _ = w.Write(raw)
		_ = w.Close()
	case "gzip":
		w := gzip.NewWriter(&buf)
		_, _ = w.Write(raw)
		_ = w.Close()
	default:
		panic("unknown compression " + compression)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func itoa(v uint32) string { return strconv.FormatUint(uint64(v), 10) }
