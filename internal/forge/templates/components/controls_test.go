package components

import "testing"

func TestSegmentedControl(t *testing.T) {
	lens := []SegmentOption{
		{Value: string(LensAuthored), Label: "AUTHORED", Accent: LensAuthored.Accent()},
		{Value: string(LensLive), Label: "● LIVE", Accent: LensLive.Accent()},
		{Value: string(LensReplay), Label: "REPLAY", Accent: LensReplay.Accent()},
	}

	t.Run("only the selected segment is marked", func(t *testing.T) {
		out := render(t, SegmentedControl(SegmentedControlProps{
			Label: "source", Signal: "lens", Options: lens, Selected: string(LensLive),
		}))
		// Exactly one segment carries the selected class, and it is the live
		// one with its cyan accent — not amber, which is authored's hue.
		if n := countSubstring(out, "segment--selected"); n != 1 {
			t.Errorf("want 1 selected segment, got %d\n%s", n, out)
		}
		if n := countSubstring(out, "checked"); n != 1 {
			t.Errorf("want 1 checked input, got %d\n%s", n, out)
		}
		assertOutput(t, out, []string{"segment--cyan", "● LIVE", "AUTHORED"}, []string{"segment--amber", "segment--violet"})
	})

	t.Run("no selection marks nothing", func(t *testing.T) {
		out := render(t, SegmentedControl(SegmentedControlProps{Label: "source", Signal: "lens", Options: lens}))
		assertOutput(t, out, []string{"segmented"}, []string{"segment--selected", "checked"})
	})

	// The selection has to enter the signal store. A `value` attribute is not
	// a signal, so a group that only posts a URL is invisible to every other
	// action on the page.
	t.Run("every option binds the same signal", func(t *testing.T) {
		out := render(t, SegmentedControl(SegmentedControlProps{
			Label: "source", Signal: "lens", Options: lens, Selected: string(LensLive),
		}))
		if n := countSubstring(out, `data-bind="lens"`); n != len(lens) {
			t.Errorf("want %d bound inputs, got %d\n%s", len(lens), n, out)
		}
		// Datastar's radio adapter derives the shared `name` from the signal
		// path, which is what makes the group mutually exclusive.
		if n := countSubstring(out, `type="radio"`); n != len(lens) {
			t.Errorf("want %d radios, got %d", len(lens), n)
		}
	})

	t.Run("no signal, no binding", func(t *testing.T) {
		out := render(t, SegmentedControl(SegmentedControlProps{Label: "source", Options: lens}))
		assertOutput(t, out, nil, []string{"data-bind"})
	})

	t.Run("labelled for assistive tech", func(t *testing.T) {
		out := render(t, SegmentedControl(SegmentedControlProps{
			Label: "source lens", Signal: "lens", Options: lens, Selected: "live",
		}))
		// radiogroup, not a group of aria-pressed buttons: n toggles do not
		// express that exactly one is chosen.
		assertOutput(t, out, []string{`aria-label="source lens"`, `role="radiogroup"`}, nil)
	})

	t.Run("one change handler for the group", func(t *testing.T) {
		out := render(t, SegmentedControl(SegmentedControlProps{
			Label: "source", Signal: "lens", Options: lens, Action: "@post('/lens')",
		}))
		if n := countSubstring(out, "data-on:change"); n != 1 {
			t.Errorf("want 1 group handler, got %d — per-option actions are boilerplate\n%s", n, out)
		}
	})
}

func TestDropdown(t *testing.T) {
	opts := []Option{{Value: "wander", Label: "wandering_goblin"}, {Value: "idle", Label: "idle_prop"}}
	tests := []struct {
		name    string
		props   DropdownProps
		want    []string
		notWant []string
	}{
		{
			name:  "selected option carries selected",
			props: DropdownProps{Label: "Behavior", Signal: "behavior", Options: opts, Selected: "idle"},
			want:  []string{`value="idle" selected`, "wandering_goblin", "dropdown__caret"},
		},
		{
			name:    "nothing selected",
			props:   DropdownProps{Label: "Behavior", Signal: "behavior", Options: opts},
			notWant: []string{"selected"},
		},
		{
			name:  "change action",
			props: DropdownProps{Signal: "behavior", Options: opts, Action: "@post('/forge/ents/behavior')"},
			want:  []string{"data-on:change"},
		},
		{
			// An action posts the page's signals. Without a binding the server
			// gets a request it cannot interpret, so a bound signal is not
			// optional decoration.
			name:  "signal is bound",
			props: DropdownProps{Signal: "behavior", Options: opts},
			want:  []string{`data-bind="behavior"`},
		},
		{
			name:    "no signal, no binding",
			props:   DropdownProps{Options: opts},
			notWant: []string{"data-bind"},
		},
		{
			name:    "no action, no handler",
			props:   DropdownProps{Signal: "behavior", Options: opts},
			notWant: []string{"data-on"},
		},
		{
			name:  "option labels are escaped",
			props: DropdownProps{Signal: "x", Options: []Option{{Value: "a", Label: "<b>bold</b>"}}},
			want:  []string{"&lt;b&gt;"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertOutput(t, render(t, Dropdown(tt.props)), tt.want, tt.notWant)
		})
	}
}

func TestCheckbox(t *testing.T) {
	tests := []struct {
		name    string
		props   CheckboxProps
		want    []string
		notWant []string
	}{
		{
			name:  "on shows the green check",
			props: CheckboxProps{Label: "Snap to grid", Signal: "snap", Checked: true},
			want:  []string{"✓", "checkbox__box--on", "checked"},
		},
		{
			// The glyph is server-rendered, not CSS-revealed, so "off" means
			// the character is genuinely absent from the document.
			name:    "off shows an empty box",
			props:   CheckboxProps{Label: "Snap to grid", Signal: "snap"},
			want:    []string{"checkbox__box"},
			notWant: []string{"✓", "checkbox__box--on", "checked"},
		},
		{
			name:  "toggle action",
			props: CheckboxProps{Signal: "snap", Action: "@post('/forge/prefs/snap')"},
			want:  []string{"data-on:change"},
		},
		{
			name:  "signal is bound",
			props: CheckboxProps{Signal: "snap"},
			want:  []string{`data-bind="snap"`},
		},
		{
			// Box and label both take their state from Checked, server-side.
			// A CSS :checked rule on either half would desynchronise them the
			// moment a user clicked.
			name:  "label state follows the server, not the browser",
			props: CheckboxProps{Label: "Snap", Signal: "snap", Checked: true},
			want:  []string{"checkbox--on"},
		},
		{
			name:    "unchecked carries no on-state class",
			props:   CheckboxProps{Label: "Snap", Signal: "snap"},
			notWant: []string{"checkbox--on"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertOutput(t, render(t, Checkbox(tt.props)), tt.want, tt.notWant)
		})
	}
}

func TestIconButton(t *testing.T) {
	tests := []struct {
		name    string
		props   IconButtonProps
		want    []string
		notWant []string
	}{
		{
			name:  "titled for its tooltip",
			props: IconButtonProps{Glyph: "▶", Title: "preview"},
			want:  []string{`title="preview"`, `aria-label="preview"`, "▶", "icon-btn"},
		},
		{
			// An empty aria-label is skipped by accname. Omitting it is only
			// half the fix: the glyph must also stop being aria-hidden, or the
			// button is announced as nothing at all.
			name:    "no title falls back to the glyph as the name",
			props:   IconButtonProps{Glyph: "▶"},
			want:    []string{"▶"},
			notWant: []string{"aria-label", "title", "aria-hidden"},
		},
		{
			name:  "titled glyph is decoration",
			props: IconButtonProps{Glyph: "▶", Title: "preview"},
			want:  []string{`aria-hidden="true"`},
		},
		{
			name:  "variant class",
			props: IconButtonProps{Glyph: "✕", Title: "delete", Variant: ButtonDanger},
			want:  []string{"btn--danger"},
		},
		{
			// A disabled control must not also be wired to fire — the attribute
			// and the handler have to agree.
			name:    "disabled drops its action",
			props:   IconButtonProps{Glyph: "▶", Title: "preview", Disabled: true, Action: "@post('/x')"},
			want:    []string{"disabled"},
			notWant: []string{"data-on"},
		},
		{
			name:    "enabled keeps its action",
			props:   IconButtonProps{Glyph: "▶", Title: "preview", Action: "@post('/x')"},
			want:    []string{"data-on:click"},
			notWant: []string{"disabled"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertOutput(t, render(t, IconButton(tt.props)), tt.want, tt.notWant)
		})
	}
}

// Amber means "the one committing action". A caller that has not thought about
// emphasis must not be handed it by default, so the zero ButtonVariant — and
// any value outside the set — has to render ghost.
func TestButtonVariant_DefaultsToGhost(t *testing.T) {
	for _, v := range []ButtonVariant{0, ButtonGhost, ButtonVariant(99), ButtonVariant(-1)} {
		out := render(t, IconButton(IconButtonProps{Glyph: "▢", Title: "rect", Variant: v}))
		assertOutput(t, out, []string{"btn--ghost"}, []string{"btn--primary"})
	}
}

func TestButtonVariant_Classes(t *testing.T) {
	tests := []struct {
		variant ButtonVariant
		want    string
	}{
		{ButtonGhost, "btn--ghost"},
		{ButtonPrimary, "btn--primary"},
		{ButtonOutline, "btn--outline"},
		{ButtonDashed, "btn--dashed"},
		{ButtonDanger, "btn--danger"},
	}
	seen := map[string]bool{}
	for _, tt := range tests {
		if got := tt.variant.class(); got != tt.want {
			t.Errorf("variant %d: got %q, want %q", tt.variant, got, tt.want)
		}
		if seen[tt.want] {
			t.Errorf("two variants share the class %q", tt.want)
		}
		seen[tt.want] = true
	}
}
