// Package components holds Forge's shared visual vocabulary: the bordered
// panels, dense rows, chips, controls, menus and modals that every mode is
// built from.
//
// Two rules make the set reviewable. First, no component accepts a raw style
// or class-name string as a way to change how it looks — a caller that needs a
// different appearance gets an enumerated field, so the set of possible
// renderings is finite and greppable. Second, no component carries a colour;
// colours live in tokens.css and are reached through a class.
//
// Action fields hold Datastar expressions, not REST endpoints. `@post('/x')`
// sends the page's signals and the server answers with a stream of
// datastar-patch-elements / datastar-patch-signals events. A primitive carries
// the expression and nothing else; what comes back is the server's business.
package components

import (
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/migration"
	"github.com/tmbritton/ecs-db/internal/storage"
)

// Component aliases templ.Component so .templ files in this package can name a
// component-valued prop — a footer slot, say — without importing templ, which
// would collide with the import templ's own codegen adds.
type Component = templ.Component

// LensSource is the AUTHORED/LIVE/REPLAY selector state. Each value has a
// fixed accent: authored is amber (files), live is cyan (world.sqlite), replay
// is violet (the transitions log). Same panels, different data behind them.
type LensSource string

const (
	LensAuthored LensSource = "authored"
	LensLive     LensSource = "live"
	LensReplay   LensSource = "replay"
)

// Accent names the semantic hue a component renders in. It is deliberately a
// closed set: the design system's central rule is that every hue means exactly
// one thing, so a new appearance must be a new constant here — a decision —
// rather than a colour typed into a call site.
type Accent int

const (
	AccentNone   Accent = iota // inherit — amber for most controls
	AccentAmber                // authoring, action, selection
	AccentGreen                // entities, valid, on/true
	AccentCyan                 // live source, active statechart node
	AccentViolet               // engine-computed values, replay source
	AccentRed                  // danger, collision, breakpoint hit
)

// class returns the modifier suffix for an accent, or "" for AccentNone. An
// unknown value also yields "" so a zero-value or out-of-range Accent degrades
// to the inherited appearance rather than emitting a class no stylesheet has.
func (a Accent) class(prefix string) string {
	var name string
	switch a {
	case AccentAmber:
		name = "amber"
	case AccentGreen:
		name = "green"
	case AccentCyan:
		name = "cyan"
	case AccentViolet:
		name = "violet"
	case AccentRed:
		name = "red"
	default:
		return ""
	}
	return prefix + "--" + name
}

// Accent maps a lens to the hue the design system assigns it.
func (l LensSource) Accent() Accent {
	switch l {
	case LensLive:
		return AccentCyan
	case LensReplay:
		return AccentViolet
	default:
		return AccentAmber
	}
}

// ChipKind distinguishes a component the entity type requires (locked, shown
// with 🔒) from one it merely allows (detachable, shown with ✕).
type ChipKind int

const (
	ChipRequired ChipKind = iota
	ChipOptional
)

// Tone is a row's or item's content colour. It is separate from selection:
// a row can be both selected and muted, and conflating the two into parallel
// booleans would let a caller ask for two tones at once.
type Tone int

const (
	ToneDefault Tone = iota
	ToneMuted
	ToneDanger
)

func (t Tone) class(prefix string) string {
	switch t {
	case ToneMuted:
		return prefix + "--muted"
	case ToneDanger:
		return prefix + "--danger"
	default:
		return ""
	}
}

// ButtonVariant is the appearance of a button or icon button.
//
// Ghost is deliberately the zero value. Amber means "the one committing
// action" in this design language, so a caller that has not thought about
// emphasis must not be handed the most emphatic control by default — the
// primary fill has to be asked for.
type ButtonVariant int

const (
	ButtonGhost   ButtonVariant = iota // default stroke, dim label
	ButtonPrimary                      // amber fill — the one committing action
	ButtonOutline                      // amber stroke on active fill
	ButtonDashed                       // dashed stroke — "add" and empty slots
	ButtonDanger                       // red
)

func (v ButtonVariant) class() string {
	switch v {
	case ButtonPrimary:
		return "btn--primary"
	case ButtonOutline:
		return "btn--outline"
	case ButtonDashed:
		return "btn--dashed"
	case ButtonDanger:
		return "btn--danger"
	default:
		return "btn--ghost"
	}
}

// classes joins the non-empty class names. Variant helpers return "" for the
// default case, so this is what keeps `class="list-row "` — with its trailing
// space and its empty modifier — out of the output.
func classes(names ...string) string {
	kept := make([]string, 0, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			kept = append(kept, n)
		}
	}
	return strings.Join(kept, " ")
}

// when is the conditional half of classes: it yields the class name when the
// condition holds and "" otherwise, which classes then drops. templ.KV exists
// for this, but it only composes with templ.Classes — keeping every class
// expression a plain []string keeps one join rule for the whole package.
func when(cond bool, name string) string {
	if !cond {
		return ""
	}
	return name
}

// pxWidth renders a width as inline CSS. Width is the only geometry a caller
// may pass, because panel and dialog widths genuinely vary (210–290px for
// panels, 400–680px for dialogs) while colour and spacing never do.
//
// templ.SafeCSS bypasses sanitisation, so the value is constructed here from
// an int rather than accepting a caller's string, and a non-positive width
// yields "" so the attribute is omitted entirely.
func pxWidth(px int) templ.SafeCSS {
	if px <= 0 {
		return ""
	}
	return templ.SafeCSS("width:" + strconv.Itoa(px) + "px")
}

// boolAttr renders a Go bool as the string an ARIA attribute expects.
// aria-pressed is tri-state in the spec, so "false" has to be written out —
// omitting it is not the same as saying no.
// Statement is the engine's own DDL statement type. The migration panel and
// the confirmation render these rather than re-deriving what a change means.
type Statement = storage.Statement

// confirmTitle names the decision. A check that could not run is a different
// question from one that found something, and the title is the first thing
// read.
func confirmTitle(p migration.Preview) string {
	if p.Failed {
		return "This save could not be checked"
	}
	return "This save destroys data"
}

func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// listRowClass is shared by ListRow's two element forms — a <button> when the
// row is activatable, a <div> when it is not — so the appearance cannot drift
// between them.
func listRowClass(props ListRowProps) string {
	return classes(
		"list-row",
		when(props.Active, "list-row--active"),
		props.Tone.class("list-row"),
	)
}

// escapeDismiss closes on Escape and leaves every other key alone, so typing
// inside the dialog does not dismiss it.
func escapeDismiss(action string) string {
	return "evt.key === 'Escape' && (" + action + ")"
}

// scrimDismiss guards a dismiss action so it fires only for a click that
// landed on the scrim itself, not one that bubbled up from inside the dialog.
//
// `el` and `evt` are both in scope in every Datastar expression — the runtime
// compiles them as Function("el", "$", "__action", "evt", …) — so both this
// test and escapeDismiss's can be written inline rather than reached for with
// an event modifier.
func scrimDismiss(action string) string {
	return "evt.target === el && (" + action + ")"
}
