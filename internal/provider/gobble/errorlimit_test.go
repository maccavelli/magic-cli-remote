package gobble

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
)

// quotaMessage is the provider's words in acpext's recorded quota frame.
const quotaMessage = "Free usage limit reached. Free models work in OpenCode (https://opencode.ai); subscribe to OpenCode Go to keep going: https://opencode.ai/go"

// rpcError is data as gobble's failed-prompt JSON-RPC error, as the ACP SDK's
// RequestError.Error writes it.
func rpcError(t *testing.T, data json.RawMessage) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"code": -32603, "message": "Internal error", "data": data})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// frameWith is acpext's recorded frame name with members set (a nil value
// removes the member).
func frameWith(t *testing.T, name string, members map[string]any) json.RawMessage {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(metaFrame(t, name), &m); err != nil {
		t.Fatal(err)
	}
	for k, v := range members {
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// MADR 0243 Confirmation 3 for gobble (PLAN P4 step 3): acpext's recorded
// failed-prompt frames map onto agenterr.Limit; an outage and an auth failure
// describe no limit. now is not the frames' capture time, at which now plus
// retryAfterMs would equal resetAt and hide which one was read.
func TestErrorLimitFromRecordedFrames(t *testing.T) {
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		frame string
		want  agenterr.Limit
		reset string
		ok    bool
	}{
		{"prompt-quota.error.json", agenterr.Limit{Scope: agenterr.ScopeFreeTier, Provider: "opencode-zen",
			Code: "FreeUsageLimitError", ProviderMessage: quotaMessage}, "2026-11-09T14:05:00Z", true},
		{"prompt-rate-over-cap.error.json", agenterr.Limit{Scope: agenterr.ScopeRate, Provider: "openai",
			Code: "rate_limit_exceeded", ProviderMessage: "Rate limit reached for requests"}, "2026-11-09T12:33:39Z", true},
		{"prompt-after-retries.error.json", agenterr.Limit{}, "", false},
		{"prompt-auth.error.json", agenterr.Limit{}, "", false},
	}
	for _, c := range cases {
		t.Run(c.frame, func(t *testing.T) {
			l, reset, ok := errorLimitAt(rpcError(t, metaFrame(t, c.frame)), now)
			if ok != c.ok || l != c.want {
				t.Fatalf("limit = %+v ok %v, want %+v ok %v", l, ok, c.want, c.ok)
			}
			var want time.Time
			if c.reset != "" {
				want, _ = time.Parse(time.RFC3339, c.reset)
			}
			if !reset.Equal(want) {
				t.Fatalf("reset = %v, want %v", reset, want)
			}
		})
	}
}

// PLAN P4 step 2: retryAfterMs when resetAt is absent; gobble's "plan" yields
// to a window the text states; a prefix before the error object, and data
// that is a string, are handled.
func TestErrorLimitEdges(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 31, 39, 0, time.UTC)

	_, reset, ok := errorLimitAt(rpcError(t, frameWith(t, "prompt-quota.error.json", map[string]any{"resetAt": nil})), now)
	if want := now.Add(5601 * time.Second); !ok || !reset.Equal(want) {
		t.Errorf("no resetAt: reset = %v ok %v, want %v from retryAfterMs", reset, ok, want)
	}

	window := frameWith(t, "prompt-quota.error.json", map[string]any{"limit": "plan", "code": "usage_limit_reached",
		"provider": "chatgpt", "providerMessage": "You have reached your 5-hour limit.",
		"error": "llm: api status 429: llm: quota exhausted: llmprovider: quota exhausted: chatgpt HTTP 429 usage_limit_reached: You have reached your 5-hour limit."})
	if l, _, ok := errorLimitAt(rpcError(t, window), now); !ok || l.Scope != "" {
		t.Errorf("plan with a stated window: scope = %q ok %v, want empty so the text's window stands", l.Scope, ok)
	}
	if got := agenterr.Present(rpcError(t, window), now).Limit; got.Scope != agenterr.ScopeWindow || got.WindowMinutes != 300 {
		t.Errorf("the text's own reading = %+v, want window/300 (this test's premise)", got)
	}

	plan := frameWith(t, "prompt-quota.error.json", map[string]any{"limit": "plan", "error": "llm: quota exhausted"})
	if l, _, ok := errorLimitAt(rpcError(t, plan), now); !ok || l.Scope != agenterr.ScopePlan {
		t.Errorf("plan with no window stated: scope = %q ok %v, want plan", l.Scope, ok)
	}

	if l, _, ok := errorLimitAt("prompt failed: "+rpcError(t, metaFrame(t, "prompt-quota.error.json")), now); !ok || l.Scope != agenterr.ScopeFreeTier {
		t.Errorf("prefixed: scope = %q ok %v, want free_tier", l.Scope, ok)
	}
	if _, _, ok := errorLimitAt(rpcError(t, json.RawMessage(`"quota exhausted"`)), now); ok {
		t.Error("string data: ok, want false")
	}
	if _, _, ok := errorLimitAt("quota exhausted", now); ok {
		t.Error("no error object: ok, want false")
	}
}

// MADR 0243 D7 through a session: gobble's failed prompt reaches the phone's
// error event with gobble's scope, provider, code and reset, not the text's.
// The bare case carries a limit only in the data.
func TestPromptErrorCarriesGobblesLimit(t *testing.T) {
	reset := time.Date(2026, 11, 9, 14, 5, 0, 0, time.UTC)
	cases := []struct {
		name string
		data json.RawMessage
	}{
		{"recorded", metaFrame(t, "prompt-quota.error.json")},
		{"bare text", frameWith(t, "prompt-quota.error.json", map[string]any{"error": "llm: quota exhausted"})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, s, a := start(t, acptest.Options{})
			acptest.Drain(s)
			if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
				t.Fatalf("Prompt: %v", err)
			}
			a.RespondErrorData(a.Await("session/prompt"), -32603, "Internal error", c.data)
			ev := acptest.Next(t, s, event.TypeError)
			if ev.ErrorKind != string(agenterr.KindQuota) || ev.LimitScope != agenterr.ScopeFreeTier ||
				ev.LimitProvider != "opencode-zen" || ev.LimitCode != "FreeUsageLimitError" {
				t.Fatalf("event = kind %q scope %q provider %q code %q, want quota free_tier opencode-zen FreeUsageLimitError",
					ev.ErrorKind, ev.LimitScope, ev.LimitProvider, ev.LimitCode)
			}
			if !ev.RetryAt.Equal(reset) {
				t.Fatalf("retry_at = %v, want gobble's resetAt %v", ev.RetryAt, reset)
			}
			if !strings.HasPrefix(ev.Error, quotaMessage) {
				t.Fatalf("error = %q, want the provider's words first", ev.Error)
			}
		})
	}
}
