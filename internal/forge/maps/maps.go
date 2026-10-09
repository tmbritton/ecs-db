// Package maps is the editing session for a project's Tiled maps.
//
// A sibling of internal/forge/machines and internal/forge/session rather than a
// generalisation of either. They will look alike and they diverge: machines
// have mod-override order and maps do not; maps have a resolved tileset tree
// and machines do not. Two clear sessions beat one that is mostly `if kind ==`.
//
// The stdlib has a package called maps. Nothing that imports this one uses it —
// only internal/renderer does, and a renderer will never hold an editing
// session — so the name that says what this is wins over the one that avoids a
// collision nobody will meet.
package maps

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tmbritton/ecs-db/internal/forge/editable"
	"github.com/tmbritton/ecs-db/internal/forge/mapfile"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

// Config is what a session needs to find a project's maps.
type Config struct {
	// Root is the project directory: nothing outside it is served over HTTP.
	//
	// Empty falls back to the configured map's own directory, which is all a
	// session given nothing but a map path can honestly claim to know.
	Root string
	// MapPath is the map game.toml names, already resolved against the config
	// file that declared it. Empty for a project with no [map] section, which
	// is a state and not a failure: Forge opens, MAP mode says so, and every
	// other mode works.
	MapPath string
}

// Map is one of the project's maps, as the tab strip shows it.
type Map struct {
	Path string
	Name string // the file's base name, which is what a tab says
	// Configured marks the one game.toml names — the only one `ecs-db run`
	// loads. A project may hold five maps and the engine reads exactly one, so
	// editing any of the others is real work that changes nothing about the
	// game until the config says otherwise. A strip that hid that would be
	// lying by omission.
	Configured bool
}

// Session holds one editable document per map the project has.
type Session struct {
	cfg Config

	mu    sync.Mutex
	files map[string]*editable.File[*tiled.Document]
	// tilesets records whether each open map's tilesets resolve. See
	// checkTilesets for why it is recorded rather than recomputed per render.
	tilesets map[string]error
	// images is every image path the open maps' tilesets refer to, recorded by
	// Resolved. See Serves.
	images map[string]bool
	// opener reads working TSX bytes when the TILES session holds this path.
	// Nil keeps the prior disk-only behavior for projects without that session.
	opener   func(string) ([]byte, error)
	order    []string // paths, configured map first
	maps     []Map
	problems []project.Problem
}

// Open finds the project's maps and opens each one.
//
// It returns no error, and cannot: a map that will not open is a Problem rather
// than a failure — the same rule machines.Session uses, and for the same reason.
// A broken file is what someone opened the editor to fix, and refusing to start
// removes the only way to do it. A project with no [map] at all opens a session
// holding nothing, which is a state MAP mode renders.
func Open(cfg Config) *Session {
	if cfg.Root == "" && cfg.MapPath != "" {
		cfg.Root = filepath.Dir(cfg.MapPath)
	}
	if cfg.Root != "" {
		cfg.Root = filepath.Clean(cfg.Root)
	}
	if cfg.MapPath != "" {
		// One file must not become two tabs with two independent editable
		// files. config.Load leaves an absolute path uncleaned, so a game.toml
		// naming "/proj/maps/./level1.tmx" would not match the entry ReadDir
		// gives for the same file.
		cfg.MapPath = filepath.Clean(cfg.MapPath)
	}
	s := &Session{
		cfg:      cfg,
		files:    map[string]*editable.File[*tiled.Document]{},
		tilesets: map[string]error{},
		images:   map[string]bool{},
	}
	s.reload()
	return s
}

// SetTilesetOpener injects a working-byte reader from TILES. It never takes
// the tileset session lock here; Preview calls it under the map lock, while
// TILES Refresh discovers map refs before taking its own lock.
func (s *Session) SetTilesetOpener(opener func(string) ([]byte, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opener = opener
}

func (s *Session) openTileset(path string) ([]byte, error) {
	if s.opener != nil {
		return s.opener(path)
	}
	return os.ReadFile(path)
}

// Refresh re-reads which maps the project has.
//
// Called before rendering, because a map can appear beside the others without
// Forge doing anything: Tiled saving a new level into the same directory is an
// ordinary afternoon. A file already open keeps its working value, so a
// refresh triggered by somebody else's file must not discard an edit in
// progress.
//
// It costs one directory listing. It does *not* re-read the maps already open
// — those are parsed once, when they first appear — so the steady state is a
// ReadDir and nothing else.
func (s *Session) Refresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reload()
}

// reload rebuilds the set of maps. Caller holds s.mu, or has not published the
// session yet.
func (s *Session) reload() {
	s.maps, s.order, s.problems = nil, nil, nil
	if s.cfg.MapPath == "" {
		return
	}
	present := map[string]bool{}
	for _, path := range discover(s.cfg.MapPath) {
		present[path] = true
		if _, ok := s.files[path]; ok {
			s.list(path)
			continue
		}
		f, err := mapfile.Open(path)
		if err != nil {
			// Listed as a problem and not as a tab. A map that does not parse
			// has nothing to draw and nothing to paint on; what it has is a
			// reason, and that is what the mode shows.
			s.problems = append(s.problems, project.Problem{Path: path, Err: err})
			continue
		}
		s.files[path] = f
		s.checkTilesets(path)
		s.list(path)
	}
	s.forget(present)
	s.strand()
}

// list adds an open map to the tab strip, with whatever is wrong with it.
func (s *Session) list(path string) {
	s.order = append(s.order, path)
	s.maps = append(s.maps, Map{
		Path:       path,
		Name:       filepath.Base(path),
		Configured: path == s.cfg.MapPath,
	})
	if err := s.tilesets[path]; err != nil {
		s.problems = append(s.problems, project.Problem{Path: path, Err: err})
	}
}

// checkTilesets records whether a map's tilesets resolve, so the strip can say
// so. A tileset that will not open does not stop the map being edited — it is
// exactly the kind of thing an author opens Forge to fix, and a blank canvas
// with no explanation is the failure to avoid.
//
// **Recorded rather than recomputed.** Refresh runs before every render, and
// resolving means re-parsing the whole map and re-reading its tilesets — 24 ms
// on a 500x500 map, per map, measured in review. So it is done when a map is
// first opened and again whenever its own bytes change, which is every path
// that can invalidate the answer from Forge's side.
//
// What that misses is a .tsx fixed on disk with the map untouched: the problem
// stays listed until something else happens. Resolved re-reads from disk on
// every call, so the canvas shows the truth; only this line lags.
func (s *Session) checkTilesets(path string) {
	f, ok := s.files[path]
	if !ok {
		return
	}
	if s.tilesets == nil {
		s.tilesets = map[string]error{}
	}
	_, err := s.resolve(path, f.Current)
	s.tilesets[path] = err
}

// forget drops maps that are no longer on disk — deleted in a file manager, or
// moved — keeping only those with unsaved work, which strand reports.
func (s *Session) forget(present map[string]bool) {
	for path, f := range s.files {
		if present[path] {
			continue
		}
		if dirty, err := f.Dirty(); err == nil && dirty {
			continue
		}
		delete(s.files, path)
		delete(s.tilesets, path)
	}
}

// strand reports work held for a map that is no longer in any list.
//
// A map deleted from disk while Forge held unsaved edits to it is in no tab and
// reachable from nothing, so without this the work is kept and invisible —
// which is worse than losing it, because nobody knows to look. Saving it puts
// the file back, which is usually what somebody wants.
func (s *Session) strand() {
	listed := map[string]bool{}
	for _, path := range s.order {
		listed[path] = true
	}
	for path, f := range s.files {
		if listed[path] {
			continue
		}
		if dirty, err := f.Dirty(); err != nil || !dirty {
			continue
		}
		s.problems = append(s.problems, project.Problem{
			Path: path,
			Err: fmt.Errorf("this map is no longer in the project and has unsaved changes; " +
				"saving it writes the file back"),
		})
	}
}

// discover is the answer to "which maps does this project have": the configured
// one, then the .tmx and .tmj files beside it, by name.
//
// Its own directory and no other, with no recursion. Epic 17's Open Map dialog
// is where "some other file somewhere else" belongs; a tab strip is for the
// maps of the project you have open.
//
// A .tmj is listed as a problem rather than passed over. It is a map the project
// has, and Story 1's refusal says what to do about it — which is more use than
// a file that silently is not there.
func discover(configured string) []string {
	out := []string{configured}
	seen := map[string]bool{configured: true}

	entries, err := os.ReadDir(filepath.Dir(configured))
	if err != nil {
		// The configured map's own directory is unreadable. Its own open will
		// report that in terms of the file somebody actually named, which is
		// the more useful sentence of the two.
		return out
	}
	var siblings []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(filepath.Dir(configured), e.Name())
		if seen[path] || !isMap(e.Name()) {
			continue
		}
		seen[path] = true
		siblings = append(siblings, path)
	}
	// No sort: os.ReadDir returns its entries sorted by filename, so the strip
	// is already in name order. Sorting again was a second claim to the same
	// property, and nothing could tell whether it was load-bearing.
	return append(out, siblings...)
}

func isMap(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".tmx", ".tmj":
		return true
	default:
		return false
	}
}

// Configured is the map game.toml names, or empty for a project that declares
// none.
//
// Read by the mode so it can tell two states apart that both leave the tab strip
// empty: a project with no [map] at all, which is a project without a level, and
// one whose configured map is missing or will not parse, which is a broken
// project with a reason to show. Answering both with "this project declares no
// map" told the second group to add a section they already have.
func (s *Session) Configured() string { return s.cfg.MapPath }

// Root is the directory this session will serve maps from. It is what the
// server's external-change poller watches, so that a map added or removed by
// Tiled while Forge is open still reaches the page.
func (s *Session) Root() string { return s.cfg.Root }

// Maps is the project's maps, configured one first.
func (s *Session) Maps() []Map {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Map(nil), s.maps...)
}

// Paths is the same list as paths, for the handlers that check one.
func (s *Session) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.order...)
}

// Held is every map the session has a file for, including one kept back
// because it had unsaved work and then vanished from the project.
//
// Distinct from Paths, which is the tab strip. A stranded map is in no strip
// and reachable from nothing, so the operations that can still do something
// with it — writing it back, or giving up its work — check against this instead.
func (s *Session) Held() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.files))
	for path := range s.files {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// Problems is every reason a map the project has is not editable, or is
// editable and incomplete.
func (s *Session) Problems() []project.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]project.Problem(nil), s.problems...)
}

// file resolves a path to an open map, under the lock.
//
// Every operation goes through it. The path arrives from a page, and a session
// that took it on trust would let a crafted request read or overwrite any file
// on disk. The handlers check too, in different words, so a test can show which
// layer answered.
func (s *Session) file(path string) (*editable.File[*tiled.Document], error) {
	f, ok := s.files[path]
	if !ok {
		return nil, fmt.Errorf("maps: %s is not a map this project has open", path)
	}
	return f, nil
}

// Read runs fn against the working document.
//
// The document is the session's own, not a copy: a tiled.Document is the file's
// bytes plus a tree over them, and copying it would mean re-parsing on every
// read. So fn must not mutate it — Edit is what mutates — and the lock is held
// for the duration.
func (s *Session) Read(path string, fn func(*tiled.Document) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.file(path)
	if err != nil {
		return err
	}
	return fn(f.Current)
}

// Edit applies fn to the working document under the lock.
//
// An fn that returns an error leaves the map untouched, which is the document's
// own guarantee rather than this one's: every edit on tiled.Document validates
// before it changes anything, so a refusal has already changed nothing by the
// time it gets here.
func (s *Session) Edit(path string, fn func(*tiled.Document) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.file(path)
	if err != nil {
		return err
	}
	return fn(f.Current)
}

// Resolved is the map as it stands, with its tilesets read from disk.
//
// **Parsed fresh and never held.** Two reasons, and both are about staleness
// rather than tidiness. A tileset is shared state — Epic 16's TILES mode edits
// the same .tsx, and so does Tiled in another window — and a resolved tree kept
// here has no signal to invalidate on, so the palette would go quietly wrong.
// And Document.Map is memoised, so resolving into *that* value would give the
// resolution a lifetime ending at the next edit, for no reason a caller could
// predict.
//
// The cost is one parse plus a few small file reads. Story 3 renders from this
// on every patch and is where the cost will show; Story 1 already recorded that
// parsing, not the tree, is what stops scaling first.
func (s *Session) Resolved(path string) (*tiled.Map, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.file(path)
	if err != nil {
		return nil, err
	}
	return s.resolve(path, f.Current)
}

// TilesetProblem names one reference the working map could not resolve.
// Several can be broken independently; stopping at the first hides the rest.
type TilesetProblem struct {
	Index  int
	Source string
	Err    error
}

// Preview returns a detached working map with every resolvable tileset filled
// in and every unresolved reference reported. Unlike Resolved it can draw the
// valid part of a broken map, which is when an editor is most useful. Like
// Resolved, it parses afresh: resolving into Document.Map's cached value would
// mutate the session behind Read's supposedly read-only callback.
func (s *Session) Preview(path string) (*tiled.Map, []TilesetProblem, error) {
	var preview *tiled.Map
	var problems []TilesetProblem
	err := s.Read(path, func(doc *tiled.Document) error {
		var err error
		preview, err = tiled.Parse(doc.Bytes(), filepath.Base(path))
		if err != nil {
			return err
		}
		for i, ref := range preview.Tilesets {
			one := *preview
			one.Tilesets = []tiled.TilesetRef{ref}
			if err := one.ResolveTilesets(filepath.Dir(path), s.openTileset); err != nil {
				problems = append(problems, TilesetProblem{Index: i, Source: ref.Source, Err: err})
				continue
			}
			preview.Tilesets[i] = one.Tilesets[0]
		}
		// The assets of valid tilesets are still served if another reference
		// failed. recordImages is called under Read's session lock.
		s.recordImages(preview)
		return nil
	})
	return preview, problems, err
}

func (s *Session) resolve(path string, doc *tiled.Document) (*tiled.Map, error) {
	// From the bytes rather than from doc.Map(), so what comes back belongs to
	// the caller and resolving it cannot reach the memoised value.
	m, err := tiled.Parse(doc.Bytes(), filepath.Base(path))
	if err != nil {
		return nil, err
	}
	if err := m.ResolveTilesets(filepath.Dir(path), s.openTileset); err != nil {
		return nil, err
	}
	s.recordImages(m)
	return m, nil
}

// recordImages notes every picture this map's tilesets refer to. Caller holds
// s.mu.
func (s *Session) recordImages(m *tiled.Map) {
	for _, ref := range m.Tilesets {
		s.recordTilesetImages(ref.Tileset)
	}
}

// TrackTilesetImages registers art from a project-held tileset that TILES has
// just described. A TSJ can change on disk without a map changing; its new
// image must not require visiting MAP first. The caller vets the selected
// tileset; Serves still checks root containment on every asset request.
func (s *Session) TrackTilesetImages(ts *tiled.Tileset) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordTilesetImages(ts)
}

// recordTilesetImages runs under the map session lock.
func (s *Session) recordTilesetImages(ts *tiled.Tileset) {
	if ts == nil {
		return
	}
	add := func(path string) {
		if path == "" {
			return
		}
		s.images[path] = true
		if abs, err := filepath.Abs(path); err == nil {
			s.images[abs] = true // TILES holds canonical paths; MAP may be relative
		}
	}
	add(ts.Image.Path)
	for _, tile := range ts.Tiles {
		add(tile.Image.Path)
	}
}

// Serves reports whether an image may be sent to the browser.
//
// **Two gates, and both are needed.** The first is the allow-list: the set of
// files this project's own tilesets name, filled by every resolve. It makes a
// path the *request* invented unreachable — "..", an absolute path, a
// case-differing spelling — without the route doing any path arithmetic, and
// it is the reason the browser can only ask for pictures the page it was given
// told it about.
//
// The second is containment, and the first gate does not imply it. A tileset is
// a file in the project, and a project can be a clone of somebody else's
// repository: a .tsx naming `source="../../../etc/passwd"`, or an absolute
// path, or a fixture.png that is a symlink out of the tree, is a file the
// project names and is not a file this may serve. Without this, opening Forge
// on a downloaded asset pack turns /forge/asset into a general read primitive
// for anything with a picture's extension. Found by review, and the code said
// the opposite in as many words.
//
// The cost is stated rather than hidden: **art outside the project directory
// cannot be previewed.** Tiled writes an absolute source the moment art lives
// elsewhere, so that is a real project shape this refuses to draw. Reaching it
// wants a deliberate opt-in naming the other directory, not a silent default.
//
// The allow-list only ever grows: an image a tileset stops naming stays
// servable until Forge restarts. That is a stale permission to read a file the
// project referred to a moment ago and is still inside it, which is not a way
// out of anything.
func (s *Session) Serves(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.images[path] && s.contained(path)
}

// contained reports whether a path resolves to somewhere inside the project.
//
// Symlinks are followed on both sides before comparing, because a link is how
// a path inside the tree names a file outside it — and comparing the strings
// would say the link is fine. A path that will not resolve is refused: it
// either does not exist or cannot be read, and both are answered the same way
// by the route.
func (s *Session) contained(path string) bool {
	// No guard for an empty root: EvalSymlinks refuses one, which is the answer
	// a session that knows no project should give anyway.
	root, err := filepath.EvalSymlinks(s.cfg.Root)
	if err != nil {
		return false
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return false
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Dirty is every map with unsaved work: the tab strip in order, then anything
// stranded.
//
// Stranded maps included, deliberately. They are the ones nothing else can
// show, so leaving them out is how every other mode's footer comes to read
// "✓ saved" over work that is real — the exact failure the Elsewhere notice
// exists to prevent.
func (s *Session) Dirty() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []string
	listed := map[string]bool{}
	for _, path := range s.order {
		listed[path] = true
		dirty, err := s.files[path].Dirty()
		if err != nil {
			return nil, err
		}
		if dirty {
			out = append(out, path)
		}
	}
	stranded := make([]string, 0, len(s.files))
	for path := range s.files {
		if !listed[path] {
			stranded = append(stranded, path)
		}
	}
	sort.Strings(stranded)
	for _, path := range stranded {
		dirty, err := s.files[path].Dirty()
		if err != nil {
			return nil, err
		}
		if dirty {
			out = append(out, path)
		}
	}
	return out, nil
}

// Save writes one map, refusing if it changed on disk since it was opened.
func (s *Session) Save(path string) error { return s.act(path, (*editable.File[*tiled.Document]).Save) }

// SaveOverwriting is Save without the external-modification check: "keep mine".
func (s *Session) SaveOverwriting(path string) error {
	return s.act(path, (*editable.File[*tiled.Document]).SaveOverwriting)
}

// Discard restores the working document from the last save.
func (s *Session) Discard(path string) error {
	return s.act(path, (*editable.File[*tiled.Document]).Discard)
}

// Reload takes what is on disk: the other way out of a conflict.
func (s *Session) Reload(path string) error {
	return s.act(path, (*editable.File[*tiled.Document]).Reload)
}

func (s *Session) act(path string, do func(*editable.File[*tiled.Document]) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.file(path)
	if err != nil {
		return err
	}
	if err := do(f); err != nil {
		return err
	}
	// Save, discard and reload all change which bytes the map is, so whether
	// its tilesets resolve is a question with a possibly different answer.
	s.checkTilesets(path)
	return nil
}
