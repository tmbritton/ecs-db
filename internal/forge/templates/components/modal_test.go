package components

import (
	"strings"
	"testing"
)

func TestModalShell(t *testing.T) {
	t.Run("titled, closable, with a body", func(t *testing.T) {
		out := renderChildren(t, ModalShell(ModalShellProps{
			Title: "PREFERENCES", CloseAction: "@post('/forge/modal/close')", Width: 680,
		}), "modal body here")
		assertOutput(t, out, []string{
			"modal-backdrop", `role="dialog"`, `aria-modal="true"`,
			"PREFERENCES", "modal__close", "✕",
			"width:680px", "modal body here",
		}, nil)
	})

	t.Run("footer slot", func(t *testing.T) {
		out := renderChildren(t, ModalShell(ModalShellProps{
			Title: "RESIZE MAP", Footer: text("<footer-marker>"),
		}), "body")
		assertOutput(t, out, []string{"modal__foot", "<footer-marker>"}, nil)
	})

	t.Run("no footer, no footer element", func(t *testing.T) {
		out := renderChildren(t, ModalShell(ModalShellProps{Title: "ABOUT"}), "body")
		assertOutput(t, out, []string{"modal__body"}, []string{"modal__foot"})
	})

	t.Run("title is escaped and names the dialog", func(t *testing.T) {
		out := renderChildren(t, ModalShell(ModalShellProps{Title: `<script>x</script>`}), "body")
		assertOutput(t, out, []string{"&lt;script&gt;", "aria-label="}, []string{"<script>x</script>"})
	})

	// Datastar compiles data-on-* to a plain addEventListener with no target
	// filtering. A dismiss handler on the backdrop would therefore fire for
	// every click bubbling out of the dialog — closing it whenever a user
	// touched a control inside — and would double-fire alongside the ✕ button.
	// Only the __outside modifier binds at the document and checks
	// !card.contains(target).
	t.Run("dismiss fires only for the scrim itself", func(t *testing.T) {
		out := renderChildren(t, ModalShell(ModalShellProps{
			Title: "PREFERENCES", CloseAction: "@post('/close')",
		}), "body")
		// A bare handler fires for clicks bubbling out of the dialog; the
		// __outside modifier binds at the document and fires for clicks
		// anywhere else on the page. Neither is "clicked the scrim".
		if strings.Contains(out, "__outside") {
			t.Errorf("__outside binds at the document, not this scrim\n%s", out)
		}
		if !strings.Contains(out, "evt.target === el") {
			t.Errorf("dismiss must test the click target\n%s", out)
		}
		card := out[strings.Index(out, `class="modal"`):]
		if strings.Contains(card, "data-on:click=\"evt.target") {
			t.Errorf("the scrim test belongs on the backdrop, not the card\n%s", card)
		}
	})

	// A close control that does nothing is worse than none.
	t.Run("no close action, no close button", func(t *testing.T) {
		out := renderChildren(t, ModalShell(ModalShellProps{Title: "ABOUT"}), "body")
		assertOutput(t, out, []string{"ABOUT"}, []string{"modal__close", "data-on"})
	})

	// Nothing about the shell may be a comment: templ emits HTML comments
	// verbatim, so a developer note would ship in every dialog's wire bytes.
	t.Run("no html comments reach the wire", func(t *testing.T) {
		out := renderChildren(t, ModalShell(ModalShellProps{Title: "ABOUT"}), "body")
		if strings.Contains(out, "<!--") {
			t.Errorf("rendered shell contains an HTML comment\n%s", out)
		}
	})
}

// A dialog that can only be dismissed with a mouse is a dialog some people
// cannot dismiss. Escape closes it; every other key is left alone, so typing
// in a field inside the dialog does not close it.
func TestModalShell_DismissesOnEscapeOnly(t *testing.T) {
	out := renderChildren(t, ModalShell(ModalShellProps{
		Title:       "ABOUT",
		CloseAction: "@post('/close')",
	}), "body")

	if !strings.Contains(out, "data-on:keydown=") {
		t.Fatalf("the dialog has no key handler:\n%s", out)
	}
	if !strings.Contains(out, "Escape") {
		t.Errorf("the key handler does not name Escape:\n%s", out)
	}
	// Guarded, not unconditional: without the key check every keystroke inside
	// the dialog would close it.
	if strings.Contains(out, `data-on:keydown="@post(&#39;/close&#39;)"`) {
		t.Errorf("every key closes the dialog:\n%s", out)
	}
	// Focus has to move into the dialog, or Escape lands wherever the focus
	// was before it opened and the handler never fires.
	if !strings.Contains(out, `tabindex="-1"`) || !strings.Contains(out, `data-init="el.focus()"`) {
		t.Errorf("the dialog does not take focus when it opens:\n%s", out)
	}
}

// A dialog with no close action has nothing to bind, and a keydown handler
// calling an empty expression is a syntax error in the browser.
func TestModalShell_WithNoCloseActionBindsNoKeyHandler(t *testing.T) {
	out := renderChildren(t, ModalShell(ModalShellProps{Title: "ABOUT"}), "body")
	if strings.Contains(out, "data-on:keydown") {
		t.Errorf("a dialog with no close action bound a key handler:\n%s", out)
	}
}
