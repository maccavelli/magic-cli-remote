package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// PLAN 0191 L1 step 2: a decline reaches a provider that can decline, and is a
// cancel for one that cannot (MADR 0191 D4).

// answerSession records what the manager sent it.
type answerSession struct {
	id     string
	events chan event.Event

	mu        sync.Mutex
	responded []string // "<question>:cancelled=<bool>"
	refuse    error
}

func (s *answerSession) ID() string                                       { return s.id }
func (s *answerSession) ProviderID() provider.ID                          { return "answers" }
func (s *answerSession) AgentSessionID() string                           { return s.id }
func (s *answerSession) Events() <-chan event.Event                       { return s.events }
func (s *answerSession) Prompt(context.Context, []provider.Content) error { return nil }
func (s *answerSession) Cancel(context.Context) error                     { return nil }
func (s *answerSession) Close(context.Context) error                      { return nil }

func (s *answerSession) RespondQuestion(_ context.Context, id string, _ provider.QuestionAnswers, cancelled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refuse != nil {
		return s.refuse
	}
	s.responded = append(s.responded, fmt.Sprintf("%s:cancelled=%t", id, cancelled))
	return nil
}

// declineSession can also decline.
type declineSession struct {
	*answerSession
	declined []string
}

func (s *declineSession) DeclineQuestion(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.declined = append(s.declined, id)
	return nil
}

type answerProvider struct {
	canDecline bool
	plain      *answerSession
	decliner   *declineSession
}

func (p *answerProvider) ID() provider.ID { return "answers" }
func (p *answerProvider) Ready() bool     { return true }
func (p *answerProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	p.plain = &answerSession{id: opts.LocalSessionID, events: make(chan event.Event)}
	if p.canDecline {
		p.decliner = &declineSession{answerSession: p.plain}
		return p.decliner, nil
	}
	return p.plain, nil
}

func newAnswerManager(t *testing.T, canDecline bool) (*Manager, *answerProvider, string) {
	t.Helper()
	p := &answerProvider{canDecline: canDecline}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, nil, nil, nil)
	meta, err := mgr.Create(context.Background(), "answers", provider.StartOptions{}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	return mgr, p, meta.ID
}

func TestDeclineQuestionReachesADecliningSession(t *testing.T) {
	mgr, p, id := newAnswerManager(t, true)
	if err := mgr.DeclineQuestion(context.Background(), id, "q1", "owner"); err != nil {
		t.Fatal(err)
	}
	if len(p.decliner.declined) != 1 || p.decliner.declined[0] != "q1" || len(p.plain.responded) != 0 {
		t.Fatalf("declined %v, responded %v; want q1 declined and nothing else", p.decliner.declined, p.plain.responded)
	}
}

func TestDeclineQuestionIsACancelWithoutDeclineSupport(t *testing.T) {
	mgr, p, id := newAnswerManager(t, false)
	if err := mgr.DeclineQuestion(context.Background(), id, "q1", "owner"); err != nil {
		t.Fatal(err)
	}
	if len(p.plain.responded) != 1 || p.plain.responded[0] != "q1:cancelled=true" {
		t.Fatalf("responded %v, want q1 cancelled", p.plain.responded)
	}
}

func TestDeclineQuestionIsOwnerAuthorized(t *testing.T) {
	mgr, p, id := newAnswerManager(t, true)
	if err := mgr.DeclineQuestion(context.Background(), id, "q1", "intruder"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if len(p.decliner.declined) != 0 {
		t.Fatalf("an intruder's decline reached the session: %v", p.decliner.declined)
	}
}

// TestRefusedAnswerComesBackAsInvalid: a provider's validation refusal reaches
// the caller unchanged, so the phone can keep the form open (MADR 0191 D3).
func TestRefusedAnswerComesBackAsInvalid(t *testing.T) {
	mgr, p, id := newAnswerManager(t, false)
	p.plain.refuse = fmt.Errorf("%w: age: must be a whole number", provider.ErrQuestionInvalid)
	err := mgr.RespondQuestion(context.Background(), id, "q1", provider.QuestionAnswers{"age": {"x"}}, false, "owner")
	if !errors.Is(err, provider.ErrQuestionInvalid) {
		t.Fatalf("err = %v, want ErrQuestionInvalid", err)
	}
}
