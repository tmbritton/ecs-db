//go:build ignore

package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	if err := os.MkdirAll("mods/assets/sprites", 0o755); err != nil {
		panic(err)
	}
	writePlayerSheet("mods/assets/sprites/player.png")
	writeGoblinSheet("mods/assets/sprites/goblin.png")
}

const tileSize = 32

// writePlayerSheet generates a 4-frame sheet where each frame is a triangle
// pointing in a cardinal direction: right (0), down (1), left (2), up (3).
func writePlayerSheet(path string) {
	triColor := color.RGBA{R: 0, G: 200, B: 255, A: 255}
	bg := color.RGBA{R: 30, G: 30, B: 30, A: 255}

	img := image.NewRGBA(image.Rect(0, 0, 4*tileSize, tileSize))
	for y := range tileSize {
		for x := range 4 * tileSize {
			img.Set(x, y, bg)
		}
	}

	pad := 4
	mid := tileSize / 2
	type tri struct{ ax, ay, bx, by, cx, cy int }
	// Vertices listed so the tip points in each direction.
	triangles := []tri{
		{pad, pad, tileSize - pad, mid, pad, tileSize - pad},            // right
		{pad, pad, tileSize - pad, pad, mid, tileSize - pad},            // down
		{tileSize - pad, pad, pad, mid, tileSize - pad, tileSize - pad}, // left
		{pad, tileSize - pad, mid, pad, tileSize - pad, tileSize - pad}, // up
	}

	for i, t := range triangles {
		ox := i * tileSize
		for y := range tileSize {
			for x := range tileSize {
				if inTriangle(x, y, t.ax, t.ay, t.bx, t.by, t.cx, t.cy) {
					img.Set(ox+x, y, triColor)
				}
			}
		}
	}
	writeImage(path, img)
}

// writeGoblinSheet generates a 5-frame sheet of solid green with stripe markers.
func writeGoblinSheet(path string) {
	base := color.RGBA{R: 80, G: 180, B: 80, A: 255}
	img := image.NewRGBA(image.Rect(0, 0, 5*tileSize, tileSize))
	for y := range tileSize {
		for x := range 5 * tileSize {
			img.Set(x, y, base)
		}
	}
	stripe := color.RGBA{R: 0, G: 0, B: 0, A: 120}
	for f := 1; f < 5; f++ {
		sx := f*tileSize + 4 + (f-1)*6
		for y := 4; y < tileSize-4; y++ {
			img.Set(sx, y, stripe)
			img.Set(sx+1, y, stripe)
		}
	}
	writeImage(path, img)
}

func writeImage(path string, img *image.RGBA) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

func triSign(p1x, p1y, p2x, p2y, p3x, p3y int) int {
	return (p1x-p3x)*(p2y-p3y) - (p2x-p3x)*(p1y-p3y)
}

func inTriangle(px, py, x0, y0, x1, y1, x2, y2 int) bool {
	d1 := triSign(px, py, x0, y0, x1, y1)
	d2 := triSign(px, py, x1, y1, x2, y2)
	d3 := triSign(px, py, x2, y2, x0, y0)
	hasNeg := (d1 < 0) || (d2 < 0) || (d3 < 0)
	hasPos := (d1 > 0) || (d2 > 0) || (d3 > 0)
	return !(hasNeg && hasPos)
}
