package grok

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// From acpagent/extensions_test.go.

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

// Approving grok's plan-approval request answers `approved`, which is the value
// its shell recognizes as "exit plan mode and start implementing" (probed
// against grok 0.2.112; anything it does not recognize silently means "revise").
func TestExitPlanModeApproveAnswersApproved(t *testing.T) {
	s := newRequestSession(t, 0)
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
			s := newRequestSession(t, 0)
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
		s := newRequestSession(t, 0)
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
		s := newRequestSession(t, 30*time.Millisecond)
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
	s := newRequestSession(t, 0)
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
	s := newRequestSession(t, 0)
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
	s := newRequestSession(t, 0)
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

	if err := s.RespondQuestion(context.Background(), req.QuestionID, provider.QuestionAnswers{"0": {"Hello", "Hi"}}, false); err != nil {
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
		answers   provider.QuestionAnswers
		cancelled bool
	}{
		{"rejected", nil, true},
		{"empty answer", provider.QuestionAnswers{"0": {}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newRequestSession(t, 0)
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

// A question nobody answers within PermissionTimeout is auto-dismissed, and
// its resolution must carry TimedOut so the phone can distinguish "nobody
// answered" from "the agent withdrew it" (MADR 0101 B).
func TestAskUserQuestionTimeoutMarksTimedOut(t *testing.T) {
	s := newRequestSession(t, 30*time.Millisecond)
	out := callExtension(context.Background(), s, methodAskUserQuestion, askParams(t, false))
	_ = waitEvent(t, s, event.TypeQuestion)

	res := (<-out).(askUserQuestionResult)
	if res.Outcome != askOutcomeCancelled {
		t.Fatalf("outcome = %q, want cancelled after timeout", res.Outcome)
	}
	ev := waitEvent(t, s, event.TypeQuestionResolved)
	if ev.Status != event.PermissionStatusCancelled {
		t.Fatalf("resolved status = %q, want cancelled", ev.Status)
	}
	if !ev.TimedOut {
		t.Fatal("question timeout resolution must carry TimedOut")
	}
}

// Closing a session with a question on screen must unblock the agent and tell
// the client the form is gone, exactly as for a pending permission.
func TestCloseUnblocksPendingQuestion(t *testing.T) {
	s := newRequestSession(t, 0)
	out := callExtension(context.Background(), s, methodAskUserQuestion, askParams(t, false))
	_ = waitEvent(t, s, event.TypeQuestion)

	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	res := (<-out).(askUserQuestionResult)
	if res.Outcome != askOutcomeCancelled {
		t.Fatalf("outcome = %q, want cancelled", res.Outcome)
	}
	ev := waitEvent(t, s, event.TypeQuestionResolved)
	if ev.Status != event.PermissionStatusCancelled {
		t.Fatalf("resolved status = %q, want cancelled", ev.Status)
	}
	if ev.TimedOut {
		t.Fatal("a close-path cancellation must not carry TimedOut (MADR 0101 B)")
	}
}

// TestAGrokQuestionCannotBeDeclined: grok's ask_user_question has no decline to
// return, so its form offers none, and a decline answers grok cancelled (MADR
// 0191 D4). It was a subtest of acpagent's elicitation test before grok's
// handler left acpagent.
func TestAGrokQuestionCannotBeDeclined(t *testing.T) {
	s := newRequestSession(t, 0)
	out := callExtension(context.Background(), s, methodAskUserQuestion, askParams(t, false))
	req := waitEvent(t, s, event.TypeQuestion)
	if req.AllowDecline {
		t.Fatal("grok's ask_user_question offered a decline it cannot return")
	}
	if err := s.DeclineQuestion(context.Background(), req.QuestionID); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-out:
		if res, ok := got.(askUserQuestionResult); !ok || res.Outcome != askOutcomeCancelled {
			t.Fatalf("result = %+v, want a cancelled outcome", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a decline left grok's question unanswered")
	}
}
