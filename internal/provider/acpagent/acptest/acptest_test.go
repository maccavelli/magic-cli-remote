package acptest_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
)

func newProvider(spec acpagent.Spec) *acpagent.Provider {
	spec.ID = provider.ID("fixture")
	return acpagent.New(spec, acpagent.Config{})
}

// TestStartOnRunsTheHandshake: StartOn sends initialize and session/new, and
// the session takes the id session/new answered.
func TestStartOnRunsTheHandshake(t *testing.T) {
	var info acpagent.InitializeInfo
	p := newProvider(acpagent.Spec{InitializeMeta: func(meta map[string]any) acpagent.InitializeInfo {
		info.CurrentModel, _ = meta["model"].(string)
		return info
	}})
	s, a := acptest.Start(t, p, acptest.Options{SessionID: "agent-7", InitializeMeta: json.RawMessage(`{"model":"m1"}`)})
	var methods []string
	for _, m := range a.Requests() {
		methods = append(methods, m.Method)
	}
	if !slices.Equal(methods, []string{"initialize", "session/new"}) {
		t.Fatalf("handshake sent %v, want initialize then session/new", methods)
	}
	if got := acptest.Host(t, s).AgentSessionID(); got != "agent-7" {
		t.Errorf("AgentSessionID = %q, want agent-7", got)
	}
	if info.CurrentModel != "m1" {
		t.Errorf("InitializeMeta saw model %q, want m1", info.CurrentModel)
	}
}

// TestStartOnAuthenticatesWithTheSpecsMethod: an advertised, headless-safe
// method is authenticated before session/new.
func TestStartOnAuthenticatesWithTheSpecsMethod(t *testing.T) {
	p := newProvider(acpagent.Spec{SafeAuthMethodIDs: []string{"cached_token"}, AuthPreference: []string{"cached_token"}})
	_, a := acptest.Start(t, p, acptest.Options{AuthMethods: []string{"cached_token"}})
	if got := a.ParamsFor("authenticate")["methodId"]; got != "cached_token" {
		t.Fatalf("authenticate methodId = %v, want cached_token", got)
	}
}

// TestRepliesAndUnknownMethods: a scripted method answers its result; any
// other is method-not-found, which CallExtension reports as not implemented.
func TestRepliesAndUnknownMethods(t *testing.T) {
	s, a := acptest.Start(t, newProvider(acpagent.Spec{}), acptest.Options{
		Replies: map[string]string{"_vendor/ping": `{"pong":true}`},
	})
	h := acptest.Host(t, s)
	var out struct{ Pong bool }
	if err := h.CallExtension(context.Background(), "_vendor/ping", map[string]any{}, &out); err != nil || !out.Pong {
		t.Fatalf("_vendor/ping = %+v, %v; want pong", out, err)
	}
	err := h.CallExtension(context.Background(), "_vendor/absent", map[string]any{}, nil)
	if !errors.Is(err, provider.ErrNotImplemented) {
		t.Fatalf("_vendor/absent err = %v, want ErrNotImplemented", err)
	}
	if !a.Sent("_vendor/absent") {
		t.Error("the agent did not record _vendor/absent")
	}
}

// TestAHeldPromptIsAnsweredByTheTest: session/prompt waits for the test.
func TestAHeldPromptIsAnsweredByTheTest(t *testing.T) {
	s, a := acptest.Start(t, newProvider(acpagent.Spec{}), acptest.Options{})
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	a.Respond(a.Await("session/prompt"), map[string]any{"stopReason": "end_turn"})
	if ev := acptest.Next(t, s, event.TypeTurnComplete); ev.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn", ev.StopReason)
	}
}

// TestTheAgentReachesTheSpecsHandlers: Notify reaches a notification handler
// and Call a request handler, whose answer AnswerTo returns.
func TestTheAgentReachesTheSpecsHandlers(t *testing.T) {
	noted := make(chan string, 1)
	p := newProvider(acpagent.Spec{
		ExtensionNotifications: map[string]acpagent.ExtensionNotificationHandler{
			"_vendor/note": func(_ context.Context, _ acpagent.Host, params json.RawMessage) { noted <- string(params) },
		},
		ExtensionRequests: map[string]acpagent.ExtensionRequestHandler{
			"_vendor/ask": func(context.Context, acpagent.Host, json.RawMessage) (any, error) {
				return map[string]any{"ok": true}, nil
			},
		},
	})
	_, a := acptest.Start(t, p, acptest.Options{})
	a.Notify("_vendor/note", map[string]any{"n": 1})
	select {
	case got := <-noted:
		if got != `{"n":1}` {
			t.Errorf("notification params = %s", got)
		}
	case <-time.After(acptest.Budget):
		t.Fatal("the notification never reached its handler")
	}
	m := a.AnswerTo(a.Call("_vendor/ask", map[string]any{}))
	if m.Error != nil || string(m.Result) != `{"ok":true}` {
		t.Fatalf("answer = %s %+v, want {\"ok\":true}", m.Result, m.Error)
	}
}
