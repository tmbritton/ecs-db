// Command starter-tileset writes mods/map/starter.png, the two-tile sheet
// mods/map/starter.tsx describes.
//
// A checked-in PNG is a binary nobody can read a diff of, so the thing that
// made it is checked in beside it. The two greys are the ones
// TilemapRenderer filled rectangles with before Epic 14 story 5 — 60 for a
// wall, 180 for a floor — so the migrated map looks like the map it replaces
// rather than like a new one.
//
// Run from the repository root:
//
//	go run ./scripts/starter-tileset
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

const (
	tile = 32
	// Tile 0 is floor and tile 1 is wall, which is the order starter.tsx
	// declares and the order the CSV in level1.tmx counts on.
	tiles = 2
)

var (
	floor     = color.RGBA{180, 180, 180, 255}
	floorEdge = color.RGBA{160, 160, 160, 255}
	wall      = color.RGBA{60, 60, 60, 255}
	wallTop   = color.RGBA{78, 78, 78, 255}
	wallFoot  = color.RGBA{44, 44, 44, 255}
)

func main() {
	img := image.NewRGBA(image.Rect(0, 0, tile*tiles, tile))

	for y := 0; y < tile; y++ {
		for x := 0; x < tile; x++ {
			// Floor: flat, with the last row and column darkened so the grid is
			// visible without drawing one. A floor with no seam at all reads as
			// a single expanse and hides how big a step is.
			c := floor
			if x == tile-1 || y == tile-1 {
				c = floorEdge
			}
			img.Set(x, y, c)

			// Wall: a lit top edge and a shadowed foot, which is the whole of
			// the illusion that it stands up.
			c = wall
			switch {
			case y < 2:
				c = wallTop
			case y >= tile-3:
				c = wallFoot
			}
			img.Set(tile+x, y, c)
		}
	}

	f, err := os.Create("mods/map/starter.png")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
