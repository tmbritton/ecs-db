package components

import "testing"

func TestSaveFooter(t *testing.T) {
	tests := []struct {
		name    string
		props   SaveFooterProps
		want    []string
		notWant []string
	}{
		{
			// Clean means there is nothing to commit and nothing to throw
			// away, so neither button may be pressable. The actions are passed
			// deliberately: without them the `props.Dirty &&` half of the
			// template guard is never exercised and the case cannot fail.
			name: "clean disables both buttons and drops both actions",
			props: SaveFooterProps{
				File:       "overworld.tmx",
				SaveAction: "@post('/forge/save')", DiscardAction: "@post('/forge/discard')",
			},
			want: []string{
				"✓ saved", "overworld.tmx",
				`class="save-footer__discard" disabled`,
				`class="save-footer__save" disabled`,
			},
			notWant: []string{"● unsaved", "data-on"},
		},
		{
			name: "dirty enables both buttons",
			props: SaveFooterProps{
				Dirty: true, File: "schema.json",
				SaveAction: "@post('/forge/save')", DiscardAction: "@post('/forge/discard')",
			},
			want:    []string{"● unsaved", "save-footer--dirty", "@post(&#39;/forge/save&#39;)", "@post(&#39;/forge/discard&#39;)"},
			notWant: []string{"✓ saved", "disabled"},
		},
		{
			name:    "no file, no separator",
			props:   SaveFooterProps{},
			want:    []string{"✓ saved"},
			notWant: []string{"·"},
		},
		{
			name: "missing file keeps a named restore action without a generic Save",
			props: SaveFooterProps{
				Status: "missing on disk", StatusLabel: "Restore working file",
				StatusAction: "@post('/forge/sprites/save/overwrite')",
			},
			want:    []string{`data-testid="save-footer-restore"`, "Restore working file", "missing on disk"},
			notWant: []string{">Discard</button>", ">Save</button>"},
		},
		{
			name: "invalid file offers Reload without implying it can be saved",
			props: SaveFooterProps{
				Status: "invalid on disk", ReloadAction: "@post('/forge/sprites/reload')",
			},
			want:    []string{`data-testid="save-footer-reload"`, "invalid on disk"},
			notWant: []string{">Discard</button>", ">Save</button>"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertOutput(t, render(t, SaveFooter(tt.props)), tt.want, tt.notWant)
		})
	}
}
