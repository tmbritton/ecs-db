package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// validMachineJSON uses only actions/guards registered in testRegistry()
// and targets that resolve within the machine.
const validMachineJSON = `{
	"id":"test_machine","initial":"a",
	"states":{
		"a":{"entry":["setTimer"],"on":{"TICK":"b"}},
		"b":{}
	}
}`

// validMachineV2JSON is a valid second version of test_machine with different initial state.
const validMachineV2JSON = `{
	"id":"test_machine","initial":"b",
	"states":{
		"a":{},"b":{"entry":["moveTowardTarget"]}
	}
}`

// invalidMachineJSON has an unregistered action — fails ValidateMachine.
const invalidMachineJSON = `{
	"id":"test_machine","initial":"a",
	"states":{"a":{"entry":["unknownAction"]}}
}`

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
	return path
}

func TestNewLoader(t *testing.T) {
	l := NewLoader(testRegistry(), testSchema())
	if l == nil {
		t.Fatal("NewLoader returned nil")
	}
}

func TestLoader_LoadMachine_Success(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "m.json", validMachineJSON)

	l := NewLoader(testRegistry(), testSchema())
	def, err := l.LoadMachine(path)
	if err != nil {
		t.Fatalf("LoadMachine: unexpected error: %v", err)
	}
	if def == nil {
		t.Fatal("LoadMachine: returned nil def on success")
	}
	if def.ID != "test_machine" {
		t.Errorf("def.ID = %q, want %q", def.ID, "test_machine")
	}
}

func TestLoader_LoadMachine_StoresDefinition(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "m.json", validMachineJSON)

	l := NewLoader(testRegistry(), testSchema())
	_, _ = l.LoadMachine(path)

	got, ok := l.Get("test_machine")
	if !ok {
		t.Fatal("Get: expected ok=true after successful load")
	}
	if got.ID != "test_machine" {
		t.Errorf("Get: ID = %q, want %q", got.ID, "test_machine")
	}
}

func TestLoader_LoadMachine_FileNotFound(t *testing.T) {
	l := NewLoader(testRegistry(), testSchema())
	_, err := l.LoadMachine("/nonexistent/path/m.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoader_LoadMachine_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "m.json", "not json at all")

	l := NewLoader(testRegistry(), testSchema())
	_, err := l.LoadMachine(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestLoader_LoadMachine_ValidationError_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "m.json", invalidMachineJSON)

	l := NewLoader(testRegistry(), testSchema())
	def, err := l.LoadMachine(path)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if def != nil {
		t.Errorf("expected nil def on validation failure, got %+v", def)
	}
}

func TestLoader_LoadMachine_HotReload_Success(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "m.json", validMachineJSON)

	l := NewLoader(testRegistry(), testSchema())
	_, _ = l.LoadMachine(path)

	if err := os.WriteFile(path, []byte(validMachineV2JSON), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	def, err := l.LoadMachine(path)
	if err != nil {
		t.Fatalf("second LoadMachine: %v", err)
	}
	if def.Initial != "b" {
		t.Errorf("def.Initial = %q, want %q", def.Initial, "b")
	}
}

func TestLoader_LoadMachine_HotReload_FailureRetainsPrevious(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "m.json", validMachineJSON)

	l := NewLoader(testRegistry(), testSchema())
	first, _ := l.LoadMachine(path)

	if err := os.WriteFile(path, []byte(invalidMachineJSON), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := l.LoadMachine(path)
	if err == nil {
		t.Fatal("expected error for invalid reload, got nil")
	}

	retained, ok := l.Get("test_machine")
	if !ok {
		t.Fatal("Get: expected previous def to be retained after failed reload")
	}
	if retained.Initial != first.Initial {
		t.Errorf("retained.Initial = %q, want %q", retained.Initial, first.Initial)
	}
}

func TestLoader_Get_Miss(t *testing.T) {
	l := NewLoader(testRegistry(), testSchema())
	_, ok := l.Get("notLoaded")
	if ok {
		t.Error("Get: expected ok=false for unknown machine ID")
	}
}
