package acpagent

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// surfaceOps returns Ops with every 0214 PLAN P6a operation set, each
// recording its name in calls.
func surfaceOps(calls *[]string) Ops {
	note := func(name string) { *calls = append(*calls, name) }
	return Ops{
		ListWorkspace: func(context.Context, Host, string) ([]provider.WorkspaceEntry, error) {
			note("list")
			return nil, nil
		},
		ReadWorkspace: func(context.Context, Host, string) (provider.WorkspaceContent, error) {
			note("read")
			return provider.WorkspaceContent{}, nil
		},
		SearchWorkspace: func(context.Context, Host, string, string) (provider.WorkspaceSearch, error) {
			note("search")
			return provider.WorkspaceSearch{}, nil
		},
		Shell:    func(context.Context, Host, string) error { note("shell"); return nil },
		Revert:   func(context.Context, Host, string, string) error { note("revert"); return nil },
		Unrevert: func(context.Context, Host) error { note("unrevert"); return nil },
		Diff: func(context.Context, Host, string) (provider.DiffResult, error) {
			note("diff")
			return provider.DiffResult{}, nil
		},
		RefreshSkills: func(context.Context, Host) error { note("refresh"); return nil },
	}
}

// callSurfaces calls each operation once and returns their errors.
func callSurfaces(s *session) []error {
	ctx := context.Background()
	_, e1 := s.ListWorkspace(ctx, ".")
	_, e2 := s.ReadWorkspace(ctx, "a")
	_, e3 := s.SearchWorkspace(ctx, "file", "a")
	_, e7 := s.Diff(ctx, "")
	return []error{e1, e2, e3, s.Shell(ctx, "ls"), s.Revert(ctx, "m1", ""), s.Unrevert(ctx), e7, s.RefreshSkills(ctx)}
}

// capabilityFlags is what the start's session_capabilities event tells the
// phone about the three it gates on.
func capabilityFlags(t *testing.T, s *session) (workspace, skills, shell bool) {
	t.Helper()
	s.emitCapabilities()
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeSessionCapabilities && ev.Capabilities != nil {
			return ev.Capabilities.WorkspaceRead, ev.Capabilities.SkillRefresh, ev.Capabilities.Shell
		}
	}
	t.Fatal("no session_capabilities event")
	return false, false, false
}

// TestSurfacesFollowTheSpecsOps pins 0214 PLAN P6a: each operation runs the
// Spec's hook and is declared, and told to the phone, only when the Spec sets
// it; workspace needs all three parts and revert needs unrevert. Without the
// hook it answers "not offered".
func TestSurfacesFollowTheSpecsOps(t *testing.T) {
	surfaces := []provider.Capability{provider.CapWorkspace, provider.CapShell, provider.CapRevert,
		provider.CapDiff, provider.CapSkillRefresh}

	t.Run("all set", func(t *testing.T) {
		var calls []string
		s := loadingSession()
		s.loading = false
		s.ops = surfaceOps(&calls)
		for _, err := range callSurfaces(s) {
			if err != nil {
				t.Errorf("an offered operation failed: %v", err)
			}
		}
		if want := []string{"list", "read", "search", "diff", "shell", "revert", "unrevert", "refresh"}; !slices.Equal(calls, want) {
			t.Errorf("hooks called %v, want %v", calls, want)
		}
		got := declared(t, s)
		for _, c := range surfaces {
			if !got[c] {
				t.Errorf("%q not declared with its hook set", c)
			}
		}
		if w, sk, sh := capabilityFlags(t, s); !w || !sk || !sh {
			t.Errorf("session_capabilities workspace %v, skill refresh %v, shell %v; want all true", w, sk, sh)
		}
	})

	t.Run("none set", func(t *testing.T) {
		s := loadingSession()
		s.loading = false
		for i, err := range callSurfaces(s) {
			if !errors.Is(err, provider.ErrNotImplemented) {
				t.Errorf("operation %d: %v, want not offered", i, err)
			}
		}
		got := declared(t, s)
		for _, c := range surfaces {
			if got[c] {
				t.Errorf("%q declared with no hook", c)
			}
		}
		if w, sk, sh := capabilityFlags(t, s); w || sk || sh {
			t.Errorf("session_capabilities workspace %v, skill refresh %v, shell %v; want all false", w, sk, sh)
		}
	})

	t.Run("part of workspace, revert without unrevert", func(t *testing.T) {
		var calls []string
		ops := surfaceOps(&calls)
		ops.SearchWorkspace = nil
		ops.Unrevert = nil
		s := loadingSession()
		s.loading = false
		s.ops = ops
		got := declared(t, s)
		if got[provider.CapWorkspace] || got[provider.CapRevert] {
			t.Errorf("declared %v; a partial workspace or a revert with no inverse is not offered", got)
		}
		if _, err := s.ListWorkspace(context.Background(), "."); !errors.Is(err, provider.ErrNotImplemented) {
			t.Errorf("ListWorkspace on a partial workspace: %v, want not offered", err)
		}
		if err := s.Revert(context.Background(), "m1", ""); !errors.Is(err, provider.ErrNotImplemented) {
			t.Errorf("Revert with no inverse: %v, want not offered", err)
		}
	})
}

// TestDiagnosticsJoinTheSpecsAndMCP: the Spec's diagnostics, with MCP from the
// session's own list unless the Spec's answer carries it.
func TestDiagnosticsJoinTheSpecsAndMCP(t *testing.T) {
	s := loadingSession()
	s.loading = false
	s.mcpStatus = []provider.MCPServerStatus{{Name: "session-server"}}
	s.ops.Diagnostics = func(context.Context, Host) (provider.Diagnostics, error) {
		return provider.Diagnostics{Branch: "main", Skills: []provider.SkillInfo{{Name: "s1"}}}, nil
	}
	d, err := s.Diagnostics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Branch != "main" || len(d.Skills) != 1 || len(d.MCP) != 1 || d.MCP[0].Name != "session-server" {
		t.Errorf("diagnostics = %+v, want the Spec's branch and skills and the session's MCP", d)
	}
	s.ops.Diagnostics = func(context.Context, Host) (provider.Diagnostics, error) {
		return provider.Diagnostics{MCP: []provider.MCPServerStatus{{Name: "spec-server"}}}, nil
	}
	if d, _ := s.Diagnostics(context.Background()); len(d.MCP) != 1 || d.MCP[0].Name != "spec-server" {
		t.Errorf("MCP = %+v, want the Spec's own", d.MCP)
	}
}

// TestBusyIsTheTurn: Host.Busy reports a running turn.
func TestBusyIsTheTurn(t *testing.T) {
	h := &heldPrompts{}
	s, err := startOnScript(t, New(Spec{ID: "vendor"}, Config{}), provider.StartOptions{}, h.handle)
	if err != nil {
		t.Fatal(err)
	}
	host := s.(Host)
	if host.Busy() {
		t.Error("busy before any prompt")
	}
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	if !host.Busy() {
		t.Error("not busy while the prompt is held")
	}
	h.answerNext(t)
	waitIdle(t, host)
}

// waitIdle waits for h's turn to end.
func waitIdle(t *testing.T, h Host) {
	t.Helper()
	for i := 0; i < 500 && h.Busy(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if h.Busy() {
		t.Error("still busy after the prompt was answered")
	}
}

// TestNativeMessageIDs pins 0214 PLAN P6a: an agent's message id on its output
// and on a replayed prompt is the event's native message id, which
// transcript_remove works by.
func TestNativeMessageIDs(t *testing.T) {
	s := loadingSession()
	replay(t, s, `{"sessionUpdate":"user_message_chunk","messageId":"msg-u1","content":{"type":"text","text":"the prompt"}}`)
	replay(t, s, `{"sessionUpdate":"agent_thought_chunk","messageId":"msg-a1","content":{"type":"text","text":"thinking"}}`)
	replay(t, s, `{"sessionUpdate":"agent_message_chunk","messageId":"msg-a1","content":{"type":"text","text":"answer"}}`)
	s.flushReplayedUser()
	s.drainChunks() // the answer's chunk waits in the coalescing buffer otherwise
	got := map[event.Type]string{}
	for _, ev := range drainEvents(s) {
		got[ev.Type] = ev.NativeMessageID
	}
	if got[event.TypeUserMessage] != "msg-u1" || got[event.TypeThoughtChunk] != "msg-a1" || got[event.TypeAssistantChunk] != "msg-a1" {
		t.Errorf("native ids = %v, want the prompt's msg-u1 and the answer's msg-a1", got)
	}
}
