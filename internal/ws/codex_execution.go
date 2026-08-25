package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// Codex execution phone operations (MADR 0109 D10/D11/D36/D37, plan P7).
//
// Two operations carry the whole surface: codex.execution.read for bounded
// terminal and environment reads, codex.execution.write for the three
// execution authorities plus terminal control and environment selection.
// Consolidating them keeps one decode/authorize/bound path per direction
// instead of a dozen near-identical registry rows, which is what P6 settled
// on for threads.
//
// The three execution authorities are deliberately separate actions with
// separate confirmations. "exec" is argv-only under a permission profile.
// "shell" is full host access. "spawn" is the default-off standalone process
// surface. No action can escalate into another: exec never accepts shell
// text, and both unsandboxed actions require their own exact confirmation
// phrase on every single invocation — never a session-wide grant.

const (
	// confirmUnsandboxed is required, fresh, for every unsandboxed shell run
	// and every standalone spawn.
	confirmUnsandboxed = "run unsandboxed"
	// confirmEnvironment is required to change which host executes work.
	confirmEnvironment = "change execution environment"
	// maxTerminalStdinBytes bounds one decoded stdin write.
	maxTerminalStdinBytes = 256 << 10
	// maxExecArgv bounds a submitted argv vector.
	maxExecArgv = 256
)

// sessionScopedExecutionRead lists the read actions that need session
// ownership. Environment reads are host-administration projections and are
// available to any authenticated device that negotiated the Codex surface.
func sessionScopedExecutionRead(action string) bool {
	return action == "terminals" || action == "output"
}

func decodeCodexExecutionRead(env protocol.Envelope) (string, error) {
	var payload protocol.CodexExecutionReadPayload
	if err := protocol.DecodePayload(env, &payload); err != nil {
		return "", err
	}
	switch payload.Action {
	case "terminals":
	case "output":
		if payload.TerminalID == "" || len(payload.TerminalID) > 256 {
			return "", fmt.Errorf("bounded terminal_id required")
		}
	case "environments":
	case "environment_status", "environment_info":
		if payload.EnvironmentID == "" || len(payload.EnvironmentID) > 256 {
			return "", fmt.Errorf("bounded environment_id required")
		}
	default:
		return "", fmt.Errorf("unknown Codex execution read action")
	}
	if sessionScopedExecutionRead(payload.Action) && payload.SessionID == "" {
		return "", fmt.Errorf("session_id required")
	}
	if !sessionScopedExecutionRead(payload.Action) {
		return "", nil
	}
	return payload.SessionID, nil
}

func decodeCodexExecutionWrite(env protocol.Envelope) (string, error) {
	var payload protocol.CodexExecutionWritePayload
	if err := protocol.DecodePayload(env, &payload); err != nil {
		return "", err
	}
	switch payload.Action {
	case "exec", "spawn":
		if len(payload.Argv) == 0 || len(payload.Argv) > maxExecArgv {
			return "", fmt.Errorf("bounded nonempty argv required")
		}
	case "shell":
		if payload.Command == "" || len(payload.Command) > 64<<10 {
			return "", fmt.Errorf("bounded nonempty command required")
		}
	case "write", "resize", "stop":
		if payload.TerminalID == "" || len(payload.TerminalID) > 256 {
			return "", fmt.Errorf("bounded terminal_id required")
		}
	case "stop_all":
	case "select_environment":
		if !payload.DisableEnvironment && (payload.EnvironmentID == "" || len(payload.EnvironmentID) > 256) {
			return "", fmt.Errorf("bounded environment_id or an explicit disable is required")
		}
	default:
		return "", fmt.Errorf("unknown Codex execution write action")
	}
	if payload.SessionID == "" {
		return "", fmt.Errorf("session_id required")
	}
	if len(payload.DataBase64) > 4*maxTerminalStdinBytes || len(payload.Env) > 64 ||
		len(payload.RuntimeWorkspaceRoots) > 100 || len(payload.Confirm) > 64 ||
		payload.Rows < 0 || payload.Cols < 0 || payload.TimeoutMS < 0 || payload.OutputBytesCap < 0 {
		return "", fmt.Errorf("invalid Codex execution write bounds")
	}
	return payload.SessionID, nil
}

// authorizeCodexExecution requires session ownership for session-scoped work
// and accepts any authenticated Codex-surface device for the host-admin
// environment projections, which carry no endpoint or credential.
func authorizeCodexExecution(s *Server, sessionID, deviceID string) error {
	if sessionID == "" {
		return nil
	}
	return s.sessions.Authorize(sessionID, deviceID, false)
}

// executionErrorCode maps a provider execution failure onto the registered
// protocol vocabulary. The ambiguity case matters most: a non-idempotent
// execution that may already have reached the host must never be reported as
// a plain failure, because a phone that retries it would run it twice.
func executionErrorCode(err error, write bool) string {
	switch {
	case errors.Is(err, provider.ErrExecutionOutcomeUnknown):
		return protocol.ErrOutcomeUnknown
	case errors.Is(err, provider.ErrNativeUnavailable):
		return protocol.ErrNativeUnavailable
	case errors.Is(err, provider.ErrTerminalNotFound):
		return "not_found"
	case errors.Is(err, session.ErrInvalidShellCommand):
		return "bad_payload"
	case write:
		return protocol.ErrCodexExecutionWriteFailed
	default:
		return protocol.ErrCodexExecutionReadFailed
	}
}

func (s *Server) handleCodexExecutionRead(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var payload protocol.CodexExecutionReadPayload
	if err := protocol.DecodePayload(env, &payload); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	result := protocol.CodexExecutionReadResultPayload{}
	switch payload.Action {
	case "terminals":
		terminals, err := s.sessions.ListExecutionTerminals(ctx, payload.SessionID, deviceID)
		if err != nil {
			return s.writeError(ctx, c, env.ID, executionErrorCode(err, false), "Codex terminals could not be listed")
		}
		result.Terminals = terminals
	case "output":
		output, gap, err := s.sessions.ReplayExecutionTerminal(ctx, payload.SessionID, payload.TerminalID, payload.AfterSequence, deviceID)
		if err != nil {
			return s.writeError(ctx, c, env.ID, executionErrorCode(err, false), "Codex terminal output could not be replayed")
		}
		result.Output, result.SequenceGap = output, gap
	case "environments", "environment_status", "environment_info":
		p, err := s.codexProvider()
		if err != nil {
			return s.writeError(ctx, c, env.ID, "unavailable", err.Error())
		}
		if err := s.readCodexEnvironment(ctx, p, payload, &result); err != nil {
			return s.writeError(ctx, c, env.ID, executionErrorCode(err, false), "Codex execution environment could not be read")
		}
	}
	out, _ := protocol.NewEnvelope(protocol.TypeCodexExecutionReadResult, env.ID, result)
	return s.writeJSON(ctx, c, out)
}

// readCodexEnvironment fills exactly one environment arm. The catalog is
// host-owned: the projection carries ids and allowed roots, never the
// exec-server URL, connect timeout, or any remote credential.
func (s *Server) readCodexEnvironment(ctx context.Context, p codexEnvironmentReader, payload protocol.CodexExecutionReadPayload, result *protocol.CodexExecutionReadResultPayload) error {
	switch payload.Action {
	case "environments":
		environments, err := p.ListExecutionEnvironments(ctx)
		if err != nil {
			return err
		}
		result.Environments = environments
	case "environment_status":
		status, err := p.ReadExecutionEnvironmentStatus(ctx, payload.EnvironmentID)
		if err != nil {
			return err
		}
		result.EnvironmentStatus = &status
	case "environment_info":
		info, err := p.ReadExecutionEnvironmentInfo(ctx, payload.EnvironmentID)
		if err != nil {
			return err
		}
		result.EnvironmentInfo = &info
	}
	return nil
}

// codexEnvironmentReader is the narrow view of the Codex provider this file
// needs, so the environment projection can be tested without an engine.
type codexEnvironmentReader interface {
	ListExecutionEnvironments(context.Context) ([]provider.ExecutionEnvironment, error)
	ReadExecutionEnvironmentStatus(context.Context, string) (provider.EnvironmentStatus, error)
	ReadExecutionEnvironmentInfo(context.Context, string) (provider.EnvironmentInfo, error)
}

func (s *Server) handleCodexExecutionWrite(ctx context.Context, c *client, env protocol.Envelope, deviceID string) error {
	var payload protocol.CodexExecutionWritePayload
	if err := protocol.DecodePayload(env, &payload); err != nil {
		return s.writeError(ctx, c, env.ID, "bad_payload", err.Error())
	}
	// Both unsandboxed authorities need their own fresh confirmation. This is
	// checked before any provider call so a missing phrase can never start
	// host work, and it is never cached against the session.
	if (payload.Action == "shell" || payload.Action == "spawn") && payload.Confirm != confirmUnsandboxed {
		return s.writeError(ctx, c, env.ID, protocol.ErrConfirmRequired,
			provider.ExecutionLabelUnsandboxed+" requires a fresh confirmation for every command")
	}
	if payload.Action == "select_environment" && payload.Confirm != confirmEnvironment {
		return s.writeError(ctx, c, env.ID, protocol.ErrConfirmRequired,
			"Changing the execution environment requires confirmation")
	}
	result := protocol.CodexExecutionWriteResultPayload{OK: true}
	var err error
	switch payload.Action {
	case "exec":
		var exec provider.ExecResult
		exec, err = s.sessions.RunSandboxedExec(ctx, payload.SessionID, execRequestFromPhone(payload), deviceID)
		result.Exec = &exec
	case "shell":
		var execution provider.ExecutionResult
		execution, err = s.sessions.RunUnsandboxedShell(ctx, payload.SessionID, payload.Command, deviceID)
		result.Execution = &execution
	case "spawn":
		var process provider.ProcessInfo
		process, err = s.sessions.SpawnStandaloneProcess(ctx, payload.SessionID, processRequestFromPhone(payload), deviceID)
		result.Process = &process
	case "write":
		var data []byte
		if data, err = decodeTerminalStdin(payload.DataBase64); err == nil {
			err = s.sessions.WriteExecutionTerminal(ctx, payload.SessionID, payload.TerminalID, data, payload.CloseStdin, deviceID)
		}
		// Stdin can carry a password typed into an interactive prompt, so the
		// plaintext is dropped the moment the provider call returns rather
		// than being left for the garbage collector.
		clear(data)
	case "resize":
		err = s.sessions.ResizeExecutionTerminal(ctx, payload.SessionID, payload.TerminalID, payload.Rows, payload.Cols, deviceID)
	case "stop":
		err = s.sessions.StopExecutionTerminal(ctx, payload.SessionID, payload.TerminalID, deviceID)
	case "stop_all":
		result.Stopped, err = s.sessions.StopAllExecutionTerminals(ctx, payload.SessionID, deviceID)
	case "select_environment":
		err = s.sessions.SetExecutionEnvironment(ctx, payload.SessionID, environmentSelectionFromPhone(payload), deviceID)
	}
	if err != nil {
		return s.writeError(ctx, c, env.ID, executionErrorCode(err, true), "Codex execution request was not completed")
	}
	out, _ := protocol.NewEnvelope(protocol.TypeCodexExecutionWriteResult, env.ID, result)
	return s.writeJSON(ctx, c, out)
}

func decodeTerminalStdin(data string) ([]byte, error) {
	if data == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("terminal stdin must be base64")
	}
	if len(decoded) > maxTerminalStdinBytes {
		return nil, fmt.Errorf("terminal stdin exceeds bound")
	}
	return decoded, nil
}

func execRequestFromPhone(payload protocol.CodexExecutionWritePayload) provider.ExecRequest {
	return provider.ExecRequest{
		Argv: append([]string(nil), payload.Argv...), CWD: payload.CWD, Env: payload.Env,
		PermissionProfileID: payload.PermissionProfileID, ProcessID: payload.TerminalID,
		Stream: payload.Stream, TTY: payload.TTY, Rows: payload.Rows, Cols: payload.Cols,
		OutputBytesCap: payload.OutputBytesCap,
		Timeout:        time.Duration(payload.TimeoutMS) * time.Millisecond,
	}
}

func processRequestFromPhone(payload protocol.CodexExecutionWritePayload) provider.ProcessSpawnRequest {
	return provider.ProcessSpawnRequest{
		Argv: append([]string(nil), payload.Argv...), CWD: payload.CWD, Env: payload.Env,
		TTY: payload.TTY, Stream: payload.Stream, Rows: payload.Rows, Cols: payload.Cols,
		OutputBytesCap: payload.OutputBytesCap,
		Timeout:        time.Duration(payload.TimeoutMS) * time.Millisecond,
	}
}

// environmentSelectionFromPhone returns nil for an explicit disable, which
// clears any sticky upstream selection. Omitting the operation entirely is
// what preserves it; there is no "unchanged" value on this path.
func environmentSelectionFromPhone(payload protocol.CodexExecutionWritePayload) *provider.EnvironmentSelection {
	if payload.DisableEnvironment {
		return nil
	}
	return &provider.EnvironmentSelection{
		EnvironmentID:         payload.EnvironmentID,
		CWD:                   payload.CWD,
		RuntimeWorkspaceRoots: append([]string(nil), payload.RuntimeWorkspaceRoots...),
	}
}

// BroadcastCodexTerminalOutput pushes one live terminal chunk to the session
// owner's Codex-surface connections.
//
// Terminal bytes deliberately do not travel as event.Event: every event type
// enters session history and the retained ring, and a build's worth of
// terminal output would both flood that ring and persist command output the
// plan scopes to a bounded in-memory replay buffer. Clients that miss a chunk
// recover with codex.execution.read/output plus after_sequence, which also
// tells them whether the buffer already dropped their position.
func (s *Server) BroadcastCodexTerminalOutput(sessionID string, chunk provider.TerminalOutput) {
	if sessionID == "" || s.sessions == nil {
		return
	}
	owner, ownerKnown := s.sessions.OwnerOf(sessionID)
	if !ownerKnown {
		return
	}
	env, err := protocol.NewEnvelope(protocol.TypeCodexTerminalOutput, "",
		protocol.CodexTerminalOutputPayload{SessionID: sessionID, Output: chunk})
	if err != nil {
		s.log.Error("broadcast: encoding codex.terminal.output failed; dropping",
			slog.String("err", err.Error()))
		return
	}
	s.mu.Lock()
	targets := make([]*client, 0, len(s.clients))
	for cl := range s.clients {
		// An unclaimed session (empty owner) still restricts to devices that
		// negotiated the Codex surface; a v1 client has no way to render this.
		if !cl.authed || cl.negotiated < protocol.V2 || cl.codexSurfaceVersion < 1 {
			continue
		}
		if owner != "" && cl.deviceID != owner {
			continue
		}
		targets = append(targets, cl)
	}
	s.mu.Unlock()
	if len(targets) == 0 {
		return
	}
	b, err := json.Marshal(env)
	if err != nil {
		s.log.Error("broadcast: encoding codex.terminal.output failed; dropping",
			slog.String("err", err.Error()))
		return
	}
	for _, cl := range targets {
		_ = s.writeBytes(cl, b)
	}
}
