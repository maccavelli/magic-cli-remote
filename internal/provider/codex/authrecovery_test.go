package codex

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func newNotificationTestSession() *session {
	return &session{
		localID: "local-ar",
		agentID: "thread-ar",
		events:  make(chan event.Event, 64),
		log:     slog.Default(),
	}
}

// TestAuthRecoveryNotificationsAreRouted is MADR 0137 F9.
//
// Codex reports credential recovery as it happens. That is worth surfacing
// precisely because MADRs 0133, 0134 and 0136 had to INFER credential state
// from files, lock contention and a `codex doctor` probe — an inference that
// wedged a host into recovery_required for ten days. The engine saying "I am
// recovering auth for this provider" is the direct observation those records
// worked around not having.
func TestAuthRecoveryNotificationsAreRouted(t *testing.T) {
	for _, method := range []string{
		"modelProvider/authRecoveryStarted",
		"modelProvider/authRecoveryCompleted",
	} {
		t.Run(method, func(t *testing.T) {
			s := newNotificationTestSession()
			params := json.RawMessage(`{"threadId":"t1","turnId":"u1",` +
				`"provider":"openai","message":"Refreshing ChatGPT credentials"}`)
			s.handleNotification(method, params)

			ev := firstEvent(t, s, event.TypeNotice)
			if ev.Text != "Refreshing ChatGPT credentials" {
				t.Fatalf("text = %q, want the engine's own message", ev.Text)
			}
			// A notice, not an error: recovery starting is not a failure, and
			// a turn that recovers and continues must not be decorated with a
			// red bubble.
			if ev.Type != event.TypeNotice {
				t.Fatalf("type = %s, want notice", ev.Type)
			}
		})
	}
}

// TestAuthRecoveryFallsBackToItsOwnWording proves an empty message still
// produces something a reader can act on, rather than an empty bubble.
func TestAuthRecoveryFallsBackToItsOwnWording(t *testing.T) {
	s := newNotificationTestSession()
	s.handleNotification("modelProvider/authRecoveryCompleted",
		json.RawMessage(`{"provider":"openai"}`))
	ev := firstEvent(t, s, event.TypeNotice)
	if ev.Text == "" {
		t.Fatal("an empty engine message produced an empty notice")
	}
	if !contains(ev.Text, "completed") || !contains(ev.Text, "openai") {
		t.Fatalf("fallback text = %q, want it to name the phase and provider", ev.Text)
	}
}

// TestUnknownNotificationsAreStillIgnored is the guard on the other side: the
// six notifications MADR 0137 step 7.7 declines must stay declined, and an
// unknown method must never be forwarded into a transcript.
func TestUnknownNotificationsAreStillIgnored(t *testing.T) {
	for _, method := range []string{
		"rawResponse/started", "thread/realtime/update", "some/new/method",
	} {
		s := newNotificationTestSession()
		s.handleNotification(method, json.RawMessage(`{}`))
		if len(s.events) != 0 {
			t.Errorf("%s produced %d events; unrouted notifications must be ignored",
				method, len(s.events))
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func firstEvent(t *testing.T, s *session, typ event.Type) event.Event {
	t.Helper()
	for {
		select {
		case ev := <-s.events:
			if ev.Type == typ {
				return ev
			}
		default:
			t.Fatalf("no %s event emitted", typ)
			return event.Event{}
		}
	}
}
