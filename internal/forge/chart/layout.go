package chart

import (
	"encoding/json"

	"github.com/tmbritton/ecs-db/internal/agent"
)

// forgeMeta is the shape Forge writes under a state's meta.
//
// Story 1 settled that canvas coordinates live in each state's own meta, under
// a "forge" key, rather than in a behaviors/<id>.layout.json sidecar — ScanDir
// loads every .json in a behaviours directory, so a sidecar does not fail, it
// registers itself as a machine with an empty id. meta is XState's sanctioned
// place for arbitrary per-state data, it survives a round trip since Story 1,
// and it is colocated: deleting a state deletes its layout with it.
type forgeMeta struct {
	Forge *struct {
		X *float64 `json:"x"`
		Y *float64 `json:"y"`
	} `json:"forge"`
}

// Position reads where the file says a state sits, if it says.
//
// Nothing here is an error. meta is hand-editable, it is shared with Stately
// Studio, and users have their own keys in it — so a note where an object was
// expected, a string where a number was, or no meta at all is a state with no
// recorded position, and it gets the fallback slot. A chart that refused to
// draw a machine because someone left a comment in meta would be worse than one
// that placed the node itself.
//
// Read-only: Story 5 does the writing, and has to merge rather than replace,
// for the same reason.
func Position(state *agent.StateNode) (x, y float64, ok bool) {
	if state == nil {
		return 0, 0, false
	}
	// The key as authored: extraFields matches the modelled set
	// case-insensitively but stores keys verbatim, so a file spelled "Meta"
	// carries a different extra. Forge writes "meta", which is what XState
	// writes.
	raw, found := state.Extra["meta"]
	if !found {
		return 0, 0, false
	}
	var meta forgeMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return 0, 0, false
	}
	if meta.Forge == nil || meta.Forge.X == nil || meta.Forge.Y == nil {
		return 0, 0, false
	}
	return *meta.Forge.X, *meta.Forge.Y, true
}
