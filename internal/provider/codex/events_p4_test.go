package codex

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func p4Session() *session {
	zero := time.Duration(0)
	return &session{localID: "local-1", agentID: "thread-1", events: make(chan event.Event, 64), log: slog.Default(), cfg: Config{StreamCoalesce: &zero}}
}

func TestItemLifecycleHasOneStableUpsertKey(t *testing.T) {
	s := p4Session()
	s.handleNotification("item/started", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","item":{"type":"commandExecution","id":"item-1","command":"go test ./...","cwd":"/workspace","status":"inProgress","commandActions":[]}}`))
	s.handleNotification("item/commandExecution/outputDelta", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"ok"}`))
	s.handleNotification("item/commandExecution/terminalInteraction", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","processId":"proc-1","stdin":"y\n"}`))
	s.handleNotification("item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","completedAtMs":1,"item":{"type":"commandExecution","id":"item-1","command":"go test ./...","cwd":"/workspace","status":"completed","commandActions":[],"exitCode":0,"aggregatedOutput":"ok"}}`))
	// Authoritative repeats are idempotent at the reducer boundary: they may
	// update the existing card, but can never invent a second key.
	s.handleNotification("item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","completedAtMs":2,"item":{"type":"commandExecution","id":"item-1","command":"go test ./...","cwd":"/workspace","status":"completed","commandActions":[],"exitCode":0}}`))
	events := drainEvents(s)
	starts := 0
	sawExit := false
	for _, ev := range events {
		if ev.ToolID == "item-1" && ev.Type == event.TypeToolCall {
			starts++
		}
		if ev.ToolID == "item-1" && strings.Contains(ev.Text, "Exit code 0") {
			sawExit = true
		}
		if ev.ToolID == "item-1" || (ev.Codex != nil && ev.Codex.Key == "item:item-1") {
			continue
		}
		if ev.Type == event.TypeToolUpdate || ev.Type == event.TypeCodexTerminalInteraction {
			t.Errorf("unstable item key in %+v", ev)
		}
	}
	if starts != 1 {
		t.Fatalf("tool starts = %d, want 1", starts)
	}
	if !sawExit {
		t.Fatal("authoritative command completion omitted exit code")
	}
}

func TestEventFidelityNotificationPairs(t *testing.T) {
	tests := []struct {
		method string
		body   string
		typ    event.Type
		text   string
	}{
		{"item/reasoning/summaryTextDelta", `{"threadId":"thread-1","turnId":"t","itemId":"r","summaryIndex":0,"delta":"Readable reason"}`, event.TypeThoughtChunk, "Readable reason"},
		{"item/mcpToolCall/progress", `{"threadId":"thread-1","turnId":"t","itemId":"m","message":"Downloaded 2/4"}`, event.TypeCodexProgress, "Downloaded 2/4"},
		{"item/fileChange/outputDelta", `{"threadId":"thread-1","turnId":"t","itemId":"f","delta":"applying"}`, event.TypeToolUpdate, "applying"},
		{"item/fileChange/patchUpdated", `{"threadId":"thread-1","turnId":"t","itemId":"f","changes":[{"path":"safe.txt","kind":{"type":"update"}}]}`, event.TypeCodexProgress, "safe.txt"},
		{"model/rerouted", `{"threadId":"thread-1","turnId":"t","fromModel":"a","toModel":"b","reason":"highRiskCyberActivity"}`, event.TypeCodexModelReroute, "b"},
		{"model/verification", `{"threadId":"thread-1","turnId":"t","verifications":["trustedAccessForCyber"]}`, event.TypeCodexModelVerification, "trustedAccessForCyber"},
		{"model/safetyBuffering/updated", `{"threadId":"thread-1","turnId":"t","model":"b","useCases":["cyber"],"reasons":["safety"],"showBufferingUi":true,"fasterModel":"c"}`, event.TypeCodexProgress, "safety"},
	}
	for _, tc := range tests {
		t.Run(strings.ReplaceAll(tc.method, "/", "_"), func(t *testing.T) {
			s := p4Session()
			s.handleNotification(tc.method, json.RawMessage(tc.body))
			evs := drainEvents(s)
			if len(evs) != 1 || evs[0].Type != tc.typ {
				t.Fatalf("events = %+v, want one %s", evs, tc.typ)
			}
			b, _ := json.Marshal(evs[0])
			if !strings.Contains(string(b), tc.text) {
				t.Errorf("event %s does not contain %q", b, tc.text)
			}
		})
	}
}

func TestWarningGuardianAndNoticesAreTypedAndBounded(t *testing.T) {
	p := New(Config{})
	s := p4Session()
	p.sessions[s.localID] = s
	for _, tc := range []struct{ method, body, kind string }{
		{"warning", `{"threadId":"thread-1","message":"ordinary warning"}`, "warning"},
		{"guardianWarning", `{"threadId":"thread-1","message":"review denied"}`, "guardian"},
		{"configWarning", `{"message":"config warning"}`, "config"},
		{"deprecationNotice", `{"summary":"old setting","details":"use new setting"}`, "deprecation"},
	} {
		p.handleProviderNotification(tc.method, json.RawMessage(tc.body))
		evs := drainEvents(s)
		if len(evs) != 1 || evs[0].Type != event.TypeCodexWarning || evs[0].Codex == nil || evs[0].Codex.Kind != tc.kind {
			t.Fatalf("%s events = %+v", tc.method, evs)
		}
	}
}

func TestUnknownItemFailsClosedUnlessItAffectsCompletion(t *testing.T) {
	s := p4Session()
	s.handleNotification("item/started", json.RawMessage(`{"threadId":"thread-1","turnId":"t","item":{"type":"futureTelemetry","id":"u1","secret":"do-not-leak"}}`))
	if got := drainEvents(s); len(got) != 0 {
		t.Fatalf("unknown start leaked: %+v", got)
	}
	s.handleNotification("item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"t","completedAtMs":1,"item":{"type":"futureAction","id":"u2","status":"failed","secret":"do-not-leak"}}`))
	evs := drainEvents(s)
	if len(evs) != 1 || evs[0].Type != event.TypeCodexUnsupportedItem || evs[0].Codex == nil {
		t.Fatalf("completion events = %+v", evs)
	}
	b, _ := json.Marshal(evs[0])
	if strings.Contains(string(b), "do-not-leak") {
		t.Fatalf("unknown item raw payload leaked: %s", b)
	}
}
