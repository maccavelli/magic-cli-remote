package acphttp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestListAgentSessionsMapsBoundedMetadata(t *testing.T) {
	p := New(Spec{ID: provider.IDGoose, DefaultBin: "test-agent"}, Config{})
	p.mu.Lock()
	p.eng = &engine{url: "http://127.0.0.1:1"}
	p.agentCaps = acp.AgentCapabilities{
		SessionCapabilities: acp.SessionCapabilities{List: &acp.SessionListCapabilities{}},
	}
	p.fr = &fakeFramer{respond: map[string]json.RawMessage{
		"session/list": json.RawMessage(`{"sessions":[{"sessionId":"one","cwd":"/work","title":"First","updatedAt":"2026-07-26T12:00:00Z"},{"sessionId":"two","cwd":"/other","updatedAt":"not-a-time"},{"cwd":"/skip"}]}`),
	}}
	p.mu.Unlock()

	got, err := p.ListAgentSessions(context.Background())
	if err != nil {
		t.Fatalf("ListAgentSessions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("sessions = %#v, want two valid ids", got)
	}
	if got[0].ID != "one" || got[0].CWD != "/work" || got[0].Title != "First" || got[0].UpdatedAt.IsZero() {
		t.Fatalf("session[0] = %#v", got[0])
	}
	if got[1].ID != "two" || !got[1].UpdatedAt.IsZero() {
		t.Fatalf("session[1] = %#v", got[1])
	}
}

func TestListAgentSessionsRequiresNegotiatedCapability(t *testing.T) {
	p := New(Spec{ID: provider.IDGoose, DefaultBin: "test-agent"}, Config{})
	p.mu.Lock()
	p.eng = &engine{url: "http://127.0.0.1:1"}
	p.mu.Unlock()
	if _, err := p.ListAgentSessions(context.Background()); err == nil {
		t.Fatal("ListAgentSessions succeeded without session/list capability")
	}
}

// TestHandleWSErrorKillsEngine pins the zombie-engine fix: a dead WebSocket
// with a live engine process used to leave every session hanging in its last
// state forever. The read failure must kill the process so the
// wait-goroutine teardown (serverDied) fires and the next Start respawns.
// helperBlockEnv arms TestHelperProcessBlocks. It is set only on the child's
// environment, never on this process's, so the helper cannot fire in the
// parent no matter how the suite is scheduled.
const helperBlockEnv = "MC_HELPER_BLOCK"

// TestHelperProcessBlocks is not a test. It is the long-lived engine process
// that TestHandleWSErrorKillsEngine needs something to kill, reached by
// re-executing the test binary — Go's standard helper-process idiom.
//
// It replaced `sleep 60`, which resolved from PATH: present under Git Bash,
// absent under PowerShell, where cmd.Start failed and the test took its
// t.Skipf. So it never failed anywhere and quietly stopped asserting that a
// ws read error kills the engine (MADR 0147 F14, D10).
//
// The 60s bound matches the sleep it replaced: long enough that the 5s
// assertion below is decisive, short enough that a failure to kill leaves a
// process that reaps itself rather than one that outlives the run.
func TestHelperProcessBlocks(t *testing.T) {
	if os.Getenv(helperBlockEnv) != "1" {
		return
	}
	time.Sleep(60 * time.Second)
}

func TestHandleWSErrorKillsEngine(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^TestHelperProcessBlocks$")
	cmd.Env = append(os.Environ(), helperBlockEnv+"=1")
	procutil.SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn helper engine: %v", err)
	}
	// The assertion is that handleWSError kills it; this only stops a failing
	// run from leaving the helper behind for its full 60s.
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	p := &Provider{
		spec:     Spec{ID: provider.IDGoose},
		log:      slog.Default(),
		eng:      &engine{cmd: cmd, dead: make(chan struct{})},
		sessions: make(map[string]*session),
	}
	p.handleWSError(errors.New("read: connection reset"))

	select {
	case <-waitCh:
	case <-time.After(5 * time.Second):
		t.Fatal("engine process still alive after ws read failure")
	}
}

// TestHandleWSErrorAfterShutdownIsNoop covers the orderly path: Shutdown
// closes the socket, the read pump reports the error, and nothing may be
// killed or logged as a crash.
func TestHandleWSErrorAfterShutdownIsNoop(t *testing.T) {
	p := &Provider{
		spec:     Spec{ID: provider.IDGoose},
		log:      slog.Default(),
		closed:   true,
		sessions: make(map[string]*session),
	}
	// Must not panic with nil engine/websocket fields.
	p.handleWSError(errors.New("use of closed network connection"))
}

// TestFramerAccessorGuardsEngineDown covers the nil-framer contract: before
// start (and after death) the accessor reports ErrEngineDown instead of
// handing back a nil framer for sessions to dereference.
func TestFramerAccessorGuardsEngineDown(t *testing.T) {
	p := &Provider{
		spec:     Spec{ID: provider.IDGoose},
		log:      slog.Default(),
		sessions: make(map[string]*session),
	}
	if _, err := p.framer(); !errors.Is(err, ErrEngineDown) {
		t.Fatalf("want ErrEngineDown, got %v", err)
	}
	p.mu.Lock()
	p.fr = &fakeFramer{}
	p.mu.Unlock()
	if _, err := p.framer(); err != nil {
		t.Fatalf("want live framer, got %v", err)
	}
}
