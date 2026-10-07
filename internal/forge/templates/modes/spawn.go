package modes

import (
	"math"
	"strconv"

	"github.com/a-h/templ"

	"github.com/tmbritton/ecs-db/internal/forge/spawn"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

func spawnReason(s schema.DatabaseSchema, name string) string { return spawn.Reason(&s, name) }

func spawnDragType(name string) string {
	// The same escaping as layer names. Datastar rewrites @action( even inside
	// a quoted JS literal, so strconv.Quote alone corrupts a valid type name.
	return "evt.dataTransfer.setData('text/plain', " + quoteJS("type:"+name) + ")"
}

func spawnStyle(o tiled.Object, tileHeight int) templ.SafeCSS {
	y := o.Y
	if o.GID != 0 {
		y -= float64(tileHeight)
	}
	return templ.SafeCSS("--spawn-x:" + ftoa(o.X) + ";--spawn-y:" + ftoa(y))
}

func spawnCellLabel(o tiled.Object, w, h int) string {
	if w <= 0 || h <= 0 {
		return "?"
	}
	y := o.Y
	if o.GID != 0 {
		y--
	}
	return strconv.Itoa(int(math.Floor(o.X/float64(w)))) + "," +
		strconv.Itoa(int(math.Floor(y/float64(h))))
}

func spawnDropAction(path string) string {
	base := "'/forge/map/spawn/"
	where := "?map=" + urlValue(path)
	xy := " + '&x=' + evt.detail.x + '&y=' + evt.detail.y"
	return "evt.detail.kind === 'type' ? @post(" + base + "place" + where + "&group=' + $group + '&type=' + encodeURIComponent(evt.detail.value)" + xy + ") : " +
		"@post(" + base + "move" + where + "&id=' + evt.detail.value" + xy + ")"
}

func spawnDeleteAction(path string, id int) string {
	return "@post('/forge/map/spawn/delete?map=" + urlValue(path) + "&id=" + itoa(id) + "')"
}
