package tilesets

import (
	"path/filepath"

	"github.com/tmbritton/ecs-db/internal/forge/editable"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func Codec(path string) editable.Codec[*tiled.TilesetDocument] {
	return editable.Codec[*tiled.TilesetDocument]{
		Marshal: func(d *tiled.TilesetDocument) ([]byte, error) { return d.Bytes(), nil },
		Unmarshal: func(data []byte) (*tiled.TilesetDocument, error) {
			return tiled.ParseTilesetDocument(data, filepath.Base(path), filepath.Dir(path))
		},
		Validate: func(d *tiled.TilesetDocument) error {
			_, err := d.Tileset()
			return err
		},
	}
}
