package renderer

import (
	"context"
	"database/sql"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAnimLoader_WatchedBindingSaveUpdatesExistingSpriteSheet(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT)`,
		`CREATE TABLE comp_sprite (entity_id INTEGER, sheet TEXT)`,
		`INSERT INTO entities (id, entity_type) VALUES (1, 'Goblin')`,
		`INSERT INTO comp_sprite (entity_id, sheet) VALUES (1, 'sprites/old.png')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "animations.toml")
	write := func(sheet string) {
		t.Helper()
		tmp, err := os.CreateTemp(filepath.Dir(path), "animation-*.toml")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmp.Name())
		content := "# no entity-sheet bindings\n"
		if sheet != "" {
			content = "[[entity_asset]]\nentity_type = \"Goblin\"\nsheet = \"" + sheet + "\"\n"
		}
		if _, err := tmp.WriteString(content); err != nil {
			tmp.Close()
			t.Fatal(err)
		}
		if err := tmp.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp.Name(), path); err != nil {
			t.Fatal(err)
		}
	}
	write("sprites/old.png")
	loader := NewAnimLoader()
	if err := loader.Load(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loader.WatchWithDatabase(ctx, path, db) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("watcher returned: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond)
	write("sprites/new.png")
	deadline := time.Now().Add(2 * time.Second)
	for {
		var sheet string
		if err := db.QueryRow(`SELECT sheet FROM comp_sprite WHERE entity_id = 1`).Scan(&sheet); err != nil {
			t.Fatal(err)
		}
		if sheet == "sprites/new.png" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("watched entity_asset change never reached comp_sprite.sheet: %q", sheet)
		}
		time.Sleep(10 * time.Millisecond)
	}
	write("")
	deadline = time.Now().Add(2 * time.Second)
	for {
		var sheet string
		if err := db.QueryRow(`SELECT sheet FROM comp_sprite WHERE entity_id = 1`).Scan(&sheet); err != nil {
			t.Fatal(err)
		}
		if sheet == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("removed watched entity_asset still owns comp_sprite.sheet: %q", sheet)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAnimLoader_OverlappingWatchedSavesDoNotPublishASecondDefinitionBeforeFirstSync(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT)`,
		`CREATE TABLE comp_sprite (entity_id INTEGER, sheet TEXT)`,
		`INSERT INTO entities (id, entity_type) VALUES (1, 'Goblin')`,
		`INSERT INTO comp_sprite (entity_id, sheet) VALUES (1, 'sprites/old.png')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "animations.toml")
	write := func(sheet string) {
		t.Helper()
		tmp, err := os.CreateTemp(filepath.Dir(path), "animation-*.toml")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.WriteString("[[entity_asset]]\nentity_type = \"Goblin\"\nsheet = \"" + sheet + "\"\n"); err != nil {
			tmp.Close()
			t.Fatal(err)
		}
		if err := tmp.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp.Name(), path); err != nil {
			t.Fatal(err)
		}
	}
	write("sprites/old.png")
	loader := NewAnimLoader()
	if err := loader.Load(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loader.WatchWithDatabase(ctx, path, db) }()
	defer func() { cancel(); <-done }()
	time.Sleep(100 * time.Millisecond)
	// Occupy the only DB connection after the watcher starts. The first
	// callback can load its file but cannot finish synchronizing it yet.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	write("sprites/first.png")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if sheet, ok := loader.SheetForEntityType("Goblin"); ok && sheet == "sprites/first.png" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first watched save never loaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	write("sprites/second.png")
	time.Sleep(150 * time.Millisecond) // let the second debounced event fire
	if sheet, _ := loader.SheetForEntityType("Goblin"); sheet != "sprites/first.png" {
		t.Errorf("second definition published before the first DB sync: %q", sheet)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		var sheet string
		if err := db.QueryRow(`SELECT sheet FROM comp_sprite WHERE entity_id = 1`).Scan(&sheet); err != nil {
			t.Fatal(err)
		}
		if sheet == "sprites/second.png" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("latest watched binding never reached the database: %q", sheet)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAnimLoader_FailedBindingSyncKeepsRemovedTypeForLaterRetry(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT)`,
		`CREATE TABLE comp_sprite (entity_id INTEGER, sheet TEXT)`,
		`INSERT INTO entities (id, entity_type) VALUES (1, 'Goblin')`,
		`INSERT INTO comp_sprite (entity_id, sheet) VALUES (1, 'sprites/old.png')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "animations.toml")
	if err := os.WriteFile(path, []byte("[[entity_asset]]\nentity_type = \"Goblin\"\nsheet = \"sprites/old.png\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := NewAnimLoader()
	if err := loader.Load(path); err != nil {
		t.Fatal(err)
	}
	if err := loader.SyncToDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# binding removed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loader.Load(path); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE comp_sprite`); err != nil {
		t.Fatal(err)
	}
	if err := loader.SyncToDatabase(ctx, db); err == nil {
		t.Fatal("sync appeared successful with no sprite table")
	}
	for _, statement := range []string{
		`CREATE TABLE comp_sprite (entity_id INTEGER, sheet TEXT)`,
		`INSERT INTO comp_sprite (entity_id, sheet) VALUES (1, 'sprites/old.png')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := loader.SyncToDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	var sheet string
	if err := db.QueryRow(`SELECT sheet FROM comp_sprite WHERE entity_id = 1`).Scan(&sheet); err != nil || sheet != "" {
		t.Fatalf("retry forgot removed binding and left sheet %q: %v", sheet, err)
	}
}

func TestAnimLoader_RemovingBindingPreservesAnIndependentlyChangedSheet(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT)`,
		`CREATE TABLE comp_sprite (entity_id INTEGER, sheet TEXT)`,
		`INSERT INTO entities (id, entity_type) VALUES (1, 'Goblin'), (2, 'Goblin')`,
		`INSERT INTO comp_sprite (entity_id, sheet) VALUES (1, ''), (2, '')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "animations.toml")
	if err := os.WriteFile(path, []byte("[[entity_asset]]\nentity_type = 'Goblin'\nsheet = 'sprites/goblin.png'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := NewAnimLoader()
	if err := loader.Load(path); err != nil {
		t.Fatal(err)
	}
	if err := loader.SyncToDatabase(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE comp_sprite SET sheet = 'sprites/custom.png' WHERE entity_id = 2`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# binding removed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loader.Load(path); err != nil {
		t.Fatal(err)
	}
	if err := loader.SyncToDatabase(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		id   int
		want string
	}{{1, ""}, {2, "sprites/custom.png"}} {
		var got string
		if err := db.QueryRow(`SELECT sheet FROM comp_sprite WHERE entity_id = ?`, tt.id).Scan(&got); err != nil || got != tt.want {
			t.Errorf("sprite %d sheet = %q, %v; want %q", tt.id, got, err, tt.want)
		}
	}
}

func TestAnimLoader_ResolvesAssetsRelativeSheetForTheGame(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sprites"), 0o700); err != nil {
		t.Fatal(err)
	}
	art, err := os.Create(filepath.Join(root, "sprites", "goblin.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(art, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		art.Close()
		t.Fatal(err)
	}
	if err := art.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "animations.toml")
	if err := os.WriteFile(path, []byte("[[entity_asset]]\nentity_type = 'Goblin'\nsheet = 'sprites/goblin.png'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT)`,
		`CREATE TABLE comp_sprite (entity_id INTEGER, sheet TEXT)`,
		`INSERT INTO entities VALUES (1, 'Goblin')`,
		`INSERT INTO comp_sprite VALUES (1, '')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	loader := NewAnimLoader()
	if err := loader.Load(path); err != nil {
		t.Fatal(err)
	}
	if err := loader.SyncToDatabase(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var sheet string
	if err := db.QueryRow(`SELECT sheet FROM comp_sprite WHERE entity_id = 1`).Scan(&sheet); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "sprites", "goblin.png"); loader.ResolveSheet(sheet) != want {
		t.Errorf("game opens sheet %q, want selected assets-mod path %q", loader.ResolveSheet(sheet), want)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectRelative, err := filepath.Rel(cwd, filepath.Join(root, "sprites", "goblin.png"))
	if err != nil {
		t.Fatal(err)
	}
	if got := loader.ResolveSheet(projectRelative); got != projectRelative {
		t.Errorf("existing cwd-relative sheet path changed from %q to %q", projectRelative, got)
	}
	f, err := os.Open(loader.ResolveSheet(sheet))
	if err != nil {
		t.Fatalf("the game cannot open the authored sheet: %v", err)
	}
	defer f.Close()
	if _, err := png.Decode(f); err != nil {
		t.Fatalf("the game's PNG decoder cannot read the authored sheet: %v", err)
	}
}

func TestAnimLoader_AssetsRelativeSpriteTakesPriorityOverCWD(t *testing.T) {
	root := t.TempDir()
	cwd, assets := filepath.Join(root, "game-cwd"), filepath.Join(root, "first-assets")
	for _, dir := range []string{filepath.Join(cwd, "sprites"), filepath.Join(assets, "sprites")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{cwd, assets} {
		if err := os.WriteFile(filepath.Join(dir, "sprites", "same.png"), []byte("image"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(assets, "animations.toml")
	if err := os.WriteFile(path, []byte("[[entity_asset]]\nentity_type = 'Goblin'\nsheet = 'sprites/same.png'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	loader := NewAnimLoader()
	if err := loader.Load(path); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sprites/same.png", "./sprites/same.png"} {
		if got, want := loader.ResolveSheet(name), filepath.Join(assets, "sprites", "same.png"); got != want {
			t.Errorf("game chose cwd's image over Forge's selected mod for %q: %q, want %q", name, got, want)
		}
	}
}

func TestAnimLoader_SheetResolutionCacheIsInvalidatedOnReload(t *testing.T) {
	root := t.TempDir()
	loader := NewAnimLoader()
	for _, mod := range []string{"a", "b"} {
		dir := filepath.Join(root, mod)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "animations.toml")
		if err := os.WriteFile(path, []byte("[[entity_asset]]\nentity_type = 'Goblin'\nsheet = 'sprites/hero.png'\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := loader.Load(path); err != nil {
			t.Fatal(err)
		}
		if got, want := loader.ResolveSheet("sprites/hero.png"), filepath.Join(dir, "sprites", "hero.png"); got != want {
			t.Errorf("resolved using an earlier loaded assets mod: %q, want %q", got, want)
		}
	}
}

func TestAnimLoader_ProjectRelativeSpriteWorksOutsideProjectWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(filepath.Join(assets, "sprites"), 0o700); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(assets, "sprites", "hero.png")
	if err := os.WriteFile(imagePath, []byte("sprite"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(assets, "animations.toml")
	if err := os.WriteFile(path, []byte("[[entity_asset]]\nentity_type = 'Hero'\nsheet = 'assets/sprites/hero.png'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	loader := NewAnimLoader()
	if err := loader.SetProjectRoot(root); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeConfigPath, err := filepath.Rel(cwd, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Load(relativeConfigPath); err != nil {
		t.Fatal(err)
	}
	if got := loader.ResolveSheet("assets/sprites/hero.png"); got != imagePath {
		t.Errorf("game resolved project-relative sheet as %q instead of %q", got, imagePath)
	}
}

func TestAnimLoader_BindingSyncIsAtomicWhenALaterTypeFails(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY, entity_type TEXT)`,
		`CREATE TABLE comp_sprite (entity_id INTEGER, sheet TEXT CHECK (sheet <> 'sprites/rejected.png'))`,
		`INSERT INTO entities (id, entity_type) VALUES (1, 'Goblin'), (2, 'Player')`,
		`INSERT INTO comp_sprite (entity_id, sheet) VALUES (1, 'sprites/old-goblin.png'), (2, 'sprites/old-player.png')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "animations.toml")
	content := `[[entity_asset]]
entity_type = "Goblin"
sheet = "sprites/new-goblin.png"
[[entity_asset]]
entity_type = "Player"
sheet = "sprites/rejected.png"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := NewAnimLoader()
	if err := loader.Load(path); err != nil {
		t.Fatal(err)
	}
	if err := loader.SyncToDatabase(context.Background(), db); err == nil {
		t.Fatal("a constrained sprite update was reported as successful")
	}
	var sheet string
	if err := db.QueryRow(`SELECT sheet FROM comp_sprite WHERE entity_id = 1`).Scan(&sheet); err != nil || sheet != "sprites/old-goblin.png" {
		t.Fatalf("partial sync left Goblin's sheet on the unsaved new binding: %q, %v", sheet, err)
	}
}

func TestAnimLoader_WatchSurvivesRepeatedAtomicSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "animations.toml")
	write := func(fps string) {
		t.Helper()
		f, err := os.CreateTemp(filepath.Dir(path), "animation-*.toml")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(f.Name())
		if _, err := f.WriteString(`[[animation]]
name = "walk"
sheet = "sprites/player.png"
frames = [0]
fps = ` + fps + `
loop = true
`); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(f.Name(), path); err != nil {
			t.Fatal(err)
		}
	}
	write("1")
	loader := NewAnimLoader()
	if err := loader.Load(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loader.Watch(ctx, path) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("watcher exited: %v", err)
		}
	}()
	// The watch is installed by Watch's first fsnotify.Add. Wait a moment
	// before the first replacement; the assertions below have their own bounds.
	time.Sleep(100 * time.Millisecond)
	for _, tt := range []struct {
		fps  string
		want float64
	}{{"8", 8}, {"16", 16}} {
		write(tt.fps)
		deadline := time.Now().Add(2 * time.Second)
		for {
			if def, ok := loader.Get("walk"); ok && def.FPS == tt.want {
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("watched atomic replacement did not reload fps %g", tt.want)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
