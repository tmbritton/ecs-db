package tilemap

import (
	"os"
	"strings"
	"testing"
)

func TestStarterTileset_ArtworkDoesNotDeclareMovementRules(t *testing.T) {
	raw, err := os.ReadFile("../../mods/map/starter.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `name="passable"`) {
		t.Fatal("starter art still authors legacy tile passability")
	}
}
