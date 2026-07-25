package acpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// grok drives two client-side interactions over ACP *extension* methods rather
// than standard ACP: presenting a finished plan for approval when its model
// calls exit_plan_mode, and asking the user a multiple-choice question mid-turn.
// Both are requests, so a client that answers method-not-found fails the tool
// call — which is exactly what happened before MADR 0022 phase 2: plan mode was
// enterable and usable, but the approval step at the end of it always failed.
//
// The names are vendor-scoped (`_x.ai/…`), so implementing them here is inert
// for any other ACP agent: nothing else sends them, and anything we do not
// recognise still gets method-not-found.
const (
	methodExitPlanMode    = "_x.ai/exit_plan_mode"
	methodAskUserQuestion = "_x.ai/ask_user_question"
)

// Plan-approval option ids. This vocabulary is ours, not grok's: the phone
// answers with one of these through the ordinary permission path and we
// translate it to the wire outcome below.
const (
	planOptionApprove = "plan_approve"
	planOptionChanges = "plan_changes"
	planOptionAbandon = "plan_abandon"
)

// Wire outcomes for _x.ai/exit_plan_mode, verified live against grok 0.2.112:
//
//	approved  → plan mode exits, the agent is told to start implementing
//	abandoned → plan mode is turned off and the agent is told not to re-enter it
//	anything else → "the user wants to revise the plan", plan mode stays active,
//	                and an optional feedback string is quoted back to the model
//
// The revise case is grok's *fallback*: an unrecognised outcome (or a reply it
// cannot parse at all) lands there silently, with no error surfaced to the
// agent. We therefore spell the value out rather than relying on the fallback,
// so a future grok that does start validating outcomes keeps working.
const (
	planOutcomeApproved  = "approved"
	planOutcomeAbandoned = "abandoned"
	planOutcomeChanges   = "changes"
)

// Outcomes for _x.ai/ask_user_question. grok's deserializer enumerated the set
// when handed a bogus one: accepted, chat_about_this, skip_interview, cancelled.
// `accepted` additionally requires the answers map or the tool call fails.
const (
	askOutcomeAccepted  = "accepted"
	askOutcomeCancelled = "cancelled"
)

// planContentLimit caps how much plan markdown travels in the approval event.
// Plans are written for humans and occasionally run long; the phone shows this
// text on the approval card, and the plan file itself stays on the agent host.
const planContentLimit = 8000

var _ acp.ExtensionMethodHandler = (*session)(nil)
var _ provider.QuestionSession = (*session)(nil)

// HandleExtensionMethod answers the ACP extension requests we understand and
// reports method-not-found for the rest, which is what the SDK would have done
// on its own. Extension *notifications* we do not handle are dropped by the SDK
// without logging, so they need no case here.
func (s *session) HandleExtensionMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case methodExitPlanMode:
		return s.handleExitPlanMode(ctx, params)
	case methodAskUserQuestion:
		return s.handleAskUserQuestion(ctx, params)
	default:
		return nil, acp.NewMethodNotFound(method)
	}
}

// exitPlanModeParams is grok's ExitPlanModeExtRequest. planContent is null when
// the model left plan mode without writing (or with an unreadable) plan file —
// grok still expects an answer, and the user can still approve.
type exitPlanModeParams struct {
	SessionID   string  `json:"sessionId"`
	ToolCallID  string  `json:"toolCallId"`
	PlanContent *string `json:"planContent"`
}

type exitPlanModeResult struct {
	Outcome  string `json:"outcome"`
	Feedback string `json:"feedback,omitempty"`
}

// handleExitPlanMode turns grok's plan approval into an ordinary
// permission_request, so the phone's existing approval UI renders it with no
// client change: three options, one answer, the same RespondPermission path.
//
// AlwaysApprove deliberately does NOT auto-approve here. It is a tool-permission
// setting, and grok itself keeps plan mode armed underneath always-approve
// ("file edits are blocked until you approve exiting plan mode"), so
// auto-approving would silently defeat the mode the user asked for. This matches
// the existing rule that always-approve does not auto-answer questions either.
func (s *session) handleExitPlanMode(ctx context.Context, raw json.RawMessage) (any, error) {
	var p exitPlanModeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
	}

	plan := ""
	if p.PlanContent != nil {
		plan = strings.TrimSpace(*p.PlanContent)
	}
	detail := plan
	if detail == "" {
		detail = "The agent left plan mode without writing a plan. " +
			"Approve to start implementing anyway, or ask for changes."
	} else if len(detail) > planContentLimit {
		detail = detail[:planContentLimit] + "\n\n… plan truncated for display."
	}

	permID := uuid.NewString()
	res := s.awaitDecision(ctx, permID, event.Event{
		Type:         event.TypePermission,
		SessionID:    s.localID,
		Timestamp:    time.Now().UTC(),
		PermissionID: permID,
		ToolID:       p.ToolCallID,
		ToolName:     "Plan ready for review",
		Text:         detail,
		Status:       "pending",
		Options: []event.PermissionOption{
			{OptionID: planOptionApprove, Name: "Approve plan", Kind: "allow_once"},
			{OptionID: planOptionChanges, Name: "Request changes", Kind: "reject_once"},
			{OptionID: planOptionAbandon, Name: "Abandon plan", Kind: "reject_always"},
		},
	})

	switch {
	case res.cancelled || res.optionID == "":
		// Nobody decided (turn cancelled, session closed, or the request timed
		// out). Keep planning rather than approving or discarding work on the
		// user's behalf: revise is the only outcome that changes nothing.
		return exitPlanModeResult{
			Outcome:  planOutcomeChanges,
			Feedback: "The plan approval was dismissed without an answer. Plan mode is still on.",
		}, nil
	case res.optionID == planOptionApprove:
		return exitPlanModeResult{Outcome: planOutcomeApproved}, nil
	case res.optionID == planOptionAbandon:
		return exitPlanModeResult{Outcome: planOutcomeAbandoned}, nil
	default:
		// Request changes. We have no free-text channel on the permission path,
		// so say so plainly — grok's own next step is to ask what to change with
		// ask_user_question, which we answer below.
		return exitPlanModeResult{
			Outcome:  planOutcomeChanges,
			Feedback: "The user wants changes to the plan but did not say what. Ask what to change.",
		}, nil
	}
}

// askUserQuestionParams is grok's AskUserQuestionExtRequest.
type askUserQuestionParams struct {
	SessionID  string `json:"sessionId"`
	ToolCallID string `json:"toolCallId"`
	// Mode is the session mode the question was asked in ("plan" during plan
	// mode). Informational only.
	Mode      string `json:"mode"`
	Questions []struct {
		Question string `json:"question"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
		MultiSelect *bool `json:"multiSelect"`
	} `json:"questions"`
}

// askUserQuestionResult is grok's AskUserQuestionExtResponse. Answers is keyed
// by the *question text* (grok mirrors the Claude Code tool shape here), and a
// single string per question — a list is also accepted, but the string form is
// what grok echoes back to the model verbatim.
type askUserQuestionResult struct {
	Outcome string            `json:"outcome"`
	Answers map[string]string `json:"answers,omitempty"`
}

// handleAskUserQuestion maps grok's question form onto the daemon's existing
// question flow (question_request → question.respond → question_resolved), the
// one built for OpenCode's questions. Answers arrive as a label list per
// question, in question order.
func (s *session) handleAskUserQuestion(ctx context.Context, raw json.RawMessage) (any, error) {
	var p askUserQuestionParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
	}
	if len(p.Questions) == 0 {
		return askUserQuestionResult{Outcome: askOutcomeCancelled}, nil
	}

	items := make([]event.QuestionItem, 0, len(p.Questions))
	headers := make([]string, 0, len(p.Questions))
	for _, q := range p.Questions {
		opts := make([]event.PermissionOption, 0, len(q.Options))
		for _, o := range q.Options {
			if o.Label == "" {
				continue
			}
			// The label IS the answer on grok's wire, so it doubles as the id.
			// grok also sends a per-option description; the question card has no
			// field for it yet, same as the OpenCode question path.
			opts = append(opts, event.PermissionOption{
				OptionID: o.Label,
				Name:     o.Label,
			})
		}
		items = append(items, event.QuestionItem{
			Text:     q.Question,
			Multiple: q.MultiSelect != nil && *q.MultiSelect,
			Options:  opts,
		})
		headers = append(headers, truncateRunes(q.Question, 40))
	}

	qID := uuid.NewString()
	res := s.awaitAnswers(ctx, qID, event.Event{
		Type:       event.TypeQuestion,
		SessionID:  s.localID,
		Timestamp:  time.Now().UTC(),
		QuestionID: qID,
		ToolID:     p.ToolCallID,
		Status:     "pending",
		Text:       strings.Join(headers, " · "),
		Questions:  items,
	})
	if res.cancelled {
		return askUserQuestionResult{Outcome: askOutcomeCancelled}, nil
	}

	answers := make(map[string]string, len(p.Questions))
	for i, q := range p.Questions {
		if i >= len(res.answers) || len(res.answers[i]) == 0 {
			continue
		}
		answers[q.Question] = strings.Join(res.answers[i], ", ")
	}
	if len(answers) == 0 {
		// An "accepted" outcome with no answers is rejected by grok, and an
		// empty form is not an answer anyway.
		return askUserQuestionResult{Outcome: askOutcomeCancelled}, nil
	}
	return askUserQuestionResult{Outcome: askOutcomeAccepted, Answers: answers}, nil
}

// awaitAnswers is awaitDecision for question forms: emit the request, block for
// the client's answer, and emit the terminal question_resolved on every path.
func (s *session) awaitAnswers(ctx context.Context, qID string, req event.Event) questionResult {
	w := &questionWaiter{ch: make(chan questionResult, 1)}
	s.mu.Lock()
	if s.closed {
		s.emitLocked(s.questionResolvedEvent(qID, event.PermissionStatusCancelled))
		s.mu.Unlock()
		return questionResult{cancelled: true}
	}
	if s.questions == nil {
		s.questions = make(map[string]*questionWaiter)
	}
	s.questions[qID] = w
	s.mu.Unlock()

	claim := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		cur, ok := s.questions[qID]
		if !ok || cur != w || w.resolved {
			return false
		}
		w.resolved = true
		delete(s.questions, qID)
		return true
	}

	s.emit(req)

	var timeout <-chan time.Time
	if s.cfg.PermissionTimeout > 0 {
		t := time.NewTimer(s.cfg.PermissionTimeout)
		defer t.Stop()
		timeout = t.C
	}

	applyResult := func(res questionResult) questionResult {
		if res.cancelled {
			s.emit(s.questionResolvedEvent(qID, event.PermissionStatusCancelled))
			return questionResult{cancelled: true}
		}
		s.emit(s.questionResolvedEvent(qID, event.PermissionStatusResolved))
		return res
	}

	select {
	case <-ctx.Done():
		if !claim() {
			return applyResult(<-w.ch)
		}
		s.emit(s.questionResolvedEvent(qID, event.PermissionStatusCancelled))
		return questionResult{cancelled: true}
	case <-timeout:
		if !claim() {
			return applyResult(<-w.ch)
		}
		s.emit(event.Event{
			Type:      event.TypeNotice,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Text: fmt.Sprintf(
				"The agent's question timed out after %s and was dismissed. "+
					"Prompt again to retry.", s.cfg.PermissionTimeout),
		})
		s.emit(s.questionResolvedEvent(qID, event.PermissionStatusCancelled))
		return questionResult{cancelled: true}
	case res := <-w.ch:
		return applyResult(res)
	}
}

// RespondQuestion implements [provider.QuestionSession]: the client's answer to
// a form raised by _x.ai/ask_user_question.
func (s *session) RespondQuestion(_ context.Context, questionID string, answers [][]string, cancelled bool) error {
	// Same single-winner latch as RespondPermission: whoever flips resolved
	// first owns the outcome, so an answer landing as the timeout fires cannot
	// report success while the waiter already cancelled.
	s.mu.Lock()
	w, ok := s.questions[questionID]
	if !ok || w.resolved {
		s.mu.Unlock()
		return fmt.Errorf("unknown or expired question %q", questionID)
	}
	w.resolved = true
	delete(s.questions, questionID)
	s.mu.Unlock()
	w.ch <- questionResult{answers: answers, cancelled: cancelled}
	// A question answered mid-turn can be the last thing blocking a queued
	// prompt, exactly as for permissions.
	s.tryDrainQueue()
	return nil
}

// questionResolvedEvent builds the terminal event for a question form, so a
// client never keeps a form on screen that will never be answered.
func (s *session) questionResolvedEvent(qID, status string) event.Event {
	return event.Event{
		Type:       event.TypeQuestionResolved,
		SessionID:  s.localID,
		Timestamp:  time.Now().UTC(),
		QuestionID: qID,
		Status:     status,
	}
}
