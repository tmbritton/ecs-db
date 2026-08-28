// Acknowledge the promises a cross-document view transition creates.
//
// `@view-transition { navigation: auto }` is pure CSS, but the browser still
// hands the new document a ViewTransition on `pagereveal`, and its `ready` and
// `finished` promises *reject* when the transition is skipped — which is
// ordinary: another navigation starting, the tab being hidden, a document that
// misses the frame budget. A rejected promise nobody observes is an unhandled
// rejection, and this file is the only thing that observes them.
//
// Honest about its evidence: the browser suite saw `AbortError: Transition was
// skipped` as an uncaught exception exactly once, in a spec that navigates
// twice in quick succession, and it has not been reproduced since — not by
// interrupting navigations by hand, nor by running that spec on its own. So
// this is the documented mitigation for a mechanism that fits the symptom,
// rather than a fix demonstrated against a failing case.
//
// It is kept because the cost is four lines and the alternative is an
// occasional uncaught exception in a tool whose console is worth reading. It is
// also the only place to look if that error ever returns: if it does, this file
// did not cover it, and the source is somewhere else.
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
addEventListener("pagereveal", (event) => {
  const transition = event.viewTransition;
  if (!transition) return;
  transition.ready.catch(() => {});
  transition.finished.catch(() => {});
});
