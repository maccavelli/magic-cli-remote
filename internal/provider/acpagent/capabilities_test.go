package acpagent

import (
	"context"
	"encoding/json"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// declared is the set of capabilities s declares, after checking that each has
// the interface that performs it.
func declared(t *testing.T, s provider.Session) map[provider.Capability]bool {
	t.Helper()
	out := map[provider.Capability]bool{}
	for c, ok := range s.Capabilities() {
		if !ok {
			continue
		}
		if !provider.Implements(s, c) {
			t.Errorf("%T declares %q without the interface that performs it", s, c)
		}
		out[c] = true
	}
	return out
}

func wantDeclared(t *testing.T, got map[provider.Capability]bool, want ...provider.Capability) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("declared %v, want exactly %v", got, want)
	}
	for _, c := range want {
		if !got[c] {
			t.Errorf("declared %v, want %q among them", got, c)
		}
	}
}

// TestCapabilitiesFollowTheSpecAndTheAgent pins MADR 0208 D1 for ACP: a session
// declares the operations its Spec offers, and a model or thinking switch when
// the agent has the config option or the Spec a fallback. Nothing else, whatever
// methods the session type has.
func TestCapabilitiesFollowTheSpecAndTheAgent(t *testing.T) {
	bare := func() *session {
		return &session{localID: "local-1", agentID: "agent-1", events: make(chan event.Event, 8), done: make(chan struct{})}
	}

	t.Run("nothing offered", func(t *testing.T) {
		wantDeclared(t, declared(t, bare()))
	})

	t.Run("the Spec's operations", func(t *testing.T) {
		s := bare()
		s.ops = Ops{
			Fork: func(context.Context, Host, provider.ForkOptions) (provider.ForkResult, error) {
				return provider.ForkResult{}, nil
			},
			Compact:       func(context.Context, Host) error { return nil },
			Rename:        func(context.Context, Host, string) error { return nil },
			UndoLast:      func(context.Context, Host) (string, error) { return "", nil },
			RuntimeUsage:  func(context.Context, Host) (provider.Result, error) { return provider.Result{}, nil },
			RuntimeStatus: func(context.Context, Host) (provider.Result, error) { return provider.Result{}, nil },
		}
		wantDeclared(t, declared(t, s), provider.CapFork, provider.CapCompact, provider.CapRename,
			provider.CapUndo, provider.CapUsage, provider.CapStatus)
	})

	t.Run("the Spec's fallbacks", func(t *testing.T) {
		s := bare()
		s.provider = New(Spec{
			SetModelFallback:    func(context.Context, Host, string) (string, error) { return "", nil },
			SetThinkingFallback: func(context.Context, Host, string, string) (string, error) { return "", nil },
		}, Config{})
		wantDeclared(t, declared(t, s), provider.CapSetModel, provider.CapSetThinkingLevel)
	})

	t.Run("the agent's config options, as they change", func(t *testing.T) {
		s := bare()
		update := func(options string) {
			t.Helper()
			var n acp.SessionNotification
			raw := `{"sessionId":"agent-1","update":{"sessionUpdate":"config_option_update","configOptions":` + options + `}}`
			if err := json.Unmarshal([]byte(raw), &n); err != nil {
				t.Fatalf("notification: %v", err)
			}
			if err := s.SessionUpdate(context.Background(), n); err != nil {
				t.Fatalf("SessionUpdate: %v", err)
			}
		}
		update(configOptionsJSON("m1", "low"))
		wantDeclared(t, declared(t, s), provider.CapSetModel, provider.CapSetThinkingLevel)
		update(`[{"type":"select","id":"model","name":"Model","category":"model","currentValue":"m2",` +
			`"options":[{"value":"m1","name":"M1"},{"value":"m2","name":"M2"}]}]`)
		wantDeclared(t, declared(t, s), provider.CapSetModel)
	})
}
