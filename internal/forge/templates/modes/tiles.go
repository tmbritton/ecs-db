package modes

import (
	"path/filepath"
	"strconv"

	"github.com/tmbritton/ecs-db/internal/forge/tilesets"
)

// tilesetRowTestID keeps rows addressable when separate maps reference files
// with the same basename in different directories.
func tilesetRowTestID(entries []tilesets.Entry, path string) string {
	base := filepath.Base(path)
	n := 0
	for _, entry := range entries {
		if filepath.Base(entry.Path) == base {
			n++
		}
		if entry.Path == path {
			break
		}
	}
	if n > 1 {
		return "tileset-" + base + "-" + strconv.Itoa(n)
	}
	return "tileset-" + base
}
