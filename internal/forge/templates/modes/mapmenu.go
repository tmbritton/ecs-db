package modes

import (
	"strconv"

	"github.com/a-h/templ"
	"github.com/tmbritton/ecs-db/internal/forge/templates/components"
)

// mapMenuAction sends a target and viewport coordinates. Only canvas targets
// also need map-cell coordinates; the browser knows the zoom and scroll while
// the server knows which visible tile at that cell is topmost.
func mapMenuAction(path, target string, params ...string) string {
	u := "/forge/map/menu?map=" + urlValue(path) + "&target=" + target
	for i := 0; i+1 < len(params); i += 2 {
		u += "&" + params[i] + "=" + urlValue(params[i+1])
	}
	return "evt.preventDefault(); evt.stopPropagation(); @post('" + u + "&x=' + evt.clientX + '&y=' + evt.clientY)"
}

func mapCanvasMenuAction(path string) string {
	u := "/forge/map/menu?map=" + urlValue(path) + "&target=canvas"
	return "evt.preventDefault(); @post('" + u +
		"&cellx=' + Math.floor((evt.clientX - el.getBoundingClientRect().left) / Number(el.dataset.cellW))" +
		" + '&celly=' + Math.floor((evt.clientY - el.getBoundingClientRect().top) / Number(el.dataset.cellH))" +
		" + '&x=' + evt.clientX + '&y=' + evt.clientY)"
}

func mapMenuStyle(m MapMenu) templ.SafeCSS {
	return templ.SafeCSS("left:" + num(m.X) + "px;top:" + num(m.Y) + "px")
}

func mapCloseAction(m MapMenu) string {
	return "@post('/forge/map/menu?close=1&token=" + urlValue(m.Token) + "')"
}

func mapMenuTitle(m MapMenu) string {
	switch m.Kind {
	case "layer":
		return "LAYER · " + m.LayerName
	case "spawn":
		return "SPAWN · #" + itoa(m.ObjectID)
	default:
		return "TILE · " + itoa(m.CellX) + "," + itoa(m.CellY)
	}
}

func mapMenuItems(m MapMenu) []components.MenuItem {
	close := mapCloseAction(m)
	switch m.Kind {
	case "layer":
		base := "/forge/map/layer?map=" + urlValue(m.Path) + "&layer=" + itoa(m.Layer) + "&id=" + itoa(m.LayerID) + "&token=" + urlValue(m.Token)
		hide := "$" + LayerHideSignal(m.ViewID, m.Layer)
		items := []components.MenuItem{
			{Label: "Rename…", Action: "(name => name && @post('" + base + "&op=rename&name=' + encodeURIComponent(name)))(prompt('Rename layer to:', " + quoteJS(m.LayerName) + "))"},
			{Label: "Show / hide in Forge", Action: hide + " = !" + hide + "; " + close},
		}
		if m.CanMoveUp {
			items = append(items, components.MenuItem{Label: "Move up", Action: "@post('" + base + "&op=up')"})
		}
		if m.CanMoveDown {
			items = append(items, components.MenuItem{Label: "Move down", Action: "@post('" + base + "&op=down')"})
		}
		if m.CanDelete {
			items = append(items, components.MenuItem{Divider: true}, components.MenuItem{
				Label: "Delete layer", Danger: true,
				Action: "confirm(" + quoteJS("Delete "+m.LayerName+"? On the next game load, this removes all tiles the layer contributed.") +
					") && @post('" + base + "&op=delete')",
			})
		}
		return items
	case "spawn":
		base := "?map=" + urlValue(m.Path) + "&id=" + itoa(m.ObjectID) + "&token=" + urlValue(m.Token)
		return []components.MenuItem{
			{Label: "Duplicate spawn", Action: "@post('/forge/map/spawn/duplicate" + base + "')"},
			{Divider: true},
			{Label: "Delete spawn", Danger: true, Action: "@post('/forge/map/spawn/delete" + base + "')"},
		}
	case "canvas":
		tile := m.Tile
		return []components.MenuItem{{
			Label: "Pick tile " + strconv.FormatUint(uint64(tile.GID), 10),
			Action: "$tile = " + strconv.FormatUint(uint64(tile.GID), 10) +
				"; $flipH = " + strconv.FormatBool(tile.FlipH) +
				"; $flipV = " + strconv.FormatBool(tile.FlipV) +
				"; $flipD = " + strconv.FormatBool(tile.FlipD) + "; " + close,
		}}
	default:
		return nil
	}
}
