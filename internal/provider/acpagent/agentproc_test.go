package acpagent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/acp-go-sdk2/agentproc"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/launch"
	"github.com/maccavelli/magic-cli-remote/internal/wirecap"
)

// testAgentEnv switches the test binary into a helper agent, so spawnAgent can
// start a real process through agentproc (MADR 0207 D10). "serve" answers
// initialize over stdio, and goes on running for a minute after its input ends,
// as a wedged agent would, so only a kill ends it promptly. "exit" exits at
// once.
const testAgentEnv = "MCREMOTE_TEST_ACP_AGENT"

// testAgentPIDFileEnv, when set, names a file the "serve" agent writes its PID
// to before it serves, so a test can find the process it has no handle on.
const testAgentPIDFileEnv = "MCREMOTE_TEST_ACP_AGENT_PIDFILE"

func TestMain(m *testing.M) {
	switch os.Getenv(testAgentEnv) {
	case "serve":
		serveTestAgent()
		os.Exit(0)
	case "exit":
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// testAgent answers initialize; nothing else is called on it. The embedded
// interface is nil, so any other method panics, which a test would see.
type testAgent struct{ acp.Agent }

func (testAgent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersionNumber}, nil
}

func serveTestAgent() {
	if path := os.Getenv(testAgentPIDFileEnv); path != "" {
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			os.Exit(2)
		}
	}
	conn := acp.NewAgentSideConnection(testAgent{}, os.Stdout, os.Stdin)
	<-conn.Done()
	// Ignore the input's end: a test that sees this process gone within its
	// budget has seen Magic kill it. Bounded, so a regression leaves no
	// process behind for long.
	time.Sleep(time.Minute)
}

// testAgentProvider is a Provider whose agent is this test binary in "serve"
// mode, with its PID written to the returned file.
func testAgentProvider(t *testing.T) (*Provider, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(t.TempDir(), "agent.pid")
	t.Setenv(testAgentEnv, "serve")
	t.Setenv(testAgentPIDFileEnv, pidFile)
	p := &Provider{log: slog.New(slog.DiscardHandler), cfg: Config{Bin: exe}}
	p.spec.ID = "testagent"
	return p, pidFile
}

// testAgentPID reads the PID the helper agent wrote at start.
func testAgentPID(t *testing.T, pidFile string) int {
	t.Helper()
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the helper agent wrote no PID: %v", err)
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// waitProcDone waits for the agent process to have exited and been reaped.
func waitProcDone(t *testing.T, s *session) {
	t.Helper()
	select {
	case <-s.proc.Done():
	case <-time.After(wireBudget):
		t.Fatal("the agent process was still running")
	}
}

// exitedAgentProcess is an agentproc process that has exited and been reaped:
// the test binary in "exit" mode.
func exitedAgentProcess(t *testing.T) *agentproc.Process {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(testAgentEnv, "exit")
	resolved, err := launch.Resolve(exe)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := launch.Command(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	proc, err := (&agentproc.Command{Cmd: cmd}).Start(&session{})
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("the exit agent ended with %v", err)
	}
	return proc
}

// TestSpawnAgentStartsThroughAgentproc is MADR 0207 D10: spawnAgent starts the
// agent through agentproc, completes initialize, records the agent's raw
// output through the wire capture's tap, and Close ends the process.
func TestSpawnAgentStartsThroughAgentproc(t *testing.T) {
	capDir := t.TempDir()
	t.Setenv(wirecap.EnvDir, capDir)
	p, _ := testAgentProvider(t)

	s, err := p.spawnAgent(context.Background(), nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.proc == nil {
		t.Fatal("spawnAgent left no agentproc process on the session")
	}
	closed := time.Now()
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitProcDone(t, s)
	// agentproc also stops an agent whose connection has ended, but only after
	// its grace, with SIGTERM. Gone sooner means Close's Kill ended it, as MADR
	// 0207 Q6 chose. The helper ignores its input's end, so nothing else does.
	if took := time.Since(closed); took >= agentGrace {
		t.Fatalf("the agent ended %v after Close, not before agentproc's grace of %v: Close did not kill it",
			took, agentGrace)
	}
	_ = s.proc.Wait() // the tap has written what it holds once Wait returns

	frames, err := os.ReadFile(filepath.Join(capDir, "testagent", "frames.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(string(frames), "\n")
	if !strings.Contains(first, `"result"`) || !strings.Contains(first, `"protocolVersion"`) {
		t.Fatalf("the capture's first frame is not the initialize answer: %q", first)
	}
}

// TestSpawnedAgentDeathIsOneDisconnect: an agent that dies by itself, killed
// from outside Magic, ends the session with one error and one disconnected
// status, as before agentproc.
func TestSpawnedAgentDeathIsOneDisconnect(t *testing.T) {
	p, pidFile := testAgentProvider(t)
	s, err := p.spawnAgent(context.Background(), nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	agent, err := os.FindProcess(testAgentPID(t, pidFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Kill(); err != nil {
		t.Fatal(err)
	}
	waitProcDone(t, s)

	var errs, disconnects int
	deadline := time.After(wireBudget)
	for disconnects == 0 {
		select {
		case ev := <-s.events:
			switch {
			case ev.Type == event.TypeError:
				errs++
			case ev.Type == event.TypeSessionStatus && ev.Status == "disconnected":
				disconnects++
			}
		case <-deadline:
			t.Fatal("no disconnected status after the agent died")
		}
	}
	// Both watchers fire; the latch lets one pair through.
	time.Sleep(200 * time.Millisecond)
	for len(s.events) > 0 {
		ev := <-s.events
		if ev.Type == event.TypeError {
			errs++
		}
		if ev.Type == event.TypeSessionStatus && ev.Status == "disconnected" {
			disconnects++
		}
	}
	if errs != 1 || disconnects != 1 {
		t.Fatalf("one agent death gave %d errors and %d disconnects; want 1 and 1", errs, disconnects)
	}
}
