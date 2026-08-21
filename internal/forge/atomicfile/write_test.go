package atomicfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWrite_CreatesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")

	if err := Write(path, []byte("first")); err != nil {
		t.Fatalf("Write (create): %v", err)
	}
	if got := read(t, path); got != "first" {
		t.Errorf("content = %q, want %q", got, "first")
	}

	if err := Write(path, []byte("second")); err != nil {
		t.Fatalf("Write (overwrite): %v", err)
	}
	if got := read(t, path); got != "second" {
		t.Errorf("content = %q, want %q", got, "second")
	}
	assertNoTempFiles(t, dir)
}

// A save must not change who can read the file.
func TestWrite_PreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")
	if err := os.WriteFile(path, []byte("original"), 0o640); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	if err := Write(path, []byte("updated")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("mode = %v, want 0640 — a save changed the file's permissions", got)
	}
}

// The point of the whole exercise. A non-atomic write passes every other test
// in this file: it is only visible to a reader watching the file while it is
// being written, which is exactly what the engine's fsnotify watcher is.
func TestWrite_ReaderNeverSeesAPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "machine.json")

	// Two documents of very different length, so a torn write is structurally
	// invalid rather than coincidentally parseable.
	//
	// The long one is about a megabyte on purpose. At 3.5KB the truncate-then-
	// write window closes so fast that a non-atomic implementation slipped
	// past this test roughly one run in six — measured. The window has to be
	// wide enough that the reader lands in it reliably, or the only test
	// defending atomicity is a coin flip.
	short := []byte(`{"id":"s"}`)
	long := []byte(`{"id":"l","states":{` + strings.Repeat(`"a":{},`, 100_000) + `"z":{}}}`)
	if err := Write(path, short); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	const iterations = 300
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stop)
		for i := range iterations {
			payload := short
			if i%2 == 0 {
				payload = long
			}
			if err := Write(path, payload); err != nil {
				t.Errorf("Write: %v", err)
				return
			}
		}
	}()

	var reads, torn, sawShort, sawLong int
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				// The file must never be absent: rename replaces in place.
				t.Errorf("the file disappeared mid-write: %v", err)
				return
			}
			reads++
			if len(raw) == len(short) {
				sawShort++
			} else if len(raw) == len(long) {
				sawLong++
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				torn++
				t.Errorf("read a partial document (%d bytes): %v", len(raw), err)
				return
			}
		}
	}()

	wg.Wait()
	// Evidence that the reader was actually present while the file was
	// changing, rather than passing by never having looked. Counting reads
	// against writes would be the wrong measure — the two goroutines are
	// unsynchronised, and under -race the reader gets proportionally fewer
	// turns — so the assertion is that it observed the file in *both* states,
	// which it can only do by reading across transitions.
	if sawShort == 0 || sawLong == 0 {
		t.Fatalf("the reader saw only one version (%d short, %d long, %d reads); it did not sample the window",
			sawShort, sawLong, reads)
	}
	t.Logf("%d reads (%d short, %d long), %d torn", reads, sawShort, sawLong, torn)
	assertNoTempFiles(t, dir)
}

// A write that fails partway must leave the original exactly as it was.
func TestWrite_FailureLeavesTheOriginalIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")
	original := []byte(`{"good":true}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// A failure at the rename, after the temp file has been written in full.
	// This is the mid-write case the story asks for; an earlier version aimed
	// at a subdirectory that was never created, so Write quietly succeeded and
	// the assertion tested nothing.
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("creating the directory: %v", err)
	}
	if err := Write(sub, []byte("x")); err == nil {
		t.Error("writing over a directory should fail")
	}

	// And a failure at temp creation, before any bytes are written.
	if err := Write(filepath.Join(dir, "nope", "file.json"), []byte("x")); err == nil {
		t.Error("writing into a missing directory should fail")
	}

	if got := read(t, path); got != string(original) {
		t.Errorf("the original was modified: %q", got)
	}
	assertNoTempFiles(t, dir)
}

// Concurrent saves of the same file must not collide over a temp name.
func TestWrite_ConcurrentWritesToOneFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := []byte(`{"writer":` + string(rune('0'+i%10)) + `}`)
			if err := Write(path, body); err != nil {
				t.Errorf("Write: %v", err)
			}
		}()
	}
	wg.Wait()

	var doc map[string]any
	if err := json.Unmarshal([]byte(read(t, path)), &doc); err != nil {
		t.Errorf("the surviving file is not a complete document: %v", err)
	}
	assertNoTempFiles(t, dir)
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(raw)
}

// A temp file left in the target's directory is litter in someone's project,
// and — worse — a file the engine's watcher may try to interpret.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") || e.IsDir() {
			continue
		}
		t.Errorf("left a temp file behind: %s", e.Name())
	}
}

// The claim that makes the whole thing safe next to a running engine: the temp
// file must not look like a machine to a watcher filtering on
// filepath.Ext(name) == ".json". It had no test at all.
func TestWrite_TempFileIsInvisibleToTheEngineWatcher(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "machine.json")

	// Catch the temp file mid-write by watching the directory from a second
	// goroutine while a large payload is being written.
	big := []byte(`{"id":"x","pad":"` + strings.Repeat("a", 2_000_000) + `"}`)

	var seen []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := Write(path, big); err != nil {
			t.Errorf("Write: %v", err)
		}
	}()
	for {
		select {
		case <-done:
			goto check
		default:
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Name() != "machine.json" {
				seen = append(seen, e.Name())
			}
		}
	}

check:
	if len(seen) == 0 {
		t.Skip("never caught the temp file; the write completed too quickly")
	}
	for _, name := range seen {
		if filepath.Ext(name) == ".json" {
			t.Errorf("temp file %q has extension .json — the engine's watcher would try to load it", name)
		}
	}
}

// A '*' in the target's base name makes os.CreateTemp substitute there, so the
// temp file can end in .json and become visible to the watcher. Refused rather
// than silently risked.
func TestWrite_RefusesAStarInTheFilename(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ma*chine.json", "*.json", "trailing*"} {
		t.Run(name, func(t *testing.T) {
			err := Write(filepath.Join(dir, name), []byte(`{}`))
			if err == nil {
				t.Fatalf("Write(%q) succeeded; the temp file could shadow a machine", name)
			}
			if !strings.Contains(err.Error(), "*") {
				t.Errorf("error = %v, does not explain the problem", err)
			}
		})
	}
	// A '*' in a parent directory is harmless — only the base name is a pattern.
	starDir := filepath.Join(dir, "a*b")
	if err := os.MkdirAll(starDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := Write(filepath.Join(starDir, "fine.json"), []byte(`{}`)); err != nil {
		t.Errorf("a '*' in a directory name should be fine: %v", err)
	}
}

// A file Forge creates must be readable by the game process, which may not be
// the same user. The dependency copies an existing file's mode but leaves a new
// one at the temp file's 0600.
func TestWrite_NewFileIsReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.json")
	if err := Write(path, []byte(`{}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0o044 == 0 {
		t.Errorf("mode = %v; a new file is not group- or world-readable", info.Mode().Perm())
	}
}

// Errors reach a person, so they have to name the file.
func TestWrite_ErrorNamesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "file.json")
	err := Write(path, []byte(`{}`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %v, does not name %s", err, path)
	}
	// The package prefix, specifically. The dependency's own message happens to
	// contain the path too — it builds the temp name from it — so asserting
	// only on the path passes whether or not this package wraps anything.
	if !strings.HasPrefix(err.Error(), "atomicfile: ") {
		t.Errorf("error = %v, want it wrapped with the package name", err)
	}
}
