package acpagent_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// fixtureState is a fixture Spec's own per-session state.
type fixtureState struct {
	mu    sync.Mutex
	calls int
}

func (f *fixtureState) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.calls
}

// fixtureSpec is a Spec outside acpagent, held to what it exports: a vendor
// notification, a `_meta` key, an extension request and a limit, each acted on
// through acpagent.Host (MADR 0208, amendment of 2026-11-01; PLAN P6a step 1).
func fixtureSpec() acpagent.Spec {
	return acpagent.Spec{
		ID:              provider.ID("fixture"),
		NewSessionState: func() any { return &fixtureState{} },
		ExtensionNotifications: map[string]acpagent.ExtensionNotificationHandler{
			"vendor/child": func(_ context.Context, h acpagent.Host, params json.RawMessage) {
				var p struct {
					ID, Title, Status string
				}
				_ = json.Unmarshal(params, &p)
				h.SpecState().(*fixtureState).count()
				h.NoteSubagent(p.ID, p.Title, p.Title, p.Status)
			},
			"_vendor/limit": func(_ context.Context, h acpagent.Host, params json.RawMessage) {
				var p struct {
					At time.Time `json:"at"`
				}
				_ = json.Unmarshal(params, &p)
				h.EndTurnWithLimit("Rate limit reached for requests", p.At)
			},
		},
		MetaHandlers: map[string]acpagent.MetaHandler{
			"vendor/retry": func(_ context.Context, h acpagent.Host, _ acpagent.MetaSource, _ json.RawMessage) {
				h.Notice("retrying")
			},
		},
		ExtensionRequests: map[string]acpagent.ExtensionRequestHandler{
			"_vendor/ask": func(_ context.Context, h acpagent.Host, _ json.RawMessage) (any, error) {
				return map[string]any{"session": h.AgentSessionID(), "calls": h.SpecState().(*fixtureState).count()}, nil
			},
		},
	}
}

// nextOf returns the next event of type want, or fails.
func nextOf(t *testing.T, s provider.Session, want event.Type) event.Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %s event", want)
			return event.Event{}
		}
	}
}

func TestHostDrivesTheSubagentPanel(t *testing.T) {
	s, w := acpagent.NewTestWire(t, fixtureSpec())
	w.Send(`{"jsonrpc":"2.0","method":"vendor/child","params":{"ID":"c1","Title":"read","Status":"running"}}`)
	ev := nextOf(t, s, event.TypeSubagents)
	if len(ev.Subagents) != 1 || ev.Subagents[0].ID != "c1" || ev.Subagents[0].Status != event.SubagentStatusRunning {
		t.Fatalf("subagents = %+v, want c1 running", ev.Subagents)
	}
}

func TestHostPostsANotice(t *testing.T) {
	s, w := acpagent.NewTestWire(t, fixtureSpec())
	w.Send(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"agent-1","update":{"sessionUpdate":"session_info_update","_meta":{"vendor/retry":{"attempt":1}}}}}`)
	if ev := nextOf(t, s, event.TypeNotice); ev.Text != "retrying" {
		t.Fatalf("notice = %q, want retrying", ev.Text)
	}
}

func TestHostAnswersAnExtensionRequestWithSpecState(t *testing.T) {
	_, w := acpagent.NewTestWire(t, fixtureSpec())
	for want := 1; want <= 2; want++ {
		id := `"q` + string(rune('0'+want)) + `"`
		w.Send(`{"jsonrpc":"2.0","id":` + id + `,"method":"_vendor/ask","params":{}}`)
		res, code := w.AnswerTo(id)
		var r struct {
			Session string
			Calls   int
		}
		if err := json.Unmarshal(res, &r); err != nil || code != 0 || r.Session != "agent-1" || r.Calls != want {
			t.Fatalf("answer %d = %s (code %d), want session agent-1 and calls %d", want, res, code, want)
		}
	}
}

func TestHostEndsATurnWithALimitAndItsRetryTime(t *testing.T) {
	s, w := acpagent.NewTestWire(t, fixtureSpec())
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	w.AwaitPrompt()
	at := time.Date(2026, 11, 1, 12, 0, 5, 0, time.UTC)
	w.Send(`{"jsonrpc":"2.0","method":"_vendor/limit","params":{"at":"` + at.Format(time.RFC3339) + `"}}`)
	if ev := nextOf(t, s, event.TypeTurnComplete); ev.Status != "error" {
		t.Fatalf("turn_complete status = %q, want error", ev.Status)
	}
	if ev := nextOf(t, s, event.TypeError); !ev.RetryAt.Equal(at) {
		t.Fatalf("error %q retry at %v, want %v", ev.Error, ev.RetryAt, at)
	}
}
