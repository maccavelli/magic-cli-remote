package session

import (
	"context"
	"errors"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// ErrInvalidShellCommand rejects blank, oversized, or control-character shell
// text before it reaches the host. It is a caller mistake, not a host failure.
var ErrInvalidShellCommand = errors.New("unsandboxed shell command is blank, oversized, or contains control characters")

// Execution delegation (MADR 0109 D10/D11/D37, plan phase P7).
//
// Every method here resolves the live provider session, checks paired-device
// ownership, and delegates. The manager deliberately owns no execution policy
// of its own: authority labels, sandbox class, capability gating, argv/root
// validation, output bounds, and handle ownership all live in the provider
// adapter, so a second provider implementing ExecutionSession inherits them
// rather than re-deriving them here. What the manager does own is the part
// only it can know: which paired device is allowed to touch this session.
//
// A provider that does not implement ExecutionSession returns
// provider.ErrNativeUnavailable rather than a generic failure, so the phone
// can distinguish "this agent has no terminals" from "the terminal call
// broke".

// executionSession resolves the authorized live session's execution surface.
func (m *Manager) executionSession(id, deviceID string) (provider.ExecutionSession, error) {
	if err := m.Authorize(id, deviceID, true); err != nil {
		return nil, err
	}
	sess, err := m.liveSession(id)
	if err != nil {
		return nil, err
	}
	execution, ok := sess.(provider.ExecutionSession)
	if !ok {
		return nil, provider.ErrNativeUnavailable
	}
	return execution, nil
}

// RunSandboxedExec runs one argv-only command under the session's permission
// profile. It never accepts shell text; see RunUnsandboxedShell for that.
func (m *Manager) RunSandboxedExec(ctx context.Context, id string, request provider.ExecRequest, deviceID string) (provider.ExecResult, error) {
	execution, err := m.executionSession(id, deviceID)
	if err != nil {
		return provider.ExecResult{}, err
	}
	return execution.RunSandboxedExec(ctx, request)
}

// RunUnsandboxedShell runs explicit shell text with full host access. The
// caller must have taken a fresh confirmation for this exact command.
func (m *Manager) RunUnsandboxedShell(ctx context.Context, id, command, deviceID string) (provider.ExecutionResult, error) {
	execution, err := m.executionSession(id, deviceID)
	if err != nil {
		return provider.ExecutionResult{}, err
	}
	if !shellCommandValid(command) {
		return provider.ExecutionResult{}, ErrInvalidShellCommand
	}
	return execution.RunUnsandboxedShell(ctx, command)
}

// SpawnStandaloneProcess starts a default-off unsandboxed process. The caller
// must have taken a fresh confirmation for this exact spawn.
func (m *Manager) SpawnStandaloneProcess(ctx context.Context, id string, request provider.ProcessSpawnRequest, deviceID string) (provider.ProcessInfo, error) {
	execution, err := m.executionSession(id, deviceID)
	if err != nil {
		return provider.ProcessInfo{}, err
	}
	return execution.SpawnStandaloneProcess(ctx, request)
}

// WriteExecutionTerminal sends bounded stdin to one owned terminal.
func (m *Manager) WriteExecutionTerminal(ctx context.Context, id, terminalID string, data []byte, closeStdin bool, deviceID string) error {
	execution, err := m.executionSession(id, deviceID)
	if err != nil {
		return err
	}
	return execution.WriteTerminal(ctx, terminalID, data, closeStdin)
}

// ResizeExecutionTerminal resizes one owned PTY.
func (m *Manager) ResizeExecutionTerminal(ctx context.Context, id, terminalID string, rows, cols int, deviceID string) error {
	execution, err := m.executionSession(id, deviceID)
	if err != nil {
		return err
	}
	return execution.ResizeTerminal(ctx, terminalID, rows, cols)
}

// ReplayExecutionTerminal returns retained chunks after the client's last
// sequence. The bool reports that the bounded buffer already dropped the
// requested position, so the client must treat its view as discontinuous.
func (m *Manager) ReplayExecutionTerminal(ctx context.Context, id, terminalID string, after uint64, deviceID string) ([]provider.TerminalOutput, bool, error) {
	execution, err := m.executionSession(id, deviceID)
	if err != nil {
		return nil, false, err
	}
	return execution.ReplayTerminal(ctx, terminalID, after)
}

// ListExecutionTerminals returns daemon-owned and negotiated native terminals
// for this session's native thread.
func (m *Manager) ListExecutionTerminals(ctx context.Context, id, deviceID string) ([]provider.TerminalInfo, error) {
	execution, err := m.executionSession(id, deviceID)
	if err != nil {
		return nil, err
	}
	return execution.ListTerminals(ctx)
}

// StopExecutionTerminal terminates exactly one terminal id.
func (m *Manager) StopExecutionTerminal(ctx context.Context, id, terminalID, deviceID string) error {
	execution, err := m.executionSession(id, deviceID)
	if err != nil {
		return err
	}
	return execution.StopTerminal(ctx, terminalID)
}

// StopAllExecutionTerminals terminates every known terminal for this session
// and returns how many were still running when it ran.
func (m *Manager) StopAllExecutionTerminals(ctx context.Context, id, deviceID string) (int, error) {
	execution, err := m.executionSession(id, deviceID)
	if err != nil {
		return 0, err
	}
	return execution.StopAllTerminals(ctx)
}
