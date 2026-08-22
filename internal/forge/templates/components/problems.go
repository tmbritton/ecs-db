package components

import (
	"fmt"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/validation"
)

// Problem is the validator's own type. Aliased rather than converted: the
// primitive renders what the check produced, and a parallel view type is a
// second place for "is this blocking" to be decided.
type Problem = validation.Problem

// problemClass picks the hue. Red is the design's danger colour and an error is
// what it is for; a warning stays amber, the tool's own colour, because the
// schema is legal and only the machine that reads it will object.
func problemClass(p Problem) string {
	if p.Blocking {
		return "problem--error"
	}
	return "problem--warning"
}

// problemBadge labels the severity in words as well as in colour. The two
// states differ in what they do — one stops the save and one does not — and a
// difference that only exists as a hue is not available to everyone reading.
func problemBadge(p Problem) string {
	if p.Blocking {
		return "error"
	}
	return "warning"
}

// ProblemsID builds the element id a control's aria-describedby points at.
//
// One function for both ends of the association: the id is written on the list
// and referenced on the input, and two format strings spelled out separately
// eventually stop matching — at which point the association silently does
// nothing, which is exactly the failure aria-describedby exists to prevent and
// exactly the failure that is invisible on screen.
//
// Component, entity-type and field names are authored, so they can hold
// anything a JSON key can. Every character outside [A-Za-z0-9] is escaped to
// _XX, which means a literal underscore never survives, which in turn makes __
// an unambiguous separator between parts.
func ProblemsID(parts ...string) string {
	escaped := make([]string, 0, len(parts)+1)
	escaped = append(escaped, "p")
	for _, part := range parts {
		escaped = append(escaped, escapeID(part))
	}
	return strings.Join(escaped, "__")
}

func escapeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		for _, c := range []byte(string(r)) {
			fmt.Fprintf(&b, "_%02X", c)
		}
	}
	return b.String()
}

// Blocking reports whether any of these problems would stop a save, which is
// what aria-invalid states. A warning must not set it: the field's value is
// acceptable, and telling a screen reader otherwise is a different claim from
// the one on screen.
func Blocking(problems []Problem) bool {
	for _, p := range problems {
		if p.Blocking {
			return true
		}
	}
	return false
}

// DescribedBy is the aria-describedby value for a control, or "" when there is
// nothing to describe it with. Pointing at an id that renders nothing is worse
// than pointing at nothing: it reads as an association that exists.
func DescribedBy(id string, problems []Problem) string {
	if len(problems) == 0 {
		return ""
	}
	return id
}
