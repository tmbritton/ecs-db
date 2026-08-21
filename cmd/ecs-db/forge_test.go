package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tmbritton/ecs-db/internal/config"
)

// runForge blocks until interrupted, so the test cancels the context instead of
// sending a signal. This also covers the nil-context guard: cobra leaves
// cmd.Context() nil when RunE is invoked outside Execute, and
// signal.NotifyContext(nil, ...) panics.
func TestRunForge_ServesThenShutsDownCleanly(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep any stray writes out of the real HOME

	// Port 0: let the kernel pick, so concurrent runs don't collide.
	cfgPath := writeProject(t, validSchema)
	appendToFile(t, cfgPath, "\n[forge]\naddr = \"127.0.0.1:0\"\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncBuffer{}
	forgeCmd.SetOut(out)
	forgeCmd.SetContext(ctx)
	t.Cleanup(func() { forgeCmd.SetOut(nil) })

	// PersistentPreRunE normally loads config; call it directly since we are
	// not going through Execute.
	if err := rootCmd.PersistentPreRunE(forgeCmd, nil); err != nil {
		t.Fatalf("loading config: %v", err)
	}
	cfgPathFlagReset(t, cfgPath)

	done := make(chan error, 1)
	go func() { done <- runForge(forgeCmd, nil) }()

	addr := waitForAddr(t, out)
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz on %s: %v", addr, err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runForge returned %v after cancel, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runForge did not return after context cancel")
	}
}

// A bind failure must be reported, and must not be preceded by a cheerful
// "listening on ..." line.
func TestRunForge_BindFailureIsReported(t *testing.T) {
	// Reserve a port, then ask Forge for the same one.
	busy := mustListen(t)
	defer busy.Close()

	cfgPath := writeProject(t, validSchema)
	appendToFile(t, cfgPath, "\n[forge]\naddr = \""+busy.Addr().String()+"\"\n")

	out := &syncBuffer{}
	forgeCmd.SetOut(out)
	forgeCmd.SetContext(context.Background())
	t.Cleanup(func() { forgeCmd.SetOut(nil) })

	if err := rootCmd.PersistentPreRunE(forgeCmd, nil); err != nil {
		t.Fatalf("loading config: %v", err)
	}
	cfgPathFlagReset(t, cfgPath)

	err := runForge(forgeCmd, nil)
	if err == nil {
		t.Fatal("want a bind error, got nil")
	}
	if strings.Contains(out.String(), "listening") {
		t.Errorf("announced listening despite bind failure: %q", out.String())
	}
}

// --- helpers ---

// syncBuffer is written by runForge's goroutine and read by the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitForAddr parses the announced address out of "⚒ Forge listening on http://host:port".
func waitForAddr(t *testing.T, out *syncBuffer) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, addr, ok := strings.Cut(out.String(), "http://"); ok {
			return strings.TrimSpace(addr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Forge never announced an address; output was %q", out.String())
	return ""
}

func appendToFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatalf("appending to %s: %v", path, err)
	}
}

// cfgPathFlagReset re-runs config.Init for path, since the package-level cfgPath
// flag is not set when RunE is called outside Execute.
func cfgPathFlagReset(t *testing.T, path string) {
	t.Helper()
	if err := config.Init(path); err != nil {
		t.Fatalf("config.Init(%s): %v", path, err)
	}
}

func mustListen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	return ln
}
