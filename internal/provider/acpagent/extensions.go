package acpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
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
	MethodGrokExitPlanMode    = "_x.ai/exit_plan_mode"
	MethodGrokAskUserQuestion = "_x.ai/ask_user_question"
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

// ExtensionNotificationHandler is invoked for extension notifications registered by a Spec.
type ExtensionNotificationHandler func(ctx context.Context, h Host, params json.RawMessage)

// MetaSource names the message a `_meta` key arrived in.
type MetaSource string

// The messages whose `_meta` keys a Spec can register handlers for.
const (
	MetaFromSessionInfo  MetaSource = "session_info_update"
	MetaFromPromptResult MetaSource = "prompt_result"
)

// MetaHandler is invoked with the value of a `_meta` key a Spec registered, as
// the agent sent it: a key the agent clears with null arrives as `null`. It
// runs where the message is handled: a session_info_update's on the SDK's
// notification loop, under HandleExtensionMethod's rule, and a prompt
// result's on the turn goroutine, before turn_complete.
type MetaHandler func(ctx context.Context, h Host, from MetaSource, value json.RawMessage)

// handleMeta passes each key of meta the Spec registered a handler for to that
// handler, in key order, and reports whether any key had none.
func (s *session) handleMeta(ctx context.Context, from MetaSource, meta map[string]any) (unhandled bool) {
	for _, key := range slices.Sorted(maps.Keys(meta)) {
		h, ok := s.metaHandlers[key]
		if !ok {
			unhandled = true
			continue
		}
		// A value decoded from JSON always encodes.
		value, err := json.Marshal(meta[key])
		if err != nil {
			unhandled = true
			continue
		}
		h(ctx, s, from, value)
	}
	return unhandled
}

var _ acp.ExtensionMethodHandler = (*session)(nil)
var _ provider.QuestionSession = (*session)(nil)

// HandleExtensionMethod dispatches the extension requests and notifications a
// Spec registered to their handlers, and reports method-not-found for the
// rest, which is what the SDK would have done on its own.
//
// The notification handlers run on the SDK's notification loop, as
// SessionUpdate does, and keep its rule under OverflowBlock: none waits for a
// request or a $/cancel_request the agent sends later, nor for code that waits
// for one, nor takes a lock held across such a wait (MADR 0224 D5).
func (s *session) HandleExtensionMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if h, ok := s.extRequestHandlers[method]; ok {
		return h(ctx, s, params)
	}
	if h, ok := s.extNotificationHandlers[method]; ok {
		h(ctx, s, params)
		return nil, nil
	}
	return nil, acp.NewMethodNotFound(method)
}

// hasVendorNotifications reports whether the Spec registered a notification
// method whose name lacks the leading `_`, such as opencode's
// `opencode/session/child_update`.
func (s *session) hasVendorNotifications() bool {
	for method := range s.extNotificationHandlers {
		if !isExtensionMethod(method) {
			return true
		}
	}
	return false
}

// vendorNotifications is receiving middleware that hands a notification whose
// method the Spec registered, and whose name lacks the leading `_`, to that
// handler. acp-go-sdk2 routes only `_` names to HandleExtensionMethod and
// drops the rest as unknown, so without it such a notification never reaches
// Magic (MADR 0214 D14, corrected again; acp-go-sdk2 extensions.go:181-197 at
// v1.31.1). Requests, `_` names and unregistered methods pass through.
//
// It runs on the SDK's notification worker, so a handler keeps
// HandleExtensionMethod's rule under OverflowBlock (MADR 0224 D5), and must
// not keep params after it returns.
func (s *session) vendorNotifications(next acp.MethodHandler) acp.MethodHandler {
	return func(ctx context.Context, method string, params json.RawMessage) (any, *acp.RequestError) {
		if acp.IsNotification(ctx) && !isExtensionMethod(method) {
			if h, ok := s.extNotificationHandlers[method]; ok {
				h(ctx, s, params)
				return nil, nil
			}
		}
		return next(ctx, method, params)
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

// HandleGrokExitPlanMode turns grok's plan approval into an ordinary
// permission_request, so the phone's existing approval UI renders it with no
// client change: three options, one answer, the same RespondPermission path.
//
// AlwaysApprove deliberately does NOT auto-approve here. It is a tool-permission
// setting, and grok itself keeps plan mode armed underneath always-approve
// ("file edits are blocked until you approve exiting plan mode"), so
// auto-approving would silently defeat the mode the user asked for. This matches
// the existing rule that always-approve does not auto-answer questions either.
func HandleGrokExitPlanMode(ctx context.Context, h Host, raw json.RawMessage) (any, error) {
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

	optionID, cancelled := h.AwaitPermission(ctx, event.Event{
		ToolID:   p.ToolCallID,
		ToolName: "Plan ready for review",
		Text:     detail,
		Options: []event.PermissionOption{
			{OptionID: planOptionApprove, Name: "Approve plan", Kind: "allow_once"},
			{OptionID: planOptionChanges, Name: "Request changes", Kind: "reject_once"},
			{OptionID: planOptionAbandon, Name: "Abandon plan", Kind: "reject_always"},
		},
	})

	switch {
	case cancelled || optionID == "":
		// Nobody decided (turn cancelled, session closed, or the request timed
		// out). Keep planning rather than approving or discarding work on the
		// user's behalf: revise is the only outcome that changes nothing.
		return exitPlanModeResult{
			Outcome:  planOutcomeChanges,
			Feedback: "The plan approval was dismissed without an answer. Plan mode is still on.",
		}, nil
	case optionID == planOptionApprove:
		return exitPlanModeResult{Outcome: planOutcomeApproved}, nil
	case optionID == planOptionAbandon:
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

// HandleGrokAskUserQuestion maps grok's question form onto the daemon's existing
// question flow (question_request → question.respond → question_resolved), the
// one built for OpenCode's questions. Answers arrive as a label list per
// question, in question order.
func HandleGrokAskUserQuestion(ctx context.Context, h Host, raw json.RawMessage) (any, error) {
	var p askUserQuestionParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
	}
	if len(p.Questions) == 0 {
		return askUserQuestionResult{Outcome: askOutcomeCancelled}, nil
	}

	items := make([]event.QuestionItem, 0, len(p.Questions))
	headers := make([]string, 0, len(p.Questions))
	for i, q := range p.Questions {
		opts := make([]event.PermissionOption, 0, len(q.Options))
		for _, o := range q.Options {
			if o.Label == "" {
				continue
			}
			// The label IS the answer on grok's wire, so it doubles as the id.
			opts = append(opts, event.PermissionOption{
				OptionID:    o.Label,
				Name:        o.Label,
				Description: o.Description,
			})
		}
		items = append(items, event.QuestionItem{
			ID:       strconv.Itoa(i),
			Text:     q.Question,
			Multiple: q.MultiSelect != nil && *q.MultiSelect,
			Options:  opts,
		})
		headers = append(headers, truncateRunes(q.Question, 40))
	}

	got, cancelled := h.AwaitAnswers(ctx, event.Event{
		ToolID:    p.ToolCallID,
		Text:      strings.Join(headers, " · "),
		Questions: items,
	})
	if cancelled {
		return askUserQuestionResult{Outcome: askOutcomeCancelled}, nil
	}

	answers := make(map[string]string, len(p.Questions))
	for i, q := range p.Questions {
		if i >= len(got) || len(got[i]) == 0 {
			continue
		}
		answers[q.Question] = strings.Join(got[i], ", ")
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
	return s.awaitQuestion(ctx, qID, req, &questionWaiter{})
}

// awaitQuestion is awaitAnswers with the caller's waiter, so an elicitation
// form can bring its own answer check and decline (MADR 0191 D3).
func (s *session) awaitQuestion(ctx context.Context, qID string, req event.Event, w *questionWaiter) questionResult {
	w.ch = make(chan questionResult, 1)
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
		if res.declined {
			// The user answered "no": the form is resolved, not abandoned.
			s.emit(s.questionResolvedEvent(qID, event.PermissionStatusResolved))
			return questionResult{declined: true}
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
		s.emit(s.questionExpiredEvent(qID))
		return questionResult{cancelled: true}
	case res := <-w.ch:
		return applyResult(res)
	}
}

// RespondQuestion implements [provider.QuestionSession]: the client's answer to
// a form raised by _x.ai/ask_user_question.
func (s *session) RespondQuestion(_ context.Context, questionID string, answers provider.QuestionAnswers, cancelled bool) error {
	// Same single-winner latch as RespondPermission: whoever flips resolved
	// first owns the outcome, so an answer landing as the timeout fires cannot
	// report success while the waiter already cancelled.
	s.mu.Lock()
	w, ok := s.questions[questionID]
	if !ok || w.resolved {
		s.mu.Unlock()
		return fmt.Errorf("unknown or expired question %q", questionID)
	}
	// An elicitation form checks the answer before anything is claimed: a
	// refusal leaves the question pending, so the user can correct it
	// (MADR 0191 D3).
	var content map[string]acp.ElicitationContentValue
	if !cancelled && w.form != nil {
		var err error
		if content, err = w.form(answers); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	w.resolved = true
	delete(s.questions, questionID)
	s.mu.Unlock()
	// The caller clears its answers once this returns, while the waiter reads
	// them later on the agent's goroutine, so the waiter gets its own copy
	// (MADR 0191, PLAN L6).
	ordered := provider.OrderedQuestionAnswers(answers)
	for i := range ordered {
		ordered[i] = slices.Clone(ordered[i])
	}
	w.ch <- questionResult{answers: ordered, cancelled: cancelled, content: content}
	// A question answered mid-turn can be the last thing blocking a queued
	// prompt, exactly as for permissions.
	s.tryDrainQueue()
	return nil
}

// DeclineQuestion implements [provider.QuestionDeclineSession]: the user's
// explicit "no". A form whose protocol has no decline, such as grok's
// ask_user_question, gets a cancel instead (MADR 0191 D4).
func (s *session) DeclineQuestion(_ context.Context, questionID string) error {
	s.mu.Lock()
	w, ok := s.questions[questionID]
	if !ok || w.resolved {
		s.mu.Unlock()
		return fmt.Errorf("unknown or expired question %q", questionID)
	}
	w.resolved = true
	delete(s.questions, questionID)
	s.mu.Unlock()
	if w.allowDecline {
		w.ch <- questionResult{declined: true}
	} else {
		w.ch <- questionResult{cancelled: true}
	}
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

// questionExpiredEvent mirrors permissionExpired: the timeout arm's
// resolution and no one else's carries TimedOut (MADR 0101 B).
func (s *session) questionExpiredEvent(qID string) event.Event {
	ev := s.questionResolvedEvent(qID, event.PermissionStatusCancelled)
	ev.TimedOut = true
	return ev
}
