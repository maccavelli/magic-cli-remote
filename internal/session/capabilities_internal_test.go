package session

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
)

// fixtureACPProvider starts each session on a new fixture ACP agent.
type fixtureACPProvider struct {
	*acpagent.Provider
	t *testing.T

	mu     sync.Mutex
	agents []*acptest.Agent
}

func (p *fixtureACPProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	s, a := acptest.Start(p.t, p.Provider, acptest.Options{Start: opts})
	p.mu.Lock()
	p.agents = append(p.agents, a)
	p.mu.Unlock()
	return s, nil
}

func (p *fixtureACPProvider) agent(t *testing.T) *acptest.Agent {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.agents) != 1 {
		t.Fatalf("%d fixture agents started, want 1", len(p.agents))
	}
	return p.agents[0]
}

// TestUndeclaredOperationsAreUnavailable is MADR 0208 D1's resolver test
// (Confirmation, as amended at P6d). The Spec's table maps commands to
// operations, but the Spec offers none: no Ops, no config options and no
// fallbacks. Nothing it did not declare is available, and running the commands,
// a phone fork and a phone rename sends the agent nothing after session/new.
func TestUndeclaredOperationsAreUnavailable(t *testing.T) {
	spec := acpagent.Spec{
		ID: provider.ID("vendor"),
		Commands: command.Table{
			"compact":  {Kind: command.KindOp, Op: command.OpCompact},
			"fork":     {Kind: command.KindOp, Op: command.OpFork},
			"undo":     {Kind: command.KindOp, Op: command.OpUndo},
			"usage":    {Kind: command.KindOp, Op: command.OpUsage},
			"status":   {Kind: command.KindOp, Op: command.OpStatus},
			"thinking": {Kind: command.KindOp, Op: command.OpSetThinkingLevel},
			"model":    {Kind: command.KindOp, Op: command.OpSetModel},
		},
	}
	p := &fixtureACPProvider{Provider: acpagent.New(spec, acpagent.Config{}), t: t}
	reg := provider.NewRegistry()
	reg.Register(p)
	m := NewManager(reg, nil, nil, func(event.Event) {})
	ctx := context.Background()
	meta, err := m.Create(ctx, "vendor", provider.StartOptions{Name: "s", LocalSessionID: "sess-1"}, "dev-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	a := p.agent(t)

	for _, name := range []string{"compact", "fork", "undo", "usage", "status", "thinking"} {
		if res, _ := m.resolveCommand(meta.ID, name); res.Available {
			t.Errorf("/%s resolves available (%+v); the session declared no such operation", name, res.Mapping)
		}
	}
	if res, _ := m.resolveCommand(meta.ID, "model"); res.Mapping.Kind != command.KindDaemon {
		t.Errorf("/model resolves to %+v, want the daemon's relaunch: the session declared no in-place switch", res.Mapping)
	}

	for _, text := range []string{"/compact", "/fork", "/undo", "/usage", "/status", "/thinking high"} {
		_ = m.Prompt(ctx, meta.ID, text, nil, "dev-a")
	}
	if _, err := m.Fork(ctx, meta.ID, "", "dev-a"); err == nil {
		t.Error("a phone fork succeeded on a session that declared no fork")
	}
	if _, err := m.Rename(ctx, meta.ID, "renamed", "dev-a"); err == nil {
		t.Error("a phone rename succeeded on a session that declared no rename")
	}
	// The same for workspace, shell and skill refresh (0214 PLAN P6a).
	if _, err := m.workspaceSession(meta.ID, "dev-a"); err == nil {
		t.Error("the workspace opened on a session that declared none")
	}
	if err := m.Shell(ctx, meta.ID, "ls", "dev-a"); err == nil {
		t.Error("a shell command ran on a session that declared no shell")
	}
	if err := m.RefreshSkills(ctx, meta.ID, "dev-a"); err == nil {
		t.Error("a skill refresh ran on a session that declared none")
	}
	for _, method := range a.Methods() {
		if !slices.Contains([]string{"initialize", "session/new"}, method) {
			t.Errorf("the agent was sent %q; an undeclared operation reached the wire", method)
		}
	}
}

// configSession declares a thinking switch only once the test says its agent
// has the config option.
type configSession struct {
	events   chan event.Event
	thinking atomic.Bool
	once     sync.Once
}

func (s *configSession) Capabilities() provider.Capabilities {
	if s.thinking.Load() {
		return provider.NewCapabilities(provider.CapSetThinkingLevel)
	}
	return nil
}
func (s *configSession) ID() string                                       { return "cfg-1" }
func (s *configSession) ProviderID() provider.ID                          { return "cfg" }
func (s *configSession) AgentSessionID() string                           { return "agent-cfg" }
func (s *configSession) Prompt(context.Context, []provider.Content) error { return nil }
func (s *configSession) Cancel(context.Context) error                     { return nil }
func (s *configSession) Events() <-chan event.Event                       { return s.events }
func (s *configSession) Close(context.Context) error {
	s.once.Do(func() { close(s.events) })
	return nil
}

type configProvider struct{ s *configSession }

func (p configProvider) ID() provider.ID { return "cfg" }
func (p configProvider) Ready() bool     { return true }
func (p configProvider) Start(context.Context, provider.StartOptions) (provider.Session, error) {
	return p.s, nil
}

// TestSessionConfigReresolvesCommands pins MADR 0208 D1's re-resolution: an
// agent's config options can add an operation mid-session, so a session config
// event re-runs command resolution and clients see the command become
// available.
func TestSessionConfigReresolvesCommands(t *testing.T) {
	s := &configSession{events: make(chan event.Event, 8)}
	reg := provider.NewRegistry()
	reg.Register(configProvider{s: s})
	var mu sync.Mutex
	var thinking []bool
	m := NewManager(reg, nil, nil, func(ev event.Event) {
		if ev.Type != event.TypeRemoteCommands {
			return
		}
		for _, c := range ev.RemoteCommands {
			if c.Name == "thinking" {
				mu.Lock()
				thinking = append(thinking, c.Available)
				mu.Unlock()
			}
		}
	})
	if _, err := m.Create(context.Background(), "cfg", provider.StartOptions{Name: "s", LocalSessionID: "cfg-1"}, "dev-a"); err != nil {
		t.Fatalf("create: %v", err)
	}
	last := func() (bool, bool) {
		mu.Lock()
		defer mu.Unlock()
		if len(thinking) == 0 {
			return false, false
		}
		return thinking[len(thinking)-1], true
	}
	if got, ok := last(); !ok || got {
		t.Fatalf("before the config event /thinking advertised=%v available=%v, want advertised and unavailable", ok, got)
	}

	s.thinking.Store(true)
	s.events <- event.Event{Type: event.TypeSessionConfig, SessionID: s.ID()}
	deadline := time.Now().Add(acptest.Budget)
	for {
		if got, _ := last(); got {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("/thinking was not advertised available after the agent's config options added it")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
