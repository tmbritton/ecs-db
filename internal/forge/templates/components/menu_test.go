package components

import "testing"

func TestContextMenu(t *testing.T) {
	// The layer-row menu from the design system's right-click spec.
	props := ContextMenuProps{
		Title: "TERRAIN · LAYER",
		Items: []MenuItem{
			{Label: "Toggle visibility", Action: "@post('/x')"},
			{Label: "Rename…", Shortcut: "F2"},
			{Label: "Move up", Submenu: true},
			{Divider: true},
			{Label: "Delete", Shortcut: "Del", Danger: true},
		},
	}

	t.Run("structure", func(t *testing.T) {
		out := render(t, ContextMenu(props))
		assertOutput(t, out, []string{
			"ctx-menu__title", "TERRAIN · LAYER",
			"ctx-menu__shortcut", "F2",
			"ctx-menu__divider",
			"ctx-menu__item--danger",
			"▸",
		}, nil)
		if n := countSubstring(out, "ctx-menu__item--danger"); n != 1 {
			t.Errorf("want exactly 1 danger item, got %d", n)
		}
		// A divider is a rule, not a clickable row.
		if n := countSubstring(out, "ctx-menu__item"); n != 5 {
			t.Errorf("want 4 items + 1 danger modifier = 5 matches, got %d\n%s", n, out)
		}
	})

	t.Run("no title, no title element", func(t *testing.T) {
		out := render(t, ContextMenu(ContextMenuProps{Items: []MenuItem{{Label: "Cut"}}}))
		assertOutput(t, out, []string{"Cut"}, []string{"ctx-menu__title"})
	})

	t.Run("labels are escaped", func(t *testing.T) {
		out := render(t, ContextMenu(ContextMenuProps{Items: []MenuItem{{Label: "<script>x</script>"}}}))
		assertOutput(t, out, []string{"&lt;script&gt;"}, []string{"<script>x</script>"})
	})
}
