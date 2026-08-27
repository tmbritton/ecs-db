package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// assetTypes are the picture formats this route will serve, by extension.
//
// A second gate behind the allow-list, and a narrow one: the allow-list already
// makes it impossible to ask for a file no tileset names, and this makes it
// impossible to talk the route into serving one of those files as something it
// is not — a .tsx that a tileset happened to name as its image, say.
var assetTypes = map[string]string{
	".png":  "image/png",
	".gif":  "image/gif",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

func (s *Server) registerAssetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /forge/asset", s.handleAsset)
}

// assetURL is how a resolved image path reaches the browser.
//
// One function, used by the canvas builder and by nothing else, so the route
// and the links to it cannot drift.
func assetURL(path string) string {
	return "/forge/asset?path=" + url.QueryEscape(path)
}

// handleAsset serves a picture one of the project's tilesets refers to.
//
// **The first route in Forge that reads a filesystem path out of a file the
// user controls**, and the whole risk of it is being talked into reading
// something else. The session answers both halves of that: whether one of the
// project's tilesets names this path, and whether it resolves to somewhere
// inside the project. Neither alone is enough — a .tsx in a downloaded asset
// pack can name a symlink out of the tree — and the route itself does no path
// arithmetic. See maps.Session.Serves.
func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.mapSession(w)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" || !sess.Serves(path) {
		// The same answer for "no such file" and "not yours to read", so the
		// route cannot be used to find out what exists.
		http.NotFound(w, r)
		return
	}
	mime, ok := assetTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		http.NotFound(w, r)
		return
	}
	// Stat before open, and this order is the point. A FIFO named by a tileset
	// blocks os.Open until somebody opens the other end — for ever, in practice
	// — which leaks a goroutine per request, and the server deliberately has no
	// write timeout because SSE streams live for the life of a page. Checking
	// the mode off the *open file* is therefore a check that never runs.
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", mime)
	// The first route in Forge to send bytes out of a user's file on Forge's own
	// origin. A response sniffed as HTML there would be same-origin script with
	// the schema and map write routes in reach, so the type this sets is the
	// type the browser has to use.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Not cached: this is a project file somebody is editing, and Epic 16's
	// TILES mode changes the very pictures this serves. ServeContent's
	// modification-time handling gives the browser a conditional request
	// instead, which is the freshness this wants without a stale palette.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}
