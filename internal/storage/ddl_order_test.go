package storage

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/schema"
)

// orderedComponent has six properties in a deliberately non-alphabetical
// order. Six, not two: with two, map iteration produces the same order often
// enough that a non-deterministic generator passes a repeated run.
func orderedComponent() schema.Component {
	return schema.Component{
		Type: schema.ComponentTypeObject,
		Properties: map[string]schema.Property{
			"sheet":     {Type: "string"},
			"animation": {Type: "string"},
			"flip_x":    {Type: "boolean"},
			"zeta":      {Type: "number"},
			"alpha":     {Type: "number"},
			"middle":    {Type: "integer"},
		},
		PropertyOrder: []string{"sheet", "animation", "flip_x", "zeta", "alpha", "middle"},
	}
}

// In the engine the non-determinism was invisible: the DDL runs once at startup
// and nobody reads it. In Forge's live preview the columns visibly reshuffle as
// you type.
func TestComponentTableSQL_IsDeterministic(t *testing.T) {
	first, err := componentTableSQL("Sprite", orderedComponent())
	if err != nil {
		t.Fatalf("componentTableSQL: %v", err)
	}
	for range 50 {
		again, err := componentTableSQL("Sprite", orderedComponent())
		if err != nil {
			t.Fatalf("componentTableSQL: %v", err)
		}
		if again != first {
			t.Fatalf("column order changes between calls\n--- first ---\n%s\n--- again ---\n%s", first, again)
		}
	}
}

// Authored order, not sorted. Since Epic 11 the order the author wrote is
// recorded, so the DDL can read like the file it came from — deterministic and
// recognisable rather than merely deterministic.
func TestComponentTableSQL_UsesAuthoredOrder(t *testing.T) {
	got, err := componentTableSQL("Sprite", orderedComponent())
	if err != nil {
		t.Fatalf("componentTableSQL: %v", err)
	}
	assertColumnOrder(t, got, "sheet", "animation", "flip_x", "zeta", "alpha", "middle")
}

// A component built in code has no recorded order, so it sorts — deterministic
// either way, never map order.
func TestComponentTableSQL_SortsWithoutRecordedOrder(t *testing.T) {
	comp := orderedComponent()
	comp.PropertyOrder = nil

	got, err := componentTableSQL("Sprite", comp)
	if err != nil {
		t.Fatalf("componentTableSQL: %v", err)
	}
	assertColumnOrder(t, got, "alpha", "animation", "flip_x", "middle", "sheet", "zeta")
}

// The two halves of the generator must agree. buildNewColumns feeds the rebuild
// path, and it sorted while componentTableSQL used map order, so a rebuilt
// table could come out with its columns in a different order than the original.
func TestBuildNewColumns_MatchesComponentTableSQL(t *testing.T) {
	comp := orderedComponent()

	create, err := componentTableSQL("Sprite", comp)
	if err != nil {
		t.Fatalf("componentTableSQL: %v", err)
	}
	rebuild := buildNewColumns(comp)

	createOrder := columnNames(create)
	rebuildOrder := make([]string, 0, len(rebuild))
	for _, col := range rebuild {
		rebuildOrder = append(rebuildOrder, strings.Fields(strings.TrimSpace(col))[0])
	}

	if strings.Join(createOrder, ",") != strings.Join(rebuildOrder, ",") {
		t.Errorf("the two halves of the generator disagree\n create:  %v\n rebuild: %v",
			createOrder, rebuildOrder)
	}
}

func assertColumnOrder(t *testing.T, ddl string, want ...string) {
	t.Helper()
	got := columnNames(ddl)
	// entity_id is always first and is not a property.
	if len(got) == 0 || got[0] != "entity_id" {
		t.Fatalf("first column = %v, want entity_id", got)
	}
	got = got[1:]
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("column order = %v, want %v\n%s", got, want, ddl)
	}
}

func columnNames(ddl string) []string {
	var out []string
	for _, line := range strings.Split(ddl, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		if line == "" || strings.HasPrefix(line, "CREATE") || line == ")" || strings.HasPrefix(line, ");") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			out = append(out, fields[0])
		}
	}
	return out
}
