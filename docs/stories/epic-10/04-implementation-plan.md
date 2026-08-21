# Epic 10 Story 4: Templ primitives — Implementation Plan

**Goal:** every visual element Forge repeats exists once, as a typed templ component that reads only from tokens.

**Architecture:** One file per primitive under `internal/forge/templates/components/`, each exporting a `XxxProps` struct and a `templ Xxx(props XxxProps)`. Variants are named Go types with a small closed set of constants, so an invalid appearance is a compile error rather than a typo in a class string. Tests render to a buffer and assert on the state-carrying signal only.

**Depends on:** Story 3.

---

## Files

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/forge/templates/components/panel.templ` | `Panel`, `SectionHeading` |
| Create | `internal/forge/templates/components/row.templ` | `ListRow` |
| Create | `internal/forge/templates/components/chip.templ` | `Chip` |
| Create | `internal/forge/templates/components/controls.templ` | `SegmentedControl`, `Dropdown`, `Checkbox`, `IconButton` |
| Create | `internal/forge/templates/components/menu.templ` | `ContextMenu` |
| Create | `internal/forge/templates/components/modal.templ` | `ModalShell` |
| Create | `internal/forge/templates/components/footer.templ` | `SaveFooter` |
| Create | `internal/forge/templates/components/types.go` | Enumerated variant types |
| Create | `internal/forge/templates/components/*_test.go` | One test file per primitive |
| Modify | `internal/forge/web/static/css/forge.css` | Primitive classes, hover/focus/disabled states |
| Modify | `internal/forge/templates/devtokens.templ` | Components gallery |

---

## Task 1: Variant types

### `internal/forge/templates/components/types.go`

```go
package components

// LensSource is the AUTHORED/LIVE/REPLAY selector state. Each value has a
// fixed accent: authored is amber (files), live is cyan (world.sqlite),
// replay is violet (the transitions log).
type LensSource string

const (
	LensAuthored LensSource = "authored"
	LensLive     LensSource = "live"
	LensReplay   LensSource = "replay"
)

// ChipKind distinguishes a component the entity type requires (locked, shown
// with 🔒) from one it merely allows (detachable, shown with ✕).
type ChipKind int

const (
	ChipRequired ChipKind = iota
	ChipOptional
)

type ButtonVariant int

const (
	ButtonPrimary ButtonVariant = iota // amber fill — the one committing action
	ButtonGhost                        // bordered, transparent
	ButtonDanger                       // red text/border
)
```

Keep this file `.go`, not `.templ` — it is types, and templ files should hold markup.

---

## Task 2: Panel and SectionHeading

```templ
package components

type SectionHeadingProps struct {
	Label  string // rendered uppercase by CSS, not by Go
	Source string // optional "◂ world.sqlite" suffix, rendered cyan
}

templ SectionHeading(props SectionHeadingProps) {
	<div class="section-heading">
		{ props.Label }
		if props.Source != "" {
			<span class="section-heading__source">{ props.Source }</span>
		}
	</div>
}

type PanelProps struct {
	Heading *SectionHeadingProps // nil = no heading
	Width   int                  // px; 0 = flex
	Class   string               // extra class hook, never a style string
}

templ Panel(props PanelProps) {
	<div class={ "panel", props.Class } style={ panelWidth(props.Width) }>
		if props.Heading != nil {
			@SectionHeading(*props.Heading)
		}
		<div class="panel__body">
			{ children... }
		</div>
	</div>
}
```

`panelWidth` returns a `templ.SafeCSS`-typed value (or nothing when `Width == 0`). Width is the only geometry a caller may pass, because panel widths genuinely vary per mode (210–290px); colours and spacing never do.

---

## Task 3: ListRow, Chip

`ListRow` props: `Label`, `Glyph`, `Value`, `Active bool`, `Muted bool`, plus a Datastar action attribute for selection. Active emits `list-row--active`, which CSS renders as an amber left border plus `--active` fill.

`Chip` props: `Name`, `Kind ChipKind`, `ContextSeeded bool`. `ChipRequired` renders `🔒` with `title="required by type"`; `ChipOptional` renders `✕` as a remove control. `ContextSeeded` appends the `ƒ ctx` badge, which is read-only by definition — the value comes from the machine's context manifest.

---

## Task 4: Controls

`SegmentedControl` props: `Name`, `Options []SegmentOption{Value, Label, Accent}`, `Selected string`. The source lens is its primary caller, so `Accent` maps to the amber/cyan/violet trio; the TILES tabs pass no accent and inherit amber.

`Dropdown` props: `Label`, `Options []Option{Value, Label}`, `Selected`, plus the Datastar action URL. It renders a real `<select>` styled to match, not a custom popup — keyboard behaviour for free, and Datastar binds to it natively.

`Checkbox` props: `Label`, `Checked bool`, Datastar action URL. Checked renders `✓` in `--green`; unchecked renders an empty bordered box. There is no indeterminate state.

`IconButton` props: `Glyph`, `Title`, `Variant ButtonVariant`, `Disabled bool`, Datastar action URL. 26px square.

---

## Task 5: ContextMenu, ModalShell, SaveFooter

`ContextMenu` props: `Title` (mono caps, e.g. `GOBLIN · SPAWN #12`), `Items []MenuItem{Label, Shortcut, Danger bool, Divider bool}`.

`ModalShell` props: `Title`, `CloseAction`, with `{ children... }` for the scrollable body and a `Footer templ.Component` slot. Applies `--shadow-modal`.

`SaveFooter` props: `Dirty bool`, `SaveAction`, `DiscardAction`. Dirty shows `● unsaved` in amber and enables both buttons; clean shows `✓ saved` and disables them. It knows nothing about *what* is being saved — that is the caller's business.

---

## Task 6: Tests

One table-driven test per primitive, rendering to a buffer:

```go
func renderToString(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}
```

Assert on the state-carrying signal, never whole-HTML equality:

| primitive | case | assert |
|---|---|---|
| `ListRow` | active | contains `list-row--active` |
| `ListRow` | inactive | does **not** contain `list-row--active` |
| `Chip` | required | contains `🔒`, not `✕` |
| `Chip` | optional | contains `✕`, not `🔒` |
| `Chip` | context-seeded | contains `badge-ctx` |
| `Checkbox` | off | does not contain `✓` |
| `SegmentedControl` | live selected | live option carries the selected class, others do not |
| `SaveFooter` | clean | contains `✓ saved`; both buttons `disabled` |
| `SaveFooter` | dirty | contains `● unsaved`; neither button `disabled` |
| `ContextMenu` | danger item | danger row carries the danger class |
| `Panel` | no heading | no `section-heading` element |

Also assert escaping once, explicitly: a `ListRow` whose `Label` is `<script>alert(1)</script>` must render escaped. templ does this by default, and a test pins it so a future refactor to raw HTML is caught.

---

## Task 7: Components gallery

Extend `devtokens.templ` with a section rendering every primitive in every state from the table above, grouped by primitive. This is the page reviewed against `Forge Design System.dc.html`.

---

## Task 8: Mark story complete

Tick `docs/stories/epic-10/04-primitives.md`, add `## As Implemented`, tick **Templ primitives** in `docs/plan.md`.

---

## Verification

```bash
make generate && go test ./internal/forge/templates/... -v
go test ./...
make build-headless && ./bin/ecs-db-headless forge
```

Open `/dev/tokens` beside `Forge Design System.dc.html` and check the components section against it: chip lock/remove affordances, the three-way lens with its three accents, row density (4–5px), icon buttons at 26px, modal shadow offset, and the save footer in both states.

Before committing, launch a fresh-context code-review subagent over the staged diff, per `AGENTS.md`.
