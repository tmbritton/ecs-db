package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/validation"
)

func renderProblems(t *testing.T, props ProblemsProps) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Problems(props).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestProblems_RendersNothingWhenThereIsNothingWrong(t *testing.T) {
	got := renderProblems(t, ProblemsProps{ID: "p__x", Testid: "empty"})
	if got != "" {
		t.Errorf("an empty list must render nothing at all, got %q", got)
	}
}

func TestProblems_DistinguishesErrorsFromWarnings(t *testing.T) {
	got := renderProblems(t, ProblemsProps{Problems: []Problem{
		{Message: "this one blocks", Blocking: true},
		{Message: "this one does not"},
	}})

	for _, want := range []string{
		`data-blocking="true"`,
		`data-blocking="false"`,
		"problem--error",
		"problem--warning",
		">error<",
		">warning<",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered problems do not contain %q:\n%s", want, got)
		}
	}
	// The severity is in words, not only in a class: the two states differ in
	// what they do, and a difference carried only by a hue is not readable by
	// everyone.
	if strings.Index(got, ">error<") > strings.Index(got, ">warning<") {
		t.Error("problems were reordered; they must render in the order given")
	}
}

func TestProblems_EscapesTheMessage(t *testing.T) {
	got := renderProblems(t, ProblemsProps{Problems: []Problem{
		{Message: `component "<script>alert(1)</script>" is not valid`},
	}})
	if strings.Contains(got, "<script>") {
		t.Errorf("message was not escaped:\n%s", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("message is missing entirely:\n%s", got)
	}
}

func TestProblemsID_SurvivesNamesThatAreNotIdentifiers(t *testing.T) {
	// Component, entity-type and field names are authored and can hold anything
	// a JSON key can. An id built by concatenation would break the document the
	// moment one held a quote or a space.
	tests := []struct {
		name  string
		parts []string
	}{
		{"a space", []string{"component", "My Component", "behavior"}},
		{"a quote", []string{"component", `He said "hi"`, "behavior"}},
		{"a bracket", []string{"component", "a>b", "behavior"}},
		{"non-ascii", []string{"component", "héllo", "behavior"}},
	}
	seen := map[string]string{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := ProblemsID(tc.parts...)
			for _, r := range id {
				ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
					(r >= '0' && r <= '9') || r == '_'
				if !ok {
					t.Fatalf("id %q holds %q, which needs escaping in a selector", id, r)
				}
			}
			if prev, dup := seen[id]; dup {
				t.Fatalf("id %q collides with the one built for %q", id, prev)
			}
			seen[id] = tc.name
		})
	}
}

// The separator has to be one an escaped part cannot produce, or two different
// splits of the same parts would give one id.
func TestProblemsID_DoesNotCollideAcrossPartBoundaries(t *testing.T) {
	a := ProblemsID("component", "a_b", "c")
	b := ProblemsID("component", "a", "b_c")
	if a == b {
		t.Errorf("both %q and %q produced %q", "a_b|c", "a|b_c", a)
	}
}

func TestDescribedBy_PointsAtNothingWhenThereIsNothingToRead(t *testing.T) {
	if got := DescribedBy("p__x", nil); got != "" {
		t.Errorf("want no association, got %q — an aria-describedby pointing at an\n"+
			"element that renders nothing reads as an association that exists", got)
	}
	if got := DescribedBy("p__x", []Problem{{Message: "x"}}); got != "p__x" {
		t.Errorf("want p__x, got %q", got)
	}
}

func TestBlocking_IsAboutErrorsOnly(t *testing.T) {
	if Blocking([]Problem{{Message: "a warning"}}) {
		t.Error("a warning must not set aria-invalid: the value is acceptable, and\n" +
			"saying otherwise is a different claim from the one on screen")
	}
	if !Blocking([]Problem{{Message: "a warning"}, {Message: "an error", Blocking: true}}) {
		t.Error("one blocking problem makes the control invalid")
	}
}

// The pair, rendered together: the list carries the id and the control points
// at it. Half of this is invisible on screen, which is exactly why it is
// asserted rather than eyeballed.
func TestDropdown_AssociatesItsProblemsWithTheControl(t *testing.T) {
	id := ProblemsID("component", "Health", validation.FieldBehavior)
	var buf bytes.Buffer
	err := Dropdown(DropdownProps{
		Label:     "Behavior",
		Options:   []Option{{Value: "", Label: "none"}},
		ProblemID: id,
		Problems:  []Problem{{Message: "no such machine", Blocking: true}},
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, `aria-describedby="`+id+`"`) {
		t.Errorf("the select does not point at its message:\n%s", got)
	}
	if !strings.Contains(got, `id="`+id+`"`) {
		t.Errorf("the message list has no id to point at:\n%s", got)
	}
	if !strings.Contains(got, `aria-invalid="true"`) {
		t.Errorf("a blocking problem must mark the control invalid:\n%s", got)
	}
}

// A wrapping <label> names its control from its whole subtree, so a message
// rendered inside it becomes part of the select's accessible name — announced
// once as the name and again through aria-describedby, and changing every time
// validation changes. It also puts flow content inside a phrasing-content
// element, which makes clicking the message open the dropdown.
//
// Structural, because nothing about the rendered page looks wrong either way.
func TestDropdown_KeepsItsMessagesOutOfTheControlsAccessibleName(t *testing.T) {
	var buf bytes.Buffer
	err := Dropdown(DropdownProps{
		Label:     "Behavior",
		Options:   []Option{{Value: "", Label: "none"}},
		ProblemID: ProblemsID("component", "Health", validation.FieldBehavior),
		Problems:  []Problem{{Message: "no such machine"}},
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := buf.String()

	closingLabel := strings.Index(got, "</label>")
	list := strings.Index(got, `class="problems"`)
	if closingLabel < 0 || list < 0 {
		t.Fatalf("expected both a label and a message list:\n%s", got)
	}
	if list < closingLabel {
		t.Errorf("the message list is inside the <label>, so its text becomes part of\n"+
			"the select's accessible name:\n%s", got)
	}
}

func TestDropdown_SaysNothingWhenTheSelectionIsFine(t *testing.T) {
	var buf bytes.Buffer
	err := Dropdown(DropdownProps{
		Label:     "Behavior",
		Options:   []Option{{Value: "", Label: "none"}},
		ProblemID: ProblemsID("component", "Health", validation.FieldBehavior),
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := buf.String()
	for _, unwanted := range []string{"aria-describedby", "aria-invalid", "class=\"problems\""} {
		if strings.Contains(got, unwanted) {
			t.Errorf("a sound control still carries %q:\n%s", unwanted, got)
		}
	}
}

func TestSaveFooter_RefusesToOfferASaveTheEngineWouldReject(t *testing.T) {
	var buf bytes.Buffer
	err := SaveFooter(SaveFooterProps{
		Dirty:         true,
		File:          "schema.json",
		Blocked:       true,
		BlockedReason: "2 problems to fix first",
		SaveAction:    "@post('/forge/schema/save')",
		DiscardAction: "@post('/forge/schema/discard')",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := buf.String()

	// templ escapes the quotes in an attribute value, so the assertions match
	// what is actually written to the page rather than what was passed in.
	if strings.Contains(got, "/forge/schema/save") {
		t.Errorf("the save action is still wired on a blocked footer — a disabled\n"+
			"attribute is a hint, not a guard:\n%s", got)
	}
	if !strings.Contains(got, "/forge/schema/discard") {
		t.Errorf("Discard is the way out of an invalid state and must stay offered:\n%s", got)
	}
	if !strings.Contains(got, "2 problems to fix first") {
		t.Errorf("the footer does not say why Save is disabled:\n%s", got)
	}
	if !strings.Contains(got, `data-blocked="true"`) {
		t.Errorf("blocked state is not reported on the footer:\n%s", got)
	}
}

func TestSaveFooter_LeavesAValidSaveAlone(t *testing.T) {
	var buf bytes.Buffer
	err := SaveFooter(SaveFooterProps{
		Dirty: true, File: "schema.json",
		SaveAction: "@post('/forge/schema/save')",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "data-on:click") || !strings.Contains(got, "/forge/schema/save") {
		t.Errorf("a valid dirty session must still offer Save:\n%s", got)
	}
	if !strings.Contains(got, `data-blocked="false"`) {
		t.Errorf("want data-blocked=false:\n%s", got)
	}
}
