package acpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// ExtensionNotificationHandler is invoked for extension notifications registered by a Spec.
type ExtensionNotificationHandler func(ctx context.Context, h Host, params json.RawMessage)

// MetaSource names the message a `_meta` key arrived in.
type MetaSource string

// The messages whose `_meta` keys a Spec can register handlers for.
const (
	MetaFromSessionInfo  MetaSource = "session_info_update"
	MetaFromPromptResult MetaSource = "prompt_result"
	// MetaFromCompaction is a compaction_update's `_meta`, such as an agent's
	// own facts about that compaction (MADR 0179 A2).
	MetaFromCompaction MetaSource = "compaction_update"
)

// MetaHandler is invoked with the value of a `_meta` key a Spec registered, as
// the agent sent it: a key the agent clears with null arrives as `null`. It
// runs where the message is handled: a session_info_update's and a
// compaction_update's on the SDK's notification loop, under
// HandleExtensionMethod's rule, a compaction_update's after that
// compaction's event; and a prompt result's on the turn goroutine, before
// turn_complete.
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
// method whose name lacks the leading `_`, such as a vendor's child-session
// update.
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
// a form a Spec raised with Host.AwaitAnswers, or an elicitation form.
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
// explicit "no". A form whose protocol has no decline, such as one a Spec
// raised with Host.AwaitAnswers, gets a cancel instead (MADR 0191 D4).
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
