package tilesets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/editable"
	"github.com/tmbritton/ecs-db/internal/forge/maps"
	"github.com/tmbritton/ecs-db/internal/tiled"
)

const sharedTSX = `<?xml version="1.0"?>
<tileset name="shared" tilewidth="8" tileheight="8" tilecount="1" columns="1">
  <image source="shared.png" width="8" height="8"/>
  <!-- custom editor metadata survives Save -->
  <tile id="0" type="floor"><properties><property name="entityType" value="Floor"/></properties></tile>
</tileset>`

func sessionFixture(t *testing.T) (root string, mapsSession *maps.Session, tsxPath string) {
	t.Helper()
	root = t.TempDir()
	for _, folder := range []string{"maps", "tiles"} {
		if err := os.Mkdir(filepath.Join(root, folder), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tsxPath = filepath.Join(root, "tiles", "shared.tsx")
	for name, content := range map[string]string{
		"tiles/shared.tsx": sharedTSX,
		"tiles/legacy.tsj": `{"type":"tileset","name":"legacy","tilewidth":8,"tileheight":8,"tilecount":1,"columns":1,"image":"legacy.png","imagewidth":8,"imageheight":8}`,
		"maps/a.tmx":       `<map width="1" height="1" tilewidth="8" tileheight="8"><tileset firstgid="1" source="../tiles/shared.tsx"/><layer id="1" width="1" height="1"><data encoding="csv">1</data></layer></map>`,
		"maps/b.tmx":       `<map width="1" height="1" tilewidth="8" tileheight="8"><tileset firstgid="1" source="../tiles/shared.tsx"/><tileset firstgid="2" source="../tiles/legacy.tsj"/><layer id="1" width="1" height="1"><data encoding="csv">1</data></layer></map>`,
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mapsSession = maps.Open(maps.Config{Root: root, MapPath: filepath.Join(root, "maps", "a.tmx")})
	return root, mapsSession, tsxPath
}

func TestSession_DiscoversOneEditableSharedTSXAndReadOnlyTSJ(t *testing.T) {
	root, mapSession, tsx := sessionFixture(t)
	s := Open(root, mapSession)
	entries := s.Entries()
	if len(entries) != 2 {
		t.Fatalf("discovered tilesets = %+v, want shared TSX and read-only TSJ", entries)
	}
	for _, entry := range entries {
		switch filepath.Ext(entry.Path) {
		case ".tsx":
			if entry.Path != tsx || !entry.Writable || len(entry.Maps) != 2 || entry.Problem != "" {
				t.Errorf("shared TSX listing = %+v", entry)
			}
		case ".tsj":
			if entry.Writable || len(entry.Maps) != 1 || !strings.Contains(entry.Problem, "read-only") {
				t.Errorf("TSJ listing = %+v", entry)
			}
		default:
			t.Errorf("unexpected entry %+v", entry)
		}
	}
	first := (*tiled.TilesetDocument)(nil)
	if err := s.Read(tsx, func(d *tiled.TilesetDocument) error { first = d; return nil }); err != nil || first == nil {
		t.Fatalf("read shared TSX = %v, %v", first, err)
	}
	if dirty, err := s.Dirty(); err != nil || len(dirty) != 0 {
		t.Fatalf("fresh TSX session dirty: %v,%v", dirty, err)
	}
}

func TestSession_WorkingTMXReferenceRefreshesDiscoveryWithoutLosingTSXWork(t *testing.T) {
	root, mapSession, tsx := sessionFixture(t)
	s := Open(root, mapSession)
	if err := s.Edit(tsx, func(d *tiled.TilesetDocument) error { return d.SetTileClass(0, "water") }); err != nil {
		t.Fatal(err)
	}
	newTSX := filepath.Join(root, "tiles", "new.tsx")
	if err := os.WriteFile(newTSX, []byte(sharedTSX), 0o600); err != nil {
		t.Fatal(err)
	}
	newMap := filepath.Join(root, "maps", "c.tmx")
	if err := os.WriteFile(newMap, []byte(`<map width="1" height="1" tilewidth="8" tileheight="8"><tileset firstgid="1" source="../tiles/new.tsx"/><layer id="1" width="1" height="1"><data encoding="csv">1</data></layer></map>`), 0o600); err != nil {
		t.Fatal(err)
	}
	mapSession.Refresh()
	if err := mapSession.Edit(newMap, func(d *tiled.Document) error { return d.SetLayerData(0, []uint32{0}) }); err != nil {
		t.Fatal(err)
	}
	// Tiled has changed the on-disk reference, but MAP still owns its unsaved
	// working TMX. Discovery must follow that working value until Reload.
	onDisk := `<map width="1" height="1" tilewidth="8" tileheight="8"><tileset firstgid="1" source="../tiles/shared.tsx"/><layer id="1" width="1" height="1"><data encoding="csv">1</data></layer></map>`
	if err := os.WriteFile(newMap, []byte(onDisk), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	found := false
	for _, entry := range s.Entries() {
		if entry.Path == newTSX && len(entry.Maps) == 1 && entry.Maps[0] == newMap {
			found = true
		}
	}
	if !found {
		t.Fatalf("refresh ignored the new working map's tileset: %+v", s.Entries())
	}
	if dirty, err := s.Dirty(); err != nil || len(dirty) != 1 || dirty[0] != tsx {
		t.Fatalf("refresh discarded unsaved TSX edit: %v,%v", dirty, err)
	}
}

func TestSession_OutsideProjectAndSymlinkedTilesetsAreNotReadable(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source func(root, outside string) (string, error)
	}{
		{"absolute external path", func(_, outside string) (string, error) { return outside, nil }},
		{"symlink escape", func(root, outside string) (string, error) {
			alias := filepath.Join(root, "tiles", "escape.tsx")
			return "../tiles/escape.tsx", os.Symlink(outside, alias)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, ms, _ := sessionFixture(t)
			outside := filepath.Join(t.TempDir(), "private.tsx")
			if err := os.WriteFile(outside, []byte(sharedTSX), 0o600); err != nil {
				t.Fatal(err)
			}
			source, err := tt.source(root, outside)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "maps", "a.tmx")
			body := `<map width="1" height="1" tilewidth="8" tileheight="8"><tileset firstgid="1" source="` + source + `"/><layer id="1" width="1" height="1"><data encoding="csv">1</data></layer></map>`
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := ms.Reload(path); err != nil {
				t.Fatal(err)
			}
			s := Open(root, ms)
			found := false
			for _, entry := range s.Entries() {
				found = found || strings.Contains(entry.Problem, "outside the project root")
			}
			if !found {
				t.Fatalf("external tileset was offered for editing: %+v", s.Entries())
			}
			if err := s.Read(outside, func(*tiled.TilesetDocument) error { t.Fatal("external file opened for editing"); return nil }); err == nil {
				t.Fatal("outside path was accepted")
			}
		})
	}
}

func TestSession_UnsavedSharedTSXUpdatesBothMapPreviewsWithoutTouchingDisk(t *testing.T) {
	root, mapSession, tsx := sessionFixture(t)
	s := Open(root, mapSession)
	mapSession.SetTilesetOpener(s.WorkingBytes)
	if err := s.Edit(tsx, func(d *tiled.TilesetDocument) error { return d.SetTileClass(0, "water") }); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.tmx", "b.tmx"} {
		preview, problems, err := mapSession.Preview(filepath.Join(root, "maps", name))
		if err != nil || len(problems) != 0 || preview.Tilesets[0].Tileset.Tiles[0].Type != "water" {
			t.Fatalf("%s did not preview shared unsaved TSX: %+v, %v, %v", name, preview, problems, err)
		}
	}
	onDisk, err := os.ReadFile(tsx)
	if err != nil || !strings.Contains(string(onDisk), `type="floor"`) {
		t.Fatalf("previewing an edit wrote TSX to disk: %s, %v", onDisk, err)
	}
}

func TestSession_SaveConflictReloadOverwriteAndDiscardPreserveTheRightBytes(t *testing.T) {
	root, ms, tsx := sessionFixture(t)
	s := Open(root, ms)
	setClass := func(value string) {
		t.Helper()
		if err := s.Edit(tsx, func(d *tiled.TilesetDocument) error { return d.SetTileClass(0, value) }); err != nil {
			t.Fatal(err)
		}
	}
	setClass("water")
	if err := s.Discard(tsx); err != nil {
		t.Fatal(err)
	}
	if dirty, err := s.Dirty(); err != nil || len(dirty) != 0 {
		t.Fatalf("discard did not restore TSX snapshot: %v, %v", dirty, err)
	}
	setClass("lava")
	onDisk := strings.Replace(sharedTSX, `type="floor"`, `type="outside"`, 1)
	if err := os.WriteFile(tsx, []byte(onDisk), 0o600); err != nil {
		t.Fatal(err)
	}
	var conflict *editable.ConflictError
	if err := s.Save(tsx); !errors.As(err, &conflict) {
		t.Fatalf("saving over an external TSX edit = %v, want conflict", err)
	}
	if data, err := os.ReadFile(tsx); err != nil || string(data) != onDisk {
		t.Fatalf("conflicted save overwrote the external version: %s, %v", data, err)
	}
	if err := s.Reload(tsx); err != nil {
		t.Fatal(err)
	}
	if err := s.Read(tsx, func(d *tiled.TilesetDocument) error {
		set, err := d.Tileset()
		if err == nil && set.Tiles[0].Type != "outside" {
			t.Errorf("Reload kept the unsaved working version: %s", set.Tiles[0].Type)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	setClass("lava")
	if err := s.SaveOverwriting(tsx); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(tsx); err != nil || !strings.Contains(string(data), `type="lava"`) || !strings.Contains(string(data), "custom editor metadata survives") {
		t.Fatalf("overwriting lost authored or unknown XML: %s, %v", data, err)
	}
	if dirty, err := s.Dirty(); err != nil || len(dirty) != 0 {
		t.Fatalf("saved TSX is dirty: %v, %v", dirty, err)
	}
}

func TestSession_DisappearedTSXKeepsItsWorkingValueReachable(t *testing.T) {
	root, ms, tsx := sessionFixture(t)
	s := Open(root, ms)
	ms.SetTilesetOpener(s.WorkingBytes)
	if err := s.Edit(tsx, func(d *tiled.TilesetDocument) error { return d.SetTileClass(0, "water") }); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tsx); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	if err := s.Read(tsx, func(d *tiled.TilesetDocument) error {
		if !strings.Contains(string(d.Bytes()), `type="water"`) {
			t.Error("disappearance lost the unsaved TSX edit")
		}
		return nil
	}); err != nil {
		t.Fatalf("deleted file lost its held working TSX: %v", err)
	}
	preview, problems, err := ms.Preview(filepath.Join(root, "maps", "a.tmx"))
	if err != nil || len(problems) != 0 || preview.Tilesets[0].Tileset.Tiles[0].Type != "water" {
		t.Fatalf("MAP preview dropped held TSX after file disappeared: %+v, %v, %v", preview, problems, err)
	}
	if err := s.SaveOverwriting(tsx); err != nil {
		t.Fatalf("could not recreate disappeared TSX from held working value: %v", err)
	}
	if data, err := os.ReadFile(tsx); err != nil || !strings.Contains(string(data), `type="water"`) {
		t.Fatalf("recreated TSX is not the held version: %s,%v", data, err)
	}
}

func TestSession_UnreferencedCleanTSXIsNotAStaleTab(t *testing.T) {
	root, ms, _ := sessionFixture(t)
	newTSX := filepath.Join(root, "tiles", "new.tsx")
	if err := os.WriteFile(newTSX, []byte(sharedTSX), 0o600); err != nil {
		t.Fatal(err)
	}
	newMap := filepath.Join(root, "maps", "c.tmx")
	if err := os.WriteFile(newMap, []byte(`<map width="1" height="1" tilewidth="8" tileheight="8"><tileset firstgid="1" source="../tiles/new.tsx"/><layer id="1" width="1" height="1"><data encoding="csv">1</data></layer></map>`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Open(root, ms)
	if err := os.Remove(newMap); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	for _, entry := range s.Entries() {
		if entry.Path == newTSX {
			t.Fatalf("unreferenced clean TSX lingered as a project tab: %+v", entry)
		}
	}
}

func TestSession_DescribeReadsHeldTSXAndListedTSJButNotOtherPaths(t *testing.T) {
	root, ms, tsx := sessionFixture(t)
	s := Open(root, ms)
	if err := s.Edit(tsx, func(d *tiled.TilesetDocument) error { return d.SetTileClass(0, "water") }); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, path, wantName, wantClass string
		wantErr                         bool
	}{
		{"working TSX", tsx, "shared", "water", false},
		{"read-only TSJ", filepath.Join(root, "tiles", "legacy.tsj"), "legacy", "", false},
		{"unreferenced file", filepath.Join(root, "tiles", "unlisted.tsx"), "", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "unreferenced file" {
				if err := os.WriteFile(tt.path, []byte(sharedTSX), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.Describe(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Describe(%s) opened a path no map references", tt.path)
				}
				return
			}
			if err != nil || got.Name != tt.wantName || (tt.wantClass != "" && got.Tiles[0].Type != tt.wantClass) {
				t.Fatalf("Describe(%s) = %+v, %v", tt.path, got, err)
			}
		})
	}
}

func TestSession_HeldFileRejectsSymlinkSwappedOutsideTheProject(t *testing.T) {
	root, ms, tsx := sessionFixture(t)
	s := Open(root, ms)
	if err := s.Edit(tsx, func(d *tiled.TilesetDocument) error { return d.SetTileClass(0, "unsaved") }); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.tsx")
	if err := os.WriteFile(outside, []byte(sharedTSX), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tsx); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, tsx); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	for _, entry := range s.Entries() {
		if entry.Path == tsx && entry.Writable {
			t.Errorf("swapped external symlink still offers writable actions: %+v", entry)
		}
	}
	if _, err := s.Describe(tsx); err == nil {
		t.Error("selected held TSX bypassed the swapped symlink check")
	}
	if _, err := s.WorkingBytes(tsx); err == nil {
		t.Error("MAP opener followed the swapped symlink outside the project")
	}
	for _, tt := range []struct {
		name string
		act  func(string) error
	}{
		{"reload", s.Reload}, {"overwrite", s.SaveOverwriting}, {"discard", s.Discard},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.act(tsx); err == nil {
				t.Fatal("a swapped external symlink was accepted as a held TSX")
			}
		})
	}
	for _, name := range []string{"a.tmx", "b.tmx"} {
		mapPath := filepath.Join(root, "maps", name)
		data, err := os.ReadFile(mapPath)
		if err != nil {
			t.Fatal(err)
		}
		body := strings.ReplaceAll(string(data), `<tileset firstgid="1" source="../tiles/shared.tsx"/>`, "")
		if err := os.WriteFile(mapPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ms.Reload(mapPath); err != nil {
			t.Fatal(err)
		}
	}
	s.Refresh()
	for _, entry := range s.Entries() {
		if entry.Path == tsx && entry.Writable {
			t.Errorf("unreferenced dirty TSX with external symlink still offers Save: %+v", entry)
		}
	}
}

func TestSession_DescribeCaseInsensitiveTSJ(t *testing.T) {
	root, ms, _ := sessionFixture(t)
	from := filepath.Join(root, "tiles", "legacy.tsj")
	to := filepath.Join(root, "tiles", "legacy.TSJ")
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	mapPath := filepath.Join(root, "maps", "b.tmx")
	data, err := os.ReadFile(mapPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mapPath, []byte(strings.ReplaceAll(string(data), "legacy.tsj", "legacy.TSJ")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ms.Reload(mapPath); err != nil {
		t.Fatal(err)
	}
	s := Open(root, ms)
	got, err := s.Describe(to)
	if err != nil || got.Name != "legacy" {
		t.Fatalf("Describe uppercase TSJ = %+v, %v", got, err)
	}
}
