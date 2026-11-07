package codex

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func TestProviderGlobalRoutingWorksWithoutThreadID(t *testing.T) {
	s, _ := permSession(t)
	p := &Provider{
		log:      silentLogger(),
		sessions: map[string]*session{"thread-1": s},
	}
	p.routeNotification("account/rateLimits/updated", json.RawMessage(`{
		"rateLimits":{"primary":{"usedPercent":100,"windowDurationMins":60,"resetsAt":1}}
	}`))
	events := drainEvents(s)
	if len(events) != 1 || events[0].Type != event.TypeError || events[0].ErrorKind != "rate_limit" {
		t.Fatalf("provider-global rate limit was unreachable: %+v", events)
	}
}

func TestUnknownNotificationRedactsParams(t *testing.T) {
	const sentinel = "ROUTING-SECRET-MUST-NOT-LOG"
	var logs bytes.Buffer
	p := &Provider{
		log:      slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		sessions: map[string]*session{},
	}
	p.routeNotification("future/private/notification", json.RawMessage(`{"token":"`+sentinel+`"}`))
	if strings.Contains(logs.String(), sentinel) || !strings.Contains(logs.String(), "future/private/notification") {
		t.Fatalf("unknown notification log is not bounded/redacted: %s", logs.String())
	}
}

// TestThreadSectionChangedIsADeclaredDrop is MADR 0236 D2 (Q1 A). Both shapes
// codex 0.167.0 sent on the wire — a move into a section, and a move out with
// sectionId null — reach the declared-drop case, emit nothing to a session, and
// log the method without either id.
func TestThreadSectionChangedIsADeclaredDrop(t *testing.T) {
	raw, err := os.ReadFile("testdata/wire/0.167.0/thread-section-changed.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var frames []struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		var frame struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("fixture frame: %v", err)
		}
		frames = append(frames, frame)
	}
	if len(frames) != 2 || !strings.Contains(string(frames[1].Params), `"sectionId":null`) {
		t.Fatalf("fixture must hold the two wire shapes, into a section and out of one: %+v", frames)
	}

	s, _ := permSession(t)
	var logs bytes.Buffer
	p := &Provider{
		log:      slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		sessions: map[string]*session{"thread-1": s},
	}
	for _, frame := range frames {
		if frame.Method != "thread/section/changed" {
			t.Fatalf("fixture method = %q", frame.Method)
		}
		p.routeNotification(frame.Method, frame.Params)
	}
	if got := strings.Count(logs.String(), "codex: declared notification not consumed"); got != 2 {
		t.Errorf("declared-drop log lines = %d, want 2:\n%s", got, logs.String())
	}
	for _, id := range []string{"01a1b756-92f6-7831-b46e-3ee86d61c6d5", "01a1b756-92d8-7a71-a5fa-e681a4606554"} {
		if strings.Contains(logs.String(), id) {
			t.Errorf("log carries an id from the params: %s", logs.String())
		}
	}
	if events := drainEvents(s); len(events) != 0 {
		t.Errorf("a declared drop emitted session events: %+v", events)
	}
}

func TestRoutingDestinations(t *testing.T) {
	tests := map[string]notificationRoute{
		"account/rateLimits/updated":              notificationRouteProvider,
		"warning":                                 notificationRouteProvider,
		"project/changed":                         notificationRouteProvider,
		"thread/project/updated":                  notificationRouteSession,
		"serverRequest/resolved":                  notificationRouteSession,
		"autoApprovalReview/strictReviewRequired": notificationRouteSession,
		"item/agentMessage/delta":                 notificationRouteSession,
		"future/private/notification":             notificationRouteUnknown,
	}
	for method, want := range tests {
		if got := notificationRouteFor(method); got != want {
			t.Errorf("%s route = %v, want %v", method, got, want)
		}
	}
}
