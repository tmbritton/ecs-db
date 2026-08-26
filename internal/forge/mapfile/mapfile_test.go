package mapfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mapfile"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func copyShippedLevel(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../../mods/map/level1.tmx")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "level1.tmx")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpen_AFreshlyOpenedMapIsClean(t *testing.T) {
	path := copyShippedLevel(t)
	f, err := mapfile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	dirty, err := f.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("a map nobody edited reports unsaved changes")
	}
}

func TestSave_WritesWhatWasEditedAndNothingElse(t *testing.T) {
	path := copyShippedLevel(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := mapfile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := f.Current.Map()
	if err != nil {
		t.Fatal(err)
	}
	gids := append([]uint32(nil), m.Layers[0].Data...)
	gids[21] = 2 // a floor cell becomes a wall
	if err := f.Current.SetLayerData(0, gids); err != nil {
		t.Fatal(err)
	}
	dirty, err := f.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Fatal("an edit did not register as unsaved")
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) == string(before) {
		t.Fatal("the edit was not written")
	}
	// Everything outside the one changed row is untouched, including the file's
	// long leading comment.
	if !strings.Contains(string(after), "migrated from the character format") {
		t.Error("the save dropped the map's comment")
	}
	beforeLines, afterLines := strings.Split(string(before), "\n"), strings.Split(string(after), "\n")
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("the save changed the line count: %d then %d", len(beforeLines), len(afterLines))
	}
	changed := 0
	for i := range beforeLines {
		if beforeLines[i] != afterLines[i] {
			changed++
		}
	}
	if changed != 1 {
		t.Errorf("painting one cell changed %d lines, want 1", changed)
	}

	dirty, err = f.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("a saved map still reports unsaved changes")
	}
}

func TestSave_AnEditAndItsExactReversalLeavesTheMapClean(t *testing.T) {
	path := copyShippedLevel(t)
	f, err := mapfile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := f.Current.Map()
	if err != nil {
		t.Fatal(err)
	}
	original := append([]uint32(nil), m.Layers[0].Data...)
	painted := append([]uint32(nil), original...)
	painted[0] = 1
	if err := f.Current.SetLayerData(0, painted); err != nil {
		t.Fatal(err)
	}
	if err := f.Current.SetLayerData(0, original); err != nil {
		t.Fatal(err)
	}
	dirty, err := f.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("an edit and its exact reversal left the map unsaved")
	}
}

func TestDiscard_RestoresTheMapAsItWasOpened(t *testing.T) {
	path := copyShippedLevel(t)
	f, err := mapfile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Current.AddObject(0, tiled.Object{Type: "Goblin", X: 0, Y: 32}); err != nil {
		t.Fatal(err)
	}
	if err := f.Discard(); err != nil {
		t.Fatal(err)
	}
	dirty, err := f.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("discard left the map unsaved")
	}
}

func TestSave_RefusesAMapChangedUnderneathIt(t *testing.T) {
	path := copyShippedLevel(t)
	f, err := mapfile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Current.AddObject(0, tiled.Object{Type: "Goblin", X: 0, Y: 32}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err == nil {
		t.Fatal("saving over an outside edit was accepted")
	}
	if err := f.SaveOverwriting(); err != nil {
		t.Fatalf("keeping mine was refused: %v", err)
	}
}

func TestOpen_RefusesAFileThatIsNotAMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "level.tmj")
	if err := os.WriteFile(path, []byte(`{"width":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := mapfile.Open(path)
	if err == nil {
		t.Fatal("a .tmj was opened for editing")
	}
	if !strings.Contains(err.Error(), ".tmx") {
		t.Errorf("refusal %q does not say what to do about it", err)
	}
}
