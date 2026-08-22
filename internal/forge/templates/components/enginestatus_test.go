package components

import (
	"regexp"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/status"
)

func TestEngineStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  status.Status
		want    []string
		notWant []string
	}{
		{
			name: "connected names the schema version and the mod",
			status: status.Status{
				State: status.StateConnected, SchemaVersion: 3, DBVersion: 3, ModName: "core",
			},
			want: []string{
				"engine-status__dot--ok",
				"schema.json v3",
				"mods/core",
				"hot-reload live",
			},
			notWant: []string{"engine-status__dot--bad", "watcher offline"},
		},
		{
			// A stale database means something quite different from an absent
			// one — the engine migrates it on its next start — so it must be
			// distinguishable, and must show both numbers.
			name: "mismatch shows both versions",
			status: status.Status{
				State: status.StateMismatch, SchemaVersion: 4, DBVersion: 3, ModName: "core",
			},
			want:    []string{"engine-status__dot--bad", "schema v4", "db v3", "migrates on next start"},
			notWant: []string{"engine-status__dot--ok", "hot-reload live", "watcher offline"},
		},
		{
			name:   "offline explains the consequence, not just the state",
			status: status.Status{State: status.StateOffline, SchemaVersion: 3, ModName: "core"},
			want: []string{
				"engine-status__dot--bad",
				"watcher offline",
				"edits queue until game restarts",
			},
			notWant: []string{"engine-status__dot--ok", "hot-reload live"},
		},
		{
			name: "an unreadable schema names the file, not the database",
			status: status.Status{
				State: status.StateSchemaUnreadable, ModName: "core",
			},
			want:    []string{"engine-status__dot--bad", "schema.json unreadable"},
			notWant: []string{"engine-status__dot--ok", "hot-reload live", "≠ db v"},
		},
		{
			// The zero Status renders before the first check completes. It must
			// not claim a connection.
			name:    "the zero status renders as offline",
			status:  status.Status{},
			want:    []string{"engine-status__dot--bad", "watcher offline"},
			notWant: []string{"hot-reload live"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, EngineStatus(EngineStatusProps{Status: tt.status}))
			assertOutput(t, got, tt.want, tt.notWant)
		})
	}
}

// Story 5 left `id="engine-status"` in the menu bar as the patch target. The id
// has to live on the component itself: Datastar's default patch mode is `outer`
// and matches on the element's own id, so without it the patch has nothing to
// morph and the readout silently never updates.
func TestEngineStatus_CarriesThePatchTarget(t *testing.T) {
	got := render(t, EngineStatus(EngineStatusProps{}))
	// Anchored on the leading space: `data-testid="engine-status"` ends with
	// the substring `id="engine-status"`, so a plain Contains counts two and a
	// plain Contains for presence would be satisfied by the test id alone —
	// which is not what Datastar patches against.
	idRE := regexp.MustCompile(`\sid="engine-status"`)
	if n := len(idRE.FindAllString(got, -1)); n != 1 {
		t.Errorf("found %d real id attributes, want exactly 1\n%s", n, got)
	}
}

// Every value in the readout comes from the engine rather than the user, which
// is what the mono face signals throughout Forge.
func TestEngineStatus_IsMono(t *testing.T) {
	got := render(t, EngineStatus(EngineStatusProps{}))
	if !strings.Contains(got, "mono") {
		t.Errorf("the readout is not mono\n%s", got)
	}
}

// The dot is decorative — the sentence beside it already says the state — so it
// must not be read out, and the readout as a whole should announce changes.
func TestEngineStatus_Accessibility(t *testing.T) {
	got := render(t, EngineStatus(EngineStatusProps{}))
	if !strings.Contains(got, `aria-hidden="true"`) {
		t.Errorf("the status dot should be hidden from assistive tech\n%s", got)
	}
	// It updates without a page load, so a screen reader needs to be told.
	if !strings.Contains(got, `role="status"`) {
		t.Errorf("the readout should be a live region\n%s", got)
	}
}
