package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fixturePNG is a 1x1 PNG, so the route has a real file to serve.
var fixturePNG = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02, 0x00, 0x00, 0x00,
	0x90, 0x77, 0x53, 0xde,
	0x00, 0x00, 0x00, 0x0c, 'I', 'D', 'A', 'T',
	0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00, 0x03, 0x01, 0x01, 0x00,
	0x18, 0xdd, 0x8d, 0xb0,
	0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

func assetServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv, _, _, configured := mapServer(t)
	dir := filepath.Dir(configured)
	if err := os.WriteFile(filepath.Join(dir, "fixture.png"), fixturePNG, 0o600); err != nil {
		t.Fatal(err)
	}
	return srv, dir
}

func TestAsset_ServesAnImageATilesetNames(t *testing.T) {
	srv, dir := assetServer(t)
	// A render is what asks the session to resolve, and resolving is what fills
	// the allow-list — but opening the project already did both, so the first
	// page load can draw.
	res, err := srv.Client().Get(srv.URL + "/forge/asset?path=" + url.QueryEscape(filepath.Join(dir, "fixture.png")))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET = %d, want 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type is %q", got)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("a project file being edited is cached as %q", got)
	}
}

// The whole risk of this route. It is not defended with path arithmetic — the
// session answers whether a tileset names the path — so these are refused
// without the route reasoning about any of them.
func TestAsset_RefusesAnythingNoTilesetNames(t *testing.T) {
	srv, dir := assetServer(t)
	for _, target := range []string{
		"/etc/passwd",
		"../../etc/passwd",
		filepath.Join(dir, "..", "..", "etc", "passwd"),
		filepath.Join(dir, "level1.tmx"),
		filepath.Join(dir, "fixture.tsx"),
		filepath.Join(dir, "..", "secret.png"),
		"",
	} {
		t.Run(target, func(t *testing.T) {
			res, err := srv.Client().Get(srv.URL + "/forge/asset?path=" + url.QueryEscape(target))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusNotFound {
				t.Errorf("GET %q = %d, want 404", target, res.StatusCode)
			}
		})
	}
}

// A tileset that names something that is not a picture must not turn this into
// a general file server.
func TestAsset_RefusesAFileThatIsNotAPicture(t *testing.T) {
	srv, _, sess, configured := mapServer(t)
	dir := filepath.Dir(configured)
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Point the tileset's image at it, so the allow-list would let it through.
	tsx := filepath.Join(dir, "fixture.tsx")
	raw, err := os.ReadFile(tsx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsx, []byte(strings.Replace(string(raw), `source="fixture.png"`, `source="secret.txt"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(configured); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Resolved(configured); err != nil {
		t.Fatal(err)
	}

	res, err := srv.Client().Get(srv.URL + "/forge/asset?path=" + url.QueryEscape(secret))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("a tileset naming a text file made it servable: %d", res.StatusCode)
	}
}

func TestAsset_RefusesWhenNoProjectIsOpen(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())
	srv := httptest.NewServer(s.routes())
	defer srv.Close()
	res, err := srv.Client().Get(srv.URL + "/forge/asset?path=/anything.png")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Errorf("GET = %d, want 409", res.StatusCode)
	}
}

// The first route in Forge to send bytes out of a user's file on Forge's own
// origin. A response sniffed as HTML there would be same-origin script with the
// schema and map write routes in reach.
func TestAsset_TellsTheBrowserNotToSniff(t *testing.T) {
	srv, dir := assetServer(t)
	res, err := srv.Client().Get(srv.URL + "/forge/asset?path=" + url.QueryEscape(filepath.Join(dir, "fixture.png")))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options is %q", got)
	}
}

// os.Open on a FIFO blocks until somebody opens the other end. The server has
// no write timeout — deliberately, because SSE streams live for the life of a
// page — so a tileset naming one leaks a goroutine per request, for ever.
//
// The mutation this guards against does not make the route answer wrongly; it
// makes it never answer. So the assertion is that an answer arrives at all.
func TestAsset_RefusesAFileThatIsNotARegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs")
	}
	srv, _, sess, configured := mapServer(t)
	dir := filepath.Dir(configured)
	fifo := filepath.Join(dir, "fixture.png")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if _, err := sess.Resolved(configured); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		res, err := srv.Client().Get(srv.URL + "/forge/asset?path=" + url.QueryEscape(fifo))
		if err != nil {
			done <- 0
			return
		}
		defer res.Body.Close()
		done <- res.StatusCode
	}()

	select {
	case code := <-done:
		if code != http.StatusNotFound {
			t.Errorf("GET = %d, want 404", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the route opened a FIFO and never came back")
	}
}
