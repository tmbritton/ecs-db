// Package mapfile is one editable Tiled map on disk.
//
// It is the seam between tiled.Document, which knows the format and nothing
// about files, and editable.File, which knows about unsaved work and atomic
// writes and nothing about maps. Everything here is the two of them meeting:
// there is no map logic in this package and there should never be any.
//
// The dirty comparison is what makes it worth having its own package rather
// than being three lines inside a session. editable.File decides "unsaved" by
// serialising the working value and comparing it to the bytes on disk, which is
// only meaningful because a Document that has not been edited renders back
// byte-for-byte. That is a real dependency in both directions: if the writer
// ever stops being faithful, it shows up here as a map that is permanently
// unsaved for no visible reason.
package mapfile

import (
	"github.com/tmbritton/ecs-db/internal/forge/editable"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

// Codec is how editable.File reads and writes a map at this path.
//
// Per-path rather than a package-level value, because parsing needs the name:
// every refusal tiled raises names the file it is about, and a codec that did
// not know it would report "this map has a typo" without saying which map.
func Codec(path string) editable.Codec[*tiled.Document] {
	return editable.Codec[*tiled.Document]{
		Marshal: func(d *tiled.Document) ([]byte, error) { return d.Bytes(), nil },
		Unmarshal: func(data []byte) (*tiled.Document, error) {
			return tiled.ParseDocument(data, path)
		},
		// Cheap, because Document memoises its own parse — and worth doing
		// anyway: writing a map the engine will refuse to load helps nobody,
		// and an edit that broke the document is exactly what a save should
		// catch.
		Validate: func(d *tiled.Document) error {
			_, err := d.Map()
			return err
		},
	}
}

// Open reads a map for editing.
func Open(path string) (*editable.File[*tiled.Document], error) {
	return editable.Open(path, Codec(path))
}
