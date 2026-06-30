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

// writePlayerSheet generates a 5-frame sheet:
//
//	0: square (idle)
//	1: triangle right
//	2: triangle down
//	3: triangle left
//	4: triangle up
func writePlayerSheet(path string) {
	triColor := color.RGBA{R: 0, G: 200, B: 255, A: 255}
	bg := color.RGBA{R: 30, G: 30, B: 30, A: 255}

	img := image.NewRGBA(image.Rect(0, 0, 5*tileSize, tileSize))
	for y := range tileSize {
		for x := range 5 * tileSize {
			img.Set(x, y, bg)
		}
	}

	pad := 4

	// Frame 0: filled square (idle).
	for y := pad; y < tileSize-pad; y++ {
		for x := pad; x < tileSize-pad; x++ {
			img.Set(x, y, triColor)
		}
	}

	mid := tileSize / 2
	type tri struct{ ax, ay, bx, by, cx, cy int }
	triangles := []tri{
		{pad, pad, tileSize - pad, mid, pad, tileSize - pad},            // right (frame 1)
		{pad, pad, tileSize - pad, pad, mid, tileSize - pad},            // down  (frame 2)
		{tileSize - pad, pad, pad, mid, tileSize - pad, tileSize - pad}, // left  (frame 3)
		{pad, tileSize - pad, mid, pad, tileSize - pad, tileSize - pad}, // up    (frame 4)
	}

	for i, t := range triangles {
		ox := (i + 1) * tileSize // frames 1–4
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

// writeGoblinSheet generates a 5-frame sheet:
//
//	0: filled square (idle)
//	1–4: solid green with stripe markers (walk)
func writeGoblinSheet(path string) {
	base := color.RGBA{R: 80, G: 180, B: 80, A: 255}
	bg := color.RGBA{R: 30, G: 30, B: 30, A: 255}
	img := image.NewRGBA(image.Rect(0, 0, 5*tileSize, tileSize))
	for y := range tileSize {
		for x := range 5 * tileSize {
			img.Set(x, y, bg)
		}
	}

	pad := 4

	// Frame 0: filled square (idle).
	for y := pad; y < tileSize-pad; y++ {
		for x := pad; x < tileSize-pad; x++ {
			img.Set(x, y, base)
		}
	}

	// Frames 1–4: solid green fill + stripe marker.
	stripe := color.RGBA{R: 0, G: 0, B: 0, A: 120}
	for f := 1; f < 5; f++ {
		ox := f * tileSize
		for y := range tileSize {
			for x := range tileSize {
				img.Set(ox+x, y, base)
			}
		}
		sx := ox + 4 + (f-1)*6
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
