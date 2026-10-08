package modes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/mapcanvas"
)

// The Page functions in Registry, curried.
//
// templ components are not curried, so a mode whose assembly needs a constant
// alongside its regions — a test id, or the order the three MAP regions go in —
// needs one of these between the registry and the template.

// TwoPanePage assembles a list rail and an editor panel from two rendered
// regions.
func TwoPanePage(testid string) func([]string) templ.Component {
	return func(rendered []string) templ.Component {
		return twoPanePage(testid, rendered)
	}
}

// MapPage assembles MAP's five regions: the rail, head, canvas, inspector,
// and foot. The canvas and inspector share a row but remain separate patches.
func MapPage(rendered []string) templ.Component {
	return mapPage(at(rendered, 0), at(rendered, 1), at(rendered, 2), at(rendered, 3), at(rendered, 4))
}

// at is a region that may not have been rendered — which happens only if
// Registry and the page assembly disagree about how many regions a mode has.
// regions_test.go asserts every region reaches the page, so this returning
// empty is a test failure rather than a blank panel in front of anyone.
func at(rendered []string, i int) string {
	if i >= len(rendered) {
		return ""
	}
	return rendered[i]
}

// Render assembles a mode's whole content from Data.
//
// The server does not use this: it renders the regions one at a time so it can
// send them one at a time, which is the point of having regions. This is for
// the callers that want the finished thing — tests asking "what does this mode
// look like", and anything that needs a mode rendered in one piece.
//
// It goes through the same regions and the same Page, so what it produces is
// what the server would assemble, not a second opinion about it.
func Render(slug string, data Data) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		content, ok := Registry[slug]
		if !ok {
			return fmt.Errorf("modes: no content registered for %q", slug)
		}
		rendered := make([]string, len(content.Regions))
		for i, region := range content.Regions {
			var buf bytes.Buffer
			if err := region.Render(data).Render(ctx, &buf); err != nil {
				return fmt.Errorf("modes: rendering %s/%s: %w", slug, region.ID, err)
			}
			rendered[i] = buf.String()
		}
		return content.Page(rendered).Render(ctx, w)
	})
}

// MapSignals is MAP's client-owned view state: what is zoomed to, which tile is
// in hand, which layer a stroke lands on, and which layers are drawn.
//
// None of it is in the URL and none of it is rendered by the server, which is
// not a preference but a correctness requirement. The stream's subscription is
// built when the page loads and cannot change afterwards, so anything the
// server renders from it is frozen at that moment — and the next unrelated
// event, a save or another tab's edit, would re-render the view as it was and
// undo whatever has happened since.
//
// Seeded here and then let go of. Each hide signal starts from what the *file*
// says and follows a stable layer ID when one exists. layerID keeps paint
// selection attached to that layer if a menu changes the file order.
func MapSignals(data Data) string {
	var b strings.Builder
	b.WriteString(`{"zoom":`)
	b.WriteString(strconv.Itoa(mapcanvas.InitialScale(data.Canvas)))
	// The tool and the stamp's orientation. They are the browser's, and travel
	// to the server with each stroke; see turnAction.
	b.WriteString(`,"tool":"stamp","flipH":false,"flipV":false,"flipD":false`)
	b.WriteString(`,"inspectX":`)
	b.WriteString(strconv.Itoa(data.MapView.CellX))
	b.WriteString(`,"inspectY":`)
	b.WriteString(strconv.Itoa(data.MapView.CellY))
	// The grid is drawn, like Tiled's and like the prototype's default. View
	// state, never written to the file — it is scaffolding for the eye.
	b.WriteString(`,"grid":true`)
	b.WriteString(`,"group":0`)
	b.WriteString(`,"tile":0,"layer":`)
	b.WriteString(strconv.Itoa(selectedLayerIndex(data)))
	b.WriteString(`,"layerID":`)
	if i := selectedLayerIndex(data); i < len(data.Canvas.Layers) {
		b.WriteString(strconv.Itoa(data.Canvas.Layers[i].ID))
	} else {
		b.WriteByte('0')
	}
	for _, layer := range data.Canvas.Layers {
		b.WriteString(`,"`)
		b.WriteString(LayerHideSignal(layer.ID, layer.Index))
		b.WriteString(`":`)
		b.WriteString(strconv.FormatBool(layer.HiddenInFile))
	}
	b.WriteByte('}')
	return b.String()
}

func selectedLayerIndex(data Data) int {
	if data.MapView.LayerID > 0 {
		for _, layer := range data.Canvas.Layers {
			if layer.ID == data.MapView.LayerID {
				return layer.Index
			}
		}
	}
	return activeLayerIndex(data.Canvas)
}

// activeLayerIndex is the layer a stroke lands on when the page opens: the
// first one the file draws, so that painting works before anyone has chosen.
func activeLayerIndex(c mapcanvas.Canvas) int {
	for _, layer := range c.Layers {
		if !layer.HiddenInFile {
			return layer.Index
		}
	}
	return 0
}
