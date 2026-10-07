// Acknowledge the promises a cross-document view transition creates.
//
// `@view-transition { navigation: auto }` is pure CSS, but the browser still
// hands both documents a ViewTransition on `pageswap` / `pagereveal`, and its `ready` and
// `finished` promises *reject* when the transition is skipped — which is
// ordinary: another navigation starting, the tab being hidden, a document that
// misses the frame budget. A rejected promise nobody observes is an unhandled
// rejection, and this file is the only thing that observes them.
//
// The new-page pagereveal handler stopped the first occurrence, but the
// browser suite reproduced the rejection in the old page while two AGENTS
// navigations happened close together. The old page sees pageswap instead;
// both documents have to acknowledge their own promises.
//
// Loaded as a classic script at the top of <head>, unlike every other script
// here. A module is deferred to end-of-parse and `pagereveal` fires at the new
// document's first rendering opportunity, so a module could lose that race on a
// slow parse — and losing it looks exactly like this file not existing, which
// is the likeliest reason the error was seen once and never again.
//
// Datastar is not that source — it wraps a patch in a transition only when the
// SSE response asks for one, and Forge never does. See tokens.css for why the
// patches deliberately stay instant.
function acknowledgeTransition(event) {
  const transition = event.viewTransition;
  if (!transition) return;
  transition.ready.catch(() => {});
  transition.finished.catch(() => {});
}

addEventListener("pageswap", acknowledgeTransition);
addEventListener("pagereveal", acknowledgeTransition);
