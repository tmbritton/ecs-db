package jsonorder

import (
	"reflect"
	"strings"
	"testing"
)

func TestKeys(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr string
	}{
		{name: "document order, not alphabetical", raw: `{"zebra":1,"apple":2,"mango":3}`, want: []string{"zebra", "apple", "mango"}},
		{name: "nested values are skipped whole", raw: `{"a":{"z":1,"y":2},"b":[1,2,{"c":3}]}`, want: []string{"a", "b"}},
		{name: "empty object", raw: `{}`},
		{name: "absent section", raw: ``},
		{name: "whitespace only", raw: "  \n\t "},
		// A section written as null means "no keys", not a malformed object.
		// Erroring here once made LoadSchema reject a schema it had accepted,
		// which was enough to stop the engine starting.
		{name: "explicit null", raw: `null`},
		{name: "null with whitespace", raw: "  null  "},
		{name: "not an object", raw: `[1,2,3]`, wantErr: "expected a JSON object"},
		{name: "a bare string", raw: `"nope"`, wantErr: "expected a JSON object"},
		{name: "malformed", raw: `{"a":`, wantErr: "EOF"},
		// json.Unmarshal keeps the last value for a duplicate key; the order
		// scan sees both, and Apply's dedup handles it.
		{name: "duplicate keys are reported as they appear", raw: `{"a":1,"b":2,"a":3}`, want: []string{"a", "b", "a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Keys([]byte(tt.raw))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want an error containing %q, got none (keys %v)", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Keys: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Keys = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApply(t *testing.T) {
	tests := []struct {
		name     string
		recorded []string
		present  []string
		want     []string
	}{
		{
			name:     "recorded order is honoured",
			recorded: []string{"zebra", "apple"},
			present:  []string{"apple", "zebra"},
			want:     []string{"zebra", "apple"},
		},
		{
			name:     "unrecorded keys are appended sorted",
			recorded: []string{"zebra"},
			present:  []string{"zebra", "mango", "apple"},
			want:     []string{"zebra", "apple", "mango"},
		},
		{
			name:     "recorded keys that are gone are skipped",
			recorded: []string{"zebra", "deleted", "apple"},
			present:  []string{"zebra", "apple"},
			want:     []string{"zebra", "apple"},
		},
		{
			name:     "a duplicate in the recorded order is emitted once",
			recorded: []string{"a", "b", "a"},
			present:  []string{"a", "b"},
			want:     []string{"a", "b"},
		},
		{
			name:     "no recorded order sorts",
			recorded: nil,
			present:  []string{"zebra", "apple", "mango"},
			want:     []string{"apple", "mango", "zebra"},
		},
		{
			name:    "nothing present",
			present: nil,
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			present := map[string]int{}
			for i, k := range tt.present {
				present[k] = i
			}
			got := Apply(tt.recorded, present)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Apply = %v, want %v", got, tt.want)
			}
		})
	}
}

// Apply feeds a file writer, so map iteration must never reach the output.
func TestApply_IsDeterministic(t *testing.T) {
	present := map[string]int{"d": 1, "a": 2, "c": 3, "b": 4, "e": 5, "f": 6}
	first := Apply([]string{"c"}, present)
	for range 50 {
		if got := Apply([]string{"c"}, present); !reflect.DeepEqual(got, first) {
			t.Fatalf("Apply is not deterministic: %v then %v", first, got)
		}
	}
}
