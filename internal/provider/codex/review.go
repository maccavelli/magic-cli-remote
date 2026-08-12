package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func reviewTargetJSON(t provider.ReviewTarget) (map[string]any, error) {
	switch t.Kind {
	case provider.ReviewUncommitted:
		return map[string]any{"type": provider.ReviewUncommitted}, nil
	case provider.ReviewBaseBranch:
		if strings.TrimSpace(t.Branch) == "" {
			return nil, provider.ErrReviewInvalid
		}
		return map[string]any{"type": provider.ReviewBaseBranch, "branch": t.Branch}, nil
	case provider.ReviewCommit:
		if strings.TrimSpace(t.SHA) == "" {
			return nil, provider.ErrReviewInvalid
		}
		return map[string]any{"type": provider.ReviewCommit, "sha": t.SHA, "title": nil}, nil
	case provider.ReviewCustom:
		if strings.TrimSpace(t.Instructions) == "" {
			return nil, provider.ErrReviewInvalid
		}
		return map[string]any{"type": provider.ReviewCustom, "instructions": t.Instructions}, nil
	default:
		return nil, provider.ErrReviewInvalid
	}
}

func (s *session) acquireTurn() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("session closed")
	}
	if s.turnBusy {
		return provider.ErrTurnBusy
	}
	s.turnBusy = true
	return nil
}

func (s *session) StartReview(ctx context.Context, target provider.ReviewTarget) error {
	payload, err := reviewTargetJSON(target)
	if err != nil {
		return err
	}
	if err := s.acquireTurn(); err != nil {
		return err
	}
	s.mu.Lock()
	s.reviewing = true
	s.reviewSawText = false
	s.reviewFallbackUsed = false
	s.reviewThreadID = ""
	gen := s.engineGeneration
	threadID := s.agentID
	s.mu.Unlock()

	fr := s.p.framer()
	if fr == nil {
		s.finishReview()
		s.clearTurnBusy()
		return fmt.Errorf("engine not running")
	}

	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.localID,
		Timestamp: time.Now().UTC(),
		Status:    "running",
	})

	baseCtx := context.WithoutCancel(ctx)
	turnCtx, cancel := context.WithCancel(baseCtx)
	s.mu.Lock()
	s.turnCancel = cancel
	s.limitNotified = false
	s.limitRaw = ""
	s.mu.Unlock()
	go s.runReview(turnCtx, cancel, fr, threadID, gen, payload)
	return nil
}

func (s *session) runReview(ctx context.Context, cancel context.CancelFunc, fr *conn, threadID string, gen int, target map[string]any) {
	defer func() {
		cancel()
		s.mu.Lock()
		s.turnCancel = nil
		s.limitNotified = false
		s.limitRaw = ""
		s.mu.Unlock()
	}()
	s.lastActivity.Store(time.Now().UnixNano())

	params := map[string]any{
		"threadId": threadID,
		"target":   target,
		"delivery": "inline",
	}
	raw, err := fr.sendRequest(ctx, "review/start", params)
	if err != nil {
		s.finishReview()
		s.mu.Lock()
		wasBusy := s.turnBusy
		s.turnBusy = false
		s.turnID = ""
		s.steerable = false
		s.mu.Unlock()
		if !wasBusy {
			return
		}
		if errors.Is(err, errConnLost) || errors.Is(err, context.Canceled) {
			s.emitTurnComplete("cancelled", "")
		} else {
			s.emitTurnComplete("error", err.Error())
		}
		return
	}
	var resp struct {
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
		ReviewThreadID string `json:"reviewThreadId"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		s.finishReview()
		s.clearTurnBusy()
		s.emitTurnComplete("error", "review/start: decode")
		return
	}
	s.mu.Lock()
	if s.closed || s.engineGeneration != gen {
		s.mu.Unlock()
		return
	}
	if resp.Turn.ID != "" {
		s.turnID = resp.Turn.ID
		s.steerable = false
	}
	alias := strings.TrimSpace(resp.ReviewThreadID)
	if alias != "" && alias != s.agentID {
		s.reviewThreadID = alias
	}
	s.mu.Unlock()
	if alias != "" && alias != threadID && s.p != nil {
		s.p.aliasThread(alias, s)
	}
}

func (s *session) finishReview() {
	s.mu.Lock()
	alias := s.reviewThreadID
	s.reviewing = false
	s.reviewThreadID = ""
	s.reviewSawText = false
	s.reviewFallbackUsed = false
	owner := s.p
	self := s
	s.mu.Unlock()
	if alias != "" && owner != nil {
		owner.unaliasThread(alias, self)
	}
}

func (p *Provider) aliasThread(id string, s *session) {
	if p == nil || id == "" || s == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessions == nil {
		p.sessions = make(map[string]*session)
	}
	p.sessions[id] = s
}

func (p *Provider) unaliasThread(id string, s *session) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessions[id] == s && (s == nil || id != s.agentID) {
		delete(p.sessions, id)
	}
}

func (s *session) noteReviewAssistant() {
	s.mu.Lock()
	if s.reviewing {
		s.reviewSawText = true
	}
	s.mu.Unlock()
}

func (s *session) handleReviewItem(itemType string, item json.RawMessage, started bool) bool {
	switch itemType {
	case "enteredReviewMode":
		if started {
			s.emit(event.Event{
				Type:           event.TypeNotice,
				SessionID:      s.localID,
				Timestamp:      time.Now().UTC(),
				Text:           "Entered review mode",
				AgentSessionID: s.agentID,
			})
		}
		return true
	case "exitedReviewMode":
		if started {
			s.emit(event.Event{
				Type:           event.TypeNotice,
				SessionID:      s.localID,
				Timestamp:      time.Now().UTC(),
				Text:           "Exited review mode",
				AgentSessionID: s.agentID,
			})
		}
		s.maybeEmitReviewFallback(item)
		return true
	}
	return false
}

func (s *session) maybeEmitReviewFallback(item json.RawMessage) {
	text := reviewFallbackText(item)
	if text == "" {
		return
	}
	s.mu.Lock()
	if s.reviewSawText || s.reviewFallbackUsed {
		s.mu.Unlock()
		return
	}
	s.reviewFallbackUsed = true
	s.mu.Unlock()
	s.emit(event.Event{
		Type:           event.TypeAssistantChunk,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Text:           text,
		AgentSessionID: s.agentID,
	})
}

func reviewFallbackText(item json.RawMessage) string {
	var p struct {
		Review json.RawMessage `json:"review"`
	}
	if json.Unmarshal(item, &p) != nil || len(p.Review) == 0 || string(p.Review) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(p.Review, &s) == nil {
		return strings.TrimSpace(s)
	}
	var obj struct {
		Text    string `json:"text"`
		Content string `json:"content"`
		Body    string `json:"body"`
	}
	if json.Unmarshal(p.Review, &obj) != nil {
		return ""
	}
	for _, v := range []string{obj.Text, obj.Content, obj.Body} {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

func (s *session) snapshotSettings() reviewSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return reviewSettings{
		Model:     s.opts.Model,
		Thinking:  s.thinkingLevel,
		Collab:    s.collabMode,
		Approval:  s.approvalPolicy,
		Sandbox:   s.sandboxMode,
		Tier:      s.serviceTier,
		Persona:   s.personality,
		Goal:      s.goal,
		GoalOn:    s.goalPresent,
		Reviewing: s.reviewing,
	}
}

type reviewSettings struct {
	Model, Thinking, Collab, Approval, Sandbox, Tier, Persona string
	Goal                                                      provider.Goal
	GoalOn, Reviewing                                         bool
}

var _ provider.ReviewSession = (*session)(nil)
