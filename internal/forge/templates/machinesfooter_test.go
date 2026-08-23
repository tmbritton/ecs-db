package templates

import (
	"context"
	"strings"
	"testing"
)

func renderFooter(t *testing.T, invalid map[string]int) string {
	t.Helper()
	var b strings.Builder
	c := MachinesFooter("goblin.json", true, "goblin.json", false, invalid)
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// The AGENTS Save writes every dirty machine, so a problem in any of them is a
// save that fails. A footer reporting only the machine on screen would offer a
// Save that fails on a file in another row of the same list.
func TestMachinesFooter_BlocksOnAnyMachineTheSaveWouldWrite(t *testing.T) {
	clean := renderFooter(t, nil)
	if strings.Contains(clean, `data-blocked="true"`) {
		t.Error("a valid project blocks the save")
	}

	blocked := renderFooter(t, map[string]int{"/p/goblin.json": 2})
	if !strings.Contains(blocked, `data-blocked="true"`) {
		t.Error("a machine the engine would refuse does not block the save")
	}
	// Disabled, and Discard is not: Discard is the way out of an invalid state.
	if !strings.Contains(blocked, `class="save-footer__save" disabled`) {
		t.Errorf("the Save button is not disabled: %s", blocked)
	}
	if strings.Contains(blocked, `class="save-footer__discard" disabled`) {
		t.Error("Discard was taken away, leaving no way out")
	}
}

// A control disabled for a reason nobody can see is indistinguishable from one
// that is broken.
func TestMachinesFooter_SaysWhyTheSaveIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name    string
		invalid map[string]int
		want    string
	}{
		{"one problem in one machine", map[string]int{"/p/a.json": 1}, "1 problem in 1 machine"},
		{"several in one", map[string]int{"/p/a.json": 3}, "3 problems in 1 machine"},
		{"several in several", map[string]int{"/p/a.json": 2, "/p/b.json": 1}, "3 problems in 2 machines"},
	} {
		got := renderFooter(t, tc.invalid)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: the reason is not %q: %s", tc.name, tc.want, got)
		}
		if !strings.Contains(got, "the engine would refuse") {
			t.Errorf("%s: the reason does not say who refuses it", tc.name)
		}
	}
}

// Problems, not machines: two machines with one problem each is two problems,
// and a count that said "2 machines" would leave someone looking for a third.
func TestMachinesFooter_CountsProblemsRatherThanMachines(t *testing.T) {
	got := renderFooter(t, map[string]int{"/p/a.json": 5})
	if !strings.Contains(got, "5 problems") {
		t.Errorf("five problems in one machine is not reported as five: %s", got)
	}
}
