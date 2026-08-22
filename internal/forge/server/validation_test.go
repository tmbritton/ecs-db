package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/agent"
	"github.com/tmbritton/ecs-db/internal/forge/mode"
	"github.com/tmbritton/ecs-db/internal/forge/project"
	"github.com/tmbritton/ecs-db/internal/forge/session"
	"github.com/tmbritton/ecs-db/internal/forge/status"
)

// A schema that parses but does not validate: Player requires a component that
// is not declared. editable.Open does not validate, deliberately — opening a
// broken schema so it can be fixed is what Forge is for — so this is a state
// the editor genuinely starts in.
const invalidSchema = `{
  "schemaVersion": 3,
  "components": {
    "Position": { "type": "object", "properties": { "x": { "type": "number" } } }
  },
  "entityTypes": {
    "Player": {
      "requiredComponents": ["Position", "Nope"],
      "optionalComponents": [],
      "allowExtraComponents": false,
      "validationLevel": "strict"
    }
  }
}
`

func validationServer(t *testing.T, body string, cfg func(*Config)) (*httptest.Server, *Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	sess, err := session.Open(path)
	if err != nil {
		t.Fatalf("session.Open: %v", err)
	}
	c := Config{Addr: "127.0.0.1:0", Session: sess, Engine: status.Config{}}
	if cfg != nil {
		cfg(&c)
	}
	s := New(c, testFS())
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	return srv, s, path
}

func TestModePage_ReportsAnInvalidSchemaOnFirstPaint(t *testing.T) {
	srv, _, _ := validationServer(t, invalidSchema, nil)

	_, body := get(t, srv, "/forge/ents?type=Player")
	if !strings.Contains(body, "undeclared component") {
		t.Fatalf("nothing on the page says why this schema cannot be saved:\n%s", body)
	}
	if !strings.Contains(body, `data-blocked="true"`) {
		t.Error("the footer does not say the save would be refused")
	}
	if !strings.Contains(body, "1 problem to fix first") {
		t.Error("the footer does not say how many problems there are")
	}
	// And the Save action is genuinely unwired, not merely a disabled attribute.
	if strings.Contains(body, "/forge/schema/save") {
		t.Error("the save action is still wired on a blocked footer")
	}
}

// The footer is shell-level: the Save button is on every mode, so what it knows
// cannot depend on which mode is open.
func TestModePage_BlocksTheFooterOnEveryMode(t *testing.T) {
	srv, _, _ := validationServer(t, invalidSchema, nil)
	for _, m := range mode.All {
		t.Run(m.Slug, func(t *testing.T) {
			_, body := get(t, srv, m.Path())
			if !strings.Contains(body, `data-blocked="true"`) {
				t.Errorf("Save is offered on %s against a schema the engine would refuse", m.Slug)
			}
		})
	}
}

func TestModePage_LeavesASoundSchemaAlone(t *testing.T) {
	srv, _, _ := validationServer(t, sessionSchema, nil)
	_, body := get(t, srv, "/forge/schema")
	// The positive half first: the page rendered and the editor is on it. Two
	// bare negatives would also be satisfied by a 500.
	if !strings.Contains(body, `data-testid="component-editor"`) {
		t.Fatalf("the editor did not render at all:\n%s", body)
	}
	if !strings.Contains(body, `data-blocked="false"`) {
		t.Errorf("the footer does not report its state:\n%s", body)
	}
	if strings.Contains(body, `data-blocked="true"`) {
		t.Errorf("a valid schema is reported as unsaveable:\n%s", body)
	}
	if strings.Contains(body, `class="problems"`) {
		t.Error("a valid schema renders a problem list")
	}
	if strings.Contains(body, `data-problem="true"`) {
		t.Error("a valid schema marks a list row as broken")
	}
}

// Validation travels the page stream like everything else, so a problem
// introduced by an edit appears without a reload — which is the difference
// between validating while editing and validating on save.
func TestStream_CarriesValidationAsItChanges(t *testing.T) {
	dir := t.TempDir()
	srv, s, _ := validationServer(t, sessionSchema, func(c *Config) {
		c.BehaviorDirs = []string{dir}
	})

	if got := s.modeData(schemaRequest()).Validation.Problems; len(got) != 0 {
		t.Fatalf("the fixture does not start clean: %+v", got)
	}

	// Bind a machine that is not there. A warning, not an error: nothing in the
	// engine reads this field yet, so the file still saves.
	if code := post(t, srv, "/forge/schema/behavior?component=Position&behavior=ghost"); code != 204 {
		t.Fatalf("binding: %d", code)
	}

	data := s.modeData(schemaRequest())
	m, _ := mode.Lookup("schema")
	content, err := s.renderModeContent(m, data)
	if err != nil {
		t.Fatalf("renderModeContent: %v", err)
	}
	if !strings.Contains(content, "ghost") {
		t.Errorf("the editor does not name the machine it cannot find:\n%s", content)
	}
	if !strings.Contains(content, "aria-describedby") {
		t.Errorf("the message is not associated with the control:\n%s", content)
	}
	// And it does not take the Save button away. A binding the engine never
	// reads must not stop someone saving the change they actually made.
	footer, err := s.renderFooter("schema", data)
	if err != nil {
		t.Fatalf("renderFooter: %v", err)
	}
	if strings.Contains(footer, `data-blocked="true"`) {
		t.Errorf("a warning disabled Save:\n%s", footer)
	}
	if strings.Contains(content, "aria-invalid") {
		t.Errorf("a warning marked the control invalid:\n%s", content)
	}

	// And back: undoing the edit clears the message, without a page load.
	if code := post(t, srv, "/forge/schema/behavior?component=Position&behavior="); code != 204 {
		t.Fatalf("unbinding: %d", code)
	}
	if got := s.modeData(schemaRequest()).Validation.Problems; len(got) != 0 {
		t.Errorf("the message outlived the edit that caused it: %+v", got)
	}
}

// The engine's own behaviour-reference check takes one directory; a project has
// as many as it has mods. A binding satisfied by any of them must not be
// reported as missing.
func TestValidation_AcceptsABindingFromASecondMod(t *testing.T) {
	core, extra := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(extra, "ghost.json"), []byte(`{"id":"ghost"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, s, _ := validationServer(t, sessionSchema, func(c *Config) {
		c.BehaviorDirs = []string{core, extra}
		c.Machines = []project.Machine{{ID: "ghost", Mod: "extra"}}
	})

	if code := post(t, srv, "/forge/schema/behavior?component=Position&behavior=ghost"); code != 204 {
		t.Fatalf("binding: %d", code)
	}
	// The absence of any problem, not the absence of blocking: no binding blocks
	// any more, so a not-blocked assertion here would pass against a Forge that
	// reported the machine missing.
	if got := s.modeData(schemaRequest()).Validation.Problems; len(got) != 0 {
		t.Errorf("a binding a later mod supplies was reported as a problem: %+v", got)
	}
}

// A file that is there but did not load is a warning, and the reason comes from
// the project's own problem list rather than from the log.
func TestValidation_SaysWhyAMachineDidNotLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ghost.json")
	if err := os.WriteFile(path, []byte(`{"id":"ghost"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, s, _ := validationServer(t, sessionSchema, func(c *Config) {
		c.BehaviorDirs = []string{dir}
		c.Problems = []project.Problem{{Path: path, Err: errString(`guard "canSee" is not registered`)}}
	})

	if code := post(t, srv, "/forge/ents/behavior?type=Player&behavior=ghost"); code != 204 {
		t.Fatalf("binding: %d", code)
	}

	data := s.modeData(httptest.NewRequest(http.MethodGet, "/forge/ents?type=Player", nil))
	m, _ := mode.Lookup("ents")
	content, err := s.renderModeContent(m, data)
	if err != nil {
		t.Fatalf("renderModeContent: %v", err)
	}
	if !strings.Contains(content, "canSee") {
		t.Errorf("the editor does not say why the machine was rejected:\n%s", content)
	}
	// One warning and no errors — counted, so that "does not block" is a fact
	// about this problem rather than about there being no problem.
	errs, warns := data.Validation.Counts()
	if errs != 0 || warns != 1 {
		t.Errorf("want exactly one warning and no errors, got %d and %d: %+v",
			errs, warns, data.Validation.Problems)
	}
}

// The stream, not just the render function. The footer is patched down the
// page stream from inside the SSE loop, and that loop passes it a Data it
// gathers itself — so a footer that blocks correctly when called directly can
// still arrive unblocked in the browser.
func TestModeEvents_PushesTheBlockedFooter(t *testing.T) {
	srv, _, _ := validationServer(t, invalidSchema, func(c *Config) {
		c.PollInterval = 20 * time.Millisecond
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := frames(t, ctx, srv.URL+"/forge/ents/events?type=Player")
	f := awaitFrame(t, ch, `id="save-footer"`, "footer patch")
	if !strings.Contains(f, `data-blocked="true"`) {
		t.Errorf("the footer arriving on the stream offers a save the engine would\n"+
			"refuse:\n%s", f)
	}
	if !strings.Contains(f, "1 problem to fix first") {
		t.Errorf("the patched footer does not say why:\n%s", f)
	}
}

// The one rule in this story the engine does not decide on Forge's behalf, so
// the one that can be quietly wrong — and the whole path, from the machine list
// the server was configured with to the row of the fields table it lands on.
//
// It is reachable by editing: add a second component, rename its field to one
// a machine already seeds. That is the edit the warning exists to catch, and it
// takes two clicks.
func TestValidation_WarnsWhenAnEditMakesAContextKeyAmbiguous(t *testing.T) {
	srv, s, _ := validationServer(t, sessionSchema, func(c *Config) {
		c.Machines = []project.Machine{{
			ID: "wander",
			Definition: &agent.MachineDefinition{
				ID:      "wander",
				Context: map[string]any{"x": 0},
			},
		}}
	})

	before := s.modeData(schemaRequest())
	if len(before.Validation.Problems) != 0 {
		t.Fatalf("the fixture does not start clean: %+v", before.Validation.Problems)
	}

	if code := post(t, srv, "/forge/schema/component?add=Other"); code != 204 {
		t.Fatalf("adding a component: %d", code)
	}
	if code := post(t, srv, "/forge/schema/field?component=Other&rename=value&to=x"); code != 204 {
		t.Fatalf("renaming the field: %d", code)
	}

	data := s.modeData(httptest.NewRequest(http.MethodGet, "/forge/schema?component=Other", nil))
	if data.Validation.Blocked() {
		t.Error("two components may share a field name — the schema is legal, and\n" +
			"refusing the save would be a rule the engine does not have")
	}
	m, _ := mode.Lookup("schema")
	content, err := s.renderModeContent(m, data)
	if err != nil {
		t.Fatalf("renderModeContent: %v", err)
	}
	start := strings.Index(content, `data-testid="field-x"`)
	if start < 0 {
		t.Fatalf("the renamed field has no row at all:\n%s", content)
	}
	row := content[start:]
	if end := strings.Index(row, "</tr>"); end > 0 {
		row = row[:end]
	}
	for _, want := range []string{"Other", "Position", "wander", "aria-describedby"} {
		if !strings.Contains(row, want) {
			t.Errorf("the warning on the x row does not mention %q:\n%s", want, row)
		}
	}
	// And it is a warning, not an error: the field is acceptable.
	if strings.Contains(row, "aria-invalid") {
		t.Errorf("a warning marked the field invalid:\n%s", row)
	}

	// Undoing the edit clears it.
	if code := post(t, srv, "/forge/schema/field?component=Other&rename=x&to=value"); code != 204 {
		t.Fatalf("renaming back: %d", code)
	}
	if got := s.modeData(schemaRequest()).Validation.Problems; len(got) != 0 {
		t.Errorf("the warning outlived the edit that caused it: %+v", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
