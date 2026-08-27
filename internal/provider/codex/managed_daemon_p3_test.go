package codex

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

const managedLifecycleFixture = `{"status":"started","backend":"launchd","pid":1234,"managedCodexPath":"/opt/codex/codex","managedCodexVersion":"0.149.1","socketPath":"/tmp/codex/app-server.sock","cliVersion":"0.149.1","appServerVersion":"0.149.1"}`

func TestProxyManagedDaemonLifecycleStatuses(t *testing.T) {
	for _, status := range []daemonLifecycleStatus{
		daemonAlreadyRunning, daemonStarted, daemonRestarted,
		daemonStopped, daemonNotRunning, daemonRunning,
	} {
		raw := strings.Replace(managedLifecycleFixture, `"started"`, `"`+string(status)+`"`, 1)
		out, err := parseDaemonLifecycle([]byte(raw))
		if err != nil {
			t.Fatalf("status %s: %v", status, err)
		}
		if out.Status != status {
			t.Fatalf("status = %s, want %s", out.Status, status)
		}
	}
}

func TestProxyManagedDaemonRejectsMalformedOrMultipleJSON(t *testing.T) {
	for _, raw := range []string{
		`not json`,
		managedLifecycleFixture + ` {"status":"started"}`,
		`[]`,
		`{"status":"unknown"}`,
	} {
		if _, err := parseDaemonLifecycle([]byte(raw)); err == nil {
			t.Fatalf("accepted lifecycle output %q", raw)
		}
	}
}

func TestProxyManagedDaemonLeaseRequiresStartedAndMatchingIdentity(t *testing.T) {
	testexec.SkipIfNoPOSIXPaths(t)
	out, err := parseDaemonLifecycle([]byte(managedLifecycleFixture))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := validateManagedLease(out, BinaryIdentity{Version: "0.149.1"})
	if err != nil {
		t.Fatalf("validate lease: %v", err)
	}
	if lease.SocketPath != out.SocketPath || lease.PID != out.PID {
		t.Fatalf("lease = %+v", lease)
	}

	for name, mutate := range map[string]func(*daemonLifecycle){
		"foreign":  func(v *daemonLifecycle) { v.Status = daemonAlreadyRunning },
		"version":  func(v *daemonLifecycle) { v.AppServerVersion = "0.148.0" },
		"pid":      func(v *daemonLifecycle) { v.PID = 0 },
		"backend":  func(v *daemonLifecycle) { v.Backend = "" },
		"relative": func(v *daemonLifecycle) { v.SocketPath = "relative.sock" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := out
			mutate(&candidate)
			if _, err := validateManagedLease(candidate, BinaryIdentity{Version: "0.149.1"}); err == nil {
				t.Fatal("invalid/foreign daemon produced an ownership lease")
			}
		})
	}
}

func TestProxyManagedDaemonUnixOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := (Config{Transport: TransportManagedDaemonProxy}).validated(); err == nil {
			t.Fatal("managed daemon proxy accepted on Windows")
		}
		return
	}
	if _, err := (Config{Transport: TransportManagedDaemonProxy}).validated(); err != nil {
		t.Fatalf("managed daemon proxy rejected on Unix: %v", err)
	}
}

func TestProxyManagedDaemonStopsOnlyVerifiedOwnedLease(t *testing.T) {
	testexec.SkipIfNoPOSIXPaths(t)
	out, err := parseDaemonLifecycle([]byte(managedLifecycleFixture))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := validateManagedLease(out, BinaryIdentity{Version: "0.149.1"})
	if err != nil {
		t.Fatal(err)
	}
	var operations []string
	p := New(Config{})
	p.daemonRun = func(_ context.Context, _, operation string) (daemonLifecycle, error) {
		operations = append(operations, operation)
		switch operation {
		case "version":
			current := out
			current.Status = daemonRunning
			return current, nil
		case "stop":
			stopped := out
			stopped.Status = daemonStopped
			return stopped, nil
		default:
			t.Fatalf("unexpected operation %q", operation)
			return daemonLifecycle{}, nil
		}
	}
	p.stopManagedLease(context.Background(), lease)
	if strings.Join(operations, ",") != "version,stop" {
		t.Fatalf("operations = %v", operations)
	}

	operations = nil
	p.daemonRun = func(_ context.Context, _, operation string) (daemonLifecycle, error) {
		operations = append(operations, operation)
		foreign := out
		foreign.Status = daemonRunning
		foreign.PID++
		return foreign, nil
	}
	p.stopManagedLease(context.Background(), lease)
	if strings.Join(operations, ",") != "version" {
		t.Fatalf("foreign daemon operations = %v, want passive version only", operations)
	}
}

func TestProxyManagedDaemonLossFallsBackWithoutForeignStop(t *testing.T) {
	p := NewWithLogger(Config{
		Transport:         TransportManagedDaemonProxy,
		ReconnectAttempts: 0, ReconnectAttemptsConfigured: true,
	}, testLogger(t))
	lease := &managedDaemonLease{
		Backend: "launchd", PID: 12, ManagedCodexPath: "/managed/codex",
		ManagedCodexVersion: "0.149.1", SocketPath: "/tmp/codex.sock",
		CLIVersion: "0.149.1", AppServerVersion: "0.149.1",
	}
	var operations []string
	p.daemonRun = func(_ context.Context, _, operation string) (daemonLifecycle, error) {
		operations = append(operations, operation)
		return daemonLifecycle{}, errors.New("lease no longer provable")
	}
	s := newSession(p, p.cfg, provider.StartOptions{}, testLogger(t))
	s.agentID = "thread-owned"
	s.pendingPerms["stale"] = pendingCallback{}
	p.mu.Lock()
	p.generation = 1
	p.eng = &engine{generation: 1, managedLease: lease}
	p.sessions[s.agentID] = s
	p.mu.Unlock()
	p.handleUnexpectedEngineExit(1, &engineAttempt{managedLease: lease}, errors.New("proxy lost"))

	p.mu.Lock()
	mode := p.cfg.Transport
	retained := p.sessions[s.agentID] == s
	eng := p.eng
	p.mu.Unlock()
	if mode != TransportStdio || !retained || eng != nil {
		t.Fatalf("fallback mode/retained/engine = %s/%v/%v", mode, retained, eng)
	}
	if strings.Join(operations, ",") != "version" {
		t.Fatalf("lost lease operations = %v, want passive version only", operations)
	}
	s.mu.Lock()
	pending := len(s.pendingPerms)
	closed := s.closed
	s.mu.Unlock()
	if pending != 0 || closed {
		t.Fatalf("retained session pending/closed = %d/%v", pending, closed)
	}
	_ = s.Close(context.Background())
}
