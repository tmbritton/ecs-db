package animations

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSession_PreviewUsesFirstRowAndOrderedFrameColumns(t *testing.T) {
	tests := []struct {
		name             string
		width, height    int
		frames           string
		wantColumns      int
		wantProblemMatch string
	}{
		{"valid 3-column strip", 48, 16, "[2, 0, 2]", 3, ""},
		{"out of range frame", 48, 16, "[3]", 3, "column 3"},
		{"second row unsupported", 48, 32, "[0]", 0, "one row"},
		{"incomplete frame", 49, 16, "[0]", 0, "divisible"},
		{"negative frame", 48, 16, "[-1]", 3, "column -1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, path := animationProject(t)
			folder := filepath.Join(cfg.Root, "a", "sprites")
			if err := os.Mkdir(folder, 0o700); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(folder, "strip.png"))
			if err != nil {
				t.Fatal(err)
			}
			img := image.NewRGBA(image.Rect(0, 0, tt.width, tt.height))
			img.Set(0, 0, color.RGBA{R: 255, A: 255})
			if err := png.Encode(f, img); err != nil {
				f.Close()
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			src := strings.Replace(sampleTOML, "frames = [0]", "frames = "+tt.frames, 1)
			src = strings.Replace(src, "sheet = \"sprites/player.png\" # artist choice", "sheet = \"sprites/strip.png\" # artist choice", 1)
			if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			view := Open(cfg).Preview("idle", 16)
			if view.Columns != tt.wantColumns || !strings.Contains(view.Problem, tt.wantProblemMatch) {
				t.Fatalf("preview = %+v; want %d columns, problem containing %q", view, tt.wantColumns, tt.wantProblemMatch)
			}
			if tt.wantProblemMatch == "" && (len(view.Frames) != 3 || view.Frames[0] != 2 || view.Frames[1] != 0 || view.Frames[2] != 2) {
				t.Errorf("frame order was changed: %v", view.Frames)
			}
		})
	}
}

func TestSession_PreviewReportsMissingArtAndUnknownAnimation(t *testing.T) {
	cfg, _ := animationProject(t)
	s := Open(cfg)
	if got := s.Preview("idle", 16); got.Problem == "" || got.Image != "" {
		t.Errorf("missing sheet was treated as playable: %+v", got)
	}
	if got := s.Preview("guess", 16); !strings.Contains(got.Problem, "not in") {
		t.Errorf("unknown animation silently resolved: %+v", got)
	}
	if got := s.Preview("idle", 0); !strings.Contains(got.Problem, "tileSize") {
		t.Errorf("zero tileSize was treated as playable: %+v", got)
	}
}

func TestSession_PreviewRefusesAnAuthoredNonPNGSheet(t *testing.T) {
	cfg, path := animationProject(t)
	src := strings.Replace(sampleTOML, `sheet = "sprites/player.png" # artist choice`, `sheet = "sprites/player.gif" # artist choice`, 1)
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Open(cfg).Preview("idle", 16); !strings.Contains(got.Problem, "PNG") {
		t.Errorf("non-PNG sprite was advertised as playable: %+v", got)
	}
}

func TestSession_PreviewRefusesPNGWithValidHeaderButTruncatedPixels(t *testing.T) {
	cfg, _ := animationProject(t)
	folder := filepath.Join(cfg.Root, "a", "sprites")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 32, 16))); err != nil {
		t.Fatal(err)
	}
	// PNG signature + length + IHDR chunk + CRC is a valid image header, but
	// the pixel data is absent. DecodeConfig accepts it; the game does not.
	if err := os.WriteFile(filepath.Join(folder, "player.png"), encoded.Bytes()[:33], 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Open(cfg).Preview("idle", 16); !strings.Contains(got.Problem, "cannot be decoded") {
		t.Errorf("truncated PNG was shown as playable: %+v", got)
	}
}
