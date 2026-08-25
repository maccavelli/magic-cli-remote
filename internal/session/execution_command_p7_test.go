package session_test

import (
	"context"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

type executionCommandProvider struct{ session *executionCommandSession }

func (p *executionCommandProvider) ID() provider.ID { return "execution-command" }
func (p *executionCommandProvider) Ready() bool     { return true }
func (p *executionCommandProvider) CommandTable() command.Table {
	return command.Table{
		"ps":   {Kind: command.KindOp, Op: command.OpPS},
		"stop": {Kind: command.KindOp, Op: command.OpStop},
	}
}
func (p *executionCommandProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	s := &executionCommandSession{id: opts.LocalSessionID, events: make(chan event.Event, 4)}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: s.id, Status: "idle"}
	p.session = s
	return s, nil
}

type executionCommandSession struct {
	id        string
	events    chan event.Event
	shell     []string
	stopped   []string
	stopAll   int
	terminals []provider.TerminalInfo
	exec      []string
	spawned   []string
	wrote     []string
	resized   []string
}

func (s *executionCommandSession) ID() string                 { return s.id }
func (s *executionCommandSession) ProviderID() provider.ID    { return "execution-command" }
func (s *executionCommandSession) AgentSessionID() string     { return "native-1" }
func (s *executionCommandSession) Events() <-chan event.Event { return s.events }
func (s *executionCommandSession) Prompt(context.Context, []provider.Content) error {
	return nil
}
func (s *executionCommandSession) Cancel(context.Context) error { return nil }
func (s *executionCommandSession) Close(context.Context) error  { return nil }
func (s *executionCommandSession) RunUnsandboxedShell(_ context.Context, command string) (provider.ExecutionResult, error) {
	s.shell = append(s.shell, command)
	return provider.ExecutionResult{Started: true, Label: provider.ExecutionLabelUnsandboxed}, nil
}
func (s *executionCommandSession) ListTerminals(context.Context) ([]provider.TerminalInfo, error) {
	return append([]provider.TerminalInfo(nil), s.terminals...), nil
}
func (s *executionCommandSession) StopTerminal(_ context.Context, id string) error {
	s.stopped = append(s.stopped, id)
	return nil
}
func (s *executionCommandSession) StopAllTerminals(context.Context) (int, error) {
	s.stopAll++
	return len(s.terminals), nil
}
func (s *executionCommandSession) RunSandboxedExec(_ context.Context, request provider.ExecRequest) (provider.ExecResult, error) {
	s.exec = append(s.exec, strings.Join(request.Argv, " "))
	return provider.ExecResult{Label: provider.ExecutionLabelSandboxed, AuditClass: provider.ExecutionAuditCommandExec}, nil
}
func (s *executionCommandSession) SpawnStandaloneProcess(_ context.Context, request provider.ProcessSpawnRequest) (provider.ProcessInfo, error) {
	s.spawned = append(s.spawned, strings.Join(request.Argv, " "))
	return provider.ProcessInfo{ID: "proc-1", Label: provider.ExecutionLabelStandalone, AuditClass: provider.ExecutionAuditProcess}, nil
}
func (s *executionCommandSession) WriteTerminal(_ context.Context, id string, data []byte, closeStdin bool) error {
	s.wrote = append(s.wrote, id)
	return nil
}
func (s *executionCommandSession) ResizeTerminal(_ context.Context, id string, rows, cols int) error {
	s.resized = append(s.resized, id)
	return nil
}
func (s *executionCommandSession) ReplayTerminal(_ context.Context, id string, after uint64) ([]provider.TerminalOutput, bool, error) {
	return []provider.TerminalOutput{{TerminalID: id, Sequence: after + 1, Stream: "stdout", Data: []byte("out")}}, false, nil
}

func TestShellBangFreshConfirmationAndStrictTerminalCommands(t *testing.T) {
	p := &executionCommandProvider{}
	registry := provider.NewRegistry()
	registry.Register(p)
	sink := &eventSink{}
	manager := session.NewManager(registry, nil, nil, sink.handle)
	meta, err := manager.Create(context.Background(), "execution-command", provider.StartOptions{LocalSessionID: "local-1"}, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	p.session.terminals = []provider.TerminalInfo{{ID: "term-1", Kind: provider.TerminalKindExec, Label: provider.ExecutionLabelSandboxed, Running: true}}

	for _, prompt := range []string{"!", "!echo hi", "!confirm echo bye", "!confirm echo hi", "/ps", "/stop", "/stop term-1 extra", "/stop term-1", "/stop --all"} {
		if err := manager.Prompt(context.Background(), meta.ID, prompt, nil, "device-1"); err != nil {
			t.Fatalf("%q: %v", prompt, err)
		}
	}
	if len(p.session.shell) != 1 || p.session.shell[0] != "echo hi" {
		t.Fatalf("shell calls=%v", p.session.shell)
	}
	if len(p.session.stopped) != 1 || p.session.stopped[0] != "term-1" || p.session.stopAll != 1 {
		t.Fatalf("stopped=%v stopAll=%d", p.session.stopped, p.session.stopAll)
	}
	joined := strings.Join(sink.notices(), "\n")
	for _, want := range []string{
		"UNSANDBOXED SHELL — FULL HOST ACCESS",
		"Confirm exactly: !confirm echo hi",
		"term-1",
		"Usage: /stop <id> or /stop --all",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("notices missing %q: %s", want, joined)
		}
	}
}
