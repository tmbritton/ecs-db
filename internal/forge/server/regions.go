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
// data is a function because most of the regions need it and two do not, and
// gathering it costs a read-only open of the game's database — two on SCHEMA.
// Both callers memoise it, so asking once per region costs one gather.
func (s *Server) regions(m mode.Mode, data func() modes.Data) []region {
	out := []region{
		{"engine status", s.renderEngineStatus},
		{"save reports", s.renderSaveReports},
		{"save footer", func() (string, error) { return s.renderFooter(m.Slug, data()) }},
	}
	// The mode's own regions, in its own order — one on a stub, four on MAP.
	// The shell knows only that there are some and that Page puts them back
	// together; which ones, and how many, is the mode's business.
	for _, r := range modes.Registry[m.Slug].Regions {
		out = append(out, region{
			name:   m.Slug + "/" + r.ID,
			render: func() (string, error) { return renderToString(r.Render(data())) },
		})
	}
	out = append(out, region{"save confirmation", func() (string, error) { return s.renderConfirmRegion(data()) }})
	return out
}

// modeRegionRange reports where a mode's own regions sit in the slice regions
// returns, so the page can hand exactly those to Page and nothing else.
func modeRegionRange(m mode.Mode) (first, count int) {
	return shellRegionsBefore, len(modes.Registry[m.Slug].Regions)
}

// shellRegionsBefore is how many of the shell's own regions come before a
// mode's. Engine status, save reports and the save footer, in that order.
const shellRegionsBefore = 3

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

	first, count := modeRegionRange(m)
	content, ok := modes.Registry[m.Slug]
	if !ok {
		return templates.Regions{}, "", fmt.Errorf("no content registered for mode %q", m.Slug)
	}
	// The page's mode content is assembled from the very bytes the stream would
	// send, not rendered a second time from the same Data. Two renders that
	// merely ought to agree is what the version stamp would then be asserting
	// about, and the stamp is only worth having if it cannot be wrong.
	// The page's own id travels in its signals, so every request it makes says
	// which page it is — Datastar sends signals with all of them. Merged here
	// rather than asked of each mode, because the id is the shell's business
	// and a mode should not have to remember to carry it.
	signals := mergeSignals(`{"page":"`+data.PageID+`"}`, modeSignals(content, data))
	modeContent, err := renderToString(
		templates.ModeContentRegion(content.Page(rendered[first:first+count]), signals))
	if err != nil {
		return templates.Regions{}, "", fmt.Errorf("assembling %s: %w", m.Slug, err)
	}

	return templates.Regions{
		EngineStatus: rendered[0],
		SaveReports:  rendered[1],
		SaveFooter:   rendered[2],
		ModeContent:  modeContent,
		SaveConfirm:  rendered[len(rendered)-1],
	}, strconv.FormatUint(sum.Sum64(), 16), nil
}

func renderToString(c templates.Component) (string, error) {
	var buf strings.Builder
	if err := c.Render(context.Background(), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// modeSignals is a mode's own declared view state, or "" when it has none.
func modeSignals(content modes.Content, data modes.Data) string {
	if content.Signals == nil {
		return ""
	}
	return content.Signals(data)
}

// mergeSignals joins two JSON object literals into one.
//
// String concatenation, like the literals themselves: every value written into
// them is a number, a bool, or an id this server generated from crypto/rand, so
// there is nothing here that needs escaping. The moment a caller-supplied
// string goes in, this should become encoding/json.
func mergeSignals(a, b string) string {
	switch {
	case b == "" || b == "{}":
		return a
	case a == "" || a == "{}":
		return b
	}
	return a[:len(a)-1] + "," + b[1:]
}
