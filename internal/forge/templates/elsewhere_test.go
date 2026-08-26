package templates

import "testing"

// One Save button does one thing and the footer follows the mode, so unsaved
// work the footer cannot save has to be named. Two kinds of it at once is
// exactly when reporting only one is worst.
func TestElsewhere_NamesEveryKindOfUnsavedWork(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Elsewhere
		want string
	}{
		{"nothing", Elsewhere{}, ""},
		{"one machine", Elsewhere{Machines: 1}, "1 unsaved machine in AGENTS"},
		{"two machines", Elsewhere{Machines: 2}, "2 unsaved machines in AGENTS"},
		{"one map", Elsewhere{Maps: 1}, "1 unsaved map in MAP"},
		{"three maps", Elsewhere{Maps: 3}, "3 unsaved maps in MAP"},
		{"the schema", Elsewhere{Schema: true}, "unsaved changes to schema.json in SCHEMA"},
		{
			"all three at once",
			Elsewhere{Schema: true, Machines: 2, Maps: 1},
			"unsaved changes to schema.json in SCHEMA · 2 unsaved machines in AGENTS · 1 unsaved map in MAP",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.describe(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
