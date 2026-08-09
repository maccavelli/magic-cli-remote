package acpagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// newExtSession is a session with no agent process behind it: the extension
// handlers only touch the event stream and the waiter maps.
func newExtSession(timeout time.Duration) *session {
	return &session{
		providerID: "grok",
		localID:    "l1",
		agentID:    "a1",
		events:     make(chan event.Event, 32),
		done:       make(chan struct{}),
		log:        slog.Default(),
		pending:    make(map[string]*permWaiter),
		questions:  make(map[string]*questionWaiter),
		attached:   true,
		procExited: true, // skip the process kill in Close
		cfg:        Config{PermissionTimeout: timeout},
	}
}

// waitEvent returns the next event of type want, skipping others.
func waitEvent(t *testing.T, s *session, want event.Type) event.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}

func planParams(t *testing.T, plan *string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(exitPlanModeParams{
		SessionID:   "a1",
		ToolCallID:  "call-1",
		PlanContent: plan,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// callExtension runs HandleExtensionMethod on its own goroutine (it blocks on
// the client's answer) and hands back the result.
func callExtension(ctx context.Context, s *session, method string, params json.RawMessage) <-chan any {
	out := make(chan any, 1)
	go func() {
		res, err := s.HandleExtensionMethod(ctx, method, params)
		if err != nil {
			out <- err
			return
		}
		out <- res
	}()
	return out
}

// Approving grok's plan-approval request answers `approved`, which is the value
// its shell recognizes as "exit plan mode and start implementing" (probed
// against grok 0.2.112; anything it does not recognize silently means "revise").
func TestExitPlanModeApproveAnswersApproved(t *testing.T) {
	s := newExtSession(0)
	plan := "# Plan\n\nDo the thing."
	out := callExtension(context.Background(), s, methodExitPlanMode, planParams(t, &plan))

	req := waitEvent(t, s, event.TypePermission)
	if req.Text != plan {
		t.Fatalf("approval text = %q, want the plan markdown", req.Text)
	}
	if req.ToolID != "call-1" {
		t.Fatalf("tool id = %q, want the agent's toolCallId", req.ToolID)
	}
	var ids []string
	for _, o := range req.Options {
		ids = append(ids, o.OptionID)
	}
	want := []string{planOptionApprove, planOptionChanges, planOptionAbandon}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("options = %v, want %v", ids, want)
	}

	if err := s.RespondPermission(context.Background(), req.PermissionID, planOptionApprove, false, "dev-1"); err != nil {
		t.Fatal(err)
	}
	res, ok := (<-out).(exitPlanModeResult)
	if !ok {
		t.Fatalf("result type %T", res)
	}
	if res.Outcome != planOutcomeApproved {
		t.Fatalf("outcome = %q, want %q", res.Outcome, planOutcomeApproved)
	}
	if res.Feedback != "" {
		t.Fatalf("approval must not carry feedback, got %q", res.Feedback)
	}
	if ev := waitEvent(t, s, event.TypePermissionResolved); ev.Status != event.PermissionStatusResolved {
		t.Fatalf("resolved status = %q", ev.Status)
	}
}

// The other two options map onto grok's remaining outcomes, and "request
// changes" must carry feedback: the permission path has no free-text field, so
// the feedback is what tells the model to ask what to change instead of
// re-presenting the same plan.
func TestExitPlanModeOptionsMapToOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		optionID     string
		wantOutcome  string
		wantFeedback bool
	}{
		{"abandon", planOptionAbandon, planOutcomeAbandoned, false},
		{"changes", planOptionChanges, planOutcomeChanges, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newExtSession(0)
			plan := "# Plan"
			out := callExtension(context.Background(), s, methodExitPlanMode, planParams(t, &plan))
			req := waitEvent(t, s, event.TypePermission)
			if err := s.RespondPermission(context.Background(), req.PermissionID, tc.optionID, false, "dev-1"); err != nil {
				t.Fatal(err)
			}
			res := (<-out).(exitPlanModeResult)
			if res.Outcome != tc.wantOutcome {
				t.Fatalf("outcome = %q, want %q", res.Outcome, tc.wantOutcome)
			}
			if got := res.Feedback != ""; got != tc.wantFeedback {
				t.Fatalf("feedback present = %v (%q), want %v", got, res.Feedback, tc.wantFeedback)
			}
		})
	}
}

// A dismissed approval (turn cancelled, session closed, or timed out) must not
// approve the plan and must not throw it away either: revise is the only outcome
// that changes nothing, so the user can decide again.
func TestExitPlanModeDismissedKeepsPlanning(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		s := newExtSession(0)
		s.testCancel = func(context.Context) error { return nil }
		plan := "# Plan"
		out := callExtension(context.Background(), s, methodExitPlanMode, planParams(t, &plan))
		waitEvent(t, s, event.TypePermission)
		if err := s.Cancel(context.Background()); err != nil {
			t.Fatal(err)
		}
		res := (<-out).(exitPlanModeResult)
		if res.Outcome != planOutcomeChanges {
			t.Fatalf("outcome = %q, want %q so plan mode survives", res.Outcome, planOutcomeChanges)
		}
		if ev := waitEvent(t, s, event.TypePermissionResolved); ev.Status != event.PermissionStatusCancelled {
			t.Fatalf("resolved status = %q, want cancelled", ev.Status)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		s := newExtSession(30 * time.Millisecond)
		plan := "# Plan"
		out := callExtension(context.Background(), s, methodExitPlanMode, planParams(t, &plan))
		res := (<-out).(exitPlanModeResult)
		if res.Outcome != planOutcomeChanges {
			t.Fatalf("outcome = %q, want %q", res.Outcome, planOutcomeChanges)
		}
	})
}

// grok sends planContent: null when the model exits plan mode without writing a
// readable plan file. It still expects an answer, so the approval must open with
// an explanation rather than an empty card.
func TestExitPlanModeEmptyPlanStillAsks(t *testing.T) {
	s := newExtSession(0)
	out := callExtension(context.Background(), s, methodExitPlanMode, planParams(t, nil))
	req := waitEvent(t, s, event.TypePermission)
	if !strings.Contains(req.Text, "without writing a plan") {
		t.Fatalf("empty-plan text = %q", req.Text)
	}
	if err := s.RespondPermission(context.Background(), req.PermissionID, planOptionApprove, false, "dev-1"); err != nil {
		t.Fatal(err)
	}
	if res := (<-out).(exitPlanModeResult); res.Outcome != planOutcomeApproved {
		t.Fatalf("outcome = %q", res.Outcome)
	}
}

// Plans are occasionally very long; the approval card carries a bounded excerpt
// (the plan file itself stays on the agent host).
func TestExitPlanModeTruncatesLongPlan(t *testing.T) {
	s := newExtSession(0)
	plan := strings.Repeat("x", planContentLimit+500)
	out := callExtension(context.Background(), s, methodExitPlanMode, planParams(t, &plan))
	req := waitEvent(t, s, event.TypePermission)
	if len(req.Text) >= len(plan) {
		t.Fatalf("plan text not truncated: %d bytes", len(req.Text))
	}
	if !strings.Contains(req.Text, "truncated") {
		t.Fatal("truncated plan must say so")
	}
	_ = s.RespondPermission(context.Background(), req.PermissionID, planOptionApprove, false, "dev-1")
	<-out
}

// Anything outside the two methods we implement gets the SDK's own answer, so a
// grok that adds an extension we have not built stays diagnosable instead of
// looking like a silent hang.
func TestUnknownExtensionMethodIsMethodNotFound(t *testing.T) {
	s := newExtSession(0)
	_, err := s.HandleExtensionMethod(context.Background(), "_x.ai/not_implemented", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want an error for an unknown extension method")
	}
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("error type %T (%v), want *acp.RequestError", err, err)
	}
	if reqErr.Code != -32601 {
		t.Fatalf("code = %d, want -32601 (method not found)", reqErr.Code)
	}
}

func askParams(t *testing.T, multi bool) json.RawMessage {
	t.Helper()
	body := map[string]any{
		"sessionId":  "a1",
		"toolCallId": "call-9",
		"mode":       "plan",
		"questions": []map[string]any{
			{
				"question": "Which greeting?",
				"options": []map[string]string{
					{"label": "Hello", "description": "formal"},
					{"label": "Hi", "description": "casual"},
				},
				"multiSelect": multi,
			},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// grok keys its answers by the *question text*, not by index or option id, and
// requires the accepted outcome to carry them. A multi-select answer is joined
// into the one string grok echoes to the model.
func TestAskUserQuestionAnswersKeyedByQuestionText(t *testing.T) {
	s := newExtSession(0)
	out := callExtension(context.Background(), s, methodAskUserQuestion, askParams(t, true))

	req := waitEvent(t, s, event.TypeQuestion)
	if req.ToolID != "call-9" || len(req.Questions) != 1 {
		t.Fatalf("question event = %+v", req)
	}
	q := req.Questions[0]
	if q.Text != "Which greeting?" || !q.Multiple || len(q.Options) != 2 {
		t.Fatalf("question item = %+v", q)
	}
	if q.Options[0].OptionID != "Hello" {
		t.Fatalf("option id = %q, want the label (grok answers by label)", q.Options[0].OptionID)
	}

	if err := s.RespondQuestion(context.Background(), req.QuestionID, [][]string{{"Hello", "Hi"}}, false); err != nil {
		t.Fatal(err)
	}
	res := (<-out).(askUserQuestionResult)
	if res.Outcome != askOutcomeAccepted {
		t.Fatalf("outcome = %q, want %q", res.Outcome, askOutcomeAccepted)
	}
	if got := res.Answers["Which greeting?"]; got != "Hello, Hi" {
		t.Fatalf("answers = %v", res.Answers)
	}
	if ev := waitEvent(t, s, event.TypeQuestionResolved); ev.Status != event.PermissionStatusResolved {
		t.Fatalf("resolved status = %q", ev.Status)
	}
}

// An abandoned form — and an answer that selected nothing, which grok rejects as
// an accepted outcome with no answers — both report cancelled.
func TestAskUserQuestionCancelledPaths(t *testing.T) {
	for _, tc := range []struct {
		name      string
		answers   [][]string
		cancelled bool
	}{
		{"rejected", nil, true},
		{"empty answer", [][]string{{}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newExtSession(0)
			out := callExtension(context.Background(), s, methodAskUserQuestion, askParams(t, false))
			req := waitEvent(t, s, event.TypeQuestion)
			if err := s.RespondQuestion(context.Background(), req.QuestionID, tc.answers, tc.cancelled); err != nil {
				t.Fatal(err)
			}
			res := (<-out).(askUserQuestionResult)
			if res.Outcome != askOutcomeCancelled {
				t.Fatalf("outcome = %q, want %q", res.Outcome, askOutcomeCancelled)
			}
			if len(res.Answers) != 0 {
				t.Fatalf("cancelled must send no answers, got %v", res.Answers)
			}
		})
	}
}

// Closing a session with a question on screen must unblock the agent and tell
// the client the form is gone, exactly as for a pending permission.
func TestCloseUnblocksPendingQuestion(t *testing.T) {
	s := newExtSession(0)
	out := callExtension(context.Background(), s, methodAskUserQuestion, askParams(t, false))
	_ = waitEvent(t, s, event.TypeQuestion)

	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	res := (<-out).(askUserQuestionResult)
	if res.Outcome != askOutcomeCancelled {
		t.Fatalf("outcome = %q, want cancelled", res.Outcome)
	}
	if ev := waitEvent(t, s, event.TypeQuestionResolved); ev.Status != event.PermissionStatusCancelled {
		t.Fatalf("resolved status = %q, want cancelled", ev.Status)
	}
}

// Answering a form twice (or after it expired) must fail rather than block on a
// waiter nobody owns.
func TestRespondQuestionUnknownID(t *testing.T) {
	s := newExtSession(0)
	if err := s.RespondQuestion(context.Background(), "nope", nil, false); err == nil {
		t.Fatal("want an error for an unknown question id")
	}
}
