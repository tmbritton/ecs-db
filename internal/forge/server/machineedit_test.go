package server

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/machines"
	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/forge/savereport"
	"github.com/tmbritton/ecs-db/internal/forge/session"
	"github.com/tmbritton/ecs-db/internal/forge/status"
	"github.com/tmbritton/ecs-db/internal/schema"
)

const machineSchema = `{
  "schemaVersion": 3,
  "components": {
    "Health": { "type": "object", "properties": { "hp": { "type": "integer" } } }
  },
  "entityTypes": {
    "Goblin": {
      "behavior": "wander",
      "requiredComponents": ["Health"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    }
  }
}
`

const wanderMachine = `{
  "id": "wander",
  "initial": "idle",
  "states": {
    "idle": {
      "on": {
        "GO": [{ "target": "moving" }]
      }
    },
    "moving": {}
  }
}
`

// machineServer builds a project with a schema, one mod and one machine.
func machineServer(t *testing.T, extraMods ...string) (*httptest.Server, *Server, string) {
	t.Helper()
	return machineServerWith(t, wanderMachine, extraMods...)
}

// machineServerWith is machineServer over a machine of the caller's choosing,
// for the tests that need a shape wanderMachine does not have.
func machineServerWith(t *testing.T, body string, extraMods ...string) (*httptest.Server, *Server, string) {
	t.Helper()
	root := t.TempDir()
	schemaPath := filepath.Join(root, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(machineSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(root, "core", "behaviors")
	if err := os.MkdirAll(core, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(core, "wander.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	mods := []project.Mod{{Name: "core", Behaviors: core}}
	for _, name := range extraMods {
		dir := filepath.Join(root, name, "behaviors")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, project.Mod{Name: name, Behaviors: dir})
	}

	sess, err := session.Open(schemaPath)
	if err != nil {
		t.Fatalf("session.Open: %v", err)
	}
	ms, err := machines.Open(machines.Config{
		Mods: mods,
		Schema: func() schema.DatabaseSchema {
			var out schema.DatabaseSchema
			sess.Read(func(d schema.DatabaseSchema) { out = d })
			return out
		},
	})
	if err != nil {
		t.Fatalf("machines.Open: %v", err)
	}

	s := New(Config{
		Addr: "127.0.0.1:0", Session: sess, MachineSession: ms, Engine: status.Config{},
	}, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	return srv, s, core
}

func agentsRequest(machine string) *http.Request {
	url := "/forge/agents"
	if machine != "" {
		url += "?machine=" + machine
	}
	return httptest.NewRequest(http.MethodGet, url, nil)
}

func TestAgentsPage_ListsTheProjectsMachines(t *testing.T) {
	srv, _, _ := machineServer(t)
	_, body := get(t, srv, "/forge/agents")

	if !strings.Contains(body, `data-testid="machine-wander"`) {
		t.Fatalf("the machine is not listed:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="machine-editor"`) {
		t.Error("no machine is selected on first paint")
	}
}

// One Save button must do one thing, so the footer follows the mode. Asserted
// with something actually unsaved: the footer wires no action at all when
// clean, so a clean page satisfies "does not offer the schema save" whatever
// the dispatch does.
func TestAgentsPage_FooterSavesMachinesNotTheSchema(t *testing.T) {
	srv, s, dir := machineServer(t)
	path := filepath.Join(dir, "wander.json")
	if err := s.cfg.MachineSession.Edit(path, func(d *agent.MachineDefinition) error {
		d.Initial = "moving"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	_, body := get(t, srv, "/forge/agents")
	if !strings.Contains(body, "/forge/agents/save") {
		t.Errorf("the AGENTS footer does not save machines:\n%s", body)
	}
	if strings.Contains(body, "/forge/schema/save") {
		t.Error("the AGENTS footer offers the schema save")
	}
	if !strings.Contains(body, "save-footer--dirty") {
		t.Error("an edited machine does not mark the footer dirty")
	}
	if !strings.Contains(body, "wander.json") {
		t.Error("the footer does not name the unsaved machine")
	}

	// And the other way: an unsaved schema marks SCHEMA's footer, not this one.
	if err := s.cfg.Session.Edit(func(d *schema.DatabaseSchema) error {
		d.SchemaVersion = 9
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, schemaBody := get(t, srv, "/forge/schema")
	if !strings.Contains(schemaBody, "/forge/schema/save") {
		t.Error("SCHEMA lost its own save action")
	}
	if strings.Contains(schemaBody, "/forge/agents/save") {
		t.Error("SCHEMA offers the machine save")
	}
}

func TestAgentsPage_KeepsTheSchemaFooterOnSchemaModes(t *testing.T) {
	srv, _, _ := machineServer(t)
	for _, slug := range []string{"schema", "ents", "map"} {
		t.Run(slug, func(t *testing.T) {
			_, body := get(t, srv, "/forge/"+slug)
			if !strings.Contains(body, "/forge/schema/save") {
				t.Errorf("%s lost the schema footer", slug)
			}
		})
	}
}

func TestMachineEdit_CreateRenameAndDelete(t *testing.T) {
	srv, s, dir := machineServer(t)

	if code := post(t, srv, "/forge/agents/machine?add=patrol"); code != 204 {
		t.Fatalf("create: %d", code)
	}
	created := filepath.Join(dir, "patrol.json")
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	// Immediately part of the resolved set — no restart, which is the
	// staleness Epic 12 Story 7 deferred to this story.
	if !strings.Contains(renderAgents(t, s, ""), `data-testid="machine-patrol"`) {
		t.Error("the new machine is not in the list")
	}

	if code := post(t, srv, "/forge/agents/machine?machine="+created+"&renameID=guard"); code != 204 {
		t.Fatalf("rename id: %d", code)
	}
	if code := post(t, srv, "/forge/agents/save"); code != 204 {
		t.Fatalf("save: %d", code)
	}
	if body, _ := os.ReadFile(created); !strings.Contains(string(body), `"id": "guard"`) {
		t.Errorf("the id did not change on disk:\n%s", body)
	}

	if code := post(t, srv, "/forge/agents/machine?machine="+created+"&renameFile=sentinel"); code != 204 {
		t.Fatalf("rename file: %d", code)
	}
	moved := filepath.Join(dir, "sentinel.json")
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("the file did not move: %v", err)
	}

	if code := post(t, srv, "/forge/agents/machine?delete="+moved); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(moved); !os.IsNotExist(err) {
		t.Error("the file is still there after deleting")
	}
}

// A machine created in Forge has to be bindable from ENTS at once.
func TestMachineEdit_NewMachineIsBindableImmediately(t *testing.T) {
	srv, s, _ := machineServer(t)
	if code := post(t, srv, "/forge/agents/machine?add=patrol"); code != 204 {
		t.Fatalf("create: %d", code)
	}

	data := s.modeData(httptest.NewRequest(http.MethodGet, "/forge/ents?type=Goblin", nil))
	m, _ := mode.Lookup("ents")
	content, err := s.renderModeContent(m, data)
	if err != nil {
		t.Fatalf("renderModeContent: %v", err)
	}
	if !strings.Contains(content, `value="patrol"`) {
		t.Errorf("the new machine is not offered in the behaviour dropdown:\n%s", content)
	}
}

func TestMachineEdit_RefusesAndSaysWhy(t *testing.T) {
	srv, s, _ := machineServer(t)

	// An id another machine already has.
	if code := post(t, srv, "/forge/agents/machine?add=wander"); code != 204 {
		t.Fatalf("create: %d", code)
	}
	if got := problemOf(s); !strings.Contains(got, "already declared by") {
		t.Errorf("the refusal is not reported to the editor: %q", got)
	}
	if !strings.Contains(renderAgents(t, s, ""), "already declared by") {
		t.Error("the reason is not on the page")
	}
}

// The choice of mod is the user's when there is one to make.
func TestMachineEdit_RefusesToGuessTheMod(t *testing.T) {
	srv, s, _ := machineServer(t, "extra")

	if code := post(t, srv, "/forge/agents/machine?add=patrol"); code != 204 {
		t.Fatalf("create: %d", code)
	}
	if got := problemOf(s); !strings.Contains(got, "name the one to use") {
		t.Errorf("Forge picked a mod rather than asking: %q", got)
	}

	if code := post(t, srv, "/forge/agents/machine?add=patrol&mod=extra"); code != 204 {
		t.Fatalf("create with a mod: %d", code)
	}
	if got := problemOf(s); got != "" {
		t.Errorf("naming the mod still failed: %q", got)
	}
}

// A path from the query string must be checked against what is open, or a
// crafted request addresses any file on disk.
func TestMachineEdit_RefusesAPathItDoesNotHold(t *testing.T) {
	srv, s, _ := machineServer(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(outside, []byte(wanderMachine), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := post(t, srv, "/forge/agents/machine?delete="+outside); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("a file outside the project was deleted")
	}
	// The handler's own wording, not the session's: both check, and asserting a
	// message they shared could not tell which one answered.
	if got := problemOf(s); !strings.Contains(got, "not a machine this project has open") {
		t.Errorf("the handler passed an unchecked path straight through, got %q", got)
	}
}

func TestMachineSave_ReportsEachMachineSeparately(t *testing.T) {
	srv, s, dir := machineServer(t)
	if code := post(t, srv, "/forge/agents/machine?add=patrol"); code != 204 {
		t.Fatalf("create: %d", code)
	}
	good := filepath.Join(dir, "wander.json")
	bad := filepath.Join(dir, "patrol.json")

	if err := s.cfg.MachineSession.Edit(good, func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.MachineSession.Edit(bad, func(d *agent.MachineDefinition) error { d.Initial = "nowhere"; return nil }); err != nil {
		t.Fatal(err)
	}

	if code := post(t, srv, "/forge/agents/save"); code != 204 {
		t.Fatalf("save: %d", code)
	}
	reports := s.saves.All()
	byPath := map[string]bool{}
	for _, r := range reports {
		byPath[r.Path] = r.Outcome == savereport.OutcomeSaved ||
			r.Outcome == savereport.OutcomeSavedNoEngine
	}
	if !byPath[good] {
		t.Errorf("the valid machine was not reported as saved: %+v", reports)
	}
	if byPath[bad] {
		t.Errorf("the invalid machine was reported as saved: %+v", reports)
	}
	// And the valid one really is on disk: one refusal must not hold the rest.
	if body, _ := os.ReadFile(good); !strings.Contains(string(body), `"initial": "moving"`) {
		t.Errorf("a refusal blocked an unrelated save:\n%s", body)
	}
}

func renderAgents(t *testing.T, s *Server, machine string) string {
	t.Helper()
	m, ok := mode.Lookup("agents")
	if !ok {
		t.Fatal("no agents mode")
	}
	out, err := s.renderModeContent(m, s.modeData(agentsRequest(machine)))
	if err != nil {
		t.Fatalf("rendering AGENTS: %v", err)
	}
	return out
}

func TestAgentsFooter_CountsWhenMoreThanOneIsUnsaved(t *testing.T) {
	srv, s, dir := machineServer(t)
	if code := post(t, srv, "/forge/agents/machine?add=patrol"); code != 204 {
		t.Fatalf("create: %d", code)
	}
	for _, name := range []string{"wander.json", "patrol.json"} {
		path := filepath.Join(dir, name)
		if err := s.cfg.MachineSession.Edit(path, func(d *agent.MachineDefinition) error {
			d.Initial = "moving"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	_, body := get(t, srv, "/forge/agents")
	if !strings.Contains(body, "2 unsaved machines") {
		t.Errorf("the footer does not count them:\n%s", body)
	}
	// The count says how many; the title says which.
	for _, name := range []string{"wander.json", "patrol.json"} {
		if !strings.Contains(body, name) {
			t.Errorf("the footer does not name %s anywhere", name)
		}
	}
}

// A page subscribes its stream with the path it is showing. Renaming the file
// moves that path, and without a trail the stream asks for one that no longer
// exists — the fallback picks the first machine and the editor swaps under the
// user, two seconds after they renamed something.
func TestMachineEdit_RenamingAFileKeepsTheEditorOnIt(t *testing.T) {
	srv, s, dir := machineServer(t)
	if code := post(t, srv, "/forge/agents/machine?add=patrol"); code != 204 {
		t.Fatalf("create: %d", code)
	}
	// patrol sorts after wander is created, so the one being renamed is
	// deliberately not the one the fallback would land on.
	original := filepath.Join(dir, "wander.json")

	if code := post(t, srv, "/forge/agents/machine?machine="+original+"&renameFile=wandering"); code != 204 {
		t.Fatalf("rename: %d", code)
	}
	// The stream still names the old path, as a live page's would.
	content := renderAgents(t, s, original)
	if !strings.Contains(content, `data-machine="wandering"`) {
		t.Errorf("the editor did not follow the renamed file:\n%s", content)
	}
	if strings.Contains(content, `data-machine="patrol"`) {
		t.Error("the editor fell back to a different machine")
	}
}

// The stream URL is what the SSE subscription re-renders against, and it used
// to carry ?component= alone. On AGENTS that meant no machine was named, the
// fallback picked the first, and the page swapped to a different machine two
// seconds after the user clicked one. Invisible in every Go test until this one.
func TestAgentsPage_SubscribesWithTheMachineItIsShowing(t *testing.T) {
	srv, s, dir := machineServer(t)
	if code := post(t, srv, "/forge/agents/machine?add=patrol"); code != 204 {
		t.Fatalf("create: %d", code)
	}
	// Not the one the fallback would land on: patrol sorts first.
	wander := filepath.Join(dir, "wander.json")

	_, body := get(t, srv, "/forge/agents?machine="+wander)
	init := dataInit(t, body)
	if !strings.Contains(init, "machine=") {
		t.Fatalf("the subscription names no machine, so the stream re-renders with\n"+
			"whichever one sorts first:\n%s", init)
	}
	if !strings.Contains(init, url.QueryEscape(wander)) {
		t.Errorf("the subscription names a different machine from the page:\n%s", init)
	}
	_ = s
}

// And the schema modes keep theirs, so the fix did not trade one for the other.
func TestSchemaPage_StillSubscribesWithItsComponent(t *testing.T) {
	srv, _, _ := machineServer(t)
	_, body := get(t, srv, "/forge/schema?component=Health")
	if init := dataInit(t, body); !strings.Contains(init, "component=Health") {
		t.Errorf("SCHEMA lost its selection from the subscription:\n%s", init)
	}
}

func dataInit(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`data-init="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the page opens no subscription at all")
	}
	return html.UnescapeString(m[1])
}

// Story 1 left this for Story 2: a machine authored in another whitespace style
// differs from its own file the moment Forge opens it. Calling that "unsaved
// changes" is how a tool teaches you to ignore the word.
func TestAgentsFooter_SaysReformattingRatherThanUnsaved(t *testing.T) {
	srv, s, dir := machineServer(t)
	compact := filepath.Join(dir, "patrol.json")
	if err := os.WriteFile(compact, []byte(
		`{"id":"patrol","initial":"a","states":{"a":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.MachineSession.Reload(); err != nil {
		t.Fatal(err)
	}

	_, body := get(t, srv, "/forge/agents")
	if !strings.Contains(body, "would be reformatted") {
		t.Errorf("the footer reports a file nobody edited as an edit:\n%s", body)
	}
	if strings.Contains(body, "unsaved machines") {
		t.Error("the footer counts a reformat as unsaved work")
	}

	// And a real edit on top of it reads as an edit again.
	if err := s.cfg.MachineSession.Edit(compact, func(d *agent.MachineDefinition) error {
		d.Initial = "a"
		d.States["b"] = &agent.StateNode{ID: "patrol.b"}
		d.StateOrder = append(d.StateOrder, "b")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, edited := get(t, srv, "/forge/agents")
	if strings.Contains(edited, "would be reformatted") {
		t.Errorf("an edited machine still reads as merely reformatting:\n%s", edited)
	}
}

// One Save button does one thing, so the footer follows the mode — which made
// switching to AGENTS with a dirty schema.json read "✓ saved" over a disabled
// Save. That is a way to lose work, not a wording miss.
func TestFooter_DoesNotHideUnsavedWorkTheOtherModeOwns(t *testing.T) {
	srv, s, dir := machineServer(t)

	if err := s.cfg.Session.Edit(func(d *schema.DatabaseSchema) error {
		d.SchemaVersion = 9
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, agents := get(t, srv, "/forge/agents")
	if strings.Contains(agents, "✓ saved") {
		t.Errorf("AGENTS claims everything is saved over an unsaved schema.json:\n%s", agents)
	}
	if !strings.Contains(agents, "schema.json") || !strings.Contains(agents, "SCHEMA") {
		t.Errorf("AGENTS does not say where the unsaved work is:\n%s", agents)
	}

	// And the other way round.
	if err := s.cfg.Session.Discard(); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.MachineSession.Edit(filepath.Join(dir, "wander.json"),
		func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
		t.Fatal(err)
	}
	_, schemaPage := get(t, srv, "/forge/schema")
	if strings.Contains(schemaPage, "✓ saved") {
		t.Errorf("SCHEMA claims everything is saved over an unsaved machine:\n%s", schemaPage)
	}
	if !strings.Contains(schemaPage, "AGENTS") {
		t.Errorf("SCHEMA does not say where the unsaved work is:\n%s", schemaPage)
	}
}

// A machine authored in another whitespace style is not pending work, so it
// must not raise the alarm from the other mode either.
func TestFooter_DoesNotCallAReformatUnsavedWorkElsewhere(t *testing.T) {
	srv, s, dir := machineServer(t)
	if err := os.WriteFile(filepath.Join(dir, "patrol.json"), []byte(
		`{"id":"patrol","initial":"a","states":{"a":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.MachineSession.Reload(); err != nil {
		t.Fatal(err)
	}
	_, body := get(t, srv, "/forge/schema")
	if strings.Contains(body, "unsaved machine") {
		t.Errorf("a file nobody edited is reported as pending work from SCHEMA:\n%s", body)
	}
}

// Only AGENTS selects with a machine, and machineData runs for every mode — so
// without a gate every page put an absolute path from the developer's
// filesystem into its subscription URL.
func TestStreamQuery_NamesAMachineOnlyWhereItMeansSomething(t *testing.T) {
	srv, _, _ := machineServer(t)
	for _, slug := range []string{"schema", "ents", "map", "tiles", "sprites"} {
		t.Run(slug, func(t *testing.T) {
			_, body := get(t, srv, "/forge/"+slug)
			if init := dataInit(t, body); strings.Contains(init, "machine=") {
				t.Errorf("%s subscribes with a machine path it has no use for:\n%s", slug, init)
			}
		})
	}
}

// modeData resolves ?component= first and only falls back to ?type=, so the
// subscription's key has to be chosen the same way round — otherwise a page
// carrying both subscribes under the wrong namespace and follows the wrong
// rename map.
func TestStreamQuery_UsesTheNamespaceModeDataResolved(t *testing.T) {
	srv, _, _ := machineServer(t)
	for _, tc := range []struct{ url, want string }{
		{"/forge/ents?type=Goblin", "type=Goblin"},
		{"/forge/schema?component=Health", "component=Health"},
		{"/forge/schema?component=Health&type=Goblin", "component=Health"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			_, body := get(t, srv, tc.url)
			if init := dataInit(t, body); !strings.Contains(init, tc.want) {
				t.Errorf("want %q in the subscription, got:\n%s", tc.want, init)
			}
		})
	}
}

// The stream has to re-render the canvas with the selection the page has, or
// its first frame silently clears it.
//
// The subscription carries the page's id and not the selection itself. It used
// to carry ?sel=, and that is precisely what made selecting a node a page load:
// a subscription URL is fixed when the page opens, so the only way to change
// what it asked for was to open another page. The selection is recorded against
// the id instead, and the stream looks it up on every render.
func TestStreamQuery_IdentifiesThePageSoTheStreamCanFindItsSelection(t *testing.T) {
	srv, s, dir := machineServer(t)
	path := filepath.Join(dir, "core", "behaviors", "wander.json")

	_, body := get(t, srv, "/forge/agents?machine="+url.QueryEscape(path)+"&sel=state:idle")
	init := dataInit(t, body)
	if !strings.Contains(init, "page=") {
		t.Fatalf("the subscription does not identify the page:\n%s", init)
	}
	if strings.Contains(init, "sel=") {
		t.Errorf("the subscription still freezes a selection into its URL:\n%s", init)
	}

	page := pageIDIn(t, init)
	if got := s.pageSel(page); got != "state:idle" {
		t.Errorf("the page has %q selected, not what its URL asked for", got)
	}

	// And only what resolved. A selection naming a state that is not there is
	// dropped by the chart, and recording it would leave the page asking for a
	// selection it does not have for as long as it stays open.
	_, body = get(t, srv, "/forge/agents?machine="+url.QueryEscape(path)+"&sel=state:gone")
	if got := s.pageSel(pageIDIn(t, dataInit(t, body))); got != "" {
		t.Errorf("the page recorded %q, a selection that resolved to nothing", got)
	}
}

// pageIDIn pulls the page id out of a subscription URL.
func pageIDIn(t *testing.T, init string) string {
	t.Helper()
	m := regexp.MustCompile(`page=([0-9a-f]{32})`).FindStringSubmatch(init)
	if m == nil {
		t.Fatalf("no page id in %q", init)
	}
	return m[1]
}

// The SSE loop suppresses a patch by comparing the rendered string, so anything
// that renders differently for one unchanged state is re-sent on a random
// fraction of ticks, forever.
func TestAgentsFooter_RendersTheSameForAnUnchangedState(t *testing.T) {
	srv, s, dir := machineServer(t)
	for _, id := range []string{"patrol", "guard", "sentry"} {
		if code := post(t, srv, "/forge/agents/machine?add="+id); code != 204 {
			t.Fatalf("create %s: %d", id, code)
		}
	}
	for _, name := range []string{"wander.json", "patrol.json", "guard.json", "sentry.json"} {
		if err := s.cfg.MachineSession.Edit(filepath.Join(dir, name),
			func(d *agent.MachineDefinition) error { d.Initial = "moving"; return nil }); err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]bool{}
	for range 30 {
		out, err := s.renderFooter("agents", s.modeData(agentsRequest("")))
		if err != nil {
			t.Fatalf("renderFooter: %v", err)
		}
		seen[out] = true
	}
	if len(seen) != 1 {
		t.Errorf("the footer rendered %d different ways for one unchanged state; the\n"+
			"stream would re-patch it on a random fraction of every tick", len(seen))
	}
}

// The manifest and the validity readout are computed per render from the
// *working* value. Neither can come off the resolved machine, which describes
// the file as last read, nor off the working definition, which is parse-derived
// and carries no manifest at all — so this is the only test that can show the
// page is showing what is being edited.
func TestAgentsPage_ShowsTheManifestForTheWorkingValue(t *testing.T) {
	srv, s, dir := machineServer(t)
	path := filepath.Join(dir, "wander.json")

	// hp is a field of Health in the fixture schema, so it maps.
	if err := s.cfg.MachineSession.Edit(path, func(d *agent.MachineDefinition) error {
		d.Context = map[string]any{"hp": float64(3)}
		d.ContextOrder = []string{"hp"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	_, body := get(t, srv, "/forge/agents")
	if !strings.Contains(body, `data-testid="manifest-hp"`) {
		t.Fatalf("the manifest does not show a key added in this session:\n%s", body)
	}
	if !strings.Contains(body, "Health") {
		t.Error("the manifest does not name the component the key comes from")
	}
	if !strings.Contains(body, `data-valid="true"`) {
		t.Error("a machine the engine accepts is not reported as valid")
	}
}

func TestAgentsPage_ReportsAMachineEditedIntoInvalidity(t *testing.T) {
	srv, s, dir := machineServer(t)
	path := filepath.Join(dir, "wander.json")

	if err := s.cfg.MachineSession.Edit(path, func(d *agent.MachineDefinition) error {
		d.States["idle"].On["GO"][0].Target = "nowhere"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	_, body := get(t, srv, "/forge/agents")
	if !strings.Contains(body, `data-testid="manifest-unavailable"`) {
		t.Errorf("a machine that does not validate still shows a manifest:\n%s", body)
	}
	// Against the edge that carries it, not in a list at the top: a transition
	// target is a transition's problem, and the canvas is where it is findable.
	if !strings.Contains(body, `data-testid="edge-idle|on|GO|0"`) {
		t.Fatal("the edge is not drawn")
	}
	if !strings.Contains(body, `data-invalid="true"`) {
		t.Error("nothing on the canvas is marked as carrying the problem")
	}
	if !strings.Contains(body, "nowhere") {
		t.Error("the problem does not name the broken target")
	}
	// And the footer refuses the save, because the save would be refused. The
	// count comes from the session rather than from the mode's data: the footer
	// is on screen in every mode and this one is only computed on AGENTS.
	if !strings.Contains(body, `data-blocked="true"`) {
		t.Error("the footer offers a save the engine would refuse")
	}
	if !strings.Contains(body, "the engine would refuse") {
		t.Error("the disabled save gives no reason")
	}
}

// The footer follows the mode, and the machines' one has to know about machines
// wherever it is rendered — a save refused on AGENTS is refused from SCHEMA too.
func TestFooter_BlocksOnAnInvalidMachineFromAnotherMode(t *testing.T) {
	srv, s, core := machineServerWith(t, canvasMachine)
	path := filepath.Join(core, "wander.json")

	if err := s.cfg.MachineSession.Edit(path, func(d *agent.MachineDefinition) error {
		d.States["idle"].On["GO"][0].Target = "nowhere"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	_, agents := get(t, srv, "/forge/agents")
	if !strings.Contains(agents, `data-blocked="true"`) {
		t.Error("AGENTS offers a save the engine would refuse")
	}
}

// A machine stranded by a re-resolve is in no list, so the only way to reach it
// is the problem panel — and the only handler that must accept it is discard.
func TestMachineDiscard_ReachesStrandedWork(t *testing.T) {
	srv, s, dir := machineServer(t, "overlay")
	path := filepath.Join(dir, "wander.json")
	sess := s.cfg.MachineSession

	if err := sess.Edit(path, func(d *agent.MachineDefinition) error {
		d.Initial = "moving"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A later mod supplies its own wander, so the core file stops resolving.
	overlay := filepath.Join(filepath.Dir(filepath.Dir(dir)), "overlay", "behaviors")
	if err := os.WriteFile(filepath.Join(overlay, "wander.json"), []byte(wanderMachine), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(); err != nil {
		t.Fatal(err)
	}

	// The page says so, and offers the one thing there is to do.
	_, body := get(t, srv, "/forge/agents")
	if !strings.Contains(body, `data-testid="machine-project-problems"`) {
		t.Fatalf("stranded work is not reported on the page:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="discard-stranded-wander.json"`) {
		t.Error("stranded work is reported with no way to give it up")
	}

	post(t, srv, "/forge/agents/discard?machine="+url.QueryEscape(path))
	for _, held := range sess.Held() {
		if held == path {
			t.Fatal("the stranded machine is still held after discarding it")
		}
	}
}

// Rename and delete stay on the resolved set. A machine in no list has no
// visible subject to rename, and the wider set exists for discard alone.
func TestMachineEdit_DoesNotReachStrandedWork(t *testing.T) {
	srv, s, dir := machineServer(t, "overlay")
	path := filepath.Join(dir, "wander.json")
	sess := s.cfg.MachineSession

	if err := sess.Edit(path, func(d *agent.MachineDefinition) error {
		d.Initial = "moving"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(filepath.Dir(filepath.Dir(dir)), "overlay", "behaviors")
	if err := os.WriteFile(filepath.Join(overlay, "wander.json"), []byte(wanderMachine), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sess.Reload(); err != nil {
		t.Fatal(err)
	}

	post(t, srv, "/forge/agents/machine?renameID=roam&machine="+url.QueryEscape(path))
	if got := problemOf(s); !strings.Contains(got, "not a machine this project has open") {
		t.Errorf("renaming a stranded machine was not refused: %q", got)
	}
}

// The create control proposes rather than repeats: the skeleton posted a fixed
// id, so the second press was refused until the first machine was renamed.
func TestAgentsPage_ProposesAFreeIDForEachNewMachine(t *testing.T) {
	srv, s, _ := machineServer(t)

	_, body := get(t, srv, "/forge/agents")
	if !strings.Contains(body, "add=NewMachine") {
		t.Fatalf("the create control proposes nothing:\n%s", body)
	}

	if _, err := s.cfg.MachineSession.Create("NewMachine", "core"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, second := get(t, srv, "/forge/agents")
	if !strings.Contains(second, "add=NewMachine2") {
		t.Errorf("the create control still proposes a taken id:\n%s", second)
	}
}
