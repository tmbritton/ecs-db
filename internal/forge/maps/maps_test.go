package maps_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/maps"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

const collectionTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="props" tilewidth="16" tileheight="16" tilecount="2" columns="0">
 <tile id="0"><image source="barrel.png" width="16" height="16"/></tile>
 <tile id="1"><image source="crate.png" width="16" height="16"/></tile>
</tileset>
`

const twoTileTSX = `<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" name="fixture" tilewidth="16" tileheight="16" tilecount="2" columns="2">
 <image source="fixture.png" width="32" height="16"/>
 <tile id="0" type="floor"><properties><property name="passable" type="bool" value="true"/></properties></tile>
 <tile id="1" type="wall"><properties><property name="passable" type="bool" value="false"/></properties></tile>
</tileset>
`

func mapWith(id, data string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" renderorder="right-down" width="2" height="2" tilewidth="16" tileheight="16" infinite="0" nextlayerid="3" nextobjectid="1">
 <properties>
  <property name="mapId" value="` + id + `"/>
 </properties>
 <tileset firstgid="1" source="fixture.tsx"/>
 <layer id="1" name="ground" width="2" height="2">
  <data encoding="csv">
` + data + `
</data>
 </layer>
 <objectgroup id="2" name="spawns"/>
</map>
`
}

// project writes a directory of maps and returns it.
//
// The tileset images are written too, and have to be: containment resolves
// symlinks, which needs the file to be there. That is the same answer the route
// gives for a picture that is missing — refused — and it is why an image a
// tileset names but nobody shipped is not servable.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func openOne(t *testing.T) (*maps.Session, string) {
	t.Helper()
	dir := project(t, map[string]string{
		"fixture.tsx": twoTileTSX,
		"fixture.png": "not really a png, and nothing here opens it",
		"level1.tmx":  mapWith("level1", "2,2,\n2,1"),
	})
	path := filepath.Join(dir, "level1.tmx")
	s := maps.Open(maps.Config{MapPath: path})
	return s, path
}

func TestOpen_AProjectWithNoMapIsNotAFailure(t *testing.T) {
	s := maps.Open(maps.Config{})
	if got := s.Maps(); len(got) != 0 {
		t.Errorf("got %d maps, want none", len(got))
	}
	if got := s.Problems(); len(got) != 0 {
		t.Errorf("a project with no map has problems: %v", got)
	}
}

func TestOpen_ListsTheConfiguredMapFirstAndMarksIt(t *testing.T) {
	dir := project(t, map[string]string{
		"fixture.tsx":  twoTileTSX,
		"zzz.tmx":      mapWith("zzz", "1,1,\n1,1"),
		"arena.tmx":    mapWith("arena", "1,1,\n1,1"),
		"level1.tmx":   mapWith("level1", "2,2,\n2,1"),
		"notes.txt":    "not a map",
		"tileset.json": `{"not":"a map either"}`,
	})
	s := maps.Open(maps.Config{MapPath: filepath.Join(dir, "level1.tmx")})
	got := s.Maps()
	want := []string{"level1.tmx", "arena.tmx", "zzz.tmx"}
	if len(got) != len(want) {
		t.Fatalf("got %d maps %v, want %v", len(got), names(got), want)
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Errorf("map %d is %q, want %q", i, got[i].Name, want[i])
		}
	}
	if !got[0].Configured {
		t.Error("the configured map is not marked")
	}
	if got[1].Configured || got[2].Configured {
		t.Error("a map the config does not name is marked as configured")
	}
}

func TestOpen_AMapThatWillNotParseIsAProblemAndNotATab(t *testing.T) {
	dir := project(t, map[string]string{
		"fixture.tsx": twoTileTSX,
		"level1.tmx":  mapWith("level1", "2,2,\n2,1"),
		"broken.tmx":  "<map>this is not a map</map>",
	})
	s := maps.Open(maps.Config{MapPath: filepath.Join(dir, "level1.tmx")})
	if n := len(s.Maps()); n != 1 {
		t.Errorf("got %d maps, want 1 — the broken one is not editable", n)
	}
	problems := s.Problems()
	if len(problems) != 1 {
		t.Fatalf("got %d problems, want 1: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0].Path, "broken.tmx") {
		t.Errorf("the problem does not name the file: %v", problems[0])
	}
	if problems[0].Err == nil {
		t.Error("the problem has no reason")
	}
}

// A .tmj is a map the project has. Hiding it would be worse than showing it
// with the reason Story 1 gives for not writing JSON.
func TestOpen_ATmjIsAProblemThatSaysWhatToDo(t *testing.T) {
	dir := project(t, map[string]string{
		"fixture.tsx": twoTileTSX,
		"level1.tmx":  mapWith("level1", "2,2,\n2,1"),
		"other.tmj":   `{"width":2,"height":2}`,
	})
	s := maps.Open(maps.Config{MapPath: filepath.Join(dir, "level1.tmx")})
	problems := s.Problems()
	if len(problems) != 1 || !strings.Contains(problems[0].Err.Error(), ".tmx") {
		t.Fatalf("a .tmj should be listed with what to do about it, got %v", problems)
	}
}

func TestOpen_TheConfiguredMapMissingIsAProblem(t *testing.T) {
	dir := project(t, map[string]string{"fixture.tsx": twoTileTSX})
	s := maps.Open(maps.Config{MapPath: filepath.Join(dir, "gone.tmx")})
	if n := len(s.Maps()); n != 0 {
		t.Errorf("got %d maps, want none", n)
	}
	if len(s.Problems()) != 1 {
		t.Errorf("problems: %v", s.Problems())
	}
}

func TestResolved_ReadsTheTilesetsFromDisk(t *testing.T) {
	s, path := openOne(t)
	m, err := s.Resolved(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Tilesets) != 1 || m.Tilesets[0].Tileset == nil {
		t.Fatalf("tilesets did not resolve: %+v", m.Tilesets)
	}
	if got := m.Tilesets[0].Tileset.Name; got != "fixture" {
		t.Errorf("tileset is %q", got)
	}
	if passable, ok := m.Tilesets[0].Tileset.Passable(1); !ok || passable {
		t.Errorf("tile 1 should be an impassable wall, got passable=%v ok=%v", passable, ok)
	}
}

func TestPreview_ReportsEachBrokenTilesetAndKeepsTheOtherOneDrawable(t *testing.T) {
	src := strings.Replace(mapWith("level1", "20,20,\n20,20"),
		`<tileset firstgid="1" source="fixture.tsx"/>`,
		`<tileset firstgid="1" source="missing-a.tsx"/>
 <tileset firstgid="10" source="missing-b.tsx"/>
 <tileset firstgid="20" source="fixture.tsx"/>`, 1)
	dir := project(t, map[string]string{
		"fixture.tsx": twoTileTSX,
		"fixture.png": "asset",
		"level1.tmx":  src,
	})
	path := filepath.Join(dir, "level1.tmx")
	s := maps.Open(maps.Config{MapPath: path})
	if _, err := s.Resolved(path); err == nil || !strings.Contains(err.Error(), "missing-a.tsx") {
		t.Fatalf("strict resolver no longer refuses the first unreadable tileset: %v", err)
	}
	m, problems, err := s.Preview(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 2 || problems[0].Index != 0 || problems[1].Index != 1 ||
		!strings.Contains(problems[0].Err.Error(), "missing-a.tsx") ||
		!strings.Contains(problems[1].Err.Error(), "missing-b.tsx") {
		t.Fatalf("preview lost a distinct loader error: %+v", problems)
	}
	if m.Tilesets[0].Tileset != nil || m.Tilesets[1].Tileset != nil ||
		m.Tilesets[2].Tileset == nil || m.Layers[0].TileAt(0, 0).GID != 20 {
		t.Fatalf("valid tileset or authored layer lost: %+v", m)
	}
	if err := s.Read(path, func(doc *tiled.Document) error {
		original, err := doc.Map()
		if err == nil && original.Tilesets[2].Tileset != nil {
			t.Error("preview resolved into the working document's memoized map")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Preview("/etc/passwd"); err == nil {
		t.Error("preview escaped the editing session")
	}
}

// Resolution is done fresh each time and never kept, so a tileset edited on
// disk — by Epic 16's TILES mode, or by Tiled in another window — is read
// again rather than served from a tree nothing can invalidate.
func TestResolved_PicksUpATilesetEditedOnDisk(t *testing.T) {
	s, path := openOne(t)
	if _, err := s.Resolved(path); err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(twoTileTSX, `name="fixture"`, `name="edited"`, 1)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "fixture.tsx"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := s.Resolved(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Tilesets[0].Tileset.Name; got != "edited" {
		t.Errorf("tileset is %q, want the edited one — resolution was cached", got)
	}
}

func TestResolved_DoesNotHandOutSomethingAnotherCallerHolds(t *testing.T) {
	s, path := openOne(t)
	first, err := s.Resolved(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Resolved(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two callers were handed the same map")
	}
	first.Layers[0].Data[0] = 999
	if second.Layers[0].Data[0] == 999 {
		t.Error("the two maps share their layer data")
	}
}

func TestOpen_ATilesetThatWillNotOpenIsAProblem(t *testing.T) {
	dir := project(t, map[string]string{"level1.tmx": mapWith("level1", "2,2,\n2,1")})
	s := maps.Open(maps.Config{MapPath: filepath.Join(dir, "level1.tmx")})
	// It is still editable — a missing tileset is what you open the editor to
	// fix — but the reason is reported rather than left as a blank canvas.
	if n := len(s.Maps()); n != 1 {
		t.Errorf("got %d maps, want 1", n)
	}
	problems := s.Problems()
	if len(problems) != 1 || !strings.Contains(problems[0].Err.Error(), "fixture.tsx") {
		t.Fatalf("problems: %v", problems)
	}
}

func TestEdit_DirtyIsAComparison(t *testing.T) {
	s, path := openOne(t)
	if dirty, err := s.Dirty(); err != nil || len(dirty) != 0 {
		t.Fatalf("a freshly opened map is unsaved: %v %v", dirty, err)
	}
	var original []uint32
	if err := s.Read(path, func(d *tiled.Document) error {
		m, err := d.Map()
		if err != nil {
			return err
		}
		original = append(original, m.Layers[0].Data...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	painted := append([]uint32(nil), original...)
	painted[0] = 1
	if err := s.Edit(path, func(d *tiled.Document) error { return d.SetLayerData(0, painted) }); err != nil {
		t.Fatal(err)
	}
	dirty, err := s.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 1 || dirty[0] != path {
		t.Fatalf("dirty is %v, want just the edited map", dirty)
	}

	if err := s.Edit(path, func(d *tiled.Document) error { return d.SetLayerData(0, original) }); err != nil {
		t.Fatal(err)
	}
	if dirty, err := s.Dirty(); err != nil || len(dirty) != 0 {
		t.Fatalf("an edit and its exact reversal left the map unsaved: %v %v", dirty, err)
	}
}

func TestSave_WritesTheMapAndLeavesItClean(t *testing.T) {
	s, path := openOne(t)
	if err := s.Edit(path, func(d *tiled.Document) error { return d.SetLayerData(0, []uint32{1, 1, 1, 1}) }); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "1,1,\n1,1") {
		t.Errorf("the edit was not written:\n%s", raw)
	}
	if dirty, _ := s.Dirty(); len(dirty) != 0 {
		t.Errorf("a saved map is still unsaved: %v", dirty)
	}
}

func TestDiscard_RestoresWithoutTouchingDisk(t *testing.T) {
	s, path := openOne(t)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Edit(path, func(d *tiled.Document) error { return d.SetLayerData(0, []uint32{1, 1, 1, 1}) }); err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(path); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := s.Dirty(); len(dirty) != 0 {
		t.Errorf("discard left the map unsaved: %v", dirty)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("discard wrote to disk")
	}
}

func TestSave_RefusesAMapChangedUnderneathIt(t *testing.T) {
	s, path := openOne(t)
	// No edit of our own: a conflict is about the file, not about our work,
	// and this story ships no way to paint.
	if err := os.WriteFile(path, []byte(mapWith("level1", "1,1,\n1,1")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(path); err == nil {
		t.Fatal("saving over an outside edit was accepted")
	}
	if err := s.Reload(path); err != nil {
		t.Fatal(err)
	}
	// Take theirs: the session now holds what is on disk and is clean.
	if dirty, _ := s.Dirty(); len(dirty) != 0 {
		t.Errorf("reload left the map unsaved: %v", dirty)
	}
	if err := s.Save(path); err != nil {
		t.Errorf("saving after a reload was refused: %v", err)
	}
}

func TestSaveOverwriting_KeepsMine(t *testing.T) {
	s, path := openOne(t)
	if err := s.Edit(path, func(d *tiled.Document) error { return d.SetLayerData(0, []uint32{1, 1, 1, 1}) }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(mapWith("level1", "2,2,\n2,2")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(path); err == nil {
		t.Fatal("the conflict was not reported")
	}
	if err := s.SaveOverwriting(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "1,1,\n1,1") {
		t.Errorf("keeping mine did not write mine:\n%s", raw)
	}
}

// Every operation is checked against the maps the session actually holds. The
// path arrives from a page, and a session that trusted it would let a crafted
// request address any file on disk.
func TestSession_RefusesAPathItDoesNotHold(t *testing.T) {
	s, _ := openOne(t)
	other := filepath.Join(t.TempDir(), "elsewhere.tmx")
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"Read", func() error { return s.Read(other, func(*tiled.Document) error { return nil }) }},
		{"Edit", func() error { return s.Edit(other, func(*tiled.Document) error { return nil }) }},
		{"Save", func() error { return s.Save(other) }},
		{"Discard", func() error { return s.Discard(other) }},
		{"Reload", func() error { return s.Reload(other) }},
		{"SaveOverwriting", func() error { return s.SaveOverwriting(other) }},
		{"Resolved", func() error { _, err := s.Resolved(other); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Error("a path the session does not hold was accepted")
			}
		})
	}
}

// An edit that fails leaves the map exactly as it was, so a handler that
// validates halfway through cannot leave a half-painted layer behind.
func TestEdit_AFailedEditChangesNothing(t *testing.T) {
	s, path := openOne(t)
	err := s.Edit(path, func(d *tiled.Document) error {
		// Too few cells for the layer: the document refuses it.
		return d.SetLayerData(0, []uint32{1})
	})
	if err == nil {
		t.Fatal("the refusal did not reach the caller")
	}
	if dirty, _ := s.Dirty(); len(dirty) != 0 {
		t.Errorf("a refused edit left the map unsaved: %v", dirty)
	}
}

func names(ms []maps.Map) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}

// A map can appear beside the others without Forge doing anything — Tiled
// saving a new level into the same directory is an ordinary afternoon.
func TestRefresh_PicksUpAMapAddedBeside(t *testing.T) {
	s, path := openOne(t)
	if n := len(s.Maps()); n != 1 {
		t.Fatalf("got %d maps, want 1", n)
	}
	added := filepath.Join(filepath.Dir(path), "arena.tmx")
	if err := os.WriteFile(added, []byte(mapWith("arena", "1,1,\n1,1")), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	if got := names(s.Maps()); len(got) != 2 || got[1] != "arena.tmx" {
		t.Errorf("maps are %v, want level1.tmx then arena.tmx", got)
	}
}

func TestRefresh_KeepsUnsavedWorkOnAMapAlreadyOpen(t *testing.T) {
	s, path := openOne(t)
	if err := s.Edit(path, func(d *tiled.Document) error { return d.SetLayerData(0, []uint32{1, 1, 1, 1}) }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "arena.tmx"),
		[]byte(mapWith("arena", "1,1,\n1,1")), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	dirty, err := s.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 1 || dirty[0] != path {
		t.Errorf("a refresh discarded an edit in progress: dirty is %v", dirty)
	}
}

func TestRefresh_DropsAMapThatIsNoLongerThere(t *testing.T) {
	s, path := openOne(t)
	arena := filepath.Join(filepath.Dir(path), "arena.tmx")
	if err := os.WriteFile(arena, []byte(mapWith("arena", "1,1,\n1,1")), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	if err := os.Remove(arena); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	if got := names(s.Maps()); len(got) != 1 {
		t.Errorf("maps are %v, want just level1.tmx", got)
	}
	if got := s.Problems(); len(got) != 0 {
		t.Errorf("dropping a clean map raised a problem: %v", got)
	}
}

// Work held for a map that is in no list is reachable from nothing, so losing
// sight of it is worse than losing it — nobody knows to look.
func TestRefresh_ReportsUnsavedWorkOnAMapThatVanished(t *testing.T) {
	s, path := openOne(t)
	arena := filepath.Join(filepath.Dir(path), "arena.tmx")
	if err := os.WriteFile(arena, []byte(mapWith("arena", "1,1,\n1,1")), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	if err := s.Edit(arena, func(d *tiled.Document) error { return d.SetLayerData(0, []uint32{2, 2, 2, 2}) }); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(arena); err != nil {
		t.Fatal(err)
	}
	s.Refresh()

	problems := s.Problems()
	if len(problems) != 1 || !strings.Contains(problems[0].Path, "arena.tmx") {
		t.Fatalf("problems: %v", problems)
	}
	if !strings.Contains(problems[0].Err.Error(), "unsaved") {
		t.Errorf("the problem does not say what is at stake: %v", problems[0].Err)
	}
	// And it can still be written back, which is usually what somebody wants.
	if err := s.SaveOverwriting(arena); err != nil {
		t.Fatalf("the stranded map could not be saved: %v", err)
	}
	if _, err := os.Stat(arena); err != nil {
		t.Errorf("saving the stranded map did not write it: %v", err)
	}
}

// Discard restores what Forge last knew, not what is on disk now. The two are
// the same until somebody edits the file underneath, and that is exactly when
// the difference matters: Discard is the "undo my mess" button and Reload is
// the "take theirs" one, and swapping them silently loses whichever the user
// did not ask for.
func TestDiscard_RestoresTheLastSaveAndNotWhatIsOnDiskNow(t *testing.T) {
	s, path := openOne(t)
	if err := s.Edit(path, func(d *tiled.Document) error { return d.SetLayerData(0, []uint32{1, 1, 1, 1}) }); err != nil {
		t.Fatal(err)
	}
	// Somebody else edits the file while we hold unsaved work.
	if err := os.WriteFile(path, []byte(mapWith("level1", "2,1,\n1,2")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(path); err != nil {
		t.Fatal(err)
	}

	var got []uint32
	if err := s.Read(path, func(d *tiled.Document) error {
		m, err := d.Map()
		if err != nil {
			return err
		}
		got = append(got, m.Layers[0].Data...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []uint32{2, 2, 2, 1} // the map as it was opened
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("discard gave back %v, want the map as opened %v — it read from disk", got, want)
		}
	}
}

// A map deleted from disk with nothing unsaved is let go of entirely, so it
// cannot be written back by a request naming a path that used to work.
func TestRefresh_LetsGoOfAMapThatVanishedWithNothingUnsaved(t *testing.T) {
	s, path := openOne(t)
	arena := filepath.Join(filepath.Dir(path), "arena.tmx")
	if err := os.WriteFile(arena, []byte(mapWith("arena", "1,1,\n1,1")), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	if err := os.Remove(arena); err != nil {
		t.Fatal(err)
	}
	s.Refresh()

	if err := s.Save(arena); err == nil {
		t.Error("a map the project no longer has was written back")
	}
	if _, err := os.Stat(arena); err == nil {
		t.Error("the deleted map was recreated")
	}
}

// The session owns the lock because editable.File is documented as unsafe for
// concurrent use, and Forge is an HTTP server. The Makefile runs -race over
// ./internal/forge/..., which proves nothing about this package unless
// something here is actually concurrent.
func TestSession_IsSafeForConcurrentUse(t *testing.T) {
	s, path := openOne(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 6 {
			case 0:
				_ = s.Edit(path, func(d *tiled.Document) error {
					return d.SetLayerData(0, []uint32{uint32(i%2 + 1), 2, 2, 1})
				})
			case 1:
				_ = s.Read(path, func(d *tiled.Document) error { _, err := d.Map(); return err })
			case 2:
				_, _ = s.Resolved(path)
			case 3:
				_, _ = s.Dirty()
			case 4:
				s.Refresh()
			default:
				_ = s.Maps()
				_ = s.Held()
				_ = s.Problems()
			}
		}(i)
	}
	wg.Wait()
}

// The asset route's allow-list: only images the project's own tilesets refer
// to, so a path nobody names is not served whatever it looks like.
func TestServes_OnlyImagesTheProjectsTilesetsName(t *testing.T) {
	s, path := openOne(t)
	dir := filepath.Dir(path)

	// Filled when the map was opened, so the first page load can draw.
	if !s.Serves(filepath.Join(dir, "fixture.png")) {
		t.Error("the tileset's own image is not allowed")
	}
	if _, err := s.Resolved(path); err != nil {
		t.Fatal(err)
	}
	if !s.Serves(filepath.Join(dir, "fixture.png")) {
		t.Error("a render lost the allow-list")
	}
	for _, denied := range []string{
		"/etc/passwd",
		"/etc/hosts",
		filepath.Join(dir, "level1.tmx"),
		filepath.Join(dir, "fixture.tsx"),
		filepath.Join(dir, "..", "outside.png"),
		"",
	} {
		if s.Serves(denied) {
			t.Errorf("%q is served and no tileset names it", denied)
		}
	}
}

// A collection tileset has no sheet: each tile is a whole file, so the pictures
// the allow-list has to know about are the tiles' own.
func TestServes_AllowsACollectionTilesOwnPictures(t *testing.T) {
	dir := project(t, map[string]string{
		"fixture.tsx": twoTileTSX,
		"fixture.png": "x",
		"barrel.png":  "x",
		"crate.png":   "x",
		"props.tsx":   collectionTSX,
		"level1.tmx": strings.Replace(mapWith("level1", "2,2,\n2,1"),
			`<tileset firstgid="1" source="fixture.tsx"/>`,
			`<tileset firstgid="1" source="fixture.tsx"/>`+"\n <tileset firstgid=\"100\" source=\"props.tsx\"/>", 1),
	})
	s := maps.Open(maps.Config{MapPath: filepath.Join(dir, "level1.tmx")})
	if got := s.Problems(); len(got) != 0 {
		t.Fatalf("problems: %v", got)
	}
	for _, name := range []string{"fixture.png", "barrel.png", "crate.png"} {
		if !s.Serves(filepath.Join(dir, name)) {
			t.Errorf("%s is not allowed, and a tileset names it", name)
		}
	}
	if s.Serves(filepath.Join(dir, "props.tsx")) {
		t.Error("the tileset file itself is allowed")
	}
}

// The second gate. The allow-list makes a path the *request* invented
// unreachable; it does not stop a path the *project* names from pointing
// outside the project — which is what a downloaded asset pack can do.
func TestServes_RefusesWhatATilesetNamesOutsideTheProject(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("a relative source that climbs out", func(t *testing.T) {
		dir := project(t, map[string]string{
			"fixture.tsx": strings.Replace(twoTileTSX, `source="fixture.png"`, `source="../secret.png"`, 1),
			"level1.tmx":  mapWith("level1", "2,2,\n2,1"),
		})
		up := filepath.Join(filepath.Dir(dir), "secret.png")
		if err := os.WriteFile(up, []byte("SECRET"), 0o600); err != nil {
			t.Fatal(err)
		}
		s := maps.Open(maps.Config{Root: dir, MapPath: filepath.Join(dir, "level1.tmx")})
		if s.Serves(up) {
			t.Error("a tileset climbed out of the project and was served")
		}
	})

	t.Run("an absolute source", func(t *testing.T) {
		target := filepath.Join(outside, "secret.png")
		dir := project(t, map[string]string{
			"fixture.tsx": strings.Replace(twoTileTSX, `source="fixture.png"`, `source="`+target+`"`, 1),
			"level1.tmx":  mapWith("level1", "2,2,\n2,1"),
		})
		s := maps.Open(maps.Config{Root: dir, MapPath: filepath.Join(dir, "level1.tmx")})
		if s.Serves(target) {
			t.Error("a tileset named an absolute path outside the project and it was served")
		}
	})

	t.Run("a symlink that leaves the project", func(t *testing.T) {
		dir := project(t, map[string]string{
			"fixture.tsx": twoTileTSX,
			"level1.tmx":  mapWith("level1", "2,2,\n2,1"),
		})
		link := filepath.Join(dir, "fixture.png")
		if err := os.Symlink(filepath.Join(outside, "secret.png"), link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		s := maps.Open(maps.Config{Root: dir, MapPath: filepath.Join(dir, "level1.tmx")})
		if s.Serves(link) {
			t.Error("a symlink out of the project was served")
		}
	})
}

// A project laid out the ordinary way keeps working: a map in one directory and
// its art in another, both under the project root.
func TestServes_AllowsArtInAnotherDirectoryOfTheSameProject(t *testing.T) {
	root := t.TempDir()
	maps_ := filepath.Join(root, "maps")
	art := filepath.Join(root, "art")
	for _, d := range []string{maps_, art} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(art, "fixture.png"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tsx := strings.Replace(twoTileTSX, `source="fixture.png"`, `source="../art/fixture.png"`, 1)
	if err := os.WriteFile(filepath.Join(maps_, "fixture.tsx"), []byte(tsx), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(maps_, "level1.tmx"), []byte(mapWith("level1", "2,2,\n2,1")), 0o600); err != nil {
		t.Fatal(err)
	}

	s := maps.Open(maps.Config{Root: root, MapPath: filepath.Join(maps_, "level1.tmx")})
	if got := s.Problems(); len(got) != 0 {
		t.Fatalf("problems: %v", got)
	}
	if !s.Serves(filepath.Join(art, "fixture.png")) {
		t.Error("art in a sibling directory of the same project is not served")
	}
}
