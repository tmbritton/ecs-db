// Honour `autofocus` in content that arrived as a patch.
//
// The browser acts on the attribute only for elements the HTML parser inserted
// — a document load, or innerHTML. Datastar builds patched content with DOM
// APIs instead, so an `autofocus` in anything the page stream delivers renders
// perfectly and does nothing. That is the house failure mode exactly: no error,
// no visual difference, and the keyboard user is the only one who finds out.
//
// One observer for the document, for the life of the page. It moves focus once
// per element, and never out of a field someone is typing in — an autofocus is
// an explicit request, but not one worth losing half a sentence to.

const focused = new WeakSet();

function isTyping(el) {
  if (!el || !el.isConnected) return false;
  if (el.isContentEditable) return true;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}

function honour(root) {
  const targets =
    root instanceof Element && root.hasAttribute("autofocus")
      ? [root]
      : root.querySelectorAll?.("[autofocus]") ?? [];
  for (const el of targets) {
    if (focused.has(el)) continue;
    focused.add(el);
    // Not out of something being typed in. Anything else gives way: an
    // autofocus is an explicit request, and a menu that opens on the element
    // you right-clicked has to take focus from it or it is not operable at all.
    if (isTyping(document.activeElement)) return;
    el.focus();
    return;
  }
}

new MutationObserver((records) => {
  for (const record of records) {
    for (const added of record.addedNodes) {
      if (added.nodeType === Node.ELEMENT_NODE) honour(added);
    }
  }
}).observe(document.documentElement, { childList: true, subtree: true });
