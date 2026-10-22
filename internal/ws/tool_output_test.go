package ws_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// longToolText is more than the phone is shown, in many lines.
var longToolText = strings.Repeat("0123456789abcdef\n", 1000)

// toolOutputProvider registers as "fake" and starts sessions that emit one
// tool update longer than the phone is shown, then idle.
type toolOutputProvider struct{}

func (toolOutputProvider) ID() provider.ID { return provider.IDFake }
func (toolOutputProvider) Ready() bool     { return true }
func (toolOutputProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	s := &toolOutputSession{id: opts.LocalSessionID, events: make(chan event.Event, 1)}
	s.events <- event.Event{Type: event.TypeToolUpdate, SessionID: s.id, ToolID: "t-long",
		Status: "completed", Text: longToolText}
	return s, nil
}

type toolOutputSession struct {
	id     string
	events chan event.Event
}

func (s *toolOutputSession) ID() string                                       { return s.id }
func (s *toolOutputSession) ProviderID() provider.ID                          { return provider.IDFake }
func (s *toolOutputSession) AgentSessionID() string                           { return s.id }
func (s *toolOutputSession) Events() <-chan event.Event                       { return s.events }
func (s *toolOutputSession) Prompt(context.Context, []provider.Content) error { return nil }
func (s *toolOutputSession) Cancel(context.Context) error                     { return nil }
func (s *toolOutputSession) Close(context.Context) error                      { return nil }

// TestWSSessionToolOutput is PLAN 0208 P2 step 5's protocol round trip: the
// phone fetches the whole of a tool output the daemon cut, and gets a stable
// error for a tool with nothing to fetch.
func TestWSSessionToolOutput(t *testing.T) {
	ws := setupWSSessionWithProvider(t, "test", toolOutputProvider{})
	defer ws.close(t)

	deadline := time.Now().Add(2 * time.Second)
	cut := false
	for !cut && time.Now().Before(deadline) {
		for _, ev := range ws.mgr.History(ws.meta.ID) {
			cut = cut || (ev.ToolID == "t-long" && ev.Truncated)
		}
		time.Sleep(time.Millisecond)
	}
	if !cut {
		t.Fatal("the long tool event never reached history marked truncated")
	}

	t.Run("whole output", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionToolOutput, "out", protocol.SessionToolOutputPayload{
			SessionID: ws.meta.ID, ToolCallID: "t-long",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type != protocol.TypeSessionToolOutputResult {
			t.Fatalf("want %s, got %s: %s", protocol.TypeSessionToolOutputResult, got.Type, got.Payload)
		}
		var res protocol.SessionToolOutputResultPayload
		if err := json.Unmarshal(got.Payload, &res); err != nil {
			t.Fatal(err)
		}
		if res.ToolCallID != "t-long" || res.Text != strings.TrimRight(longToolText, "\n") {
			t.Fatalf("result = %s, %d bytes; want the whole text", res.ToolCallID, len(res.Text))
		}
	})

	for _, tc := range []struct{ name, toolID, code string }{
		{name: "nothing to fetch", toolID: "t-none", code: "tool_output_not_found"},
		{name: "no tool id", toolID: "", code: "bad_payload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := protocol.NewEnvelope(protocol.TypeSessionToolOutput, "out-"+tc.code, protocol.SessionToolOutputPayload{
				SessionID: ws.meta.ID, ToolCallID: tc.toolID,
			})
			ws.send(t, env)
			got := ws.recvSkipEvents(t)
			var e protocol.ErrorPayload
			if got.Type != protocol.TypeError || json.Unmarshal(got.Payload, &e) != nil || e.Code != tc.code {
				t.Fatalf("got %s %s, want error %s", got.Type, got.Payload, tc.code)
			}
		})
	}
}
