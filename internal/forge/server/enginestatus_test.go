package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/status"
	"github.com/tmbritton/ecs-db/internal/schema"
	"github.com/tmbritton/ecs-db/internal/storage"
)

// A short interval keeps these tests quick without making them timing-sensitive:
// every assertion waits for a condition rather than sleeping a fixed span.
const testPoll = 40 * time.Millisecond

func bootstrapDB(t *testing.T, path string, v int) {
	t.Helper()
	s := schema.DatabaseSchema{
		SchemaVersion: v,
		Components: map[string]schema.Component{
			"Position": {Type: "object", Properties: map[string]schema.Property{"x": {Type: "number"}}},
		},
		EntityTypes: map[string]schema.EntityType{
			"Thing": {RequiredComponents: []string{"Position"}, ValidationLevel: schema.ValidationStrict},
		},
	}
	store, err := storage.NewSQLiteStore(path, s, "")
	if err != nil {
		t.Fatalf("bootstrapping v%d: %v", v, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
}

func statusServer(t *testing.T, dbPath, schemaPath string) (*httptest.Server, *Server) {
	t.Helper()
	return statusServerEvery(t, dbPath, schemaPath, testPoll)
}

func statusServerEvery(t *testing.T, dbPath, schemaPath string, poll time.Duration) (*httptest.Server, *Server) {
	t.Helper()
	s := New(Config{
		Addr:         "127.0.0.1:0",
		PollInterval: poll,
		Engine: status.Config{
			DBPath: dbPath, SchemaPath: schemaPath, ModName: "core",
		},
	}, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	return srv, s
}

// writeSchema puts a minimal, valid schema.json at version v on disk.
func writeSchema(t *testing.T, dir string, v int) string {
	t.Helper()
	p := filepath.Join(dir, "schema.json")
	body := fmt.Sprintf(`{
	  "schemaVersion": %d,
	  "components": {"Position": {"type": "object", "properties": {"x": {"type": "number"}}}},
	  "entityTypes": {"Thing": {"requiredComponents": ["Position"]}}
	}`, v)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("writing schema: %v", err)
	}
	return p
}

// frames reads `datastar-patch-elements` payloads off a live stream and
// publishes them on a channel until the context is cancelled.
func frames(t *testing.T, ctx context.Context, url string) <-chan string {
	t.Helper()
	out := make(chan string, 16)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("opening stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}

	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		var event string
		var payload strings.Builder
		flush := func() {
			if event == "datastar-patch-elements" && payload.Len() > 0 {
				select {
				case out <- payload.String():
				case <-ctx.Done():
				}
			}
			event, payload = "", strings.Builder{}
		}
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				flush()
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: elements "):
				// Newline-joined: a component rendered across several lines
				// arrives as several data: lines, and concatenating them
				// without a separator would splice tokens together and make
				// Contains unreliable in both directions.
				if payload.Len() > 0 {
					payload.WriteString("\n")
				}
				payload.WriteString(strings.TrimPrefix(line, "data: elements "))
			}
		}
		flush()
		if err := sc.Err(); err != nil && ctx.Err() == nil {
			// A payload over bufio's 64KB default would otherwise end the scan
			// in silence, surfacing later as a misattributed "no frame" error.
			t.Errorf("reading the event stream: %v", err)
		}
	}()
	return out
}

// awaitFrame waits for a frame containing want, failing if none arrives.
func awaitFrame(t *testing.T, ch <-chan string, want, msg string) string {
	t.Helper()
	return awaitFrameWithin(t, ch, want, msg, 5*time.Second)
}

func awaitFrameWithin(t *testing.T, ch <-chan string, want, msg string, within time.Duration) string {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				t.Fatalf("%s: stream ended before a frame containing %q", msg, want)
			}
			if strings.Contains(f, want) {
				return f
			}
		case <-deadline:
			t.Fatalf("%s: no frame containing %q within %v", msg, want, within)
		}
	}
}

// The readout has to be right on first paint, before any stream traffic —
// otherwise the menu bar is blank or wrong for a poll interval on every load.
func TestModePage_RendersEngineStatusServerSide(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "ecs.db")
	bootstrapDB(t, db, 3)

	srv, _ := statusServer(t, db, writeSchema(t, dir, 3))
	_, body := get(t, srv, mode.Default.Path())

	if !strings.Contains(body, "hot-reload live") {
		t.Errorf("the page did not render the connected readout on load")
	}
	if !strings.Contains(body, "schema.json v3") {
		t.Errorf("the readout does not name the schema version")
	}
}

// The first patch must not wait a full interval: a client that reconnects
// should see current state at once rather than a stale or blank readout.
//
// The poll here is 30 seconds, so the tick cannot possibly produce this frame —
// which is the whole point. An earlier version used the 40ms test poll and
// measured "under 2 seconds", so a first frame that *did* wait for a tick still
// passed; verified by moving the tick-wait above the first render, which that
// version did not notice.
func TestModeEvents_PushesStatusBeforeTheFirstTick(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "ecs.db")
	bootstrapDB(t, db, 3)

	srv, _ := statusServerEvery(t, db, writeSchema(t, dir, 3), 30*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")
	f := awaitFrame(t, ch, "hot-reload live", "first frame")
	// A patch is HTML for the browser to morph, not JSON for a client to
	// interpret — and it has to carry the id Datastar matches on.
	if !strings.Contains(f, `id="engine-status"`) {
		t.Errorf("the patch does not target #engine-status:\n%s", f)
	}
}

// The promise of the story: the readout tracks the world, both ways.
func TestModeEvents_StatusFollowsTheDatabase(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "ecs.db")
	bootstrapDB(t, db, 3)

	srv, _ := statusServer(t, db, writeSchema(t, dir, 3))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")
	awaitFrame(t, ch, "hot-reload live", "connected at first")

	// The game stops and its database is removed.
	for _, p := range []string{db, db + "-wal", db + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			t.Fatalf("removing %s: %v", p, err)
		}
	}
	awaitFrame(t, ch, "watcher offline", "after the database disappeared")

	// And back again.
	bootstrapDB(t, db, 3)
	awaitFrame(t, ch, "hot-reload live", "after the database returned")
}

// A stale database is not the same as an absent one, and the readout has to say
// so — with both numbers, since which way round it is matters.
func TestModeEvents_ReportsAVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "ecs.db")
	bootstrapDB(t, db, 3)

	srv, _ := statusServer(t, db, writeSchema(t, dir, 4)) // schema is ahead of the database
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")
	f := awaitFrame(t, ch, "schema v4", "mismatch")

	if !strings.Contains(f, "db v3") {
		t.Errorf("the mismatch readout does not name the database version:\n%s", f)
	}
	if strings.Contains(f, "watcher offline") {
		t.Errorf("a mismatch is being reported as offline:\n%s", f)
	}
}

// A tick that re-sends identical HTML forever is wasted work on both ends, and
// it makes the browser's EventStream log useless for debugging the much busier
// traffic that lands on this same stream in Epic 18.
func TestModeEvents_DoesNotRepeatAnUnchangedStatus(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "ecs.db")
	bootstrapDB(t, db, 3)

	srv, _ := statusServer(t, db, writeSchema(t, dir, 3))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")

	// Well over a dozen poll intervals with nothing changing.
	time.Sleep(20 * testPoll)

	// Counted per element, not in total. The page has two live regions — the
	// engine status and the save reports — so the opening burst is legitimately
	// two frames. What must not happen is either being sent again.
	perElement := map[string]int{}
	drain := func() {
		for {
			select {
			case f, ok := <-ch:
				if !ok {
					t.Fatal("stream ended unexpectedly")
				}
				switch {
				case strings.Contains(f, `id="engine-status"`):
					perElement["engine-status"]++
				case strings.Contains(f, `id="save-reports"`):
					perElement["save-reports"]++
				case strings.Contains(f, `id="save-footer"`):
					perElement["save-footer"]++
				case strings.Contains(f, `id="mode-content"`):
					perElement["mode-content"]++
				case strings.Contains(f, `id="save-confirm"`):
					perElement["save-confirm"]++
				default:
					perElement["unknown"]++
				}
				continue
			default:
			}
			return
		}
	}
	drain()

	if perElement["engine-status"] != 1 {
		t.Errorf("engine status patched %d times over ~20 intervals with nothing changing, want 1",
			perElement["engine-status"])
	}
	if perElement["save-reports"] != 1 {
		t.Errorf("save reports patched %d times over ~20 intervals with nothing changing, want 1",
			perElement["save-reports"])
	}
	if perElement["save-footer"] != 1 {
		t.Errorf("save footer patched %d times over ~20 intervals with nothing changing, want 1",
			perElement["save-footer"])
	}
	if perElement["mode-content"] != 1 {
		t.Errorf("mode content patched %d times over ~20 intervals with nothing changing, want 1",
			perElement["mode-content"])
	}
	if perElement["save-confirm"] != 1 {
		t.Errorf("save confirmation patched %d times over ~20 intervals with nothing changing, want 1",
			perElement["save-confirm"])
	}
	if perElement["unknown"] != 0 {
		t.Errorf("%d frames patched something unrecognised", perElement["unknown"])
	}
}

// A closed tab must not leave a goroutine and a ticker running behind it.
func TestModeEvents_EndsWhenTheClientDisconnects(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "ecs.db")
	bootstrapDB(t, db, 3)

	srv, s := statusServer(t, db, writeSchema(t, dir, 3))
	ctx, cancel := context.WithCancel(context.Background())
	// Deferred as well as called below: without it, a t.Fatal before the
	// explicit cancel leaves the request open, httptest.Server.Close blocks in
	// cleanup, and the package hits its ten-minute timeout instead of failing.
	defer cancel()

	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")
	awaitFrame(t, ch, "hot-reload live", "first frame")
	waitFor(t, func() bool { return s.openStreams() == 1 }, "the stream never registered")

	cancel()
	waitFor(t, func() bool { return s.openStreams() == 0 },
		"the handler did not return when the client disconnected")
}

// A save report has to reach the page, on the same stream everything else uses.
// Epic 12's save button is the production caller; this drives the same method
// it will.
func TestModeEvents_PushesSaveReports(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "ecs.db")
	bootstrapDB(t, db, 3)

	srv, s := statusServer(t, db, writeSchema(t, dir, 3))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")
	awaitFrame(t, ch, `id="save-reports"`, "the empty report container")

	// A save that failed validation, with more than one problem.
	s.ReportSave("/p/goblin.json", errors.Join(
		errors.New("unknown action alpha"),
		errors.New("unknown guard beta"),
	))

	f := awaitFrame(t, ch, "goblin.json", "the rejection")
	for _, want := range []string{"not saved", "unknown action alpha", "unknown guard beta"} {
		if !strings.Contains(f, want) {
			t.Errorf("the report does not mention %q:\n%s", want, f)
		}
	}

	// A different file succeeding must not clear the first one's failure.
	s.ReportSave("/p/schema.json", nil)
	f = awaitFrame(t, ch, "schema.json", "the success")
	if !strings.Contains(f, "goblin.json") {
		t.Errorf("saving one file cleared another's report:\n%s", f)
	}
	// The database is present and matches, so something is listening.
	if !strings.Contains(f, "hot-reload live") {
		t.Errorf("a save with a compatible database should say hot-reload live:\n%s", f)
	}

	// And it can be dismissed.
	s.ClearSaveReport("/p/goblin.json")
	f = awaitFrame(t, ch, `id="save-reports"`, "after clearing")
	deadline := time.After(3 * time.Second)
	for strings.Contains(f, "goblin.json") {
		select {
		case next, ok := <-ch:
			if !ok {
				t.Fatal("stream ended")
			}
			f = next
		case <-deadline:
			t.Fatal("the cleared report never disappeared")
		}
	}
}

// With no game running, a save is not a failure — that is the normal case while
// authoring, and reporting it as an error would train people to ignore it.
func TestModeEvents_SaveWithNoEngineIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	// No database at all.
	srv, s := statusServer(t, filepath.Join(dir, "absent.db"), writeSchema(t, dir, 3))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := frames(t, ctx, srv.URL+mode.Default.Path()+"/events")
	awaitFrame(t, ch, `id="save-reports"`, "the empty report container")

	s.ReportSave("/p/schema.json", nil)
	f := awaitFrame(t, ch, "schema.json", "the save")

	if !strings.Contains(f, "no game running") {
		t.Errorf("want the no-engine wording:\n%s", f)
	}
	if strings.Contains(f, "save-report--bad") {
		t.Errorf("a save with nothing listening is styled as a failure:\n%s", f)
	}
}

// Finding from review: nothing covered the server passing existing reports into
// the shell. Navigating between modes after a failed save would have dropped the
// report from first paint until the next stream tick.
func TestModePage_RendersExistingSaveReports(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "ecs.db")
	bootstrapDB(t, db, 3)

	srv, s := statusServer(t, db, writeSchema(t, dir, 3))
	s.ReportSave("/p/goblin.json", errors.New("unknown action alpha"))

	_, body := get(t, srv, mode.Default.Path())
	for _, want := range []string{"goblin.json", "unknown action alpha", "not saved"} {
		if !strings.Contains(body, want) {
			t.Errorf("the served page does not show the existing report (%q missing)", want)
		}
	}
}
