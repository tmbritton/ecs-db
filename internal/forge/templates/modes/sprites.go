package modes

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/a-h/templ"
)

func spriteHref(path, name string) string {
	return "/forge/sprites?" + (url.Values{"file": {path}, "animation": {name}}).Encode()
}

func spriteImageURL(sheet string) string {
	return "/forge/sprites/image?sheet=" + url.QueryEscape(sheet)
}

func spriteFieldAction(path, name, field string) string {
	return valueAction("/forge/sprites/edit/"+field, "value", "file", path, "name", name)
}

func spriteFrames(frames []int) string {
	parts := make([]string, len(frames))
	for i, frame := range frames {
		parts[i] = strconv.Itoa(frame)
	}
	return strings.Join(parts, ", ")
}

func spriteFrameStyle(sheet string, tileSize, column int) templ.SafeCSS {
	const scale = 4
	return templ.SafeCSS("width:" + strconv.Itoa(tileSize*scale) + "px;height:" + strconv.Itoa(tileSize*scale) +
		"px;background-image:url(" + spriteImageURL(sheet) + ");background-size:auto " + strconv.Itoa(tileSize*scale) +
		"px;background-position:-" + strconv.Itoa(column*tileSize*scale) + "px 0;image-rendering:pixelated")
}

func spriteCreateAction(kind string, path string) string {
	base := "/forge/sprites/create/" + kind + "?file=" + url.QueryEscape(path)
	if kind == "binding" {
		return "@post('" + base + "&name=' + encodeURIComponent($spriteBindingName) + '&sheet=' + encodeURIComponent($spriteBindingSheet))"
	}
	return "@post('" + base + "&name=' + encodeURIComponent($spriteCreateName) + '&sheet=' + encodeURIComponent($spriteCreateSheet) + '&frames=' + encodeURIComponent($spriteCreateFrames) + '&fps=' + encodeURIComponent($spriteCreateFPS) + '&loop=' + encodeURIComponent($spriteCreateLoop))"
}
