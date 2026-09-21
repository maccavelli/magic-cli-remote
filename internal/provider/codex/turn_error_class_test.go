package codex

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func turnErrorSession(t *testing.T) *session {
	t.Helper()
	s := &session{
		events:  make(chan event.Event, 8),
		done:    make(chan struct{}),
		agentID: "thread-x",
		log:     silentLogger(),
	}
	t.Cleanup(func() { close(s.done) })
	return s
}

func errorEvent(t *testing.T, s *session) event.Event {
	t.Helper()
	for _, ev := range drain(s) {
		if ev.Type == event.TypeError {
			return ev
		}
	}
	t.Fatalf("no error event emitted")
	return event.Event{}
}

// TestQuotaAndThrottlingAreDifferentClasses is acceptance criterion A15 of
// PLAN 0163.
//
// The two carry OPPOSITE advice: usageLimitExceeded means the credit is spent and
// waiting achieves nothing, while rateLimitExceeded means back off and retry
// shortly. Before this, neither was decoded at all and both were classified by
// parsing the message prose — which cannot separate them, because both say
// "limit" in English. Driven through handleNotification so the JSON field names
// are part of the assertion.
func TestQuotaAndThrottlingAreDifferentClasses(t *testing.T) {
	for _, tc := range []struct {
		info string
		want string
	}{
		{"usageLimitExceeded", "quota"},
		{"rateLimitExceeded", "rate_limit"},
		{"serverOverloaded", "server"},
		{"unauthorized", "auth"},
	} {
		t.Run(tc.info, func(t *testing.T) {
			s := turnErrorSession(t)
			s.handleNotification("turn/completed", []byte(`{
				"threadId": "thread-x",
				"turn": {
					"id": "turn-1",
					"status": "failed",
					"error": {
						"message": "the model stopped: limit reached",
						"codexErrorInfo": "`+tc.info+`"
					}
				}
			}`))
			if got := errorEvent(t, s).ErrorKind; got != tc.want {
				t.Errorf("codexErrorInfo %q produced ErrorKind %q, want %q", tc.info, got, tc.want)
			}
		})
	}
}

// TestUnmappedErrorInfoFallsBackToTheProseClassifier keeps the engine's
// classification from becoming a worse answer than the old one. Values whose
// right advice is unclear — a context window that is full needs a new session,
// not a wait — are deliberately unmapped, and must not silently become "quota".
func TestUnmappedErrorInfoFallsBackToTheProseClassifier(t *testing.T) {
	for _, info := range []string{"contextWindowExceeded", "sessionBudgetExceeded", "badRequest", "other", ""} {
		if kind, authoritative := codexErrorKind(info); authoritative {
			t.Errorf("codexErrorKind(%q) = %q and claims to be authoritative; "+
				"only unambiguous classes may override the prose classifier", info, kind)
		}
	}
	// The object variants of codexErrorInfo decode to no string at all, which must
	// be treated as "no classification" rather than as a class named "".
	if kind, authoritative := codexErrorKind(""); authoritative || kind != agenterr.KindNone {
		t.Errorf("an absent codexErrorInfo produced (%q, %v), want (none, false)", kind, authoritative)
	}
}

// TestMisalignmentOffersContinuationOnlyWithAnExplanation is the second half of
// A15. The schema is explicit that "a substantive localized explanation is
// required before offering continuation", so an unexplained block must not
// produce a one-tap continue: that would ask a person to approve resuming work
// the model refused, without saying why it refused.
func TestMisalignmentOffersContinuationOnlyWithAnExplanation(t *testing.T) {
	t.Run("explained", func(t *testing.T) {
		s := turnErrorSession(t)
		s.handleNotification("turn/completed", []byte(`{
			"threadId": "thread-x",
			"turn": {
				"id": "turn-1",
				"status": "failed",
				"error": {
					"message": "turn blocked",
					"codexErrorInfo": "misalignmentPolicyViolation",
					"misalignment": {
						"errorType": "someCategory",
						"detailedExplanation": "I stopped because the task looked like credential exfiltration.",
						"steer": {"message": "Continue, but do not read credential files."}
					}
				}
			}
		}`))
		ev := errorEvent(t, s)
		if ev.SteerMessage != "Continue, but do not read credential files." {
			t.Errorf("SteerMessage = %q, want the steer instruction", ev.SteerMessage)
		}
		// The explanation is better copy than the generic message, so it becomes
		// the error text the operator actually reads.
		if ev.Error != "I stopped because the task looked like credential exfiltration." {
			t.Errorf("Error = %q, want the detailed explanation", ev.Error)
		}
	})

	t.Run("unexplained", func(t *testing.T) {
		s := turnErrorSession(t)
		s.handleNotification("turn/completed", []byte(`{
			"threadId": "thread-x",
			"turn": {
				"id": "turn-1",
				"status": "failed",
				"error": {
					"message": "turn blocked",
					"codexErrorInfo": "misalignmentPolicyViolation",
					"misalignment": {
						"errorType": "someCategory",
						"steer": {"message": "Continue anyway."}
					}
				}
			}
		}`))
		if got := errorEvent(t, s).SteerMessage; got != "" {
			t.Errorf("SteerMessage = %q for an unexplained block; continuation must not be offered", got)
		}
	})
}

// TestAdditionalDetailsReachTheOperator: the field exists precisely because
// message alone is often too terse to act on, and we used to drop it.
func TestAdditionalDetailsReachTheOperator(t *testing.T) {
	s := turnErrorSession(t)
	s.handleNotification("turn/completed", []byte(`{
		"threadId": "thread-x",
		"turn": {
			"id": "turn-1",
			"status": "failed",
			"error": {"message": "request failed", "additionalDetails": "upstream returned 503 after 3 attempts"}
		}
	}`))
	if got := errorEvent(t, s).Error; got == "" || got == "request failed" {
		t.Errorf("Error = %q, want the additional details appended", got)
	}
}
