package templates

import "github.com/tmbritton/ecs-db/internal/forge/templates/components"

// Sample data for the /dev/tokens components gallery. These are illustrative
// values from the design handoff, not fixtures any mode depends on.
var (
	lensOptions = []components.SegmentOption{
		{Value: string(components.LensAuthored), Label: "AUTHORED", Accent: components.LensAuthored.Accent()},
		{Value: string(components.LensLive), Label: "● LIVE", Accent: components.LensLive.Accent()},
		{Value: string(components.LensReplay), Label: "REPLAY", Accent: components.LensReplay.Accent()},
	}

	behaviorOptions = []components.Option{
		{Value: "wander", Label: "wandering_goblin"},
		{Value: "idle", Label: "idle_prop"},
	}

	anchorOptions = []components.Option{
		{Value: "tl", Label: "top-left"},
		{Value: "c", Label: "centre"},
	}

	// The placed-spawn menu from section 05 of the design system.
	spawnMenu = components.ContextMenuProps{
		Title: "goblin · spawn #12",
		Items: []components.MenuItem{
			{Label: "Edit spawn…", Action: "@post('/dev/noop')"},
			{Label: "Change behavior", Submenu: true},
			{Label: "Duplicate", Shortcut: "⌘D"},
			{Divider: true},
			{Label: "Set breakpoint here", Action: "@post('/dev/noop')"},
			{Divider: true},
			{Label: "Delete spawn", Shortcut: "Del", Danger: true, Action: "@post('/dev/noop')"},
		},
	}
)
