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
	writeSheet("mods/assets/sprites/player.png", color.RGBA{R: 0, G: 200, B: 255, A: 255})
	writeSheet("mods/assets/sprites/goblin.png", color.RGBA{R: 80, G: 180, B: 80, A: 255})
}

func writeSheet(path string, base color.RGBA) {
	const (
		frames   = 5
		tileSize = 32
	)
	img := image.NewRGBA(image.Rect(0, 0, frames*tileSize, tileSize))

	for y := 0; y < tileSize; y++ {
		for x := 0; x < frames*tileSize; x++ {
			img.Set(x, y, base)
		}
	}

	// Frames 1-4: a dark 2-pixel stripe at a distinct x offset per frame.
	stripe := color.RGBA{R: 0, G: 0, B: 0, A: 120}
	for f := 1; f < frames; f++ {
		sx := f*tileSize + 4 + (f-1)*6
		for y := 4; y < tileSize-4; y++ {
			img.Set(sx, y, stripe)
			img.Set(sx+1, y, stripe)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}
