package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"io"
	"path/filepath"
	"strings"
)

type daemonLifecycleStatus string

const (
	daemonAlreadyRunning daemonLifecycleStatus = "alreadyRunning"
	daemonStarted        daemonLifecycleStatus = "started"
	daemonRestarted      daemonLifecycleStatus = "restarted"
	daemonStopped        daemonLifecycleStatus = "stopped"
	daemonNotRunning     daemonLifecycleStatus = "notRunning"
	daemonRunning        daemonLifecycleStatus = "running"
)

type daemonLifecycle struct {
	Status              daemonLifecycleStatus `json:"status"`
	Backend             string                `json:"backend"`
	PID                 int                   `json:"pid"`
	ManagedCodexPath    string                `json:"managedCodexPath"`
	ManagedCodexVersion string                `json:"managedCodexVersion"`
	SocketPath          string                `json:"socketPath"`
	CLIVersion          string                `json:"cliVersion"`
	AppServerVersion    string                `json:"appServerVersion"`
}

type managedDaemonLease struct {
	Backend             string
	PID                 int
	ManagedCodexPath    string
	ManagedCodexVersion string
	SocketPath          string
	CLIVersion          string
	AppServerVersion    string
}

func parseDaemonLifecycle(raw []byte) (daemonLifecycle, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var out daemonLifecycle
	if err := dec.Decode(&out); err != nil {
		return daemonLifecycle{}, fmt.Errorf("decode Codex daemon lifecycle output: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return daemonLifecycle{}, fmt.Errorf("codex daemon lifecycle emitted multiple JSON values")
		}
		return daemonLifecycle{}, fmt.Errorf("decode trailing Codex daemon lifecycle output: %w", err)
	}
	switch out.Status {
	case daemonAlreadyRunning, daemonStarted, daemonRestarted, daemonStopped, daemonNotRunning, daemonRunning:
	default:
		return daemonLifecycle{}, fmt.Errorf("unknown Codex daemon lifecycle status %q", out.Status)
	}
	return out, nil
}

func validateManagedLease(out daemonLifecycle, identity BinaryIdentity) (*managedDaemonLease, error) {
	if out.Status != daemonStarted {
		return nil, fmt.Errorf("codex daemon is not owned: start returned %q", out.Status)
	}
	if strings.TrimSpace(out.Backend) == "" || out.PID <= 0 {
		return nil, fmt.Errorf("codex daemon start omitted backend/PID ownership evidence")
	}
	if !filepath.IsAbs(out.ManagedCodexPath) || !filepath.IsAbs(out.SocketPath) {
		return nil, fmt.Errorf("codex daemon returned non-absolute managed/socket path")
	}
	version := strings.TrimSpace(identity.Version)
	if version == "" || version == "unknown" {
		return nil, fmt.Errorf("codex daemon ownership requires a known CLI version")
	}
	if out.ManagedCodexVersion != version || out.CLIVersion != version || out.AppServerVersion != version {
		return nil, fmt.Errorf("codex daemon version mismatch: managed=%q cli=%q app-server=%q expected=%q",
			out.ManagedCodexVersion, out.CLIVersion, out.AppServerVersion, version)
	}
	return &managedDaemonLease{
		Backend: out.Backend, PID: out.PID,
		ManagedCodexPath: out.ManagedCodexPath, ManagedCodexVersion: out.ManagedCodexVersion,
		SocketPath: out.SocketPath, CLIVersion: out.CLIVersion, AppServerVersion: out.AppServerVersion,
	}, nil
}

type daemonLifecycleRunner func(context.Context, string, string) (daemonLifecycle, error)

func runDaemonLifecycle(ctx context.Context, bin, operation string) (daemonLifecycle, error) {
	cmd := procutil.Command(ctx, bin, "app-server", "daemon", operation)
	raw, err := cmd.Output()
	if err != nil {
		return daemonLifecycle{}, fmt.Errorf("codex app-server daemon %s: %w", operation, err)
	}
	out, err := parseDaemonLifecycle(raw)
	if err != nil {
		return daemonLifecycle{}, fmt.Errorf("codex app-server daemon %s: %w", operation, err)
	}
	return out, nil
}

func lifecycleMatchesLease(out daemonLifecycle, lease *managedDaemonLease) bool {
	return lease != nil && out.Status == daemonRunning &&
		out.Backend == lease.Backend && out.PID == lease.PID &&
		out.ManagedCodexPath == lease.ManagedCodexPath &&
		out.ManagedCodexVersion == lease.ManagedCodexVersion &&
		out.SocketPath == lease.SocketPath && out.CLIVersion == lease.CLIVersion &&
		out.AppServerVersion == lease.AppServerVersion
}
