package animations

import (
	"fmt"
	"image"
	_ "image/png"
	"math"
	"os"
)

// Preview is the first-row, square-frame slice the renderer can actually play.
// Frames retains the authored sequence; repeating a column repeats its image.
type Preview struct {
	Name, Sheet, Image string
	Columns, TileSize  int
	Frames             []int
	FPS                float64
	Loop               bool
	Problem            string
}

func (s *Session) Preview(name string, tileSize int) Preview {
	view := Preview{Name: name, TileSize: tileSize}
	err := s.Read(func(doc *Document) error {
		for _, def := range doc.Animations() {
			if def.Name == name {
				view.Sheet, view.Frames, view.FPS, view.Loop = def.Sheet, def.Frames, def.FPS, def.Loop
				return nil
			}
		}
		return fmt.Errorf("animation %q is not in animations.toml", name)
	})
	if err != nil {
		view.Problem = err.Error()
		return view
	}
	return s.preview(view)
}

// PreviewCandidate checks fields before they become draft TOML. A missing
// image is reportable but may be authored ahead of importing it in Story 11.
func (s *Session) PreviewCandidate(sheet string, frames []int, fps float64, loop bool, tileSize int) Preview {
	return s.preview(Preview{Sheet: sheet, Frames: frames, FPS: fps, Loop: loop, TileSize: tileSize})
}

// BindingPreviewCandidate checks the renderer's crop bounds on a bound entity
// sheet. Existing tall sheets can play row-zero frames even though SPRT only
// authors one-row strips for new animations.
func (s *Session) BindingPreviewCandidate(sheet string, frames []int, fps float64, loop bool, tileSize int) Preview {
	return s.previewWithRows(Preview{Sheet: sheet, Frames: frames, FPS: fps, Loop: loop, TileSize: tileSize}, false)
}

func (s *Session) preview(view Preview) Preview {
	return s.previewWithRows(view, true)
}

func (s *Session) previewWithRows(view Preview, oneRow bool) Preview {
	tileSize := view.TileSize
	if tileSize <= 0 {
		view.Problem = "window.tileSize must be positive to preview square sprite frames"
		return view
	}
	if view.Sheet == "" {
		view.Problem = "animation needs a sprite sheet"
		return view
	}
	if err := s.ValidateSheet(view.Sheet); err != nil {
		view.Problem = err.Error()
		return view
	}
	if view.FPS <= 0 || math.IsNaN(view.FPS) || math.IsInf(view.FPS, 0) {
		view.Problem = "animation needs a positive finite fps"
		return view
	}
	if len(view.Frames) == 0 {
		view.Problem = "animation needs at least one frame"
		return view
	}
	path, err := s.AssetImage(view.Sheet)
	if err != nil {
		view.Problem = err.Error()
		return view
	}
	view.Image = path
	f, err := os.Open(path)
	if err != nil {
		view.Problem = err.Error()
		return view
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		view.Problem = fmt.Sprintf("sprite sheet %q cannot be decoded: %v", view.Sheet, err)
		return view
	}
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	if oneRow && height != tileSize {
		view.Problem = fmt.Sprintf("sprite sheet must be one row of %d-pixel square frames; height is %d", tileSize, height)
		return view
	}
	if !oneRow && height < tileSize {
		view.Problem = fmt.Sprintf("bound sheet height %d is shorter than a %d-pixel first row", height, tileSize)
		return view
	}
	if width == 0 || oneRow && width%tileSize != 0 {
		view.Problem = fmt.Sprintf("sprite sheet width %d must be divisible by tileSize %d", width, tileSize)
		return view
	}
	view.Columns = width / tileSize
	for _, column := range view.Frames {
		if column < 0 || column >= view.Columns {
			view.Problem = fmt.Sprintf("frame column %d is outside this strip's %d columns", column, view.Columns)
			return view
		}
	}
	return view
}
