package modes

import "net/url"

// mapHref is the link to a map, by path.
//
// By path rather than by name: the session is keyed on the path because that is
// what a file is, and two maps in one project may one day share a base name.
func mapHref(path string) string {
	return "/forge/map?map=" + url.QueryEscape(path)
}
