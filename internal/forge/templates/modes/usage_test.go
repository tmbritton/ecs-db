package modes

import (
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/usage"
)

// available builds counts with distinctive numbers: 7 rows of the component,
// 3 and 1 entities of the two types. Distinct so a panel rendering the wrong
// one of them shows the wrong number rather than coincidentally the right one.
func available() usage.Counts {
	return usage.Counts{
		Available:   true,
		ByType:      map[string]int{"Player": 3, "Goblin": 1},
		ByComponent: map[string]int{"Position": 7},
		DBVersion:   3,
	}
}

// The number, not merely that a number is there. Asserting on the element's
// presence leaves a panel free to render any value at all.
func TestUsagePanel_ShowsTheComponentsOwnRowCount(t *testing.T) {
	data := entsFixture()
	data.Selected = "Position"
	data.Counts = available()

	got := renderMode(t, data)
	block := section(t, got, `data-testid="live-rows-Position"`, "</p>")
	if !strings.Contains(block, ">7<") {
		t.Errorf("the component's row count is not 7:\n%s", block)
	}
	// The declaring types' populations are 3 and 1. A panel counting those
	// instead would show one of them, or their sum.
	for _, wrong := range []string{">3<", ">1<", ">4<"} {
		if strings.Contains(block, wrong) {
			t.Errorf("the panel shows %s, which is a type population rather than the component's rows:\n%s",
				wrong, block)
		}
	}
}

func TestUsagePanel_ShowsTheTypesOwnEntityCount(t *testing.T) {
	data := entsFixture()
	data.Selected = "Goblin"
	data.Counts = available()

	block := section(t, renderEnts(t, data), `data-testid="live-Goblin"`, "</p>")
	if !strings.Contains(block, ">1<") {
		t.Errorf("Goblin's count is not 1:\n%s", block)
	}
	// Player is 3 and is the first type in authored order — the value a panel
	// counting the wrong row would show.
	if strings.Contains(block, ">3<") {
		t.Errorf("the panel shows another type's count:\n%s", block)
	}
	if !strings.Contains(block, "entity of this type") {
		t.Errorf("a count of one reads as a plural:\n%s", block)
	}
}

// Plural agreement at zero as well as at one. "0 entity" is the kind of thing
// that makes a reader distrust the number beside it.
func TestUsagePanel_PluralAgreement(t *testing.T) {
	data := entsFixture()
	data.Selected = "Player"

	for _, tc := range []struct {
		n             int
		want, notWant string
	}{
		{0, "entities of this type", "entity of this type exists"},
		{1, "entity of this type", "entities of this type"},
		{2, "entities of this type", "entity of this type exists"},
	} {
		data.Counts = usage.Counts{Available: true, ByType: map[string]int{"Player": tc.n}, DBVersion: 3}
		block := section(t, renderEnts(t, data), `data-testid="live-Player"`, "</p>")
		if !strings.Contains(block, tc.want) {
			t.Errorf("a count of %d does not read %q:\n%s", tc.n, tc.want, block)
		}
		if strings.Contains(block, tc.notWant) {
			t.Errorf("a count of %d reads %q:\n%s", tc.n, tc.notWant, block)
		}
	}
}

// "Nothing is using this" and "nobody could say" lead to opposite decisions.
func TestUsagePanel_UnavailableIsNotZero(t *testing.T) {
	data := entsFixture()
	data.Selected = "Position"
	data.Counts = usage.Counts{Reason: "no database yet — the engine creates one on its first run"}

	for _, got := range []string{renderMode(t, data), renderEnts(t, withEntsSelection(data, "Player"))} {
		if !strings.Contains(got, `data-testid="live-unavailable"`) {
			t.Fatal("an unavailable count does not say so")
		}
		if !strings.Contains(got, "no database yet") {
			t.Error("the panel does not say why there is no count")
		}
		if !strings.Contains(got, "not a count of zero") {
			t.Error("nothing distinguishes it from a zero")
		}
		if strings.Contains(got, `data-testid="live-rows-Position"`) ||
			strings.Contains(got, `data-testid="live-Player"`) {
			t.Error("an unavailable count rendered a number anyway")
		}
	}
}

// A component with no table yet is a third state: nothing is stored, as
// opposed to nothing being counted.
func TestUsagePanel_AComponentWithNoTableSaysSo(t *testing.T) {
	data := entsFixture()
	data.Selected = "Anchor" // not in ByComponent
	data.Counts = available()

	got := renderMode(t, data)
	if !strings.Contains(got, `data-testid="live-not-built"`) {
		t.Fatal("a component with no table renders no explanation")
	}
	if strings.Contains(got, `data-testid="live-rows-Anchor"`) {
		t.Error("a component with no table rendered a row count")
	}
	if !strings.Contains(got, "comp_anchor") {
		t.Error("the note does not name the table that is missing")
	}
}

// A count taken against a database built to another version counts an older
// world, and a zero from it can mean "filed under a different name".
func TestUsagePanel_StaleCountsAreQualified(t *testing.T) {
	data := entsFixture()
	data.Selected = "Player"
	counts := available()
	counts.Stale = true
	counts.DBVersion = 2
	data.Counts = counts

	got := renderEnts(t, data)
	if !strings.Contains(got, `data-testid="live-stale"`) {
		t.Fatal("a stale count is not qualified")
	}
	if !strings.Contains(got, "v2") {
		t.Error("the qualification does not say which version was counted")
	}

	data.Counts = available() // not stale
	if strings.Contains(renderEnts(t, data), `data-testid="live-stale"`) {
		t.Error("a current count was qualified as stale")
	}
}

// A zero that means "not implemented" is indistinguishable from a real answer,
// and the person deciding whether a deletion is safe would act on it.
func TestUsagePanel_SpawnsAreAbsentWithAnExplanation(t *testing.T) {
	data := entsFixture()
	data.Selected = "Position"
	data.Counts = available()

	for _, got := range []string{renderMode(t, data), renderEnts(t, withEntsSelection(data, "Player"))} {
		note := section(t, got, `data-testid="spawn-note"`, "</p>")
		if !strings.Contains(note, "Epic 14") {
			t.Errorf("the spawn note does not say when spawns arrive:\n%s", note)
		}
		if !strings.Contains(note, "not shown") {
			t.Errorf("the spawn note does not say the count is absent:\n%s", note)
		}
	}
}

// The live block carries the live styling, and the chips beside it do not —
// that difference is the whole signal.
func TestUsagePanel_LiveStylingIsOnTheCountsNotTheChips(t *testing.T) {
	data := entsFixture()
	data.Selected = "Position"
	data.Counts = available()
	got := renderMode(t, data)

	live := section(t, got, `data-testid="live-counts"`, `data-testid="spawn-note"`)
	if !strings.Contains(live, "live__count") {
		t.Errorf("the count does not carry the live styling:\n%s", live)
	}
	chips := section(t, got, `class="schema__chips"`, `data-testid="live-counts"`)
	if strings.Contains(chips, "live__") {
		t.Errorf("the file-derived chips carry live styling:\n%s", chips)
	}
}

// The component half is derived from the file, so it must be there with no
// database at all — that is the fact you need when nothing has ever been run.
func TestUsagePanel_UsedByWorksWithNoDatabase(t *testing.T) {
	data := entsFixture()
	data.Selected = "Position"
	data.Counts = usage.Counts{Reason: "no database yet"}

	if !strings.Contains(renderMode(t, data), `data-testid="used-by-Player"`) {
		t.Error("the file-derived usage is missing with no database")
	}
}

func withEntsSelection(data Data, entityType string) Data {
	data.Selected = entityType
	return data
}
