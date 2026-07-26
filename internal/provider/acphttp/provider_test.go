package acphttp

import (
	"errors"
	"log/slog"
	"os/exec"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestHandleWSErrorKillsEngine pins the zombie-engine fix: a dead WebSocket
// with a live engine process used to leave every session hanging in its last
// state forever. The read failure must kill the process so the
// wait-goroutine teardown (serverDied) fires and the next Start respawns.
func TestHandleWSErrorKillsEngine(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	procutil.SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn sleep: %v", err)
	}
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
