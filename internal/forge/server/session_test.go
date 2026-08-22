package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/session"
	"github.com/tmbritton/ecs-db/internal/forge/status"
	"github.com/tmbritton/ecs-db/internal/forge/templates/modes"
	"github.com/tmbritton/ecs-db/internal/schema"
)

const sessionSchema = `{
  "schemaVersion": 3,
  "components": {
    "Position": {
      "type": "object",
      "properties": {
        "x": { "type": "number" }
      }
    }
  },
  "entityTypes": {
    "Player": {
      "requiredComponents": ["Position"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    }
  }
}
`

func sessionServer(t *testing.T) (*httptest.Server, *Server, *session.Session, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(sessionSchema), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	srv, s, sess, path := sessionServerAt(t, path, status.Config{})
	return srv, s, sess, path
}

// sessionServerAt is sessionServer over an existing file, so a test that needs
// a database alongside it can build both.
func sessionServerAt(t *testing.T, path string, engine status.Config) (*httptest.Server, *Server, *session.Session, string) {
	t.Helper()
	sess, err := session.Open(path)
	if err != nil {
		t.Fatalf("session.Open: %v", err)
	}
	s := New(Config{Addr: "127.0.0.1:0", Session: sess, Engine: engine}, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	return srv, s, sess, path
}

func post(t *testing.T, srv *httptest.Server, path string) int {
	t.Helper()
	resp, err := srv.Client().Post(srv.URL+path, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// The footer is rendered from the session, so it must tell the truth about it
// on first paint rather than waiting for a stream tick.
func TestModePage_RendersTheFooterFromTheSession(t *testing.T) {
	srv, _, sess, _ := sessionServer(t)

	_, body := get(t, srv, mode.Default.Path())
	if !strings.Contains(body, "schema.json") {
		t.Error("the footer does not name the file being edited")
	}
	if strings.Contains(body, "save-footer--dirty") {
		t.Error("the footer claims unsaved changes on a freshly opened file")
	}

	if err := sess.Edit(func(d *schema.DatabaseSchema) error { d.SchemaVersion = 4; return nil }); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	_, body = get(t, srv, mode.Default.Path())
	if !strings.Contains(body, "save-footer--dirty") {
		t.Error("the footer does not reflect an unsaved edit")
	}
}

// The actions are actions: they change the session and answer 204, and the page
// learns about it on the stream it already holds.
func TestSchemaActions(t *testing.T) {
	srv, _, sess, path := sessionServer(t)

	if err := sess.Edit(func(d *schema.DatabaseSchema) error { d.SchemaVersion = 9; return nil }); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if code := post(t, srv, "/forge/schema/save"); code != http.StatusNoContent {
		t.Errorf("save = %d, want 204", code)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if !strings.Contains(string(raw), `"schemaVersion": 9`) {
		t.Errorf("save did not write the file:\n%s", raw)
	}
	if dirty, err := sess.Dirty(); err != nil || dirty {
		t.Errorf("Dirty = %v (err %v) after save, want false", dirty, err)
	}

	// Discard restores the last saved state.
	if err := sess.Edit(func(d *schema.DatabaseSchema) error { d.SchemaVersion = 11; return nil }); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if code := post(t, srv, "/forge/schema/discard"); code != http.StatusNoContent {
		t.Errorf("discard = %d, want 204", code)
	}
	var version int
	sess.Read(func(d schema.DatabaseSchema) { version = d.SchemaVersion })
	if version != 9 {
		t.Errorf("SchemaVersion = %d after discard, want the last saved value", version)
	}
}

// A refused save must still answer, and must report why through the same
// channel every other outcome uses.
func TestSchemaSave_RefusedIsReportedNotSwallowed(t *testing.T) {
	srv, s, sess, path := sessionServer(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	if err := sess.Edit(func(d *schema.DatabaseSchema) error {
		et := d.EntityTypes["Player"]
		et.RequiredComponents = []string{"Nope"}
		d.EntityTypes["Player"] = et
		return nil
	}); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	if code := post(t, srv, "/forge/schema/save"); code != http.StatusNoContent {
		t.Errorf("save = %d, want 204 — the outcome travels on the stream", code)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(before) != string(after) {
		t.Error("an invalid schema was written")
	}

	report, err := s.renderSaveReports()
	if err != nil {
		t.Fatalf("rendering reports: %v", err)
	}
	if !strings.Contains(report, "not saved") {
		t.Errorf("the refusal was not reported:\n%s", report)
	}
}

// With no project open the actions must not panic; there is nothing to save.
func TestSchemaActions_WithNoSession(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)

	for _, path := range []string{
		"/forge/schema/save", "/forge/schema/discard",
		"/forge/schema/reload", "/forge/schema/overwrite",
	} {
		if code := post(t, srv, path); code != http.StatusConflict {
			t.Errorf("POST %s = %d, want 409", path, code)
		}
	}

	// And the modes still render.
	resp, _ := get(t, srv, mode.Default.Path())
	if resp.StatusCode != http.StatusOK {
		t.Errorf("mode page = %d with no session, want 200", resp.StatusCode)
	}
}

// Actions are actions: there is no page to GET at these paths.
func TestSchemaActions_AreNotPages(t *testing.T) {
	srv, _, _, _ := sessionServer(t)
	for _, path := range []string{"/forge/schema/save", "/forge/schema/discard"} {
		resp, _ := get(t, srv, path)
		if resp.StatusCode == http.StatusOK {
			t.Errorf("GET %s returned a page; these are actions", path)
		}
	}
}

// Reload means "take what is on disk", which makes the previous save outcome
// describe a version of the file that is no longer being edited.
func TestSchemaReload_ClearsTheStaleSaveReport(t *testing.T) {
	srv, s, sess, _ := sessionServer(t)

	if err := sess.Edit(func(d *schema.DatabaseSchema) error {
		et := d.EntityTypes["Player"]
		et.RequiredComponents = []string{"Nope"}
		d.EntityTypes["Player"] = et
		return nil
	}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if code := post(t, srv, "/forge/schema/save"); code != http.StatusNoContent {
		t.Fatalf("save = %d", code)
	}

	report, err := s.renderSaveReports()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if !strings.Contains(report, "not saved") {
		t.Fatalf("the refusal was not recorded:\n%s", report)
	}

	if code := post(t, srv, "/forge/schema/reload"); code != http.StatusNoContent {
		t.Fatalf("reload = %d", code)
	}
	report, err = s.renderSaveReports()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if strings.Contains(report, "not saved") {
		t.Errorf("the stale report survived a reload:\n%s", report)
	}
}

// Forge binds loopback, which is not a security boundary: a POST with no custom
// headers is a simple request, so any page the user visits can fire one with no
// preflight. These endpoints write to the user's schema.json.
func TestSchemaActions_RefuseCrossOriginWrites(t *testing.T) {
	srv, _, sess, path := sessionServer(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if err := sess.Edit(func(d *schema.DatabaseSchema) error { d.SchemaVersion = 42; return nil }); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	for _, target := range []string{
		"/forge/schema/save", "/forge/schema/discard",
		"/forge/schema/reload", "/forge/schema/overwrite",
		"/forge/schema/version", "/forge/schema/component",
		"/forge/schema/shape", "/forge/schema/behavior", "/forge/schema/field",
	} {
		t.Run(target, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, srv.URL+target, strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("status = %d, want 403", resp.StatusCode)
			}
		})
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(before) != string(after) {
		t.Error("a cross-site request wrote to the user's schema")
	}
}

// A same-origin write from the page itself still works, and so does a
// non-browser client that sends no such header.
func TestSchemaActions_AllowSameOriginAndNonBrowsers(t *testing.T) {
	for _, site := range []string{"same-origin", "none", ""} {
		t.Run("Sec-Fetch-Site="+site, func(t *testing.T) {
			srv, _, _, _ := sessionServer(t)
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/forge/schema/discard", strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			if site != "" {
				req.Header.Set("Sec-Fetch-Site", site)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Errorf("status = %d, want 204", resp.StatusCode)
			}
		})
	}
}

// Discard and reload write nothing, so reporting a save outcome for them would
// be a claim about the user's file that is not true.
func TestSchemaActions_NonWritingActionsReportNothing(t *testing.T) {
	srv, s, sess, _ := sessionServer(t)

	if err := sess.Edit(func(d *schema.DatabaseSchema) error { d.SchemaVersion = 4; return nil }); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	for _, target := range []string{"/forge/schema/discard", "/forge/schema/reload"} {
		t.Run(target, func(t *testing.T) {
			if code := post(t, srv, target); code != http.StatusNoContent {
				t.Fatalf("%s = %d", target, code)
			}
			report, err := s.renderSaveReports()
			if err != nil {
				t.Fatalf("rendering: %v", err)
			}
			if strings.Contains(report, "saved") {
				t.Errorf("%s reported a save outcome for a file it never wrote:\n%s", target, report)
			}
		})
	}
}

// The Save button must post to the endpoint that checks for an external edit.
// Wiring it to the overwrite endpoint would clobber someone else's change and
// still write the file, so nothing downstream would notice.
func TestSchemaSave_UsesTheConflictCheckingPath(t *testing.T) {
	srv, s, sess, path := sessionServer(t)

	if err := sess.Edit(func(d *schema.DatabaseSchema) error { d.SchemaVersion = 5; return nil }); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	theirs := strings.Replace(sessionSchema, `"schemaVersion": 3`, `"schemaVersion": 7`, 1)
	if err := os.WriteFile(path, []byte(theirs), 0o600); err != nil {
		t.Fatalf("external write: %v", err)
	}

	if code := post(t, srv, "/forge/schema/save"); code != http.StatusNoContent {
		t.Fatalf("save = %d", code)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if !strings.Contains(string(onDisk), `"schemaVersion": 7`) {
		t.Error("save overwrote an external edit; it is wired to the overwrite endpoint")
	}
	report, err := s.renderSaveReports()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if !strings.Contains(report, "changed on disk") {
		t.Errorf("the conflict was not reported:\n%s", report)
	}
	// And the conflict offers both ways out, rather than leaving Discard as the
	// only control — which would silently destroy the edit.
	for _, want := range []string{"conflict-reload", "conflict-overwrite"} {
		if !strings.Contains(report, want) {
			t.Errorf("the conflict report offers no %q control:\n%s", want, report)
		}
	}
}

// The footer the stream pushes must reflect the session, not a constant.
func TestRenderFooter_ReflectsTheSession(t *testing.T) {
	_, s, sess, _ := sessionServer(t)

	clean, err := s.renderFooter(modes.Data{})
	if err != nil {
		t.Fatalf("renderFooter: %v", err)
	}
	if strings.Contains(clean, "save-footer--dirty") {
		t.Errorf("a clean session renders as dirty:\n%s", clean)
	}
	if !strings.Contains(clean, "schema.json") {
		t.Errorf("the footer does not name the file:\n%s", clean)
	}

	if err := sess.Edit(func(d *schema.DatabaseSchema) error { d.SchemaVersion = 4; return nil }); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	dirty, err := s.renderFooter(modes.Data{})
	if err != nil {
		t.Fatalf("renderFooter: %v", err)
	}
	if !strings.Contains(dirty, "save-footer--dirty") {
		t.Errorf("an edited session renders as clean:\n%s", dirty)
	}
}

// The version badge is a real control, so bumping must actually edit.
func TestSchemaVersion_MakesARealEdit(t *testing.T) {
	srv, _, sess, _ := sessionServer(t)

	if code := post(t, srv, "/forge/schema/version"); code != http.StatusNoContent {
		t.Fatalf("bump = %d", code)
	}
	var version int
	sess.Read(func(d schema.DatabaseSchema) { version = d.SchemaVersion })
	if version != 4 {
		t.Errorf("SchemaVersion = %d, want it bumped", version)
	}
	if dirty, err := sess.Dirty(); err != nil || !dirty {
		t.Errorf("Dirty = %v (err %v) after a bump, want true", dirty, err)
	}
}
