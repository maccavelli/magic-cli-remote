package codex

import (
	"bytes"
	"encoding/json"
	"log/slog"
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
