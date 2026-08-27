package server

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/templates"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
)

// A region is one patchable area of a page: a name for the log, and how to
// render it. Each renders its own wrapper element carrying the id the patch
// targets, so a region is a complete replacement for what is on screen rather
// than something that has to be wrapped again on the way out.
type region struct {
	name   string
	render func() (string, error)
}

// regions is every patchable area of a mode's page, in a fixed order.
//
// One list, used by the page render and by the stream. They were two code paths
// producing HTML that had to match, and "had to match" is not a property code
// has unless something makes it so — the stream's whole ability to say "you
// already have this" rests on the two being byte-identical.
//
// data is a function because three of the five need it and two do not, and
// gathering it costs a read-only open of the game's database — two on SCHEMA.
func (s *Server) regions(m mode.Mode, data func() modes.Data) []region {
	return []region{
		{"engine status", s.renderEngineStatus},
		{"save reports", s.renderSaveReports},
		{"save footer", func() (string, error) { return s.renderFooter(m.Slug, data()) }},
		{"mode content", func() (string, error) { return s.renderModeContent(m, data()) }},
		{"save confirmation", func() (string, error) { return s.renderConfirmRegion(data()) }},
	}
}

// renderRegions renders a page's regions and the version stamp that identifies
// them.
//
// The stamp travels in the page's stream URL. When the stream renders the same
// regions and arrives at the same stamp, the browser already has what it was
// about to be sent, and the send is skipped — which is what stops every
// navigation parsing the page and then immediately morphing it into a
// byte-identical copy of itself.
//
// It is a hash rather than the generation counter alone. The counter answers
// "has anything been published since", which is nearly the same question and
// not quite: streamQuery rebuilds a subset of the page's query string by hand,
// so a stream can render a *different view* of unchanged state. That has
// happened three times, each time visible as the page changing on its own a
// moment after load. Comparing the bytes catches it; comparing a counter would
// have converted it into a page that silently never updates.
func (s *Server) renderRegions(m mode.Mode, data modes.Data) (templates.Regions, string, error) {
	regions := s.regions(m, func() modes.Data { return data })

	rendered := make([]string, len(regions))
	sum := fnv.New64a()
	for i, region := range regions {
		html, err := region.render()
		if err != nil {
			// A page that cannot render a region is a broken page, and a 500
			// says so. The stream logs and carries on instead, because there
			// its next pass is moments away and a dropped connection is not.
			return templates.Regions{}, "", fmt.Errorf("rendering %s: %w", region.name, err)
		}
		rendered[i] = html
		_, _ = sum.Write([]byte(html))
		// A separator, so that moving a byte from the end of one region to the
		// start of the next is a different page rather than the same hash.
		_, _ = sum.Write([]byte{0})
	}

	return templates.Regions{
		EngineStatus: rendered[0],
		SaveReports:  rendered[1],
		SaveFooter:   rendered[2],
		ModeContent:  rendered[3],
		SaveConfirm:  rendered[4],
	}, strconv.FormatUint(sum.Sum64(), 16), nil
}

// renderModeContent renders the open mode. The selection it renders for comes
// from the request's own query string — the page's, or the one the page put in
// its stream subscription.
//
// It lives here rather than beside renderFooter and renderConfirmRegion because
// it is the only region that has to look up which component to build.
func (s *Server) renderModeContent(m mode.Mode, data modes.Data) (string, error) {
	build, ok := modes.Registry[m.Slug]
	if !ok {
		return "", fmt.Errorf("no content registered for mode %q", m.Slug)
	}
	return renderToString(templates.ModeContentRegion(build(data)))
}

func renderToString(c templates.Component) (string, error) {
	var buf strings.Builder
	if err := c.Render(context.Background(), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}
