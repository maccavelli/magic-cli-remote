package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

func executionEnvelope(t *testing.T, typ string, payload any) protocol.Envelope {
	t.Helper()
	env, err := protocol.NewEnvelope(typ, "env-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// TestExecutionReadDecoderBoundsAndScope pins which read actions need a
// session and which are host-administration projections, plus the exact
// bounded selectors each one requires.
func TestExecutionReadDecoderBoundsAndScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload protocol.CodexExecutionReadPayload
		session string
		wantErr bool
	}{
		{name: "terminals need a session", payload: protocol.CodexExecutionReadPayload{Action: "terminals", SessionID: "s1"}, session: "s1"},
		{name: "terminals without a session are refused", payload: protocol.CodexExecutionReadPayload{Action: "terminals"}, wantErr: true},
		{name: "output needs a terminal id", payload: protocol.CodexExecutionReadPayload{Action: "output", SessionID: "s1"}, wantErr: true},
		{name: "output is session scoped", payload: protocol.CodexExecutionReadPayload{Action: "output", SessionID: "s1", TerminalID: "t1"}, session: "s1"},
		{name: "environments are host scoped", payload: protocol.CodexExecutionReadPayload{Action: "environments", SessionID: "s1"}, session: ""},
		{name: "environment status needs an id", payload: protocol.CodexExecutionReadPayload{Action: "environment_status"}, wantErr: true},
		{name: "environment info is host scoped", payload: protocol.CodexExecutionReadPayload{Action: "environment_info", EnvironmentID: "builder"}, session: ""},
		{name: "unknown actions fail closed", payload: protocol.CodexExecutionReadPayload{Action: "raw_rpc", SessionID: "s1"}, wantErr: true},
		{name: "oversized terminal ids are refused", payload: protocol.CodexExecutionReadPayload{Action: "output", SessionID: "s1", TerminalID: strings.Repeat("t", 257)}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeCodexExecutionRead(executionEnvelope(t, protocol.TypeCodexExecutionRead, tc.payload))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("accepted %+v", tc.payload)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.session {
				t.Fatalf("authorization scope = %q, want %q", got, tc.session)
			}
		})
	}
}

// TestExecutionWriteDecoderSeparatesAuthorities proves the wire cannot smuggle
// shell text through the argv-only action, or argv through the shell action,
// and that every write is session scoped.
func TestExecutionWriteDecoderSeparatesAuthorities(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload protocol.CodexExecutionWritePayload
		wantErr bool
	}{
		{name: "exec needs argv", payload: protocol.CodexExecutionWritePayload{Action: "exec", SessionID: "s1", Command: "rm -rf /"}, wantErr: true},
		{name: "exec with argv", payload: protocol.CodexExecutionWritePayload{Action: "exec", SessionID: "s1", Argv: []string{"go", "test"}}},
		{name: "shell needs command text", payload: protocol.CodexExecutionWritePayload{Action: "shell", SessionID: "s1", Argv: []string{"go"}}, wantErr: true},
		{name: "shell with command", payload: protocol.CodexExecutionWritePayload{Action: "shell", SessionID: "s1", Command: "printf hi"}},
		{name: "spawn needs argv", payload: protocol.CodexExecutionWritePayload{Action: "spawn", SessionID: "s1"}, wantErr: true},
		{name: "write needs a terminal id", payload: protocol.CodexExecutionWritePayload{Action: "write", SessionID: "s1"}, wantErr: true},
		{name: "stop_all needs only a session", payload: protocol.CodexExecutionWritePayload{Action: "stop_all", SessionID: "s1"}},
		{name: "every write is session scoped", payload: protocol.CodexExecutionWritePayload{Action: "stop_all"}, wantErr: true},
		{name: "environment selection needs an id or an explicit disable", payload: protocol.CodexExecutionWritePayload{Action: "select_environment", SessionID: "s1"}, wantErr: true},
		{name: "environment disable needs no id", payload: protocol.CodexExecutionWritePayload{Action: "select_environment", SessionID: "s1", DisableEnvironment: true}},
		{name: "negative bounds are refused", payload: protocol.CodexExecutionWritePayload{Action: "resize", SessionID: "s1", TerminalID: "t1", Rows: -1}, wantErr: true},
		{name: "unknown actions fail closed", payload: protocol.CodexExecutionWritePayload{Action: "raw_rpc", SessionID: "s1"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeCodexExecutionWrite(executionEnvelope(t, protocol.TypeCodexExecutionWrite, tc.payload))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("accepted %+v", tc.payload)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.payload.SessionID {
				t.Fatalf("authorization scope = %q, want %q", got, tc.payload.SessionID)
			}
		})
	}
}

// TestExecutionErrorCodeNeverMasksAmbiguity is the retry-safety contract: a
// non-idempotent execution that may already have run must surface as
// outcome_unknown, never as an ordinary write failure a phone would retry.
func TestExecutionErrorCodeNeverMasksAmbiguity(t *testing.T) {
	ambiguous := errors.New("connection lost: " + provider.ErrExecutionOutcomeUnknown.Error())
	for _, tc := range []struct {
		err   error
		write bool
		want  string
	}{
		{err: provider.ErrExecutionOutcomeUnknown, write: true, want: protocol.ErrOutcomeUnknown},
		{err: errors.Join(errors.New("spawn"), provider.ErrExecutionOutcomeUnknown), write: true, want: protocol.ErrOutcomeUnknown},
		{err: provider.ErrNativeUnavailable, write: false, want: protocol.ErrNativeUnavailable},
		{err: provider.ErrTerminalNotFound, write: true, want: "not_found"},
		{err: session.ErrInvalidShellCommand, write: true, want: "bad_payload"},
		{err: errors.New("plain"), write: true, want: protocol.ErrCodexExecutionWriteFailed},
		{err: errors.New("plain"), write: false, want: protocol.ErrCodexExecutionReadFailed},
	} {
		if got := executionErrorCode(tc.err, tc.write); got != tc.want {
			t.Fatalf("executionErrorCode(%v, %v) = %q, want %q", tc.err, tc.write, got, tc.want)
		}
	}
	// A message that merely mentions ambiguity, without wrapping the sentinel,
	// must NOT be reported as ambiguous: it is a known failure.
	if got := executionErrorCode(ambiguous, true); got != protocol.ErrCodexExecutionWriteFailed {
		t.Fatalf("unwrapped lookalike = %q", got)
	}
}

// TestTerminalStdinIsBoundedAndBase64 covers the one field that carries
// arbitrary user bytes into a live process.
func TestTerminalStdinIsBoundedAndBase64(t *testing.T) {
	if data, err := decodeTerminalStdin(""); err != nil || data != nil {
		t.Fatalf("empty stdin = %v %v", data, err)
	}
	data, err := decodeTerminalStdin(base64.StdEncoding.EncodeToString([]byte("yes\n")))
	if err != nil || string(data) != "yes\n" {
		t.Fatalf("stdin = %q %v", data, err)
	}
	if _, err := decodeTerminalStdin("not base64!!"); err == nil {
		t.Fatal("accepted non-base64 stdin")
	}
	oversized := base64.StdEncoding.EncodeToString(make([]byte, maxTerminalStdinBytes+1))
	if _, err := decodeTerminalStdin(oversized); err == nil {
		t.Fatal("accepted oversized stdin")
	}
}

// TestExecutionWritePayloadRedactsStdinAndEnvironmentValues proves the two
// log surfaces never render terminal stdin or environment values. Stdin can
// be a password typed at an interactive prompt.
func TestExecutionWritePayloadRedactsStdinAndEnvironmentValues(t *testing.T) {
	secret := "hunter2"
	payload := protocol.CodexExecutionWritePayload{
		Action: "write", SessionID: "s1", TerminalID: "t1",
		DataBase64: base64.StdEncoding.EncodeToString([]byte(secret)),
		Env:        map[string]*string{"TERM": &secret},
		Argv:       []string{"bash"},
	}
	rendered := payload.String() + payload.LogValue().String()
	if strings.Contains(rendered, secret) || strings.Contains(rendered, payload.DataBase64) {
		t.Fatalf("secret leaked into log surfaces: %s", rendered)
	}
	if !strings.Contains(rendered, "t1") || !strings.Contains(rendered, "s1") {
		t.Fatalf("log surfaces lost the identifiers needed to debug: %s", rendered)
	}
}

// TestEnvironmentProjectionHidesEndpointAndCredentials proves a phone that
// lists environments learns ids and allowed roots and nothing else. The
// exec-server URL and connect timeout stay inside the daemon.
func TestEnvironmentProjectionHidesEndpointAndCredentials(t *testing.T) {
	reader := &fakeEnvironmentReader{environments: []provider.ExecutionEnvironment{{
		ID: "builder", RuntimeWorkspaceRoots: []string{"/srv/work"},
	}}}
	result := protocol.CodexExecutionReadResultPayload{}
	server := &Server{}
	if err := server.readCodexEnvironment(context.Background(), reader,
		protocol.CodexExecutionReadPayload{Action: "environments"}, &result); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"wss://", "exec_server_url", "connect_timeout", "ExecServerURL"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("environment projection leaked %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(string(encoded), "builder") || !strings.Contains(string(encoded), "/srv/work") {
		t.Fatalf("environment projection dropped the id or allowed roots: %s", encoded)
	}
}

// TestEnvironmentSelectionDistinguishesDisableFromSelect pins the three-state
// contract: omit the operation to keep the selection, send an id to change it,
// send disable to clear it. Conflating the last two would silently move work
// back onto the local host.
func TestEnvironmentSelectionDistinguishesDisableFromSelect(t *testing.T) {
	selected := environmentSelectionFromPhone(protocol.CodexExecutionWritePayload{
		Action: "select_environment", EnvironmentID: "builder", CWD: "/srv/work",
		RuntimeWorkspaceRoots: []string{"/srv/work"},
	})
	if selected == nil || selected.EnvironmentID != "builder" || len(selected.RuntimeWorkspaceRoots) != 1 {
		t.Fatalf("selection = %+v", selected)
	}
	if disabled := environmentSelectionFromPhone(protocol.CodexExecutionWritePayload{
		Action: "select_environment", DisableEnvironment: true, EnvironmentID: "builder",
	}); disabled != nil {
		t.Fatalf("explicit disable produced a selection: %+v", disabled)
	}
}

// TestExecRequestNeverCarriesShellText proves the argv path drops Command
// entirely, so a client cannot reach the unsandboxed shell through it.
func TestExecRequestNeverCarriesShellText(t *testing.T) {
	request := execRequestFromPhone(protocol.CodexExecutionWritePayload{
		Action: "exec", Argv: []string{"go", "test"}, Command: "rm -rf /",
		CWD: "/repo", PermissionProfileID: ":workspace", TerminalID: "t1", TimeoutMS: 1000,
	})
	if len(request.Argv) != 2 || request.ProcessID != "t1" || request.Timeout.Milliseconds() != 1000 {
		t.Fatalf("exec request = %+v", request)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "rm -rf") {
		t.Fatalf("shell text reached the argv path: %s", encoded)
	}
	spawn := processRequestFromPhone(protocol.CodexExecutionWritePayload{
		Action: "spawn", Argv: []string{"bash"}, Command: "rm -rf /", CWD: "/repo",
	})
	if encodedSpawn, _ := json.Marshal(spawn); strings.Contains(string(encodedSpawn), "rm -rf") {
		t.Fatalf("shell text reached the spawn path: %s", encodedSpawn)
	}
}

type fakeEnvironmentReader struct {
	environments []provider.ExecutionEnvironment
}

func (f *fakeEnvironmentReader) ListExecutionEnvironments(context.Context) ([]provider.ExecutionEnvironment, error) {
	return f.environments, nil
}

func (f *fakeEnvironmentReader) ReadExecutionEnvironmentStatus(_ context.Context, id string) (provider.EnvironmentStatus, error) {
	return provider.EnvironmentStatus{ID: id, Status: "ready"}, nil
}

func (f *fakeEnvironmentReader) ReadExecutionEnvironmentInfo(_ context.Context, id string) (provider.EnvironmentInfo, error) {
	return provider.EnvironmentInfo{ID: id, ShellName: "bash", ShellPath: "/bin/bash"}, nil
}
