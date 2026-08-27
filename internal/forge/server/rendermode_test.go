package server

import (
	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/templates"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
)

// renderModeContent renders a mode's whole content to a string.
//
// A test helper, and it lives in a test file because that is all it is. Neither
// the page nor the stream calls it: the page assembles from the regions it has
// already rendered, so that what it ships and what the stream would send are
// the same bytes rather than two renders that agree; the stream patches the
// regions one at a time, which is the point of having them.
//
// What it is for is the tests that want to ask what a mode looks like without
// having to know how many pieces it comes in.
func (s *Server) renderModeContent(m mode.Mode, data modes.Data) (string, error) {
	return renderToString(templates.ModeContentRegion(modes.Render(m.Slug, data)))
}
