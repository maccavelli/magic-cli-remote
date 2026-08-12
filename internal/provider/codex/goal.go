package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

const maxGoalRunes = 4000

type engineGoal struct {
	Objective   string `json:"objective"`
	Status      string `json:"status"`
	TokenBudget int    `json:"tokenBudget"`
	TokenUsage  int    `json:"tokenUsage"`
}

func parseGoalMutation(arg string) (provider.GoalMutation, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return provider.GoalMutation{Kind: provider.GoalView}, nil
	}
	fields := strings.Fields(arg)
	verb := strings.ToLower(fields[0])
	switch verb {
	case "pause":
		if len(fields) != 1 {
			return provider.GoalMutation{}, provider.ErrGoalInvalid
		}
		return provider.GoalMutation{Kind: provider.GoalPause}, nil
	case "resume":
		if len(fields) != 1 {
			return provider.GoalMutation{}, provider.ErrGoalInvalid
		}
		return provider.GoalMutation{Kind: provider.GoalResume}, nil
	case "clear":
		if len(fields) != 1 {
			return provider.GoalMutation{}, provider.ErrGoalInvalid
		}
		return provider.GoalMutation{Kind: provider.GoalClear}, nil
	case "edit":
		obj := strings.TrimSpace(strings.TrimPrefix(arg, fields[0]))
		if err := validateGoalObjective(obj); err != nil {
			return provider.GoalMutation{}, err
		}
		return provider.GoalMutation{Kind: provider.GoalEdit, Objective: obj}, nil
	default:
		if err := validateGoalObjective(arg); err != nil {
			return provider.GoalMutation{}, err
		}
		return provider.GoalMutation{Kind: provider.GoalReplace, Objective: arg}, nil
	}
}

func validateGoalObjective(obj string) error {
	if strings.TrimSpace(obj) == "" {
		return provider.ErrGoalInvalid
	}
	if utf8.RuneCountInString(obj) > maxGoalRunes {
		return provider.ErrGoalInvalid
	}
	return nil
}

func (s *session) CurrentGoal() (provider.Goal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.goalPresent {
		return provider.Goal{}, false
	}
	return s.goal, true
}

func (s *session) ApplyGoal(ctx context.Context, mut provider.GoalMutation) (provider.Goal, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return provider.Goal{}, fmt.Errorf("session closed")
	}
	if mut.Kind != provider.GoalView && s.turnBusy {
		s.mu.Unlock()
		return provider.Goal{}, provider.ErrTurnBusy
	}
	inPlan := s.collabSupported && s.collabMode == collaborationModePlan
	cur, present := s.goal, s.goalPresent
	threadID := s.agentID
	gen := s.engineGeneration
	s.mu.Unlock()

	if err := checkGoalPlanMatrix(mut, cur, present, inPlan); err != nil {
		return provider.Goal{}, err
	}
	if mut.Kind == provider.GoalView {
		if !present {
			return provider.Goal{}, nil
		}
		return cur, nil
	}

	fr := s.p.framer()
	if fr == nil {
		return provider.Goal{}, fmt.Errorf("engine not running")
	}

	var (
		raw json.RawMessage
		err error
	)
	switch mut.Kind {
	case provider.GoalClear:
		raw, err = fr.sendRequest(ctx, "thread/goal/clear", map[string]any{"threadId": threadID})
	case provider.GoalReplace:
		raw, err = fr.sendRequest(ctx, "thread/goal/set", map[string]any{
			"threadId": threadID, "objective": mut.Objective, "status": provider.GoalStatusActive,
		})
	case provider.GoalEdit:
		raw, err = fr.sendRequest(ctx, "thread/goal/set", map[string]any{
			"threadId": threadID, "objective": mut.Objective,
		})
	case provider.GoalPause:
		raw, err = fr.sendRequest(ctx, "thread/goal/set", map[string]any{
			"threadId": threadID, "status": provider.GoalStatusPaused,
		})
	case provider.GoalResume:
		raw, err = fr.sendRequest(ctx, "thread/goal/set", map[string]any{
			"threadId": threadID, "status": provider.GoalStatusActive,
		})
	default:
		return provider.Goal{}, provider.ErrGoalInvalid
	}
	if err != nil {
		return provider.Goal{}, fmt.Errorf("goal request failed")
	}
	g, ok := decodeGoalResult(raw, mut.Kind == provider.GoalClear)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.engineGeneration != gen {
		return provider.Goal{}, nil
	}
	if mut.Kind == provider.GoalClear || !ok {
		s.goal = provider.Goal{}
		s.goalPresent = false
		s.mu.Unlock()
		s.emitGoal()
		s.mu.Lock()
		return provider.Goal{}, nil
	}
	s.goal = g
	s.goalPresent = true
	s.mu.Unlock()
	s.emitGoal()
	s.mu.Lock()
	return g, nil
}

func (s *session) hydrateGoalAsync() {
	if s.p == nil || s.p.framer() == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := s.HydrateGoal(ctx); err != nil {
		if s.log != nil {
			s.log.Debug("goal hydrate failed", slog.String("err", err.Error()))
		}
		return
	}
	s.emitGoal()
}

func (s *session) HydrateGoal(ctx context.Context) error {
	s.mu.Lock()
	threadID := s.agentID
	gen := s.engineGeneration
	s.mu.Unlock()
	if s.p == nil {
		return fmt.Errorf("engine not running")
	}
	fr := s.p.framer()
	if fr == nil {
		return fmt.Errorf("engine not running")
	}
	raw, err := fr.sendRequest(ctx, "thread/goal/get", map[string]any{"threadId": threadID})
	if err != nil {
		return fmt.Errorf("goal get failed")
	}
	g, ok := decodeGoalGet(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.engineGeneration != gen {
		return nil
	}
	s.goal = g
	s.goalPresent = ok
	return nil
}

func checkGoalPlanMatrix(mut provider.GoalMutation, cur provider.Goal, present, inPlan bool) error {
	active := provider.GoalIsActive(cur, present)
	switch mut.Kind {
	case provider.GoalView, provider.GoalClear, provider.GoalPause:
		return nil
	case provider.GoalReplace, provider.GoalResume:
		if inPlan {
			return provider.ErrGoalPlanConflict
		}
	case provider.GoalEdit:
		if inPlan && active {
			return provider.ErrGoalPlanConflict
		}
	}
	return nil
}

func decodeGoalResult(raw json.RawMessage, cleared bool) (provider.Goal, bool) {
	if cleared {
		var resp struct {
			Cleared bool `json:"cleared"`
		}
		if json.Unmarshal(raw, &resp) == nil && resp.Cleared {
			return provider.Goal{}, false
		}
	}
	var resp struct {
		Goal *engineGoal `json:"goal"`
	}
	if json.Unmarshal(raw, &resp) != nil || resp.Goal == nil {
		return provider.Goal{}, false
	}
	return providerGoal(*resp.Goal), true
}

func decodeGoalGet(raw json.RawMessage) (provider.Goal, bool) {
	if string(raw) == "null" || strings.TrimSpace(string(raw)) == "null" {
		return provider.Goal{}, false
	}
	var resp struct {
		Goal *engineGoal `json:"goal"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		var g engineGoal
		if json.Unmarshal(raw, &g) == nil && (g.Objective != "" || g.Status != "") {
			return providerGoal(g), true
		}
		return provider.Goal{}, false
	}
	if resp.Goal == nil {
		return provider.Goal{}, false
	}
	return providerGoal(*resp.Goal), true
}

func providerGoal(g engineGoal) provider.Goal {
	return provider.Goal{
		Objective:   g.Objective,
		Status:      g.Status,
		TokenBudget: g.TokenBudget,
		TokenUsage:  g.TokenUsage,
	}
}

func (s *session) rememberGoal(g provider.Goal, present bool) {
	s.mu.Lock()
	s.goal = g
	s.goalPresent = present
	s.mu.Unlock()
}

func (s *session) emitGoal() {
	g, ok := s.CurrentGoal()
	ev := event.Event{
		Type:      event.TypeGoal,
		SessionID: s.localID,
	}
	if ok {
		ev.Goal = &event.Goal{
			Objective:   g.Objective,
			Status:      g.Status,
			TokenBudget: g.TokenBudget,
			TokenUsage:  g.TokenUsage,
		}
	}
	s.emit(ev)
}

func (s *session) applyGoalNotification(method string, params json.RawMessage) {
	switch method {
	case "thread/goal/cleared":
		s.rememberGoal(provider.Goal{}, false)
		s.emitGoal()
	case "thread/goal/updated":
		var p struct {
			Goal *engineGoal `json:"goal"`
		}
		if json.Unmarshal(params, &p) != nil || p.Goal == nil {
			return
		}
		s.rememberGoal(providerGoal(*p.Goal), true)
		s.emitGoal()
	}
}

var _ provider.GoalSession = (*session)(nil)
